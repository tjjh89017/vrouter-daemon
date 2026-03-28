package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"

	agentpb "github.com/tjjh89017/vrouter-daemon/gen/go/agentpb"
	controlpb "github.com/tjjh89017/vrouter-daemon/gen/go/controlpb"
	"github.com/tjjh89017/vrouter-daemon/internal/agent"
	"github.com/tjjh89017/vrouter-daemon/internal/agentapi"
	"github.com/tjjh89017/vrouter-daemon/internal/cluster"
	"github.com/tjjh89017/vrouter-daemon/internal/config"
	"github.com/tjjh89017/vrouter-daemon/internal/controlapi"
	"github.com/tjjh89017/vrouter-daemon/internal/dispatch"
	"github.com/tjjh89017/vrouter-daemon/internal/registry"
)

func main() {
	cfg := config.ParseDaemon()

	if cfg.Agent.AgentID == "" {
		log.Fatal("--agent-id is required")
	}
	if cfg.Server.PodIP == "" {
		log.Fatal("--pod-ip is required (set via POD_IP env from Downward API)")
	}

	// Cluster registry + broker (Redis)
	clusterReg, redisClient, err := cluster.NewRegistry(cfg.Server.RedisAddr, cfg.Server.PodIP)
	if err != nil {
		log.Fatalf("failed to connect to Redis at %s: %v", cfg.Server.RedisAddr, err)
	}
	defer clusterReg.Close()
	log.Printf("connected to Redis at %s", cfg.Server.RedisAddr)

	broker := cluster.NewBroker(redisClient)

	reg := registry.New()
	disp := dispatch.New(reg)

	agentSvc := agentapi.New(reg, disp, clusterReg, broker)
	controlSvc := controlapi.New(clusterReg, broker)

	// Agent-facing gRPC server
	agentServer := grpc.NewServer()
	agentpb.RegisterAgentServiceServer(agentServer, agentSvc)

	// Operator-facing gRPC server
	controlServer := grpc.NewServer()
	controlpb.RegisterControlServiceServer(controlServer, controlSvc)

	agentLis, err := net.Listen("tcp", cfg.Server.AgentListenAddr)
	if err != nil {
		log.Fatalf("failed to listen on %s: %v", cfg.Server.AgentListenAddr, err)
	}
	controlLis, err := net.Listen("tcp", cfg.Server.ControlListenAddr)
	if err != nil {
		log.Fatalf("failed to listen on %s: %v", cfg.Server.ControlListenAddr, err)
	}

	go func() {
		log.Printf("AgentService listening on %s", cfg.Server.AgentListenAddr)
		if err := agentServer.Serve(agentLis); err != nil {
			log.Fatalf("AgentService serve error: %v", err)
		}
	}()

	go func() {
		log.Printf("ControlService listening on %s", cfg.Server.ControlListenAddr)
		if err := controlServer.Serve(controlLis); err != nil {
			log.Fatalf("ControlService serve error: %v", err)
		}
	}()

	// Start the embedded agent
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := agent.New(cfg.Agent.ServerAddr, cfg.Agent.AgentID)

	go func() {
		log.Printf("embedded agent connecting to %s as %q", cfg.Agent.ServerAddr, cfg.Agent.AgentID)
		if err := a.Run(ctx); err != nil && err != context.Canceled {
			log.Printf("embedded agent error: %v", err)
		}
	}()

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	log.Printf("received signal %v, shutting down", sig)

	cancel()
	agentServer.GracefulStop()
	controlServer.GracefulStop()
	log.Println("daemon stopped")
}
