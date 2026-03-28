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

**Scale-out via Redis Sentinel:**
- Agent registry (agentID → metadata) stored in Redis hashes with TTL
- ApplyConfig routed via Redis lists (RPUSH/BLPOP) — no pod-to-pod gRPC forwarding
- Redis HA via Sentinel (3 Redis + 3 Sentinel, automatic failover)

## Binaries

| Binary | Runs on | Purpose |
|--------|---------|---------|
| `vrouter-server` | Kubernetes | gRPC server (ControlService + AgentService) |
| `vrouter-agent` | Bare metal VyOS | gRPC client, connects back to server |

## Quick Start

```bash
# Build
make build

# Run server (standalone Redis for dev)
vrouter-server \
  --agent-listen :50051 \
  --control-listen :50052 \
  --redis-addr localhost:6379 \
  --pod-ip 10.0.0.1

# Run server (Redis Sentinel for production)
vrouter-server \
  --agent-listen :50051 \
  --control-listen :50052 \
  --redis-sentinel-addrs sentinel-0:26379,sentinel-1:26379,sentinel-2:26379 \
  --redis-master-name vrouter-redis \
  --pod-ip 10.0.0.1

# Run agent
vrouter-agent \
  --server grpc.example.com:50051 \
  --agent-id vyos-tokyo-1 \
  --init-config /config/vrouter-agent/init.yaml \
  --disconnect-policy keep
```

## Agent Init Config

The agent supports a two-phase init config to protect management connectivity. Format (YAML):

```yaml
# Phase 1 — before pushed config (base layer)
config: |
  interfaces {
    ethernet eth0 {
      address dhcp
    }
  }
commands: |
  set system name-server 8.8.8.8

# Phase 2 — after pushed config commit (protection layer)
after_config: |
  interfaces {
    ethernet eth0 {
      address dhcp
    }
  }
after_commands: |
  set protocols static route 0.0.0.0/0 next-hop 192.168.1.1
  set firewall name MGMT rule 10 action accept
  set firewall name MGMT rule 10 destination port 50051
  set firewall name MGMT rule 10 protocol tcp
```

All four fields are optional. Any combination works.

### Apply flow (server push)

```
configure
load [before config > pushed config > default]
before commands
pushed commands
commit                             ← first commit

merge /tmp/vrouter-after.config    ← after config overlay (only if set)
after commands                     ← after commands (only if set)
commit                             ← second commit (only if after fields set)
save
```

The after-phase ensures init config's protected settings are always the final authority, regardless of what the operator pushes. Both commits run in the same local vbash process — no network dependency between them.

### Disconnect policy

| Policy | Behavior | Use case |
|--------|----------|----------|
| `keep` (default) | Maintain current config when server unreachable | Server is down, router config is fine |
| `rollback` | Apply init config after `--init-max-retries` failures | Bad config push may have broken connectivity |

The server can override the agent's policy per-push via `disconnect_policy` field in `apply_config`.

### Reconnect backoff

Exponential backoff with ±25% jitter: 1s → 2s → 4s → 8s → 16s → 30s (cap).

## Configuration

All flags have corresponding environment variables.

### vrouter-server

| Flag | Env | Default | Description |
|------|-----|---------|-------------|
| `--agent-listen` | `AGENT_LISTEN_ADDR` | `:50051` | AgentService listen address |
| `--control-listen` | `CONTROL_LISTEN_ADDR` | `:50052` | ControlService listen address |
| `--redis-addr` | `REDIS_ADDR` | `localhost:6379` | Redis address (standalone) |
| `--redis-sentinel-addrs` | `REDIS_SENTINEL_ADDRS` | | Comma-separated Sentinel addresses (HA) |
| `--redis-master-name` | `REDIS_MASTER_NAME` | `vrouter-redis` | Sentinel master name |
| `--pod-ip` | `POD_IP` | | Pod IP (required, from Downward API) |

### vrouter-agent

| Flag | Env | Default | Description |
|------|-----|---------|-------------|
| `--server` | `SERVER_ADDR` | `localhost:50051` | Server address |
| `--agent-id` | `AGENT_ID` | | Agent ID (required) |
| `--init-config` | `INIT_CONFIG` | | Path to init config YAML |
| `--init-max-retries` | | `3` | Failures before disconnect policy kicks in |
| `--disconnect-policy` | `DISCONNECT_POLICY` | `keep` | `keep` or `rollback` |

## Kubernetes Deployment

```bash
kubectl apply -f deploy/kubernetes/
```

Creates in `vrouter-system` namespace:
- **Redis StatefulSet** (3 replicas) + headless service — master-replica replication
- **Redis Sentinel** (3 replicas) — automatic failover, quorum=2
- **vrouter-daemon** deployment (2 replicas, scalable) with Sentinel connection
- **vrouter-daemon** service (ClusterIP `:50052`) — for operator
- **vrouter-daemon-agents** service (LoadBalancer `:50051`) — for external agents

## CI/CD

GitHub Actions pipeline (`.github/workflows/ci.yaml`):
- **lint**: `golangci-lint` (errcheck + default linters)
- **test**: `go test -race` with Redis service container, uploads coverage artifact
- **build**: cross-compile `linux/amd64` + `linux/arm64`, upload artifacts
- **push-image**: multi-arch container to `ghcr.io/tjjh89017/vrouter-server` (on main/tags)

## Development

```bash
# Proto code generation
make proto

# Build
make build

# Tests (E2E tests require Redis on localhost:6379)
go test ./...

# Lint (fmt + vet + golangci-lint)
make lint
```

## License

Apache License 2.0
