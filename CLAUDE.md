# CLAUDE.md

This file provides guidance to Claude Code when working with code in this repository.

## Project Overview

vrouter-daemon is a standalone service that bridges the vrouter-operator and bare metal VyOS routers via gRPC. It exposes two gRPC services:

- **ControlService** (port 50052): operator-facing unary RPCs (IsConnected, GetStatus, ApplyConfig)
- **AgentService** (port 50051): agent-facing bidirectional streaming

The design reference lives in the sibling repo: `../vrouter-operator/docs/proposals/grpc-implementation-plan.md`

## Commands

```bash
# Proto code generation
make proto         # generates Go stubs from proto/ into gen/go/

# Build
make build         # builds all binaries into bin/
go build ./...     # quick compile check

# Build individual binaries
go build -o bin/vrouter-server ./cmd/vrouter-server/
go build -o bin/vrouter-agent  ./cmd/vrouter-agent/
go build -o bin/vrouter-daemon ./cmd/vrouter-daemon/

# Tests
go test ./...                          # all tests
go test ./internal/registry/...        # specific package
go test -run TestApplyConfig ./...     # specific test

# Lint / format
go fmt ./...
go vet ./...
```

## Git

Always use `git commit -s` (DCO sign-off) for all commits.

## Architecture

Go module: `github.com/tjjh89017/vrouter-daemon`

### Directory Structure

| Path | Purpose |
|------|---------|
| `proto/control/v1/` | Operator-facing proto (ControlService) — source of truth |
| `proto/agent/v1/` | Agent-facing proto (AgentService) |
| `gen/go/controlpb/` | Generated Go stubs for ControlService |
| `gen/go/agentpb/` | Generated Go stubs for AgentService |
| `internal/controlapi/` | ControlService gRPC handler |
| `internal/agentapi/` | AgentService gRPC handler (bidirectional streams) |
| `internal/registry/` | Agent connection registry (agentID → stream, thread-safe) |
| `internal/dispatch/` | Request-response correlation (apply_config → config_ack) |
| `internal/config/` | Server configuration (flags, env vars) |
| `cmd/vrouter-server/` | Server only binary |
| `cmd/vrouter-agent/` | Agent only binary |
| `cmd/vrouter-daemon/` | Mixed mode binary (server + agent) |

### Key Design Principles

- Everything under `internal/` — no external Go consumers
- The operator imports nothing from this repo; `control.proto` is the only shared contract
- Two gRPC services on separate ports (different network policies)
- `controlapi` and `agentapi` communicate only through `registry` and `dispatch`
