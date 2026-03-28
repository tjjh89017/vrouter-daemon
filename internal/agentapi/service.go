package agentapi

import (
	"encoding/json"
	"fmt"
	"io"
	"log"

	agentpb "github.com/tjjh89017/vrouter-daemon/gen/go/agentpb"
	"github.com/tjjh89017/vrouter-daemon/internal/dispatch"
	"github.com/tjjh89017/vrouter-daemon/internal/registry"
)

// registerPayload is the JSON body of a "register" message from the agent.
type registerPayload struct {
	AgentID string `json:"agent_id"`
	Version string `json:"version"`
}

// configAckPayload is the JSON body of a "config_ack" message from the agent.
type configAckPayload struct {
	ID           string `json:"id"`
	Success      bool   `json:"success"`
	ExitCode     int    `json:"exit_code"`
	Stdout       string `json:"stdout"`
	Stderr       string `json:"stderr"`
	ErrorMessage string `json:"error"`
}

// Service implements the AgentService gRPC handler.
type Service struct {
	agentpb.UnimplementedAgentServiceServer
	registry   *registry.Registry
	dispatcher *dispatch.Dispatcher
}

// New creates a new AgentService handler.
func New(reg *registry.Registry, disp *dispatch.Dispatcher) *Service {
	return &Service{
		registry:   reg,
		dispatcher: disp,
	}
}

// Connect handles a bidirectional stream from an agent.
func (s *Service) Connect(stream agentpb.AgentService_ConnectServer) error {
	// First message must be "register"
	msg, err := stream.Recv()
	if err != nil {
		return fmt.Errorf("recv register: %w", err)
	}
	if msg.Type != "register" {
		return fmt.Errorf("expected register message, got %q", msg.Type)
	}

	var reg registerPayload
	if err := json.Unmarshal(msg.Payload, &reg); err != nil {
		return fmt.Errorf("unmarshal register payload: %w", err)
	}
	if reg.AgentID == "" {
		return fmt.Errorf("register: agent_id is required")
	}

	if !s.registry.Register(reg.AgentID, stream) {
		return fmt.Errorf("agent %q already connected", reg.AgentID)
	}
	defer s.registry.Deregister(reg.AgentID)

	log.Printf("agent %q connected (version: %s)", reg.AgentID, reg.Version)
	defer log.Printf("agent %q disconnected", reg.AgentID)

	if reg.Version != "" {
		s.registry.UpdateStatus(reg.AgentID, reg.Version, nil)
	}

	// Message pump
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("recv from agent %q: %w", reg.AgentID, err)
		}

		switch msg.Type {
		case "status":
			s.registry.UpdateStatus(reg.AgentID, reg.Version, msg.Payload)
		case "config_ack":
			s.handleConfigAck(reg.AgentID, msg.Payload)
		default:
			log.Printf("agent %q: unknown message type %q", reg.AgentID, msg.Type)
		}
	}
}

func (s *Service) handleConfigAck(agentID string, payload []byte) {
	var ack configAckPayload
	if err := json.Unmarshal(payload, &ack); err != nil {
		log.Printf("agent %q: bad config_ack payload: %v", agentID, err)
		return
	}

	s.dispatcher.NotifyAck(ack.ID, &dispatch.ConfigResult{
		Success:      ack.Success,
		ExitCode:     ack.ExitCode,
		Stdout:       ack.Stdout,
		Stderr:       ack.Stderr,
		ErrorMessage: ack.ErrorMessage,
	})
}
