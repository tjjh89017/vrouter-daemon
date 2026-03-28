package main

import (
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"

	agentpb "github.com/tjjh89017/vrouter-daemon/gen/go/agentpb"
	controlpb "github.com/tjjh89017/vrouter-daemon/gen/go/controlpb"
	"github.com/tjjh89017/vrouter-daemon/internal/agentapi"
	"github.com/tjjh89017/vrouter-daemon/internal/config"
	"github.com/tjjh89017/vrouter-daemon/internal/controlapi"
	"github.com/tjjh89017/vrouter-daemon/internal/dispatch"
	"github.com/tjjh89017/vrouter-daemon/internal/registry"
)

func main() {
	cfg := config.Parse()

	reg := registry.New()
	disp := dispatch.New(reg)
	agentSvc := agentapi.New(reg, disp)
	controlSvc := controlapi.New(reg, disp)

	// Agent-facing gRPC server (port 50051)
	agentServer := grpc.NewServer()
	agentpb.RegisterAgentServiceServer(agentServer, agentSvc)

	// Operator-facing gRPC server (port 50052)
	controlServer := grpc.NewServer()
	controlpb.RegisterControlServiceServer(controlServer, controlSvc)

	agentLis, err := net.Listen("tcp", cfg.AgentListenAddr)
	if err != nil {
		log.Fatalf("failed to listen on %s: %v", cfg.AgentListenAddr, err)
	}
	controlLis, err := net.Listen("tcp", cfg.ControlListenAddr)
	if err != nil {
		log.Fatalf("failed to listen on %s: %v", cfg.ControlListenAddr, err)
	}

	go func() {
		log.Printf("AgentService listening on %s", cfg.AgentListenAddr)
		if err := agentServer.Serve(agentLis); err != nil {
			log.Fatalf("AgentService serve error: %v", err)
		}
	}()

	go func() {
		log.Printf("ControlService listening on %s", cfg.ControlListenAddr)
		if err := controlServer.Serve(controlLis); err != nil {
			log.Fatalf("ControlService serve error: %v", err)
		}
	}()

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	log.Printf("received signal %v, shutting down", sig)

	agentServer.GracefulStop()
	controlServer.GracefulStop()
	log.Println("server stopped")
}
