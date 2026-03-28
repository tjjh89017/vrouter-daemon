package controlapi

import (
	"context"
	"testing"

	controlpb "github.com/tjjh89017/vrouter-daemon/gen/go/controlpb"
	"github.com/tjjh89017/vrouter-daemon/internal/cluster"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func setupService(t *testing.T) *Service {
	t.Helper()
	clusterReg, redisClient := cluster.TestRegistry(t, "127.0.0.1")
	broker := cluster.TestBrokerWithPrefix(t, redisClient, clusterReg.TestKeyPrefix())
	return New(clusterReg, broker)
}

func TestIsConnected(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()

	resp, err := svc.IsConnected(ctx, &controlpb.IsConnectedRequest{AgentId: "agent-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Connected {
		t.Fatal("expected not connected")
	}

	_, _ = svc.clusterReg.Register(ctx, "agent-1", "1.0.0")
	resp, err = svc.IsConnected(ctx, &controlpb.IsConnectedRequest{AgentId: "agent-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Connected {
		t.Fatal("expected connected")
	}
}

func TestIsConnectedEmptyAgentID(t *testing.T) {
	svc := setupService(t)
	_, err := svc.IsConnected(context.Background(), &controlpb.IsConnectedRequest{AgentId: ""})
	if err == nil {
		t.Fatal("expected error")
	}
	if st, ok := status.FromError(err); !ok || st.Code() != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument, got %v", err)
	}
}

func TestGetStatus(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()

	_, err := svc.GetStatus(ctx, &controlpb.GetStatusRequest{AgentId: "agent-1"})
	if err == nil {
		t.Fatal("expected error for unconnected agent")
	}
	if st, ok := status.FromError(err); !ok || st.Code() != codes.NotFound {
		t.Fatalf("expected NotFound, got %v", err)
	}

	_, _ = svc.clusterReg.Register(ctx, "agent-1", "1.0.0")
	svc.clusterReg.UpdateStatus(ctx, "agent-1", "1.0.0", []byte(`{"up":true}`))

	resp, err := svc.GetStatus(ctx, &controlpb.GetStatusRequest{AgentId: "agent-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.HasStatus {
		t.Fatal("expected has_status to be true")
	}
	if resp.AgentVersion != "1.0.0" {
		t.Fatalf("expected version 1.0.0, got %s", resp.AgentVersion)
	}
}

func TestApplyConfigEmptyAgentID(t *testing.T) {
	svc := setupService(t)
	_, err := svc.ApplyConfig(context.Background(), &controlpb.ApplyConfigRequest{
		AgentId: "", ConfigPayload: []byte("set foo"),
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if st, ok := status.FromError(err); !ok || st.Code() != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument, got %v", err)
	}
}

func TestApplyConfigEmptyPayload(t *testing.T) {
	svc := setupService(t)
	_, err := svc.ApplyConfig(context.Background(), &controlpb.ApplyConfigRequest{
		AgentId: "agent-1", ConfigPayload: nil,
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestApplyConfigAgentNotConnected(t *testing.T) {
	svc := setupService(t)
	_, err := svc.ApplyConfig(context.Background(), &controlpb.ApplyConfigRequest{
		AgentId: "agent-missing", ConfigPayload: []byte("set foo"), TimeoutSeconds: 1,
	})
	if err == nil {
		t.Fatal("expected error for missing agent")
	}
}
