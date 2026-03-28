package controlapi

import (
	"context"
	"log"
	"time"

	controlpb "github.com/tjjh89017/vrouter-daemon/gen/go/controlpb"
	"github.com/tjjh89017/vrouter-daemon/internal/cluster"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const defaultTimeout = 30 * time.Second

// Service implements the ControlService gRPC handler.
type Service struct {
	controlpb.UnimplementedControlServiceServer
	clusterReg *cluster.Registry
	broker     *cluster.Broker
}

// New creates a new ControlService handler.
func New(clusterReg *cluster.Registry, broker *cluster.Broker) *Service {
	return &Service{
		clusterReg: clusterReg,
		broker:     broker,
	}
}

// IsConnected checks if an agent is currently connected anywhere in the cluster.
func (s *Service) IsConnected(ctx context.Context, req *controlpb.IsConnectedRequest) (*controlpb.IsConnectedResponse, error) {
	if req.AgentId == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_id is required")
	}

	connected, err := s.clusterReg.IsConnected(ctx, req.AgentId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "check connection: %v", err)
	}
	return &controlpb.IsConnectedResponse{Connected: connected}, nil
}

// GetStatus returns the cached status for an agent from Redis.
func (s *Service) GetStatus(ctx context.Context, req *controlpb.GetStatusRequest) (*controlpb.GetStatusResponse, error) {
	if req.AgentId == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_id is required")
	}

	info, err := s.clusterReg.GetInfo(ctx, req.AgentId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "get status: %v", err)
	}
	if info == nil {
		return nil, status.Errorf(codes.NotFound, "agent %q not connected", req.AgentId)
	}

	return &controlpb.GetStatusResponse{
		HasStatus:    len(info.StatusJSON) > 0,
		StatusJson:   info.StatusJSON,
		AgentVersion: info.AgentVersion,
	}, nil
}

// ApplyConfig submits a config request to the Redis broker and waits for the
// pod that owns the agent to process it and return the result.
func (s *Service) ApplyConfig(ctx context.Context, req *controlpb.ApplyConfigRequest) (*controlpb.ApplyConfigResponse, error) {
	if req.AgentId == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_id is required")
	}
	if len(req.ConfigPayload) == 0 {
		return nil, status.Error(codes.InvalidArgument, "config_payload is required")
	}

	// Check agent is connected before submitting
	connected, err := s.clusterReg.IsConnected(ctx, req.AgentId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "check connection: %v", err)
	}
	if !connected {
		return nil, status.Errorf(codes.NotFound, "agent %q not connected", req.AgentId)
	}

	timeout := defaultTimeout
	if req.TimeoutSeconds > 0 {
		timeout = time.Duration(req.TimeoutSeconds) * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	log.Printf("apply_config agent=%s payload=%d bytes timeout=%v", req.AgentId, len(req.ConfigPayload), timeout)

	result, err := s.broker.Submit(ctx, req.AgentId, req.ConfigPayload, timeout)
	if err != nil {
		if ctx.Err() != nil {
			log.Printf("apply_config agent=%s timed out", req.AgentId)
			return nil, status.Errorf(codes.DeadlineExceeded, "apply_config timed out for agent %q", req.AgentId)
		}
		log.Printf("apply_config agent=%s error: %v", req.AgentId, err)
		return nil, status.Errorf(codes.Internal, "apply_config failed: %v", err)
	}

	log.Printf("apply_config agent=%s success=%v exitCode=%d stdout=%q stderr=%q",
		req.AgentId, result.Success, result.ExitCode, result.Stdout, result.Stderr)

	return &controlpb.ApplyConfigResponse{
		Success:      result.Success,
		ExitCode:     int32(result.ExitCode),
		Stdout:       result.Stdout,
		Stderr:       result.Stderr,
		ErrorMessage: result.ErrorMessage,
	}, nil
}
