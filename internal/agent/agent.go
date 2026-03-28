package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os/exec"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	agentpb "github.com/tjjh89017/vrouter-daemon/gen/go/agentpb"
)

// registerPayload is the JSON body of a "register" message.
type registerPayload struct {
	AgentID string `json:"agent_id"`
	Version string `json:"version"`
}

// applyConfigRequest is the JSON body of an "apply_config" message from server.
// Config and Commands match VRouterConfigSpec format.
type applyConfigRequest struct {
	ID       string `json:"id"`
	Config   string `json:"config,omitempty"`
	Commands string `json:"commands,omitempty"`
}

// configAckPayload is the JSON body of a "config_ack" response.
type configAckPayload struct {
	ID           string `json:"id"`
	Success      bool   `json:"success"`
	ExitCode     int    `json:"exit_code"`
	Stdout       string `json:"stdout"`
	Stderr       string `json:"stderr"`
	ErrorMessage string `json:"error,omitempty"`
}

// ConfigHandler is called when the agent receives an apply_config message.
// It should apply the config and return stdout, stderr, exit code, and error.
type ConfigHandler func(ctx context.Context, config string) (stdout, stderr string, exitCode int, err error)

// Agent represents a gRPC agent client.
type Agent struct {
	serverAddr    string
	agentID       string
	version       string
	configHandler ConfigHandler

	reconnectMin time.Duration
	reconnectMax time.Duration

	// Init config failover
	initConfig       *InitConfig
	initMaxRetries   int  // consecutive failures before applying init config
	initApplied      bool // true if init config was already applied this disconnect cycle
}

// Option configures an Agent.
type Option func(*Agent)

// WithVersion sets the agent version reported on register.
func WithVersion(v string) Option {
	return func(a *Agent) { a.version = v }
}

// WithConfigHandler sets the handler for apply_config messages.
func WithConfigHandler(h ConfigHandler) Option {
	return func(a *Agent) { a.configHandler = h }
}

// WithReconnect sets the min/max reconnect backoff.
func WithReconnect(min, max time.Duration) Option {
	return func(a *Agent) {
		a.reconnectMin = min
		a.reconnectMax = max
	}
}

// WithInitConfig sets the init config for failover.
// maxRetries is the number of consecutive connection failures before
// the init config is applied (0 = apply on first failure).
func WithInitConfig(ic *InitConfig, maxRetries int) Option {
	return func(a *Agent) {
		a.initConfig = ic
		a.initMaxRetries = maxRetries
	}
}

// New creates a new Agent.
func New(serverAddr, agentID string, opts ...Option) *Agent {
	a := &Agent{
		serverAddr:     serverAddr,
		agentID:        agentID,
		version:        "dev",
		configHandler:  defaultConfigHandler,
		reconnectMin:   1 * time.Second,
		reconnectMax:   30 * time.Second,
		initMaxRetries: 3,
	}
	for _, o := range opts {
		o(a)
	}
	return a
}

// Run connects to the server and processes messages. It reconnects on
// disconnect until the context is cancelled.
//
// If an InitConfig is set and the agent fails to connect after
// initMaxRetries consecutive attempts, it applies the init config
// to restore management connectivity (once per disconnect cycle).
func (a *Agent) Run(ctx context.Context) error {
	backoff := a.reconnectMin
	failures := 0

	for {
		err := a.connect(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}

		failures++
		log.Printf("disconnected from server: %v, reconnecting in %v (failures=%d)", err, backoff, failures)

		// Apply init config after too many consecutive failures
		if a.shouldApplyInitConfig(failures) {
			a.applyInitConfig(ctx)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}

		backoff *= 2
		if backoff > a.reconnectMax {
			backoff = a.reconnectMax
		}
	}
}

// shouldApplyInitConfig returns true if init config should be applied now.
func (a *Agent) shouldApplyInitConfig(failures int) bool {
	if a.initConfig == nil || a.initConfig.IsEmpty() {
		return false
	}
	if a.initApplied {
		return false // already applied this disconnect cycle
	}
	return failures > a.initMaxRetries
}

// applyInitConfig renders and executes the init config script.
func (a *Agent) applyInitConfig(ctx context.Context) {
	log.Printf("applying init config after connection failures")

	script, err := a.initConfig.RenderScript()
	if err != nil {
		log.Printf("failed to render init config script: %v", err)
		return
	}

	stdout, stderr, exitCode, err := a.configHandler(ctx, string(script))
	if err != nil {
		log.Printf("init config apply error: %v", err)
		return
	}
	if exitCode != 0 {
		log.Printf("init config apply failed (exit %d): stdout=%s stderr=%s", exitCode, stdout, stderr)
		return
	}

	log.Printf("init config applied successfully")
	a.initApplied = true
}

func (a *Agent) connect(ctx context.Context) error {
	conn, err := grpc.NewClient(a.serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("dial server: %w", err)
	}
	defer conn.Close()

	client := agentpb.NewAgentServiceClient(conn)
	stream, err := client.Connect(ctx)
	if err != nil {
		return fmt.Errorf("open stream: %w", err)
	}

	// Send register message
	regPayload, _ := json.Marshal(registerPayload{
		AgentID: a.agentID,
		Version: a.version,
	})
	if err := stream.Send(&agentpb.AgentMessage{
		Type:    "register",
		Payload: regPayload,
	}); err != nil {
		return fmt.Errorf("send register: %w", err)
	}

	log.Printf("registered as %q", a.agentID)

	// Connection succeeded — reset init config state
	a.initApplied = false

	// Message pump
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("recv: %w", err)
		}

		switch msg.Type {
		case "apply_config":
			a.handleApplyConfig(ctx, stream, msg.Payload)
		default:
			log.Printf("unknown server message type: %q", msg.Type)
		}
	}
}

func (a *Agent) handleApplyConfig(ctx context.Context, stream agentpb.AgentService_ConnectClient, payload []byte) {
	var req applyConfigRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		log.Printf("bad apply_config payload: %v", err)
		return
	}

	// Render the vbash script, merging with init config if present
	script, err := a.renderApplyScript(req.Config, req.Commands)
	if err != nil {
		log.Printf("failed to render apply script: %v", err)
		a.sendConfigAck(stream, req.ID, "", "", -1, err)
		return
	}

	stdout, stderr, exitCode, err := a.configHandler(ctx, string(script))

	a.sendConfigAck(stream, req.ID, stdout, stderr, exitCode, err)
}

// renderApplyScript generates a vbash script from pushed config/commands,
// merged with init config (init config commands appended last = highest priority).
func (a *Agent) renderApplyScript(pushedConfig, pushedCommands string) ([]byte, error) {
	if a.initConfig != nil && !a.initConfig.IsEmpty() {
		return a.initConfig.RenderMergedScript(pushedConfig, pushedCommands)
	}
	return renderScript(pushedConfig, pushedCommands)
}

func (a *Agent) sendConfigAck(stream agentpb.AgentService_ConnectClient, reqID, stdout, stderr string, exitCode int, err error) {
	ack := configAckPayload{
		ID:       reqID,
		Success:  err == nil && exitCode == 0,
		ExitCode: exitCode,
		Stdout:   stdout,
		Stderr:   stderr,
	}
	if err != nil {
		ack.ErrorMessage = err.Error()
	}

	ackPayload, _ := json.Marshal(ack)
	if sendErr := stream.Send(&agentpb.AgentMessage{
		Type:    "config_ack",
		Payload: ackPayload,
	}); sendErr != nil {
		log.Printf("failed to send config_ack: %v", sendErr)
	}
}

// defaultConfigHandler executes config as a shell script.
func defaultConfigHandler(ctx context.Context, config string) (string, string, int, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", config)
	out, err := cmd.CombinedOutput()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return "", "", -1, err
		}
	}
	return string(out), "", exitCode, nil
}
