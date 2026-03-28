# vrouter-daemon

A standalone gRPC service that bridges [vrouter-operator](https://github.com/tjjh89017/vrouter-operator) and bare metal VyOS routers.

## Architecture

```
vrouter-operator               vrouter-server (k8s)
┌──────────────┐               ┌──────────────────────────────┐
│  Controller  │   gRPC        │  ControlService (port 50052) │
│       │      │──────────────→│         │                    │
│  gRPC Client │               │    Redis (broker + registry) │
└──────────────┘               │         │                    │
                               │  AgentService (port 50051)   │
                               └─────────┬────────────────────┘
                                         │ gRPC bidir stream
                                   ┌─────┴─────┐
                                   │ VyOS agents │ (bare metal)
                                   └────────────┘
```

**Two services on separate ports:**
- **ControlService** (`:50052`, ClusterIP) — operator-facing unary RPCs: `IsConnected`, `GetStatus`, `ApplyConfig`
- **AgentService** (`:50051`, LoadBalancer) — agent-facing bidirectional streaming

**Scale-out via Redis:**
- Agent registry (agentID → metadata) stored in Redis hashes with TTL
- ApplyConfig routed via Redis lists (RPUSH/BLPOP) — no pod-to-pod gRPC forwarding

## Binaries

| Binary | Runs on | Purpose |
|--------|---------|---------|
| `vrouter-server` | Kubernetes | gRPC server (ControlService + AgentService) |
| `vrouter-agent` | Bare metal VyOS | gRPC client, connects back to server |

## Quick Start

```bash
# Build
make build

# Run server (requires Redis)
vrouter-server \
  --agent-listen :50051 \
  --control-listen :50052 \
  --redis-addr localhost:6379 \
  --pod-ip 10.0.0.1

# Run agent
vrouter-agent \
  --server grpc.example.com:50051 \
  --agent-id vyos-tokyo-1 \
  --init-config /config/vrouter-agent/init.yaml
```

## Agent Init Config

The agent supports a failover init config that restores management connectivity when the server is unreachable. Format (YAML):

```yaml
config: |
  interfaces {
    ethernet eth0 {
      address dhcp
    }
  }
commands: |
  set protocols static route 0.0.0.0/0 next-hop 192.168.1.1
  set firewall name MGMT rule 10 action accept
  set firewall name MGMT rule 10 destination port 50051
  set firewall name MGMT rule 10 protocol tcp
```

**Behavior:**
- On server push (`apply_config`): init config is loaded first (base layer), then pushed config/commands on top. Single commit — no service interruption.
- On connection failure (after `--init-max-retries`): applies init config only to restore connectivity.
- Reconnect uses exponential backoff with jitter (1s → 2s → 4s → ... → 30s cap, ±25%).

## Configuration

All flags have corresponding environment variables.

### vrouter-server

| Flag | Env | Default | Description |
|------|-----|---------|-------------|
| `--agent-listen` | `AGENT_LISTEN_ADDR` | `:50051` | AgentService listen address |
| `--control-listen` | `CONTROL_LISTEN_ADDR` | `:50052` | ControlService listen address |
| `--redis-addr` | `REDIS_ADDR` | `localhost:6379` | Redis address |
| `--pod-ip` | `POD_IP` | | Pod IP (required, from Downward API) |

### vrouter-agent

| Flag | Env | Default | Description |
|------|-----|---------|-------------|
| `--server` | `SERVER_ADDR` | `localhost:50051` | Server address |
| `--agent-id` | `AGENT_ID` | | Agent ID (required) |
| `--init-config` | `INIT_CONFIG` | | Path to init config YAML |
| `--init-max-retries` | | `3` | Failures before applying init config |

## Kubernetes Deployment

```bash
kubectl apply -f deploy/kubernetes/
```

Creates in `vrouter-system` namespace:
- **Redis** deployment + service
- **vrouter-daemon** deployment (2 replicas, scalable)
- **vrouter-daemon** service (ClusterIP `:50052`) — for operator
- **vrouter-daemon-agents** service (LoadBalancer `:50051`) — for external agents

## Development

```bash
# Proto code generation
make proto

# Build
make build

# Tests (E2E tests require Redis on localhost:6379)
go test ./...

# Lint
go fmt ./...
go vet ./...
```

## License

Apache License 2.0
