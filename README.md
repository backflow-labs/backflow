# Backflow

Backflow is a background agent orchestrator for repository tasks. You submit a repo URL and prompt, Backflow runs a coding agent in an ephemeral Docker container, and you get back a branch with commits and, optionally, a pull request.

It supports:

- Local execution on the current Docker host for development
- AWS EC2 spot instances for remote execution
- Multiple harnesses, including Claude Code and Codex
- Webhook notifications and optional agent-output storage

## How It Works

1. A client submits a task over HTTP or via `scripts/create-task.sh`.
2. Backflow provisions an execution environment in `local` or `ec2` mode.
3. The selected harness runs against the target repository and prompt.
4. Backflow tracks task state, stores logs and metadata in SQLite, and can open a PR on completion.

## Requirements

- Go 1.24+
- Docker
- `jq` for `scripts/create-task.sh`
- `sqlite3` CLI for `make db-status`
- AWS CLI with configured credentials if you want to use EC2 mode

## Quick Start

For the shortest path, run Backflow in local mode.

### 1. Configure environment variables

```bash
cp .env.example .env
```

Edit `.env` and set:

- `BACKFLOW_MODE=local`
- `ANTHROPIC_API_KEY` if you want to run Claude Code tasks
- `OPENAI_API_KEY` if you want to run Codex tasks
- `GITHUB_TOKEN` if tasks need private repo access or PR creation

### 2. Build and run

```bash
make build
make run
```

The API listens on `http://localhost:8080`.

### 3. Submit a task

```bash
./scripts/create-task.sh https://github.com/org/repo "Fix the login bug" \
  --harness codex
```

## Common Commands

```bash
make build               # Build bin/backflow
make run                 # Build and run with .env loaded
make test                # Run all tests without cache
make lint                # Run go vet
make docker-build-local  # Build the agent image locally
make db-status           # Inspect tasks and instances in SQLite
```

## Submitting Tasks

The helper script is the easiest way to create tasks. Its default harness is `codex`; pass `--harness claude_code` to run Claude Code instead. Pull request creation is enabled by default; use `--no-pr` to skip it.

```bash
# Simple task
./scripts/create-task.sh https://github.com/org/repo "Fix the login bug"

# Claude Code task
./scripts/create-task.sh https://github.com/org/repo "Add tests for auth flows" \
  --harness claude_code \
  --model claude-sonnet-4-6

# Task from a plan file
./scripts/create-task.sh https://github.com/org/repo --plan plan.md \
  --branch feature/refactor-auth \
  --target-branch develop \
  --budget 15 \
  --effort high
```

You can also call the API directly. If the request omits `harness`, the server default is `claude_code` unless overridden by `BACKFLOW_DEFAULT_HARNESS`.

```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "repo_url": "https://github.com/org/repo",
    "prompt": "Fix the bug",
    "create_pr": true
  }'
```

Codex example:

```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "repo_url": "https://github.com/org/repo",
    "prompt": "Fix the bug",
    "harness": "codex",
    "create_pr": true
  }'
```

## Deployment Modes

### Local mode

Set `BACKFLOW_MODE=local` to skip EC2 provisioning and run containers on the local Docker daemon. This is the recommended mode for development.

### EC2 mode

EC2 mode runs agent containers on AWS spot instances.

One-time setup:

```bash
make setup-aws
```

That creates the supporting AWS resources and prints a launch template ID. Put that value in `.env` as `BACKFLOW_LAUNCH_TEMPLATE_ID`.

To build and push the agent image to ECR:

```bash
make docker-deploy
```

If Docker requires `sudo` on your machine:

```bash
make docker-deploy DOCKER="sudo docker"
```

## Operations

### Task lifecycle

`pending` -> `provisioning` -> `running` -> `completed` | `failed` | `interrupted` | `cancelled`

### Instance lifecycle

`pending` -> `running` -> idle for 5 minutes -> `terminated`

Spot interruptions automatically re-queue affected tasks.

### Useful API calls

```bash
# Health
curl http://localhost:8080/api/v1/health

# Task details
curl http://localhost:8080/api/v1/tasks/{id}

# Task logs
curl http://localhost:8080/api/v1/tasks/{id}/logs?tail=100

# Cancel a task
curl -X DELETE http://localhost:8080/api/v1/tasks/{id}
```

### API reference

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/v1/tasks` | Create a task |
| `GET` | `/api/v1/tasks` | List tasks with `status`, `limit`, and `offset` filters |
| `GET` | `/api/v1/tasks/{id}` | Get task details |
| `DELETE` | `/api/v1/tasks/{id}` | Cancel a task |
| `GET` | `/api/v1/tasks/{id}/logs` | Read container logs |
| `GET` | `/api/v1/health` | Health check |

## Harnesses and Auth

Supported harnesses:

- `claude_code`: Claude Code CLI
- `codex`: OpenAI Codex CLI

Auth modes:

- `api_key`: uses API keys and supports concurrent agents
- `max_subscription`: uses Claude Max credentials and runs one agent at a time

## Database

Backflow uses SQLite in WAL mode. The database path is controlled by `BACKFLOW_DB_PATH` and defaults to `backflow.db`.

- Schema and migration logic live in `internal/store/sqlite.go`
- Main tables are `tasks` and `instances`
- The schema is applied on startup with idempotent `CREATE TABLE IF NOT EXISTS` and `CREATE INDEX IF NOT EXISTS` statements

Useful commands:

```bash
make db-status
sqlite3 backflow.db ".schema"
sqlite3 backflow.db "SELECT id, status, created_at FROM tasks ORDER BY created_at DESC LIMIT 10;"
```

To reset local state, delete `backflow.db` and restart the server.

## Webhooks

Set `BACKFLOW_WEBHOOK_URL` to receive task lifecycle events. Use `BACKFLOW_WEBHOOK_EVENTS` to limit which events are delivered.

Example payload:

```json
{
  "event": "task.completed",
  "task_id": "bf_01KK...",
  "repo_url": "https://github.com/org/repo",
  "prompt": "Fix the bug",
  "message": "",
  "agent_log_tail": "last 20 lines...",
  "timestamp": "2026-03-13T22:00:00Z"
}
```

Supported events:

- `task.created`
- `task.running`
- `task.completed`
- `task.failed`
- `task.needs_input`
- `task.interrupted`

## Configuration

Backflow is configured entirely through environment variables. Start with `.env.example`; the most important variables are listed below.

| Variable | Default | Notes |
|----------|---------|-------|
| `BACKFLOW_MODE` | `ec2` | Use `local` for local Docker execution |
| `BACKFLOW_AUTH_MODE` | `api_key` | `api_key` or `max_subscription` |
| `ANTHROPIC_API_KEY` | | Required for Claude Code in `api_key` mode |
| `OPENAI_API_KEY` | | Required for Codex tasks |
| `CLAUDE_CREDENTIALS_PATH` | | Required for `max_subscription` mode |
| `GITHUB_TOKEN` | | Needed for private repo access and PR creation |
| `BACKFLOW_LISTEN_ADDR` | `:8080` | HTTP listen address |
| `BACKFLOW_DB_PATH` | `backflow.db` | SQLite database path |
| `AWS_REGION` | `us-east-1` | AWS region for EC2 mode |
| `BACKFLOW_INSTANCE_TYPE` | `m7g.xlarge` | EC2 instance type |
| `BACKFLOW_LAUNCH_TEMPLATE_ID` | | Required in EC2 mode after `make setup-aws` |
| `BACKFLOW_MAX_INSTANCES` | `5` | Maximum EC2 instances |
| `BACKFLOW_CONTAINERS_PER_INSTANCE` | `1` | Local mode also uses this value |
| `BACKFLOW_CONTAINER_CPUS` | `2` | CPU cores per container |
| `BACKFLOW_CONTAINER_MEMORY_GB` | `8` | Memory per container |
| `BACKFLOW_DEFAULT_HARNESS` | `claude_code` | Server-side default harness |
| `BACKFLOW_DEFAULT_MODEL` | `claude-sonnet-4-6` | Default Claude model |
| `BACKFLOW_DEFAULT_CODEX_MODEL` | `gpt-5.4` | Default Codex model |
| `BACKFLOW_DEFAULT_EFFORT` | `high` | Default reasoning effort |
| `BACKFLOW_DEFAULT_MAX_BUDGET` | `10` | Budget in USD |
| `BACKFLOW_DEFAULT_MAX_RUNTIME_MIN` | `30` | Runtime limit in minutes |
| `BACKFLOW_DEFAULT_MAX_TURNS` | `200` | Maximum agent turns |
| `BACKFLOW_WEBHOOK_URL` | | Webhook endpoint |
| `BACKFLOW_WEBHOOK_EVENTS` | all | Comma-separated event filter |
| `BACKFLOW_S3_BUCKET` | | Optional agent-output storage bucket |
| `BACKFLOW_POLL_INTERVAL_SEC` | `5` | Orchestrator polling interval |
| `DOCKER` | `docker` | Docker command used by the Makefile |

## Additional Docs

- `docs/schema.md`
- `docs/sizing.md`
- `docs/file-reference.md`
