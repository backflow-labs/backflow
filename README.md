# Backflow

Backflow is a Go service that runs coding agents in ephemeral Docker containers, either locally or on AWS EC2 spot instances. You submit a repo and a task over HTTP, and Backflow manages execution, commits the result to a branch, and can open a pull request.

It currently supports two agent harnesses:

- `claude_code`
- `codex`

## What It Handles

- Task intake over a REST API
- Local Docker execution for development
- EC2 spot instance orchestration for remote runs
- SQLite-backed task and instance state
- Optional pull request creation and self-review
- Optional webhook notifications
- Optional S3 upload of agent output artifacts

## Quickstart

### Prerequisites

- Go 1.24+
- Docker
- AWS CLI configured with credentials if you plan to use EC2 mode
- `sqlite3` CLI if you want to use `make db-status`
- API credentials:
  - `ANTHROPIC_API_KEY` for `claude_code`
  - `OPENAI_API_KEY` for `codex`
  - `GITHUB_TOKEN` for private repo access and PR creation

### 1. Configure the environment

```bash
cp .env.example .env
```

For local-only development, set this in `.env`:

```bash
BACKFLOW_MODE=local
```

In local mode, Backflow skips EC2 provisioning and runs agent containers on your local Docker daemon.

### 2. Build and run the service

```bash
make build
make run
```

The API listens on `http://localhost:8080` by default.

### 3. Submit a code task

```bash
./scripts/create-task.sh \
  https://github.com/org/repo \
  "Fix the login redirect loop" \
  --harness claude_code
```

Or call the API directly:

```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "repo_url": "https://github.com/org/repo",
    "prompt": "Fix the login redirect loop",
    "harness": "codex",
    "create_pr": true
  }'
```

### 4. Monitor a task

```bash
curl http://localhost:8080/api/v1/tasks/{id}
curl http://localhost:8080/api/v1/tasks/{id}/logs?tail=100
curl http://localhost:8080/api/v1/health
```

## Operating Modes

### Local mode

Use local mode when developing the service or testing task execution without AWS.

- Set `BACKFLOW_MODE=local`
- Backflow runs containers on the local Docker daemon
- `BACKFLOW_CONTAINERS_PER_INSTANCE` acts as the local concurrency limit
- Local mode is capped at 6 concurrent containers

### EC2 mode

Use EC2 mode when you want Backflow to manage remote worker capacity on spot instances.

- `BACKFLOW_MODE=ec2` is the default
- Backflow launches spot instances and runs containers over AWS SSM
- Idle instances are terminated automatically
- Spot interruptions re-queue affected tasks

## Common Workflows

### Submit a code-generation task

```bash
./scripts/create-task.sh \
  https://github.com/org/repo \
  "Add request validation to the tasks API" \
  --harness codex \
  --branch backflow/api-validation \
  --target-branch main \
  --budget 15 \
  --runtime 30 \
  --turns 200 \
  --pr-title "Add request validation to tasks API"
```

### Submit a task from a plan file

```bash
./scripts/create-task.sh \
  https://github.com/org/repo \
  --plan prompts.md \
  --harness claude_code \
  --context "Focus on regressions around session refresh"
```

### Review an existing pull request

```bash
./scripts/review-pr.sh \
  https://github.com/org/repo \
  42 \
  --prompt "Focus on correctness, migrations, and test coverage"
```

Review tasks run with `task_mode=review` and comment on an existing PR instead of creating a new branch or PR.

### Enable output storage

Set `BACKFLOW_S3_BUCKET` to upload agent output artifacts. Per task, output saving is enabled by default and can be disabled by sending `save_agent_output: false` or using `./scripts/create-task.sh --no-save-output`.

## Local Development

### Build and test

```bash
make build
make test
make lint
```

Run a single test:

```bash
go test ./internal/store/ -run TestCreateTask -v
```

Run one package:

```bash
go test ./internal/api/ -v -count=1
```

### Build the agent image locally

```bash
make docker-build-local
```

## AWS Setup

### One-time infrastructure setup

```bash
make setup-aws
```

This creates the AWS resources needed for EC2 mode, including the ECR repository, IAM role, security group, and launch template. Copy the resulting `BACKFLOW_LAUNCH_TEMPLATE_ID` into `.env`.

### Build and push the agent image

```bash
make docker-deploy
```

If Docker requires `sudo` on your machine:

```bash
make docker-deploy DOCKER="sudo docker"
```

## Task and Instance Lifecycle

### Task statuses

```text
pending -> provisioning -> running -> completed
                                  -> failed
                                  -> interrupted
                                  -> cancelled

running/provisioning -> recovering -> running
                                  -> pending
                                  -> completed
                                  -> failed
```

### Instance statuses

```text
pending -> running -> draining -> terminated
                  -> terminated
```

## API Reference

| Method | Path | Description |
| --- | --- | --- |
| `GET` | `/api/v1/health` | Health check |
| `POST` | `/api/v1/tasks` | Create a task |
| `GET` | `/api/v1/tasks` | List tasks with `status`, `limit`, and `offset` filters |
| `GET` | `/api/v1/tasks/{id}` | Get task details |
| `DELETE` | `/api/v1/tasks/{id}` | Cancel a running task or delete a queued one |
| `GET` | `/api/v1/tasks/{id}/logs` | Get task logs, optionally with `?tail=100` |

### Example create-task payload

```json
{
  "repo_url": "https://github.com/org/repo",
  "prompt": "Add integration tests for auth flows",
  "task_mode": "code",
  "harness": "claude_code",
  "branch": "backflow/auth-tests",
  "target_branch": "main",
  "model": "claude-sonnet-4-6",
  "effort": "high",
  "max_budget_usd": 10,
  "max_runtime_min": 30,
  "max_turns": 200,
  "create_pr": true,
  "self_review": true,
  "save_agent_output": true,
  "claude_md": "Prefer table-driven tests.",
  "env_vars": {
    "GOPRIVATE": "github.com/org/*"
  }
}
```

## Harnesses and Auth Modes

### Harnesses

- `claude_code`: Default API harness. Uses Anthropic credentials.
- `codex`: Uses OpenAI credentials.

Set `BACKFLOW_DEFAULT_HARNESS` to change the API default, or override the harness per task.

### Auth modes

- `api_key`: Uses API keys and supports concurrent agents.
- `max_subscription`: Uses Claude Max credentials from `CLAUDE_CREDENTIALS_PATH` and limits execution to one agent at a time.

## Webhooks

Set `BACKFLOW_WEBHOOK_URL` to receive task lifecycle events. You can optionally filter events with `BACKFLOW_WEBHOOK_EVENTS`.

Example payload:

```json
{
  "event": "task.completed",
  "task_id": "bf_01KK...",
  "repo_url": "https://github.com/org/repo",
  "prompt": "Fix the bug",
  "message": "",
  "pr_url": "https://github.com/org/repo/pull/123",
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

## Database

Backflow uses SQLite in WAL mode. The database path is controlled by `BACKFLOW_DB_PATH`, which defaults to `backflow.db`.

Useful commands:

```bash
make db-status
sqlite3 backflow.db ".schema"
sqlite3 backflow.db "SELECT id, status, created_at FROM tasks ORDER BY created_at DESC LIMIT 10;"
```

Schema is managed in `internal/store/sqlite.go` inside `migrate()`. There are no separate migration files.

## Configuration

All configuration is supplied through environment variables or a local `.env` file.

### Core settings

| Variable | Default | Description |
| --- | --- | --- |
| `BACKFLOW_MODE` | `ec2` | Execution mode: `ec2` or `local` |
| `BACKFLOW_LISTEN_ADDR` | `:8080` | HTTP listen address |
| `BACKFLOW_DB_PATH` | `backflow.db` | SQLite database path |
| `BACKFLOW_POLL_INTERVAL_SEC` | `5` | Orchestrator polling interval |

### Auth and agent defaults

| Variable | Default | Description |
| --- | --- | --- |
| `BACKFLOW_AUTH_MODE` | `api_key` | `api_key` or `max_subscription` |
| `ANTHROPIC_API_KEY` |  | Required for `api_key` mode |
| `OPENAI_API_KEY` |  | Required for `codex` tasks |
| `CLAUDE_CREDENTIALS_PATH` |  | Path to `~/.claude/` for `max_subscription` mode |
| `GITHUB_TOKEN` |  | Used for cloning private repos and creating PRs |
| `BACKFLOW_DEFAULT_HARNESS` | `claude_code` | Default API harness |
| `BACKFLOW_DEFAULT_MODEL` | `claude-sonnet-4-6` | Default Claude model |
| `BACKFLOW_DEFAULT_CODEX_MODEL` | `gpt-5.4` | Default Codex model |
| `BACKFLOW_DEFAULT_EFFORT` | `high` | Default reasoning effort |
| `BACKFLOW_DEFAULT_MAX_BUDGET` | `10` | Default budget in USD |
| `BACKFLOW_DEFAULT_MAX_RUNTIME_MIN` | `30` | Default max runtime in minutes |
| `BACKFLOW_DEFAULT_MAX_TURNS` | `200` | Default max turns |

### EC2 and container settings

| Variable | Default | Description |
| --- | --- | --- |
| `AWS_REGION` | `us-east-1` | AWS region |
| `BACKFLOW_INSTANCE_TYPE` | `m7g.xlarge` | EC2 instance type |
| `BACKFLOW_AMI` |  | Optional AMI override |
| `BACKFLOW_LAUNCH_TEMPLATE_ID` |  | Launch template for EC2 mode |
| `BACKFLOW_MAX_INSTANCES` | `5` | Maximum EC2 instances |
| `BACKFLOW_CONTAINERS_PER_INSTANCE` | `1` | Containers per instance or local concurrency limit |
| `BACKFLOW_CONTAINER_CPUS` | `2` | CPUs allocated per container |
| `BACKFLOW_CONTAINER_MEMORY_GB` | `8` | Memory allocated per container in GB |

### Integrations

| Variable | Default | Description |
| --- | --- | --- |
| `BACKFLOW_S3_BUCKET` |  | Optional S3 bucket for agent output artifacts |
| `BACKFLOW_WEBHOOK_URL` |  | Webhook endpoint |
| `BACKFLOW_WEBHOOK_EVENTS` | all events | Comma-separated event filter |

## Repository Guide

- `cmd/backflow/`: service entrypoint
- `internal/api/`: HTTP handlers and router
- `internal/orchestrator/`: task dispatch, monitoring, scaling, recovery
- `internal/store/`: SQLite persistence layer
- `internal/models/`: task and instance models
- `internal/notify/`: webhook notifier
- `docker/`: agent container image and entrypoint
- `scripts/`: helper scripts for task submission, reviews, AWS setup, and DB inspection

Additional documentation:

- `docs/schema.md`
- `docs/file-reference.md`
