package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"

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
	ID               string `json:"id"`
	Config           string `json:"config,omitempty"`
	Commands         string `json:"commands,omitempty"`
	DisconnectPolicy string `json:"disconnect_policy,omitempty"` // override agent's default: "keep" or "rollback"
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

// DisconnectPolicy controls what the agent does when it can't reach the server.
type DisconnectPolicy string

const (
	// PolicyKeep keeps the current running config (default).
	// Use when server downtime shouldn't affect the router.
	PolicyKeep DisconnectPolicy = "keep"

	// PolicyRollback applies the init config to restore a known-good baseline.
	// Use when a bad config push might have broken connectivity.
	PolicyRollback DisconnectPolicy = "rollback"
)

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
	initMaxRetries   int              // consecutive failures before policy kicks in
	disconnectPolicy DisconnectPolicy // what to do on prolonged disconnect
	initApplied      bool             // true if init config was already applied this disconnect cycle
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

// WithInitConfig sets the init config and disconnect policy.
// maxRetries is the number of consecutive connection failures before
// the policy kicks in.
func WithInitConfig(ic *InitConfig, maxRetries int, policy DisconnectPolicy) Option {
	return func(a *Agent) {
		a.initConfig = ic
		a.initMaxRetries = maxRetries
		a.disconnectPolicy = policy
	}
}

// New creates a new Agent.
func New(serverAddr, agentID string, opts ...Option) *Agent {
	a := &Agent{
		serverAddr:       serverAddr,
		agentID:          agentID,
		version:          "dev",
		configHandler:    defaultConfigHandler,
		reconnectMin:     1 * time.Second,
		reconnectMax:     30 * time.Second,
		initMaxRetries:   3,
		disconnectPolicy: PolicyKeep,
	}
	for _, o := range opts {
		o(a)
	}
	return a
}

// Run connects to the server and processes messages. It reconnects on
// disconnect with exponential backoff + jitter until the context is cancelled.
//
// If an InitConfig is set and the agent fails to connect after
// initMaxRetries consecutive attempts, it applies the init config
// to restore management connectivity (once per disconnect cycle).
// On successful reconnect, failure count and backoff reset.
func (a *Agent) Run(ctx context.Context) error {
	bo := newBackoff(a.reconnectMin, a.reconnectMax)
	failures := 0

	for {
		if err := a.connect(ctx); err != nil {
			log.Printf("connect: %v", err)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		failures++
		wait := bo.Next()
		log.Printf("disconnected from server, reconnecting in %v (failures=%d)", wait, failures)

		// Apply init config after too many consecutive failures
		if a.shouldApplyInitConfig(failures) {
			a.applyInitConfig(ctx)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

// shouldApplyInitConfig returns true if init config should be applied now.
func (a *Agent) shouldApplyInitConfig(failures int) bool {
	if a.disconnectPolicy != PolicyRollback {
		return false
	}
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

	// Write after_config to temp file if merge will be needed
	if a.initConfig.AfterConfig != "" {
		if err := os.WriteFile(AfterConfigFile, []byte(a.initConfig.AfterConfig), 0644); err != nil {
			log.Printf("failed to write after config: %v", err)
			return
		}
	}

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
	conn, err := grpc.NewClient(a.serverAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                30 * time.Second,
			Timeout:             10 * time.Second,
			PermitWithoutStream: true,
		}),
	)
	if err != nil {
		return fmt.Errorf("dial server: %w", err)
	}
	defer func() { _ = conn.Close() }()

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

	// Override disconnect policy if the server specifies one
	if req.DisconnectPolicy != "" {
		a.disconnectPolicy = DisconnectPolicy(req.DisconnectPolicy)
		log.Printf("disconnect policy overridden to %q by server", a.disconnectPolicy)
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
// merged with init config if present.
func (a *Agent) renderApplyScript(pushedConfig, pushedCommands string) ([]byte, error) {
	if a.initConfig != nil && !a.initConfig.IsEmpty() {
		// Write after_config to temp file if merge will be needed
		if a.initConfig.AfterConfig != "" {
			if err := os.WriteFile(AfterConfigFile, []byte(a.initConfig.AfterConfig), 0644); err != nil {
				return nil, fmt.Errorf("write after config to %s: %w", AfterConfigFile, err)
			}
		}
		return a.initConfig.RenderMergedScript(pushedConfig, pushedCommands)
	}
	return RenderSimpleScript(pushedConfig, pushedCommands)
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

// defaultConfigHandler writes the script to a temp file and executes it
// directly so the #!/bin/vbash shebang is respected by the kernel.
func defaultConfigHandler(ctx context.Context, config string) (string, string, int, error) {
	f, err := os.CreateTemp("", "vrouter-config-*.sh")
	if err != nil {
		return "", "", -1, fmt.Errorf("create temp script: %w", err)
	}
	defer func() { _ = os.Remove(f.Name()) }()

	if _, err := f.WriteString(config); err != nil {
		_ = f.Close()
		return "", "", -1, fmt.Errorf("write temp script: %w", err)
	}
	if err := f.Chmod(0700); err != nil {
		_ = f.Close()
		return "", "", -1, fmt.Errorf("chmod temp script: %w", err)
	}
	_ = f.Close()

	cmd := exec.CommandContext(ctx, f.Name())
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
