package controlapi

import (
	"context"
	"time"

	controlpb "github.com/tjjh89017/vrouter-daemon/gen/go/controlpb"
	"github.com/tjjh89017/vrouter-daemon/internal/dispatch"
	"github.com/tjjh89017/vrouter-daemon/internal/registry"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const defaultTimeout = 30 * time.Second

// Service implements the ControlService gRPC handler.
type Service struct {
	controlpb.UnimplementedControlServiceServer
	registry   *registry.Registry
	dispatcher *dispatch.Dispatcher
}

// New creates a new ControlService handler.
func New(reg *registry.Registry, disp *dispatch.Dispatcher) *Service {
	return &Service{
		registry:   reg,
		dispatcher: disp,
	}
}

// IsConnected checks if an agent is currently connected.
func (s *Service) IsConnected(ctx context.Context, req *controlpb.IsConnectedRequest) (*controlpb.IsConnectedResponse, error) {
	if req.AgentId == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_id is required")
	}
	return &controlpb.IsConnectedResponse{
		Connected: s.registry.IsConnected(req.AgentId),
	}, nil
}

// GetStatus returns the cached status for an agent.
func (s *Service) GetStatus(ctx context.Context, req *controlpb.GetStatusRequest) (*controlpb.GetStatusResponse, error) {
	if req.AgentId == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_id is required")
	}

	entry := s.registry.GetEntry(req.AgentId)
	if entry == nil {
		return nil, status.Errorf(codes.NotFound, "agent %q not connected", req.AgentId)
	}

	return &controlpb.GetStatusResponse{
		HasStatus:    len(entry.StatusJSON) > 0,
		StatusJson:   entry.StatusJSON,
		AgentVersion: entry.AgentVersion,
	}, nil
}

// ApplyConfig sends a configuration to an agent and waits for acknowledgment.
func (s *Service) ApplyConfig(ctx context.Context, req *controlpb.ApplyConfigRequest) (*controlpb.ApplyConfigResponse, error) {
	if req.AgentId == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_id is required")
	}
	if len(req.ConfigPayload) == 0 {
		return nil, status.Error(codes.InvalidArgument, "config_payload is required")
	}

	timeout := defaultTimeout
	if req.TimeoutSeconds > 0 {
		timeout = time.Duration(req.TimeoutSeconds) * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result, err := s.dispatcher.ApplyConfig(ctx, req.AgentId, req.ConfigPayload)
	if err != nil {
		if ctx.Err() != nil {
			return nil, status.Errorf(codes.DeadlineExceeded, "apply_config timed out for agent %q", req.AgentId)
		}
		return nil, status.Errorf(codes.Internal, "apply_config failed: %v", err)
	}

	return &controlpb.ApplyConfigResponse{
		Success:      result.Success,
		ExitCode:     int32(result.ExitCode),
		Stdout:       result.Stdout,
		Stderr:       result.Stderr,
		ErrorMessage: result.ErrorMessage,
	}, nil
}
