# Backflow

Backflow is a background orchestrator for coding agents. It accepts a repository and prompt, runs an agent in an ephemeral Docker container, and returns a pushed branch with commits and, optionally, a pull request.

It supports:

- Local execution on your Docker host for development
- AWS EC2 Spot instances for elastic execution
- Multiple agent harnesses, currently `claude_code` and `codex`
- SQLite-backed task tracking
- Webhooks for task lifecycle events

## How It Works

1. A task is submitted through the HTTP API or helper scripts.
2. Backflow stores the task in SQLite.
3. The orchestrator starts a local container or provisions an EC2 instance.
4. The selected agent works in an isolated container, commits changes, and pushes a branch.
5. Backflow updates task state and can create a pull request if requested.

## Prerequisites

- Go 1.24+
- Docker
- AWS CLI configured with credentials for EC2 mode
- `sqlite3` CLI for `make db-status`
- `curl` and `jq` if you want to use the helper scripts

## Quick Start

### 1. Configure the environment

```bash
cp .env.example .env
```

At minimum, set:

- `ANTHROPIC_API_KEY` for the default `api_key` auth mode
- `GITHUB_TOKEN` for cloning private repositories and creating pull requests

Also set `OPENAI_API_KEY` if you plan to run tasks with the `codex` harness.

For local development, add this to `.env`:

```bash
BACKFLOW_MODE=local
```

Local mode skips EC2 provisioning and runs agent containers on the local Docker daemon.

### 2. Build and run the server

```bash
make build
make run
```

The API listens on `http://localhost:8080` by default.

### 3. Verify the server

```bash
curl http://localhost:8080/api/v1/health
```

### 4. Submit a task

```bash
./scripts/create-task.sh https://github.com/org/repo "Fix the login bug"
```

## Local Development

### Common commands

```bash
make build               # Compile bin/backflow
make run                 # Build and run the server
make test                # Run all tests without cache
make lint                # Run go vet
make docker-build-local  # Build the agent image locally
make db-status           # Inspect tasks and instances in SQLite
```

### Targeted tests

```bash
go test ./internal/store/ -run TestCreateTask -v
go test ./internal/api/ -v -count=1
```

Tests use temporary SQLite databases and do not require external services.

## Submitting Tasks

You can submit work through either the helper script or the API directly.

### Helper script

```bash
./scripts/create-task.sh https://github.com/org/repo "Fix the login bug"
```

Important defaults for `scripts/create-task.sh`:

- It defaults to the `codex` harness unless you pass `--harness`.
- It creates a pull request by default; use `--no-pr` to skip PR creation.

Examples:

```bash
# Create a PR with the default script settings
./scripts/create-task.sh https://github.com/org/repo "Add unit tests"

# Force Claude Code instead of Codex
./scripts/create-task.sh https://github.com/org/repo "Refactor auth flow" \
  --harness claude_code

# Use a prompt file
./scripts/create-task.sh https://github.com/org/repo --plan prompts/fix.md

# Full example
./scripts/create-task.sh https://github.com/org/repo "Add unit tests" \
  --branch my-feature \
  --target-branch develop \
  --budget 15 \
  --runtime 30 \
  --turns 200 \
  --context "Focus on the auth module" \
  --claude-md "Always use table-driven tests" \
  --env "GOPRIVATE=github.com/org/*"
```

### API

`POST /api/v1/tasks` accepts JSON task requests.

Example using the API default harness:

```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "repo_url": "https://github.com/org/repo",
    "prompt": "Fix the bug",
    "create_pr": true
  }'
```

Example forcing Codex:

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

Backflow's API default harness is controlled by `BACKFLOW_DEFAULT_HARNESS`, which defaults to `claude_code`.

## Monitoring and Operations

```bash
# Health check
curl http://localhost:8080/api/v1/health

# List tasks
curl http://localhost:8080/api/v1/tasks

# Get one task
curl http://localhost:8080/api/v1/tasks/{id}

# Tail logs
curl http://localhost:8080/api/v1/tasks/{id}/logs?tail=100

# Cancel a task
curl -X DELETE http://localhost:8080/api/v1/tasks/{id}
```

### Task lifecycle

`pending` -> `provisioning` -> `running` -> `recovering` -> `completed` | `failed` | `interrupted` | `cancelled`

### Instance lifecycle

Instances move from `pending` to `running`, then to `draining` and `terminated` when capacity is no longer needed. Spot interruptions re-queue affected tasks.

## API Reference

| Method | Path | Description |
| --- | --- | --- |
| `POST` | `/api/v1/tasks` | Create a task |
| `GET` | `/api/v1/tasks` | List tasks with `status`, `limit`, and `offset` filters |
| `GET` | `/api/v1/tasks/{id}` | Fetch a task |
| `DELETE` | `/api/v1/tasks/{id}` | Cancel or delete a task |
| `GET` | `/api/v1/tasks/{id}/logs` | Fetch container logs, optionally with `tail` |
| `GET` | `/api/v1/health` | Health check |

## Harnesses and Auth Modes

### Harnesses

- `claude_code`: Uses the Claude Code CLI
- `codex`: Uses the Codex CLI and requires `OPENAI_API_KEY`

### Auth modes

- `api_key`: Default mode, designed for concurrent agents, requires `ANTHROPIC_API_KEY`
- `max_subscription`: Uses Claude Max credentials from `CLAUDE_CREDENTIALS_PATH` and runs only one agent at a time

## AWS Setup

Use EC2 mode when you want ephemeral remote workers instead of local Docker execution.

### One-time infrastructure setup

```bash
make setup-aws
```

This creates the supporting AWS resources, including the launch template. Copy the resulting `BACKFLOW_LAUNCH_TEMPLATE_ID` into `.env`.

### Build and push the agent image

```bash
make docker-deploy
```

If Docker requires elevated privileges in your environment:

```bash
make docker-deploy DOCKER="sudo docker"
```

This builds a multi-architecture image and pushes it to ECR.

### Access a running instance

```bash
aws ssm start-session --target i-0abc...
```

## Database

Backflow uses SQLite in WAL mode. The database path is configured with `BACKFLOW_DB_PATH`, which defaults to `backflow.db`.

### Schema

The main tables are:

- `tasks`
- `instances`

The schema is defined in `internal/store/sqlite.go`.

### Migrations

There are no standalone migration files. Schema changes are managed in the `migrate()` method in `internal/store/sqlite.go`.

When adding a column:

1. Add an idempotent `ALTER TABLE` path in `migrate()`.
2. Update the relevant `INSERT`, `UPDATE`, and `SELECT` queries.
3. Update the corresponding model in `internal/models/`.

### Inspecting the database

```bash
make db-status
sqlite3 backflow.db ".schema"
sqlite3 backflow.db "SELECT id, status, created_at FROM tasks ORDER BY created_at DESC LIMIT 10;"
```

To reset the database, delete `backflow.db`. Backflow recreates it on next startup.

## Webhooks

Set `BACKFLOW_WEBHOOK_URL` to receive lifecycle events.

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

Filter delivered events with:

```bash
BACKFLOW_WEBHOOK_EVENTS=task.completed,task.failed
```

## Configuration

All configuration is supplied through environment variables.

| Variable | Default | Description |
| --- | --- | --- |
| `BACKFLOW_MODE` | `ec2` | Execution mode: `ec2` or `local` |
| `BACKFLOW_AUTH_MODE` | `api_key` | Auth mode: `api_key` or `max_subscription` |
| `ANTHROPIC_API_KEY` | | Required when `BACKFLOW_AUTH_MODE=api_key` |
| `OPENAI_API_KEY` | | Required for the `codex` harness |
| `CLAUDE_CREDENTIALS_PATH` | | Path to `~/.claude/` for `max_subscription` |
| `GITHUB_TOKEN` | | Used for cloning private repositories and creating PRs |
| `BACKFLOW_LISTEN_ADDR` | `:8080` | HTTP listen address |
| `BACKFLOW_DB_PATH` | `backflow.db` | SQLite database path |
| `AWS_REGION` | `us-east-1` | AWS region |
| `BACKFLOW_INSTANCE_TYPE` | `m7g.xlarge` | EC2 instance type |
| `BACKFLOW_AMI` | | Optional AMI override |
| `BACKFLOW_LAUNCH_TEMPLATE_ID` | | Launch template from `make setup-aws` |
| `BACKFLOW_MAX_INSTANCES` | `5` | Maximum EC2 instances |
| `BACKFLOW_CONTAINERS_PER_INSTANCE` | `1` | Containers per EC2 instance or local host |
| `BACKFLOW_CONTAINER_CPUS` | `2` | CPU cores allocated per container |
| `BACKFLOW_CONTAINER_MEMORY_GB` | `8` | Memory allocated per container |
| `BACKFLOW_DEFAULT_HARNESS` | `claude_code` | API default harness |
| `BACKFLOW_DEFAULT_MODEL` | `claude-sonnet-4-6` | Default Claude model |
| `BACKFLOW_DEFAULT_CODEX_MODEL` | `gpt-5.4` | Default Codex model |
| `BACKFLOW_DEFAULT_EFFORT` | `high` | Default reasoning effort |
| `BACKFLOW_DEFAULT_MAX_BUDGET` | `10` | Default budget in USD |
| `BACKFLOW_DEFAULT_MAX_RUNTIME_MIN` | `30` | Default task runtime in minutes |
| `BACKFLOW_DEFAULT_MAX_TURNS` | `200` | Default turn limit |
| `BACKFLOW_S3_BUCKET` | | Optional S3 bucket for agent output |
| `BACKFLOW_WEBHOOK_URL` | | Webhook endpoint |
| `BACKFLOW_WEBHOOK_EVENTS` | all | Comma-separated event allowlist |
| `BACKFLOW_POLL_INTERVAL_SEC` | `5` | Orchestrator poll interval in seconds |
| `DOCKER` | `docker` | Docker command override for Make targets |
