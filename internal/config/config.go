package config

import (
	"flag"
	"os"
	"strings"
)

// ServerConfig holds the server-side configuration.
type ServerConfig struct {
	AgentListenAddr    string // address for AgentService (default :50051)
	ControlListenAddr  string // address for ControlService (default :50052)
	RedisAddr          string // Redis address for standalone mode
	RedisSentinelAddrs string // comma-separated Sentinel addresses (HA mode)
	RedisMasterName    string // Sentinel master name (default "vrouter-redis")
	PodIP              string // this pod's IP for cluster registry (required)
}

// AgentConfig holds the agent-side configuration.
type AgentConfig struct {
	ServerAddr       string // address of the AgentService to connect to
	AgentID          string // unique identifier for this agent
	InitConfigPath   string // path to init config YAML file (config + commands)
	InitMaxRetries   int    // consecutive failures before policy kicks in
	DisconnectPolicy string // "keep" (default) or "rollback"
}

// ParseServer reads server configuration from flags and environment variables.
func ParseServer() *ServerConfig {
	cfg := &ServerConfig{}
	flag.StringVar(&cfg.AgentListenAddr, "agent-listen", envOrDefault("AGENT_LISTEN_ADDR", ":50051"), "AgentService listen address")
	flag.StringVar(&cfg.ControlListenAddr, "control-listen", envOrDefault("CONTROL_LISTEN_ADDR", ":50052"), "ControlService listen address")
	flag.StringVar(&cfg.RedisAddr, "redis-addr", envOrDefault("REDIS_ADDR", "localhost:6379"), "Redis address (standalone mode)")
	flag.StringVar(&cfg.RedisSentinelAddrs, "redis-sentinel-addrs", envOrDefault("REDIS_SENTINEL_ADDRS", ""), "Comma-separated Sentinel addresses (HA mode)")
	flag.StringVar(&cfg.RedisMasterName, "redis-master-name", envOrDefault("REDIS_MASTER_NAME", "vrouter-redis"), "Sentinel master name")
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
	flag.IntVar(&cfg.InitMaxRetries, "init-max-retries", 3, "Consecutive connection failures before disconnect policy kicks in")
	flag.StringVar(&cfg.DisconnectPolicy, "disconnect-policy", envOrDefault("DISCONNECT_POLICY", "keep"), "Disconnect policy: keep (default) or rollback")
	flag.Parse()
	return cfg
}

// UseSentinel returns true if Sentinel addresses are configured.
func (c *ServerConfig) UseSentinel() bool {
	return c.RedisSentinelAddrs != ""
}

// SentinelAddrs returns the parsed Sentinel addresses.
func (c *ServerConfig) SentinelAddrs() []string {
	return strings.Split(c.RedisSentinelAddrs, ",")
}

func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
