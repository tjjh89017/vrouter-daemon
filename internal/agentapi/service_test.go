package agentapi

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"testing"

	agentpb "github.com/tjjh89017/vrouter-daemon/gen/go/agentpb"
	"github.com/tjjh89017/vrouter-daemon/internal/dispatch"
	"github.com/tjjh89017/vrouter-daemon/internal/registry"
	"google.golang.org/grpc/metadata"
)

// fakeAgentStream implements AgentService_ConnectServer for testing.
type fakeAgentStream struct {
	agentpb.AgentService_ConnectServer

	mu       sync.Mutex
	incoming []*agentpb.AgentMessage
	outgoing []*agentpb.ServerMessage
	recvIdx  int
	ctx      context.Context
}

func newFakeAgentStream(msgs ...*agentpb.AgentMessage) *fakeAgentStream {
	return &fakeAgentStream{
		incoming: msgs,
		ctx:      metadata.NewIncomingContext(context.Background(), metadata.MD{}),
	}
}

func (f *fakeAgentStream) Recv() (*agentpb.AgentMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.recvIdx >= len(f.incoming) {
		return nil, io.EOF
	}
	msg := f.incoming[f.recvIdx]
	f.recvIdx++
	return msg, nil
}

func (f *fakeAgentStream) Send(msg *agentpb.ServerMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.outgoing = append(f.outgoing, msg)
	return nil
}

func (f *fakeAgentStream) Context() context.Context {
	return f.ctx
}

func mustJSON(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}

func TestConnectRegisterAndStatus(t *testing.T) {
	reg := registry.New()
	disp := dispatch.New(reg)
	svc := New(reg, disp)

	stream := newFakeAgentStream(
		&agentpb.AgentMessage{
			Type:    "register",
			Payload: mustJSON(registerPayload{AgentID: "agent-1", Version: "1.0.0"}),
		},
		&agentpb.AgentMessage{
			Type:    "status",
			Payload: []byte(`{"up":true}`),
		},
	)

	err := svc.Connect(stream)
	if err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	// Agent should be deregistered after stream ends
	if reg.IsConnected("agent-1") {
		t.Fatal("expected agent-1 to be deregistered after disconnect")
	}
}

func TestConnectMissingRegister(t *testing.T) {
	reg := registry.New()
	disp := dispatch.New(reg)
	svc := New(reg, disp)

	stream := newFakeAgentStream(
		&agentpb.AgentMessage{
			Type:    "status",
			Payload: []byte(`{}`),
		},
	)

	err := svc.Connect(stream)
	if err == nil {
		t.Fatal("expected error for missing register")
	}
}

func TestConnectDuplicateAgent(t *testing.T) {
	reg := registry.New()
	disp := dispatch.New(reg)
	svc := New(reg, disp)

	// Pre-register agent-1
	reg.Register("agent-1", nil)

	stream := newFakeAgentStream(
		&agentpb.AgentMessage{
			Type:    "register",
			Payload: mustJSON(registerPayload{AgentID: "agent-1", Version: "1.0.0"}),
		},
	)

	err := svc.Connect(stream)
	if err == nil {
		t.Fatal("expected error for duplicate agent")
	}
}

func TestConnectConfigAck(t *testing.T) {
	reg := registry.New()
	disp := dispatch.New(reg)
	svc := New(reg, disp)

	stream := newFakeAgentStream(
		&agentpb.AgentMessage{
			Type:    "register",
			Payload: mustJSON(registerPayload{AgentID: "agent-1", Version: "1.0.0"}),
		},
		&agentpb.AgentMessage{
			Type: "config_ack",
			Payload: mustJSON(configAckPayload{
				ID:      "req-123",
				Success: true,
			}),
		},
	)

	// NotifyAck with no waiter should not panic
	err := svc.Connect(stream)
	if err != nil {
		t.Fatalf("Connect error: %v", err)
	}
}

func TestConnectEmptyAgentID(t *testing.T) {
	reg := registry.New()
	disp := dispatch.New(reg)
	svc := New(reg, disp)

	stream := newFakeAgentStream(
		&agentpb.AgentMessage{
			Type:    "register",
			Payload: mustJSON(registerPayload{AgentID: "", Version: "1.0.0"}),
		},
	)

	err := svc.Connect(stream)
	if err == nil {
		t.Fatal("expected error for empty agent_id")
	}
}
