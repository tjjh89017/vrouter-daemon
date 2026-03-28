package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/tjjh89017/vrouter-daemon/internal/agent"
	"github.com/tjjh89017/vrouter-daemon/internal/config"
)

func main() {
	cfg := config.ParseAgent()

	if cfg.AgentID == "" {
		log.Fatal("--agent-id is required")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		log.Printf("received signal %v, shutting down", sig)
		cancel()
	}()

	a := agent.New(cfg.ServerAddr, cfg.AgentID)
	log.Printf("connecting to server %s as %q", cfg.ServerAddr, cfg.AgentID)

	if err := a.Run(ctx); err != nil && err != context.Canceled {
		log.Fatalf("agent error: %v", err)
	}
}
