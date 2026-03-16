# Backflow

Backflow is a Go service that accepts coding or review tasks over HTTP, runs agent CLIs inside ephemeral Docker containers, and returns a branch with commits and optionally a pull request. It supports local Docker execution for development and EC2 spot instances for fleet mode.

## What It Does

Backflow coordinates four pieces:

1. A REST API for task submission and monitoring.
2. A SQLite store for task and instance state.
3. A polling orchestrator that dispatches work and reconciles failures.
4. An agent container that clones a repo, runs Claude Code or Codex, commits changes, pushes a branch, and can open a PR.

The main task modes are:

- `code`: clone a repo, make changes, commit, push, and optionally create a PR.
- `review`: inspect an existing PR and post review feedback.

## Prerequisites

- Go 1.24+
- Docker
- AWS CLI configured with credentials for EC2 mode
- `jq` for the helper scripts in `scripts/`
- `sqlite3` CLI if you want to use `make db-status`

## Quick Start

### 1. Configure the service

```bash
cp .env.example .env
```

Edit `.env` and set the values you need:

- `GITHUB_TOKEN` for cloning private repos and creating PRs
- `ANTHROPIC_API_KEY` if `BACKFLOW_AUTH_MODE=api_key`
- `OPENAI_API_KEY` if you want to run the `codex` harness
- `CLAUDE_CREDENTIALS_PATH` if `BACKFLOW_AUTH_MODE=max_subscription`

For local development, also set:

```bash
BACKFLOW_MODE=local
```

### 2. Build the agent image

Backflow launches a `backflow-agent` Docker image when it executes tasks. Build that image before submitting work in local mode:

```bash
make docker-build-local
```

For EC2 mode, use `make docker-deploy` after AWS setup instead.

### 3. Start the server

```bash
make run
```

The API listens on `http://localhost:8080` by default. The HTTP server and orchestrator poll loop start together in the same process.

### 4. Confirm the service is up

```bash
curl -s http://localhost:8080/api/v1/health | jq .
```

Responses are wrapped in a top-level `data` field:

```json
{
  "data": {
    "status": "ok",
    "auth_mode": "api_key"
  }
}
```

## Common Commands

```bash
make build              # Build bin/backflow
make run                # Build and run the server
make test               # Run go test ./... with -count=1
make lint               # Run go vet
make clean              # Remove bin/
make db-status          # Dump task and instance state from SQLite
make docker-build-local # Build the local backflow-agent image
make docker-deploy      # Build and push the multi-arch image to ECR
make setup-aws          # Create AWS infrastructure for EC2 mode
```

Single package test:

```bash
go test ./internal/store/ -run TestCreateTask -v
```

## Running Tasks

### Code tasks with the helper script

The simplest path is `scripts/create-task.sh`:

```bash
./scripts/create-task.sh https://github.com/org/repo "Fix the login bug"
```

Useful variants:

```bash
./scripts/create-task.sh https://github.com/org/repo "Add unit tests" \
  --branch backflow/add-tests \
  --target-branch main \
  --pr-title "Add unit tests for auth flows"

./scripts/create-task.sh https://github.com/org/repo --plan prompts.md \
  --harness claude_code \
  --budget 15 \
  --runtime 30 \
  --turns 200 \
  --context "Focus on the auth module"
```

Note: `scripts/create-task.sh` currently defaults to `codex` unless you pass `--harness`. The service-level default is controlled separately by `BACKFLOW_DEFAULT_HARNESS`, which defaults to `claude_code`.

### Code tasks with the API directly

```bash
curl -s -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "repo_url": "https://github.com/org/repo",
    "prompt": "Fix the bug in the login flow",
    "create_pr": true
  }' | jq '.data'
```

Codex example:

```bash
curl -s -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "repo_url": "https://github.com/org/repo",
    "prompt": "Refactor the auth middleware",
    "harness": "codex",
    "create_pr": true
  }' | jq '.data'
```

### PR review tasks

Use `scripts/review-pr.sh` for review mode:

```bash
./scripts/review-pr.sh https://github.com/org/repo 42 \
  --prompt "Focus on correctness and missing tests"
```

Or submit review mode directly:

```bash
curl -s -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "task_mode": "review",
    "repo_url": "https://github.com/org/repo",
    "review_pr_number": 42,
    "prompt": "Focus on security issues"
  }' | jq '.data'
```

## Monitoring And Operations

Inspect the current state:

```bash
make db-status
curl -s http://localhost:8080/api/v1/tasks | jq '.data'
curl -s http://localhost:8080/api/v1/tasks/{id} | jq '.data'
curl -s http://localhost:8080/api/v1/tasks/{id}/logs?tail=100
```

Cancel a task:

```bash
curl -X DELETE http://localhost:8080/api/v1/tasks/{id}
```

Task lifecycle:

```text
pending -> provisioning -> running -> completed
                                 -> failed
                                 -> interrupted
                                 -> cancelled
running/provisioning -> recovering -> running
                                    -> completed
                                    -> failed
                                    -> pending
```

Instance lifecycle:

```text
pending -> running -> draining -> terminated
                  -> terminated
```

Spot interruptions are detected and affected tasks are re-queued automatically.

## AWS Mode

Set up the one-time AWS infrastructure:

```bash
make setup-aws
```

That workflow creates the ECR repository, IAM role, security group, launch template, and an S3 bucket for optional agent output storage. Copy the emitted `BACKFLOW_LAUNCH_TEMPLATE_ID` into `.env`.

Build and push the agent image:

```bash
make docker-deploy
```

If Docker requires `sudo` in your environment:

```bash
make docker-deploy DOCKER="sudo docker"
```

For instance access and troubleshooting:

```bash
aws ssm start-session --target i-0abc...
```

## Configuration

All configuration comes from environment variables. The most commonly used settings are below.

### Core

| Variable | Default | Description |
|----------|---------|-------------|
| `BACKFLOW_MODE` | `ec2` | Execution mode: `ec2` or `local` |
| `BACKFLOW_AUTH_MODE` | `api_key` | Auth mode: `api_key` or `max_subscription` |
| `ANTHROPIC_API_KEY` | | Required when `BACKFLOW_AUTH_MODE=api_key` |
| `OPENAI_API_KEY` | | Required for the `codex` harness |
| `CLAUDE_CREDENTIALS_PATH` | | Path to `~/.claude/` for `max_subscription` mode |
| `GITHUB_TOKEN` | | Used for cloning private repos and creating PRs |
| `BACKFLOW_LISTEN_ADDR` | `:8080` | HTTP listen address |
| `BACKFLOW_DB_PATH` | `backflow.db` | SQLite database path |
| `BACKFLOW_POLL_INTERVAL_SEC` | `5` | Orchestrator poll interval |

### AWS And Capacity

| Variable | Default | Description |
|----------|---------|-------------|
| `AWS_REGION` | `us-east-1` | AWS region |
| `BACKFLOW_INSTANCE_TYPE` | `m7g.xlarge` | EC2 instance type |
| `BACKFLOW_LAUNCH_TEMPLATE_ID` | | Launch template created by `make setup-aws` |
| `BACKFLOW_MAX_INSTANCES` | `5` | Maximum EC2 instances |
| `BACKFLOW_CONTAINERS_PER_INSTANCE` | `1` | Containers scheduled per instance |
| `BACKFLOW_CONTAINER_CPUS` | `2` | CPU cores allocated per container |
| `BACKFLOW_CONTAINER_MEMORY_GB` | `8` | Memory allocated per container |

### Task Defaults

| Variable | Default | Description |
|----------|---------|-------------|
| `BACKFLOW_DEFAULT_HARNESS` | `claude_code` | Default task harness |
| `BACKFLOW_DEFAULT_MODEL` | `claude-sonnet-4-6` | Default model for Claude Code tasks |
| `BACKFLOW_DEFAULT_CODEX_MODEL` | `gpt-5.4` | Default model for Codex tasks |
| `BACKFLOW_DEFAULT_EFFORT` | `high` | Default effort level |
| `BACKFLOW_DEFAULT_MAX_BUDGET` | `10` | Default budget in USD |
| `BACKFLOW_DEFAULT_MAX_RUNTIME_MIN` | `30` | Default runtime limit in minutes |
| `BACKFLOW_DEFAULT_MAX_TURNS` | `200` | Default turn limit |

### Webhooks And Output Storage

| Variable | Default | Description |
|----------|---------|-------------|
| `BACKFLOW_WEBHOOK_URL` | | Optional webhook endpoint |
| `BACKFLOW_WEBHOOK_EVENTS` | all | Comma-separated event filter |
| `BACKFLOW_S3_BUCKET` | | Optional bucket for saving agent output |

## Webhooks

When `BACKFLOW_WEBHOOK_URL` is set, Backflow sends JSON events such as:

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
- `task.recovering`

Filter delivery with:

```bash
BACKFLOW_WEBHOOK_EVENTS=task.completed,task.failed
```

## Database

Backflow uses SQLite in WAL mode. The schema is managed inside `internal/store/sqlite.go` and is applied automatically on startup.

Useful commands:

```bash
make db-status
sqlite3 backflow.db ".schema"
sqlite3 backflow.db "SELECT id, status, created_at FROM tasks ORDER BY created_at DESC LIMIT 10;"
```

To reset the local database, delete `backflow.db` and restart the server.

## API Reference

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/v1/tasks` | Create a code or review task |
| `GET` | `/api/v1/tasks` | List tasks with optional `status`, `limit`, and `offset` |
| `GET` | `/api/v1/tasks/{id}` | Fetch task details |
| `DELETE` | `/api/v1/tasks/{id}` | Cancel or delete a task depending on state |
| `GET` | `/api/v1/tasks/{id}/logs` | Fetch container logs |
| `GET` | `/api/v1/health` | Health check |

## Additional Docs

- `docs/schema.md`: SQLite schema reference
- `docs/file-reference.md`: codebase file map
- `docs/sizing.md`: EC2 sizing guidance
