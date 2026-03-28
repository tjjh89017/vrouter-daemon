package config

import (
	"flag"
	"os"
)

// Config holds the server configuration.
type Config struct {
	AgentListenAddr   string // address for AgentService (port 50051)
	ControlListenAddr string // address for ControlService (port 50052)
}

// Parse reads configuration from flags and environment variables.
// Environment variables override flag defaults but flags override env vars.
func Parse() *Config {
	cfg := &Config{}

	flag.StringVar(&cfg.AgentListenAddr, "agent-listen", envOrDefault("AGENT_LISTEN_ADDR", ":50051"), "AgentService listen address")
	flag.StringVar(&cfg.ControlListenAddr, "control-listen", envOrDefault("CONTROL_LISTEN_ADDR", ":50052"), "ControlService listen address")
	flag.Parse()

	return cfg
}

func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
