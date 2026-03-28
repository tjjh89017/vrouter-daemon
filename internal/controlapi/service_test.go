package controlapi

import (
	"context"
	"testing"

	controlpb "github.com/tjjh89017/vrouter-daemon/gen/go/controlpb"
	"github.com/tjjh89017/vrouter-daemon/internal/dispatch"
	"github.com/tjjh89017/vrouter-daemon/internal/registry"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestIsConnected(t *testing.T) {
	reg := registry.New()
	disp := dispatch.New(reg)
	svc := New(reg, disp)

	ctx := context.Background()

	// Not connected
	resp, err := svc.IsConnected(ctx, &controlpb.IsConnectedRequest{AgentId: "agent-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Connected {
		t.Fatal("expected not connected")
	}

	// Register and check
	reg.Register("agent-1", nil)
	resp, err = svc.IsConnected(ctx, &controlpb.IsConnectedRequest{AgentId: "agent-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Connected {
		t.Fatal("expected connected")
	}
}

func TestIsConnectedEmptyAgentID(t *testing.T) {
	reg := registry.New()
	disp := dispatch.New(reg)
	svc := New(reg, disp)

	_, err := svc.IsConnected(context.Background(), &controlpb.IsConnectedRequest{AgentId: ""})
	if err == nil {
		t.Fatal("expected error")
	}
	if st, ok := status.FromError(err); !ok || st.Code() != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument, got %v", err)
	}
}

func TestGetStatus(t *testing.T) {
	reg := registry.New()
	disp := dispatch.New(reg)
	svc := New(reg, disp)

	ctx := context.Background()

	// Not connected
	_, err := svc.GetStatus(ctx, &controlpb.GetStatusRequest{AgentId: "agent-1"})
	if err == nil {
		t.Fatal("expected error for unconnected agent")
	}
	if st, ok := status.FromError(err); !ok || st.Code() != codes.NotFound {
		t.Fatalf("expected NotFound, got %v", err)
	}

	// Register and set status
	reg.Register("agent-1", nil)
	reg.UpdateStatus("agent-1", "1.0.0", []byte(`{"up":true}`))

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
	if string(resp.StatusJson) != `{"up":true}` {
		t.Fatalf("unexpected status: %s", resp.StatusJson)
	}
}

func TestApplyConfigEmptyAgentID(t *testing.T) {
	reg := registry.New()
	disp := dispatch.New(reg)
	svc := New(reg, disp)

	_, err := svc.ApplyConfig(context.Background(), &controlpb.ApplyConfigRequest{
		AgentId:       "",
		ConfigPayload: []byte("set foo"),
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if st, ok := status.FromError(err); !ok || st.Code() != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument, got %v", err)
	}
}

func TestApplyConfigEmptyPayload(t *testing.T) {
	reg := registry.New()
	disp := dispatch.New(reg)
	svc := New(reg, disp)

	_, err := svc.ApplyConfig(context.Background(), &controlpb.ApplyConfigRequest{
		AgentId:       "agent-1",
		ConfigPayload: nil,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if st, ok := status.FromError(err); !ok || st.Code() != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument, got %v", err)
	}
}

func TestApplyConfigAgentNotConnected(t *testing.T) {
	reg := registry.New()
	disp := dispatch.New(reg)
	svc := New(reg, disp)

	_, err := svc.ApplyConfig(context.Background(), &controlpb.ApplyConfigRequest{
		AgentId:       "agent-missing",
		ConfigPayload: []byte("set foo"),
	})
	if err == nil {
		t.Fatal("expected error")
	}
}
