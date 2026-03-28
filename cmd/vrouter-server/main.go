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
	"github.com/tjjh89017/vrouter-daemon/internal/cluster"
	"github.com/tjjh89017/vrouter-daemon/internal/config"
	"github.com/tjjh89017/vrouter-daemon/internal/controlapi"
	"github.com/tjjh89017/vrouter-daemon/internal/dispatch"
	"github.com/tjjh89017/vrouter-daemon/internal/registry"
)

func main() {
	cfg := config.ParseServer()

	if cfg.PodIP == "" {
		log.Fatal("--pod-ip is required (set via POD_IP env from Downward API)")
	}

	// Cluster registry + broker (Redis)
	clusterReg, redisClient, err := cluster.NewRegistry(cfg.RedisAddr, cfg.PodIP)
	if err != nil {
		log.Fatalf("failed to connect to Redis at %s: %v", cfg.RedisAddr, err)
	}
	defer clusterReg.Close()
	log.Printf("connected to Redis at %s", cfg.RedisAddr)

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
