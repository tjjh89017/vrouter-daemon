package dispatch

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/google/uuid"
	agentpb "github.com/tjjh89017/vrouter-daemon/gen/go/agentpb"
	"github.com/tjjh89017/vrouter-daemon/internal/registry"
)

// ConfigResult holds the result of an apply_config request.
type ConfigResult struct {
	Success      bool   `json:"success"`
	ExitCode     int    `json:"exit_code"`
	Stdout       string `json:"stdout"`
	Stderr       string `json:"stderr"`
	ErrorMessage string `json:"error"`
}

// applyConfigPayload is the JSON payload sent to the agent.
type applyConfigPayload struct {
	ID     string `json:"id"`
	Config string `json:"config"`
}

// Dispatcher correlates apply_config requests with config_ack responses.
type Dispatcher struct {
	mu       sync.Mutex
	pending  map[string]chan *ConfigResult
	registry *registry.Registry
}

// New creates a new Dispatcher.
func New(reg *registry.Registry) *Dispatcher {
	return &Dispatcher{
		pending:  make(map[string]chan *ConfigResult),
		registry: reg,
	}
}

// ApplyConfig sends an apply_config message to the agent and blocks until the
// agent responds with a config_ack or the context is cancelled.
func (d *Dispatcher) ApplyConfig(ctx context.Context, agentID string, configPayload []byte) (*ConfigResult, error) {
	entry := d.registry.GetEntry(agentID)
	if entry == nil {
		return nil, fmt.Errorf("agent %q not connected", agentID)
	}

	reqID := uuid.New().String()
	ch := make(chan *ConfigResult, 1)

	d.mu.Lock()
	d.pending[reqID] = ch
	d.mu.Unlock()

	defer func() {
		d.mu.Lock()
		delete(d.pending, reqID)
		d.mu.Unlock()
	}()

	payload, err := json.Marshal(applyConfigPayload{
		ID:     reqID,
		Config: string(configPayload),
	})
	if err != nil {
		return nil, fmt.Errorf("marshal apply_config payload: %w", err)
	}

	err = entry.Stream.Send(&agentpb.ServerMessage{
		Type:    "apply_config",
		Id:      reqID,
		Payload: payload,
	})
	if err != nil {
		return nil, fmt.Errorf("send apply_config to agent %q: %w", agentID, err)
	}

	select {
	case result := <-ch:
		return result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// NotifyAck is called when a config_ack message is received from an agent.
// It unblocks the corresponding ApplyConfig caller.
func (d *Dispatcher) NotifyAck(reqID string, result *ConfigResult) {
	d.mu.Lock()
	ch, ok := d.pending[reqID]
	d.mu.Unlock()

	if ok {
		ch <- result
	}
}
