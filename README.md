# Backflow

Backflow is a background orchestrator for coding agents. It accepts a repo and task prompt, runs either Claude Code or Codex in an isolated Docker container, and returns a branch with commits and, optionally, a pull request.

It supports two execution modes:

- `local`: run agent containers on the local Docker daemon.
- `ec2`: run agent containers on ephemeral AWS EC2 spot instances.

## Quickstart

### Prerequisites

- Go 1.24+
- Docker
- AWS CLI with credentials configured for `ec2` mode
- `sqlite3` CLI for `make db-status`

### 1. Configure the server

```bash
cp .env.example .env
```

For local development, set:

```bash
BACKFLOW_MODE=local
```

At minimum, you will usually need:

- `ANTHROPIC_API_KEY` for `api_key` auth mode
- `GITHUB_TOKEN` for private repo access and PR creation
- `OPENAI_API_KEY` if you plan to use the `codex` harness

### 2. Build and run

```bash
make build
make run
```

The API listens on `http://localhost:8080` by default. The HTTP server and orchestrator poll loop run in the same process.

### 3. Submit a task

```bash
./scripts/create-task.sh https://github.com/org/repo "Fix the login bug"
```

By default, `scripts/create-task.sh`:

- uses the `codex` harness
- creates a PR unless you pass `--no-pr`

Example with explicit options:

```bash
./scripts/create-task.sh https://github.com/org/repo "Add unit tests for auth flows" \
  --harness claude_code \
  --model claude-sonnet-4-6 \
  --branch backflow/auth-tests \
  --target-branch main \
  --budget 15 \
  --runtime 30 \
  --context "Focus on auth middleware and session expiry"
```

## Common Commands

```bash
make test                 # Run all tests without cache
make lint                 # Run go vet
make docker-build-local   # Build the agent image locally
make db-status            # Inspect tasks and instances in SQLite
```

Run a single test or package:

```bash
go test ./internal/store/ -run TestCreateTask -v
go test ./internal/api/ -v -count=1
```

Tests use temporary SQLite databases and do not require external services.

## Task Submission

### Using helper scripts

Create a coding task:

```bash
./scripts/create-task.sh https://github.com/org/repo "Refactor the auth middleware"
```

Read the prompt from a file:

```bash
./scripts/create-task.sh https://github.com/org/repo --plan prompts/refactor.md
```

Disable PR creation:

```bash
./scripts/create-task.sh https://github.com/org/repo "Fix flaky tests" --no-pr
```

Review an existing pull request:

```bash
./scripts/review-pr.sh https://github.com/org/repo 42 --prompt "Focus on correctness and regressions"
```

### Using the HTTP API directly

Create a coding task with the default harness:

```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "repo_url": "https://github.com/org/repo",
    "prompt": "Fix the bug",
    "create_pr": true
  }'
```

Create a coding task with Codex:

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

Create a PR review task:

```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "task_mode": "review",
    "repo_url": "https://github.com/org/repo",
    "review_pr_number": 42,
    "prompt": "Focus on security-sensitive changes",
    "create_pr": false
  }'
```

### Task modes

- `code`: clone a repo, apply changes, commit, push, and optionally create a PR
- `review`: review an existing PR and report findings

### Harnesses

- `claude_code`: Claude Code CLI
- `codex`: OpenAI Codex CLI

Set `BACKFLOW_DEFAULT_HARNESS` to change the server-side default when the request does not specify `harness`.

### Auth modes

- `api_key`: uses `ANTHROPIC_API_KEY`; supports multiple concurrent agents
- `max_subscription`: uses Claude Max credentials from `CLAUDE_CREDENTIALS_PATH`; runs one agent at a time

## Monitoring and Operations

Check task state:

```bash
curl http://localhost:8080/api/v1/tasks/{id}
```

Tail logs:

```bash
curl http://localhost:8080/api/v1/tasks/{id}/logs?tail=100
```

List tasks:

```bash
curl "http://localhost:8080/api/v1/tasks?status=running&limit=20"
```

Health check:

```bash
curl http://localhost:8080/api/v1/health
```

Open an SSM session to an EC2 worker:

```bash
aws ssm start-session --target i-0abc...
```

### Task lifecycle

`pending` -> `provisioning` -> `running` -> `completed` | `failed` | `interrupted` | `cancelled`

Tasks may also enter `recovering` while Backflow handles worker interruption and retries.

### Instance lifecycle

`pending` -> `running` -> `draining` -> `terminated`

In EC2 mode, spot interruptions re-queue affected tasks.

## Database

Backflow uses SQLite in WAL mode. The database path is controlled by `BACKFLOW_DB_PATH` and defaults to `backflow.db` in the working directory.

Schema is managed in `internal/store/sqlite.go` by the `migrate()` method. There are no standalone migration files.

Inspect the database:

```bash
make db-status
sqlite3 backflow.db ".schema"
sqlite3 backflow.db "SELECT id, status, created_at FROM tasks ORDER BY created_at DESC LIMIT 10;"
```

To reset local state, delete `backflow.db`; it will be recreated on startup.

## AWS Setup

These steps only apply to `BACKFLOW_MODE=ec2`.

### Provision infrastructure

```bash
make setup-aws
```

This creates the supporting AWS resources, including the launch template used for worker instances. Copy the resulting `BACKFLOW_LAUNCH_TEMPLATE_ID` into `.env`.

### Build and push the agent image

```bash
make docker-deploy
```

If Docker requires `sudo`:

```bash
make docker-deploy DOCKER="sudo docker"
```

This builds a multi-architecture image and pushes it to ECR.

## Webhooks

Set `BACKFLOW_WEBHOOK_URL` to receive task notifications. Use `BACKFLOW_WEBHOOK_EVENTS` to filter events; if unset, all events are sent.

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

Known events:

- `task.created`
- `task.running`
- `task.completed`
- `task.failed`
- `task.needs_input`
- `task.interrupted`

## API Reference

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/v1/tasks` | Create a task |
| `GET` | `/api/v1/tasks` | List tasks (`status`, `limit`, `offset`) |
| `GET` | `/api/v1/tasks/{id}` | Get task details |
| `DELETE` | `/api/v1/tasks/{id}` | Cancel or delete a task |
| `GET` | `/api/v1/tasks/{id}/logs` | Get container logs (`tail`) |
| `GET` | `/api/v1/health` | Health check |

## Configuration

All configuration is supplied through environment variables or a `.env` file.

| Variable | Default | Description |
|----------|---------|-------------|
| `BACKFLOW_MODE` | `ec2` | Execution mode: `ec2` or `local` |
| `BACKFLOW_AUTH_MODE` | `api_key` | Auth mode: `api_key` or `max_subscription` |
| `ANTHROPIC_API_KEY` | | Required for `api_key` mode |
| `OPENAI_API_KEY` | | Required for the `codex` harness |
| `CLAUDE_CREDENTIALS_PATH` | | Path to `~/.claude/` for `max_subscription` mode |
| `GITHUB_TOKEN` | | GitHub token for cloning private repos and creating PRs |
| `BACKFLOW_LISTEN_ADDR` | `:8080` | HTTP listen address |
| `BACKFLOW_DB_PATH` | `backflow.db` | SQLite database path |
| `AWS_REGION` | `us-east-1` | AWS region |
| `BACKFLOW_INSTANCE_TYPE` | `m7g.xlarge` | EC2 instance type |
| `BACKFLOW_AMI` | | Optional AMI override |
| `BACKFLOW_LAUNCH_TEMPLATE_ID` | | Launch template ID from AWS setup |
| `BACKFLOW_MAX_INSTANCES` | `5` | Maximum number of worker instances |
| `BACKFLOW_CONTAINERS_PER_INSTANCE` | `1` | Containers scheduled per instance |
| `BACKFLOW_CONTAINER_CPUS` | `2` | CPU cores allocated per container |
| `BACKFLOW_CONTAINER_MEMORY_GB` | `8` | Memory allocated per container in GB |
| `BACKFLOW_DEFAULT_HARNESS` | `claude_code` | Default harness for API requests that omit `harness` |
| `BACKFLOW_DEFAULT_MODEL` | `claude-sonnet-4-6` | Default Claude model |
| `BACKFLOW_DEFAULT_CODEX_MODEL` | `gpt-5.4` | Default Codex model |
| `BACKFLOW_DEFAULT_EFFORT` | `high` | Default reasoning effort |
| `BACKFLOW_DEFAULT_MAX_BUDGET` | `10` | Default budget in USD |
| `BACKFLOW_DEFAULT_MAX_RUNTIME_MIN` | `30` | Default max runtime in minutes |
| `BACKFLOW_DEFAULT_MAX_TURNS` | `200` | Default max turns |
| `BACKFLOW_S3_BUCKET` | | Optional S3 bucket for agent output artifacts |
| `BACKFLOW_WEBHOOK_URL` | | Webhook destination |
| `BACKFLOW_WEBHOOK_EVENTS` | all | Comma-separated webhook event filter |
| `BACKFLOW_POLL_INTERVAL_SEC` | `5` | Orchestrator poll interval in seconds |
| `DOCKER` | `docker` | Docker command used by `make` targets |
