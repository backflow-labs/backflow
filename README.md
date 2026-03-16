# Backflow

Backflow is a background orchestrator for coding agents. It runs Claude Code or Codex inside ephemeral Docker containers, either locally or on AWS EC2 spot instances. You submit a repository plus instructions, and Backflow drives the task to a pushed branch and, if requested, an open pull request.

## What Backflow Does

- Runs code-generation tasks against a Git repository.
- Runs review tasks against an existing pull request.
- Stores task and instance state in SQLite.
- Supports local Docker execution for development and EC2 spot execution for scale.
- Can send task webhooks and optionally upload agent output to S3.

## Prerequisites

- Go 1.24+
- Docker
- `jq` (used by the helper scripts)
- `sqlite3` CLI (used by `make db-status`)
- AWS CLI with credentials configured if you plan to use EC2 mode

## Quick Start

### Run Backflow Locally

1. Copy the example environment file:

```bash
cp .env.example .env
```

2. Edit `.env`:

- Set `GITHUB_TOKEN`.
- Set `ANTHROPIC_API_KEY` if you want to use `claude_code`.
- Set `OPENAI_API_KEY` if you want to use `codex`.
- Set `BACKFLOW_MODE=local` to skip EC2 provisioning and use your local Docker daemon.

3. Start the service:

```bash
make build
make run
```

Backflow listens on `http://localhost:8080`.

### Submit a Task

The helper script creates PRs by default. Use `--no-pr` only if you want to skip PR creation.

```bash
# Claude Code task
./scripts/create-task.sh https://github.com/org/repo "Fix the login bug" \
  --harness claude_code

# Codex task
./scripts/create-task.sh https://github.com/org/repo "Add unit tests" \
  --harness codex \
  --budget 15 \
  --branch add-tests \
  --target-branch develop \
  --pr-title "Add unit tests for auth flows"

# Prompt from a file, with self-review
./scripts/create-task.sh https://github.com/org/repo \
  --plan prompts.md \
  --self-review
```

If you care about harness selection, pass `--harness` explicitly. The API/server default harness is `claude_code`; the helper script currently defaults to `codex`.

### Review an Existing Pull Request

```bash
./scripts/review-pr.sh https://github.com/org/repo 42 \
  --prompt "Focus on correctness and regression risks"
```

## Common Commands

```bash
make test                # Run all tests
make lint                # go vet
make docker-build-local  # Build the agent image locally
make db-status           # Show tasks and instances from SQLite
```

Tests create temporary SQLite databases and do not require external services.

## Running on AWS

### One-Time Setup

```bash
make setup-aws
```

This provisions the supporting AWS resources, including an ECR repository, IAM role, security group, and launch template. Copy the emitted `BACKFLOW_LAUNCH_TEMPLATE_ID` into `.env`.

If you are not using a launch template, Backflow can also boot EC2 instances from `BACKFLOW_AMI`, but one of `BACKFLOW_LAUNCH_TEMPLATE_ID` or `BACKFLOW_AMI` must be set in EC2 mode.

### Build and Push the Agent Image

```bash
make docker-deploy
# If Docker requires sudo:
make docker-deploy DOCKER="sudo docker"
```

This builds a multi-architecture image and pushes it to ECR.

## API Usage

### Create a Task

```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "repo_url": "https://github.com/org/repo",
    "prompt": "Fix the bug",
    "harness": "claude_code",
    "create_pr": true
  }'
```

### Monitor a Task

```bash
# Health
curl http://localhost:8080/api/v1/health

# Task details
curl http://localhost:8080/api/v1/tasks/{id}

# Container logs
curl http://localhost:8080/api/v1/tasks/{id}/logs?tail=100

# Cancel a task
curl -X DELETE http://localhost:8080/api/v1/tasks/{id}
```

### Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/v1/tasks` | Create a task |
| `GET` | `/api/v1/tasks` | List tasks (`?status=`, `?limit=`, `?offset=`) |
| `GET` | `/api/v1/tasks/{id}` | Get task details |
| `DELETE` | `/api/v1/tasks/{id}` | Cancel a task |
| `GET` | `/api/v1/tasks/{id}/logs` | Fetch task logs (`?tail=100`) |
| `GET` | `/api/v1/health` | Health check |

## Task and Instance Lifecycle

### Task States

`pending` -> `provisioning` -> `running` -> `completed` | `failed` | `interrupted` | `cancelled`

Spot interruptions can also move a task through `recovering` before it is re-queued.

### Instance States

`pending` -> `running` -> idle for 5 minutes -> `terminated`

## Harnesses and Auth Modes

### Harnesses

- `claude_code`: Claude Code CLI
- `codex`: OpenAI Codex CLI

Set `BACKFLOW_DEFAULT_HARNESS` to change the server default, or choose a harness per task with the `harness` field or `--harness`.

### Auth Modes

- `api_key`: Uses Anthropic API credentials and supports multiple concurrent agents
- `max_subscription`: Uses Claude Max subscription credentials and limits execution to one agent at a time

## Database

Backflow uses SQLite in WAL mode. The database path is controlled by `BACKFLOW_DB_PATH` and defaults to `backflow.db`.

Schema management is handled directly in `internal/store/sqlite.go` via idempotent DDL in `migrate()`. There are no standalone migration files.

Useful commands:

```bash
make db-status
sqlite3 backflow.db ".schema"
sqlite3 backflow.db "SELECT id, status, created_at FROM tasks ORDER BY created_at DESC LIMIT 10;"
```

To reset the local database, delete `backflow.db` and restart the service.

## Webhooks

Set `BACKFLOW_WEBHOOK_URL` to receive task events. You can optionally filter which events are sent with `BACKFLOW_WEBHOOK_EVENTS`.

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

Supported events: `task.created`, `task.running`, `task.completed`, `task.failed`, `task.needs_input`, `task.interrupted`

## Configuration

Backflow is configured through environment variables, usually via `.env`.

| Variable | Default | Description |
|----------|---------|-------------|
| `BACKFLOW_MODE` | `ec2` | Execution mode: `ec2` or `local` |
| `BACKFLOW_AUTH_MODE` | `api_key` | Auth mode: `api_key` or `max_subscription` |
| `ANTHROPIC_API_KEY` | | Required when using `api_key` mode |
| `OPENAI_API_KEY` | | Required for the `codex` harness |
| `CLAUDE_CREDENTIALS_PATH` | | Path to `~/.claude/` for `max_subscription` mode |
| `GITHUB_TOKEN` | | Used for cloning private repositories and creating PRs |
| `BACKFLOW_LISTEN_ADDR` | `:8080` | HTTP listen address |
| `BACKFLOW_DB_PATH` | `backflow.db` | SQLite database path |
| `AWS_REGION` | `us-east-1` | AWS region |
| `BACKFLOW_INSTANCE_TYPE` | `m7g.xlarge` | EC2 instance type |
| `BACKFLOW_LAUNCH_TEMPLATE_ID` | | Launch template ID for EC2 mode |
| `BACKFLOW_AMI` | | Optional AMI override for EC2 mode |
| `BACKFLOW_MAX_INSTANCES` | `5` | Maximum EC2 instances |
| `BACKFLOW_CONTAINERS_PER_INSTANCE` | `1` | Containers per instance |
| `BACKFLOW_CONTAINER_CPUS` | `2` | CPU cores per container |
| `BACKFLOW_CONTAINER_MEMORY_GB` | `8` | Memory in GB per container |
| `BACKFLOW_S3_BUCKET` | | Optional S3 bucket for saved agent output |
| `BACKFLOW_DEFAULT_HARNESS` | `claude_code` | Default server-side harness |
| `BACKFLOW_DEFAULT_MODEL` | `claude-sonnet-4-6` | Default model for `claude_code` |
| `BACKFLOW_DEFAULT_CODEX_MODEL` | `gpt-5.4` | Default model for `codex` |
| `BACKFLOW_DEFAULT_EFFORT` | `high` | Default reasoning effort |
| `BACKFLOW_DEFAULT_MAX_BUDGET` | `10` | Default budget in USD |
| `BACKFLOW_DEFAULT_MAX_RUNTIME_MIN` | `30` | Default max runtime in minutes |
| `BACKFLOW_DEFAULT_MAX_TURNS` | `200` | Default max conversation turns |
| `BACKFLOW_WEBHOOK_URL` | | Webhook endpoint |
| `BACKFLOW_WEBHOOK_EVENTS` | all | Comma-separated webhook event filter |
| `BACKFLOW_POLL_INTERVAL_SEC` | `5` | Orchestrator poll interval in seconds |
| `DOCKER` | `docker` | Docker command override, for example `sudo docker` |
