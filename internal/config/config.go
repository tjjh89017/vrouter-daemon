package config

import (
	"flag"
	"os"
)

// ServerConfig holds the server-side configuration.
type ServerConfig struct {
	AgentListenAddr   string // address for AgentService (default :50051)
	ControlListenAddr string // address for ControlService (default :50052)
	RedisAddr         string // Redis address for cluster registry (required)
	PodIP             string // this pod's IP for cluster registry (required)
}

// AgentConfig holds the agent-side configuration.
type AgentConfig struct {
	ServerAddr     string // address of the AgentService to connect to
	AgentID        string // unique identifier for this agent
	InitConfigPath string // path to init config YAML file (config + commands)
	InitMaxRetries int    // consecutive failures before applying init config
}

// DaemonConfig holds configuration for mixed mode (server + agent).
type DaemonConfig struct {
	Server ServerConfig
	Agent  AgentConfig
}

// ParseServer reads server configuration from flags and environment variables.
func ParseServer() *ServerConfig {
	cfg := &ServerConfig{}
	flag.StringVar(&cfg.AgentListenAddr, "agent-listen", envOrDefault("AGENT_LISTEN_ADDR", ":50051"), "AgentService listen address")
	flag.StringVar(&cfg.ControlListenAddr, "control-listen", envOrDefault("CONTROL_LISTEN_ADDR", ":50052"), "ControlService listen address")
	flag.StringVar(&cfg.RedisAddr, "redis-addr", envOrDefault("REDIS_ADDR", "localhost:6379"), "Redis address for cluster registry")
	flag.StringVar(&cfg.PodIP, "pod-ip", envOrDefault("POD_IP", ""), "Pod IP for cluster registry (required)")
	flag.Parse()
	return cfg
}

// ParseAgent reads agent configuration from flags and environment variables.
func ParseAgent() *AgentConfig {
	cfg := &AgentConfig{}
	flag.StringVar(&cfg.ServerAddr, "server", envOrDefault("SERVER_ADDR", "localhost:50051"), "AgentService server address")
	flag.StringVar(&cfg.AgentID, "agent-id", envOrDefault("AGENT_ID", ""), "Agent ID (required)")
	flag.StringVar(&cfg.InitConfigPath, "init-config", envOrDefault("INIT_CONFIG", ""), "Path to init config YAML file (config + commands)")
	flag.IntVar(&cfg.InitMaxRetries, "init-max-retries", 3, "Consecutive connection failures before applying init config")
	flag.Parse()
	return cfg
}

// ParseDaemon reads mixed mode configuration from flags and environment variables.
func ParseDaemon() *DaemonConfig {
	cfg := &DaemonConfig{}
	flag.StringVar(&cfg.Server.AgentListenAddr, "agent-listen", envOrDefault("AGENT_LISTEN_ADDR", ":50051"), "AgentService listen address")
	flag.StringVar(&cfg.Server.ControlListenAddr, "control-listen", envOrDefault("CONTROL_LISTEN_ADDR", ":50052"), "ControlService listen address")
	flag.StringVar(&cfg.Server.RedisAddr, "redis-addr", envOrDefault("REDIS_ADDR", "localhost:6379"), "Redis address for cluster registry")
	flag.StringVar(&cfg.Server.PodIP, "pod-ip", envOrDefault("POD_IP", ""), "Pod IP for cluster registry (required)")
	flag.StringVar(&cfg.Agent.ServerAddr, "server", envOrDefault("SERVER_ADDR", ""), "AgentService server address (default: connect to local agent-listen)")
	flag.StringVar(&cfg.Agent.AgentID, "agent-id", envOrDefault("AGENT_ID", ""), "Agent ID (required)")
	flag.StringVar(&cfg.Agent.InitConfigPath, "init-config", envOrDefault("INIT_CONFIG", ""), "Path to init config YAML file (config + commands)")
	flag.IntVar(&cfg.Agent.InitMaxRetries, "init-max-retries", 3, "Consecutive connection failures before applying init config")
	flag.Parse()

	// In daemon mode, default to connecting to the local agent listener
	if cfg.Agent.ServerAddr == "" {
		cfg.Agent.ServerAddr = "localhost" + cfg.Server.AgentListenAddr
	}

	return cfg
}

func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
