# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Is

Backflow is a background agent orchestrator written in Go. It runs Claude Code in ephemeral Docker containers on AWS EC2 spot instances. Tasks are submitted via REST API, and the orchestrator handles provisioning, execution, monitoring, and cleanup.

## Commands

```bash
make build          # Build server binary to bin/backflow
make run            # Build + run (sources .env)
make test           # go test ./... -v -count=1
make lint           # go vet ./...
make deps           # go mod tidy
make db-status      # Dump SQLite database state
make docker-deploy  # Build + push agent image to ECR
make setup-aws      # Create AWS infrastructure
```

Run a single test:
```bash
go test ./internal/store/ -run TestCreateTask -v
```

## Architecture

The server runs two concurrent goroutines: a chi-based REST API on `:8080` and a polling orchestrator (default 5s interval).

**Request flow:** Client → REST API → SQLite store → Orchestrator picks up pending tasks → Dispatches Docker containers on EC2 via SSM → Monitors completion → Fires webhooks.

### Key modules (`internal/`)

- **api/** — chi router, REST handlers, JSON envelope responses
- **orchestrator/** — The core loop. Contains sub-components:
  - `orchestrator.go` — Main poll loop, task dispatch, completion detection
  - `scaler.go` — EC2 instance lifecycle (scale up on demand, terminate after 5min idle)
  - `docker.go` — Container operations via AWS SSM (run, inspect, stop, logs)
  - `spot.go` — Spot interruption detection and task re-queuing
  - `ec2.go` — EC2 launch/terminate/describe operations
- **store/** — `Store` interface + SQLite implementation (WAL mode)
- **models/** — `Task` and `Instance` structs with status enums
- **config/** — Environment-variable-based configuration (25+ vars)
- **notify/** — `Notifier` interface with noop and webhook implementations

### Agent container (`docker/`)

The Dockerfile builds a Node.js-based image with Claude Code CLI, git, and GitHub CLI. The `entrypoint.sh` script handles the full agent lifecycle: clone → checkout → run Claude Code → commit → push → create PR.

The orchestrator communicates with containers indirectly by reading a `status.json` file from the stopped container.

### Task statuses

`pending` → `provisioning` → `running` → `completed` | `failed` | `interrupted` | `cancelled`

### Instance statuses

`pending` → `running` → `draining` → `terminated`

## Auth modes

- **`api_key`** — Anthropic API key, supports concurrent agents
- **`max_subscription`** — Claude Max subscription, strictly serial (one agent at a time)

## Design patterns

- Interface-based abstractions (`Store`, `Notifier`) for testability
- Polling over event-driven orchestration for simplicity
- SSM instead of SSH for EC2 communication (no key management)
- ULID-based task IDs with `bf_` prefix
- Zerolog for structured logging
- **Do not store `context.Context` on structs.** Pass contexts through function parameters when called from a context-bearing caller (e.g. HTTP handlers, `Start(ctx)`). For callbacks invoked by external libraries without a context (e.g. discordgo event handlers), use `context.Background()`.
