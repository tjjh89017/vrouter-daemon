package dispatch

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	agentpb "github.com/tjjh89017/vrouter-daemon/gen/go/agentpb"
	"github.com/tjjh89017/vrouter-daemon/internal/registry"
	"google.golang.org/grpc/metadata"
)

// fakeStream implements agentpb.AgentService_ConnectServer for testing.
type fakeStream struct {
	agentpb.AgentService_ConnectServer
	mu       sync.Mutex
	messages []*agentpb.ServerMessage
}

func (f *fakeStream) Send(msg *agentpb.ServerMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages = append(f.messages, msg)
	return nil
}

func (f *fakeStream) Context() context.Context {
	return metadata.NewIncomingContext(context.Background(), metadata.MD{})
}

func TestApplyConfigAndAck(t *testing.T) {
	reg := registry.New()
	stream := &fakeStream{}
	reg.Register("agent-1", stream)

	d := New(reg)

	var result *ConfigResult
	var applyErr error
	done := make(chan struct{})

	go func() {
		defer close(done)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		result, applyErr = d.ApplyConfig(ctx, "agent-1", []byte("set interfaces eth0 address dhcp"))
	}()

	// Wait for the message to be sent
	time.Sleep(50 * time.Millisecond)

	stream.mu.Lock()
	if len(stream.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(stream.messages))
	}
	msg := stream.messages[0]
	stream.mu.Unlock()

	if msg.Type != "apply_config" {
		t.Fatalf("expected type apply_config, got %s", msg.Type)
	}

	// Parse the payload to get the request ID
	var payload applyConfigPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	// Send ack
	d.NotifyAck(payload.ID, &ConfigResult{
		Success:  true,
		ExitCode: 0,
		Stdout:   "ok",
	})

	<-done

	if applyErr != nil {
		t.Fatalf("ApplyConfig error: %v", applyErr)
	}
	if !result.Success {
		t.Fatal("expected success")
	}
	if result.Stdout != "ok" {
		t.Fatalf("expected stdout 'ok', got %q", result.Stdout)
	}
}

func TestApplyConfigTimeout(t *testing.T) {
	reg := registry.New()
	stream := &fakeStream{}
	reg.Register("agent-1", stream)

	d := New(reg)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := d.ApplyConfig(ctx, "agent-1", []byte("set foo"))
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if err != context.DeadlineExceeded {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}
}

func TestApplyConfigAgentNotConnected(t *testing.T) {
	reg := registry.New()
	d := New(reg)

	ctx := context.Background()
	_, err := d.ApplyConfig(ctx, "agent-missing", []byte("set foo"))
	if err == nil {
		t.Fatal("expected error for missing agent")
	}
}

func TestNotifyAckNoWaiter(t *testing.T) {
	reg := registry.New()
	d := New(reg)

	// Should not panic even if no one is waiting
	d.NotifyAck("nonexistent-id", &ConfigResult{Success: true})
}
