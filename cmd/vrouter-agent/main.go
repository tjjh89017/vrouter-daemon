package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/tjjh89017/vrouter-daemon/internal/agent"
	"github.com/tjjh89017/vrouter-daemon/internal/config"
)

func main() {
	cfg := config.ParseAgent()

	if cfg.AgentID == "" {
		id, err := os.ReadFile("/etc/machine-id")
		if err != nil {
			log.Fatalf("--agent-id not set and cannot read /etc/machine-id: %v", err)
		}
		cfg.AgentID = strings.TrimSpace(string(id))
		log.Printf("agent-id not set, using machine-id: %s", cfg.AgentID)
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

	var opts []agent.Option

	// Load init config if specified
	if cfg.InitConfigPath != "" {
		ic, err := agent.LoadInitConfig(cfg.InitConfigPath)
		if err != nil {
			log.Fatalf("failed to load init config from %s: %v", cfg.InitConfigPath, err)
		}
		if !ic.IsEmpty() {
			policy := agent.DisconnectPolicy(cfg.DisconnectPolicy)
			log.Printf("loaded init config from %s (config=%d bytes, commands=%d bytes, disconnect-policy=%s)",
				cfg.InitConfigPath, len(ic.Config), len(ic.Commands), policy)
			opts = append(opts, agent.WithInitConfig(ic, cfg.InitMaxRetries, policy))
		}
	}

	a := agent.New(cfg.ServerAddr, cfg.AgentID, opts...)
	log.Printf("connecting to server %s as %q", cfg.ServerAddr, cfg.AgentID)

	if err := a.Run(ctx); err != nil && err != context.Canceled {
		log.Fatalf("agent error: %v", err)
	}
}
