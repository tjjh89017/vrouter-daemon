package e2e

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	agentpb "github.com/tjjh89017/vrouter-daemon/gen/go/agentpb"
	controlpb "github.com/tjjh89017/vrouter-daemon/gen/go/controlpb"
	"github.com/tjjh89017/vrouter-daemon/internal/agent"
	"github.com/tjjh89017/vrouter-daemon/internal/agentapi"
	"github.com/tjjh89017/vrouter-daemon/internal/cluster"
	"github.com/tjjh89017/vrouter-daemon/internal/controlapi"
	"github.com/tjjh89017/vrouter-daemon/internal/dispatch"
	"github.com/tjjh89017/vrouter-daemon/internal/registry"
)

// testEnv sets up a full server + agent environment for testing.
type testEnv struct {
	reg           *registry.Registry
	disp          *dispatch.Dispatcher
	agentServer   *grpc.Server
	controlServer *grpc.Server
	agentAddr     string
	controlAddr   string
	cancelFuncs   []context.CancelFunc
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	clusterReg, redisClient := cluster.TestRegistry(t, "127.0.0.1")
	broker := cluster.TestBrokerWithPrefix(t, redisClient, clusterReg.TestKeyPrefix())

	reg := registry.New()
	disp := dispatch.New(reg)
	agentSvc := agentapi.New(reg, disp, clusterReg, broker)
	controlSvc := controlapi.New(clusterReg, broker)

	agentServer := grpc.NewServer()
	agentpb.RegisterAgentServiceServer(agentServer, agentSvc)

	controlServer := grpc.NewServer()
	controlpb.RegisterControlServiceServer(controlServer, controlSvc)

	agentLis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen agent: %v", err)
	}
	controlLis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen control: %v", err)
	}

	go func() { _ = agentServer.Serve(agentLis) }()
	go func() { _ = controlServer.Serve(controlLis) }()

	env := &testEnv{
		reg:           reg,
		disp:          disp,
		agentServer:   agentServer,
		controlServer: controlServer,
		agentAddr:     agentLis.Addr().String(),
		controlAddr:   controlLis.Addr().String(),
	}

	t.Cleanup(func() {
		for _, cancel := range env.cancelFuncs {
			cancel()
		}
		agentServer.GracefulStop()
		controlServer.GracefulStop()
	})

	return env
}

func (e *testEnv) startAgent(t *testing.T, agentID string, handler agent.ConfigHandler) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	e.cancelFuncs = append(e.cancelFuncs, cancel)

	opts := []agent.Option{
		agent.WithReconnect(50*time.Millisecond, 200*time.Millisecond),
	}
	if handler != nil {
		opts = append(opts, agent.WithConfigHandler(handler))
	}

	a := agent.New(e.agentAddr, agentID, opts...)
	go func() { _ = a.Run(ctx) }()
}

func (e *testEnv) controlClient(t *testing.T) controlpb.ControlServiceClient {
	t.Helper()
	conn, err := grpc.NewClient(e.controlAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial control: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return controlpb.NewControlServiceClient(conn)
}

func waitForAgent(t *testing.T, client controlpb.ControlServiceClient, agentID string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := client.IsConnected(context.Background(), &controlpb.IsConnectedRequest{AgentId: agentID})
		if err == nil && resp.Connected {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("agent %q did not connect within %v", agentID, timeout)
}

// --- Tests ---

func TestE2E_ApplyConfigSuccess(t *testing.T) {
	env := newTestEnv(t)
	client := env.controlClient(t)

	env.startAgent(t, "agent-1", func(ctx context.Context, config string) (string, string, int, error) {
		return config, "", 0, nil
	})

	waitForAgent(t, client, "agent-1", 3*time.Second)

	resp, err := client.ApplyConfig(context.Background(), &controlpb.ApplyConfigRequest{
		AgentId:        "agent-1",
		ConfigPayload:  []byte(`{"commands":"set interfaces eth0 address dhcp"}`),
		TimeoutSeconds: 5,
	})
	if err != nil {
		t.Fatalf("ApplyConfig error: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success, got error: %s", resp.ErrorMessage)
	}
	// Agent renders a vbash script containing the commands
	if !strings.Contains(resp.Stdout, "set interfaces eth0 address dhcp") {
		t.Fatalf("expected stdout to contain commands, got: %q", resp.Stdout)
	}
	if !strings.Contains(resp.Stdout, "#!/bin/vbash") {
		t.Fatalf("expected stdout to be a vbash script, got: %q", resp.Stdout)
	}
}

func TestE2E_ApplyConfigFailure(t *testing.T) {
	env := newTestEnv(t)
	client := env.controlClient(t)

	env.startAgent(t, "agent-1", func(ctx context.Context, config string) (string, string, int, error) {
		return "", "command not found", 127, nil
	})

	waitForAgent(t, client, "agent-1", 3*time.Second)

	resp, err := client.ApplyConfig(context.Background(), &controlpb.ApplyConfigRequest{
		AgentId:        "agent-1",
		ConfigPayload:  []byte(`{"commands":"bad-command"}`),
		TimeoutSeconds: 5,
	})
	if err != nil {
		t.Fatalf("ApplyConfig error: %v", err)
	}
	if resp.Success {
		t.Fatal("expected failure")
	}
	if resp.ExitCode != 127 {
		t.Fatalf("expected exit code 127, got %d", resp.ExitCode)
	}
}

func TestE2E_ApplyConfigTimeout(t *testing.T) {
	env := newTestEnv(t)
	client := env.controlClient(t)

	env.startAgent(t, "agent-1", func(ctx context.Context, config string) (string, string, int, error) {
		// Simulate an unresponsive agent
		<-ctx.Done()
		return "", "", 0, ctx.Err()
	})

	waitForAgent(t, client, "agent-1", 3*time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_, err := client.ApplyConfig(ctx, &controlpb.ApplyConfigRequest{
		AgentId:        "agent-1",
		ConfigPayload:  []byte(`{"commands":"slow-config"}`),
		TimeoutSeconds: 1,
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestE2E_AgentNotConnected(t *testing.T) {
	env := newTestEnv(t)
	client := env.controlClient(t)

	resp, err := client.IsConnected(context.Background(), &controlpb.IsConnectedRequest{AgentId: "agent-missing"})
	if err != nil {
		t.Fatalf("IsConnected error: %v", err)
	}
	if resp.Connected {
		t.Fatal("expected not connected")
	}

	_, err = client.ApplyConfig(context.Background(), &controlpb.ApplyConfigRequest{
		AgentId:        "agent-missing",
		ConfigPayload:  []byte(`{"commands":"set foo"}`),
		TimeoutSeconds: 1,
	})
	if err == nil {
		t.Fatal("expected error for missing agent")
	}
}

func TestE2E_ConcurrentAgents(t *testing.T) {
	env := newTestEnv(t)
	client := env.controlClient(t)

	for i := range 5 {
		id := fmt.Sprintf("agent-%d", i)
		env.startAgent(t, id, func(ctx context.Context, config string) (string, string, int, error) {
			return "ok", "", 0, nil
		})
	}

	for i := range 5 {
		waitForAgent(t, client, fmt.Sprintf("agent-%d", i), 3*time.Second)
	}

	// Apply config to all agents concurrently
	errCh := make(chan error, 5)
	for i := range 5 {
		go func(id string) {
			resp, err := client.ApplyConfig(context.Background(), &controlpb.ApplyConfigRequest{
				AgentId:        id,
				ConfigPayload:  []byte(`{"commands":"set test"}`),
				TimeoutSeconds: 5,
			})
			if err != nil {
				errCh <- fmt.Errorf("agent %s: %v", id, err)
				return
			}
			if !resp.Success {
				errCh <- fmt.Errorf("agent %s: not success", id)
				return
			}
			errCh <- nil
		}(fmt.Sprintf("agent-%d", i))
	}

	for range 5 {
		if err := <-errCh; err != nil {
			t.Fatal(err)
		}
	}
}

func TestE2E_DuplicateAgentID(t *testing.T) {
	env := newTestEnv(t)
	client := env.controlClient(t)

	env.startAgent(t, "agent-dup", func(ctx context.Context, config string) (string, string, int, error) {
		return "first", "", 0, nil
	})
	waitForAgent(t, client, "agent-dup", 3*time.Second)

	// Start a second agent with the same ID — it should fail to register
	// and keep retrying. The first agent should remain connected.
	env.startAgent(t, "agent-dup", func(ctx context.Context, config string) (string, string, int, error) {
		return "second", "", 0, nil
	})

	time.Sleep(300 * time.Millisecond)

	// First agent should still be the one handling requests
	resp, err := client.ApplyConfig(context.Background(), &controlpb.ApplyConfigRequest{
		AgentId:        "agent-dup",
		ConfigPayload:  []byte(`{"commands":"test"}`),
		TimeoutSeconds: 5,
	})
	if err != nil {
		t.Fatalf("ApplyConfig error: %v", err)
	}
	if resp.Stdout != "first" {
		t.Fatalf("expected first agent to handle request, got stdout: %q", resp.Stdout)
	}
}

func TestE2E_AgentDisconnectReconnect(t *testing.T) {
	env := newTestEnv(t)
	client := env.controlClient(t)

	ctx, cancel := context.WithCancel(context.Background())

	a := agent.New(env.agentAddr, "agent-reconnect",
		agent.WithReconnect(50*time.Millisecond, 200*time.Millisecond),
		agent.WithConfigHandler(func(ctx context.Context, config string) (string, string, int, error) {
			return "ok", "", 0, nil
		}),
	)
	go func() { _ = a.Run(ctx) }()

	waitForAgent(t, client, "agent-reconnect", 3*time.Second)

	// Cancel the agent context to force disconnect
	cancel()
	time.Sleep(300 * time.Millisecond)

	// Agent should be disconnected
	resp, _ := client.IsConnected(context.Background(), &controlpb.IsConnectedRequest{AgentId: "agent-reconnect"})
	if resp.Connected {
		t.Fatal("expected agent to be disconnected")
	}

	// Restart agent
	ctx2, cancel2 := context.WithCancel(context.Background())
	env.cancelFuncs = append(env.cancelFuncs, cancel2)

	a2 := agent.New(env.agentAddr, "agent-reconnect",
		agent.WithReconnect(50*time.Millisecond, 200*time.Millisecond),
		agent.WithConfigHandler(func(ctx context.Context, config string) (string, string, int, error) {
			return "reconnected", "", 0, nil
		}),
	)
	go func() { _ = a2.Run(ctx2) }()

	waitForAgent(t, client, "agent-reconnect", 3*time.Second)

	applyResp, err := client.ApplyConfig(context.Background(), &controlpb.ApplyConfigRequest{
		AgentId:        "agent-reconnect",
		ConfigPayload:  []byte(`{"commands":"test"}`),
		TimeoutSeconds: 5,
	})
	if err != nil {
		t.Fatalf("ApplyConfig after reconnect error: %v", err)
	}
	if applyResp.Stdout != "reconnected" {
		t.Fatalf("expected reconnected handler, got: %q", applyResp.Stdout)
	}
}

func TestE2E_GetStatus(t *testing.T) {
	env := newTestEnv(t)
	client := env.controlClient(t)

	env.startAgent(t, "agent-status", nil)
	waitForAgent(t, client, "agent-status", 3*time.Second)

	resp, err := client.GetStatus(context.Background(), &controlpb.GetStatusRequest{AgentId: "agent-status"})
	if err != nil {
		t.Fatalf("GetStatus error: %v", err)
	}
	if resp.AgentVersion != "dev" {
		t.Fatalf("expected version 'dev', got %q", resp.AgentVersion)
	}
}

func TestE2E_GracefulShutdown(t *testing.T) {
	clusterReg, redisClient := cluster.TestRegistry(t, "127.0.0.1")
	broker := cluster.TestBrokerWithPrefix(t, redisClient, clusterReg.TestKeyPrefix())

	reg := registry.New()
	disp := dispatch.New(reg)
	agentSvc := agentapi.New(reg, disp, clusterReg, broker)
	controlSvc := controlapi.New(clusterReg, broker)

	agentServer := grpc.NewServer()
	agentpb.RegisterAgentServiceServer(agentServer, agentSvc)
	controlServer := grpc.NewServer()
	controlpb.RegisterControlServiceServer(controlServer, controlSvc)

	agentLis, _ := net.Listen("tcp", "127.0.0.1:0")
	controlLis, _ := net.Listen("tcp", "127.0.0.1:0")

	go func() { _ = agentServer.Serve(agentLis) }()
	go func() { _ = controlServer.Serve(controlLis) }()

	ctx, cancel := context.WithCancel(context.Background())
	a := agent.New(agentLis.Addr().String(), "shutdown-test",
		agent.WithReconnect(50*time.Millisecond, 200*time.Millisecond),
	)
	go func() { _ = a.Run(ctx) }()

	// Wait for connection
	conn, _ := grpc.NewClient(controlLis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	defer func() { _ = conn.Close() }()
	client := controlpb.NewControlServiceClient(conn)
	waitForAgent(t, client, "shutdown-test", 3*time.Second)

	// Graceful shutdown
	cancel()
	agentServer.GracefulStop()
	controlServer.GracefulStop()
}
