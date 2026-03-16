# Backflow

Backflow is a background agent orchestrator for code tasks and PR reviews. It runs Claude Code or Codex inside short-lived Docker containers, either on your machine or on AWS EC2 Spot instances, then tracks execution through a small HTTP API backed by SQLite.

Submit a repository plus instructions, and Backflow can:

- run the task in an isolated container
- push commits to a branch
- open a pull request
- review an existing pull request
- expose logs, status, and optional webhook or S3 output

## How It Works

1. A client creates a task through the API or helper scripts.
2. Backflow schedules a container locally or on an EC2 instance.
3. The container clones the repository, runs the selected harness (`claude_code` or `codex`), and writes task status back to Backflow.
4. Backflow stores task and instance state in SQLite, exposes logs over HTTP, and can send webhooks or upload saved output to S3.

## Quickstart

### Prerequisites

- Go 1.24+
- Docker
- `curl` and `jq` if you want to use the helper scripts in `scripts/`
- `sqlite3` if you want to use `make db-status`
- AWS CLI with configured credentials if you want EC2 mode

### Run Locally

Copy the sample environment file and set the credentials you need:

```bash
cp .env.example .env
```

At minimum:

- set `ANTHROPIC_API_KEY` for Claude Code in `api_key` mode
- set `OPENAI_API_KEY` if you want to use the `codex` harness
- set `GITHUB_TOKEN` if tasks need to clone private repos or create PRs

For local-only development, set:

```bash
BACKFLOW_MODE=local
```

Then build and run the server:

```bash
make run
```

The API listens on `http://localhost:8080` by default.

### Submit a Code Task

Using the helper script:

```bash
./scripts/create-task.sh https://github.com/org/repo "Fix the login bug" \
  --harness claude_code \
  --no-pr
```

Create a task and ask Backflow to open a PR:

```bash
./scripts/create-task.sh https://github.com/org/repo "Fix the login bug" \
  --harness claude_code \
  --branch backflow/fix-login \
  --target-branch main \
  --pr-title "Fix login bug"
```

Important defaults for `scripts/create-task.sh`:

- it defaults to `--harness codex`
- it creates a PR by default
- pass `--no-pr` if you only want a pushed branch

You can also call the API directly:

```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "repo_url": "https://github.com/org/repo",
    "prompt": "Fix the login bug",
    "harness": "claude_code",
    "create_pr": true
  }'
```

Important defaults for the HTTP API:

- `harness` defaults to `BACKFLOW_DEFAULT_HARNESS` (`claude_code` by default)
- `create_pr` defaults to `false` unless you set it explicitly
- `save_agent_output` defaults to `true`

### Submit a PR Review Task

Use `scripts/review-pr.sh` to review an existing PR:

```bash
./scripts/review-pr.sh https://github.com/org/repo 42 \
  --prompt "Focus on correctness, regressions, and security risks"
```

Review tasks run with `task_mode=review` and post feedback through the GitHub CLI inside the container.

## Development Workflow

### Common Commands

```bash
make build               # Compile to bin/backflow
make run                 # Build and run the server
make test                # Run all tests without cache
make lint                # go vet
make docker-build-local  # Build the agent image locally
make db-status           # Inspect tasks and instances in SQLite
```

Tests use temporary SQLite databases and do not require external services.

### Database

Backflow uses SQLite in WAL mode. By default the database is stored at `backflow.db`, configurable through `BACKFLOW_DB_PATH`.

Useful commands:

```bash
make db-status
sqlite3 backflow.db ".schema"
sqlite3 backflow.db "SELECT id, status, created_at FROM tasks ORDER BY created_at DESC LIMIT 10;"
```

To reset local state, delete the database file and restart the server.

## Running on AWS EC2 Spot

### One-Time Setup

Provision the supporting AWS resources:

```bash
make setup-aws
```

This creates the ECR repository, IAM role and instance profile, security group, launch template, and an S3 bucket for saved agent output. Copy the values printed by the script into `.env`, especially:

- `BACKFLOW_LAUNCH_TEMPLATE_ID`
- `BACKFLOW_S3_BUCKET` if you want agent output uploaded to S3

### Build and Deploy the Agent Image

```bash
make docker-deploy
```

This builds a multi-arch image (`linux/amd64` and `linux/arm64`) and pushes it to ECR.

To run in EC2 mode, keep:

```bash
BACKFLOW_MODE=ec2
```

## API Reference

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/v1/tasks` | Create a task |
| `GET` | `/api/v1/tasks` | List tasks (`?status=`, `?limit=`, `?offset=`) |
| `GET` | `/api/v1/tasks/{id}` | Get task details |
| `DELETE` | `/api/v1/tasks/{id}` | Cancel or delete a task |
| `GET` | `/api/v1/tasks/{id}/logs` | Get container logs (`?tail=100`) |
| `GET` | `/api/v1/health` | Health check |

### Common Operational Requests

```bash
# Health check
curl http://localhost:8080/api/v1/health

# Task details
curl http://localhost:8080/api/v1/tasks/{id}

# Recent logs
curl http://localhost:8080/api/v1/tasks/{id}/logs?tail=100
```

### Task Lifecycle

`pending` -> `provisioning` -> `running` -> `completed`

Other possible outcomes:

- `failed`
- `interrupted`
- `cancelled`
- `recovering`

### Instance Lifecycle

`pending` -> `running` -> `draining` -> `terminated`

Spot interruptions re-queue affected tasks.

## Harnesses and Auth Modes

### Harnesses

- `claude_code`: uses the Claude Code CLI
- `codex`: uses the OpenAI Codex CLI

Set the default with `BACKFLOW_DEFAULT_HARNESS`, or override per task.

### Auth Modes

- `api_key`: uses `ANTHROPIC_API_KEY`; supports multiple concurrent agents
- `max_subscription`: uses cached Claude Max credentials from `CLAUDE_CREDENTIALS_PATH`; effectively serializes execution to one agent at a time

## Configuration

`.env.example` is the best source of truth for a working local configuration. The most important variables are grouped below.

### Core Settings

| Variable | Default | Description |
|----------|---------|-------------|
| `BACKFLOW_MODE` | `ec2` | Run on `ec2` or `local` |
| `BACKFLOW_LISTEN_ADDR` | `:8080` | HTTP listen address |
| `BACKFLOW_DB_PATH` | `backflow.db` | SQLite database path |
| `BACKFLOW_POLL_INTERVAL_SEC` | `5` | Orchestrator poll interval |

### Credentials and Auth

| Variable | Default | Description |
|----------|---------|-------------|
| `BACKFLOW_AUTH_MODE` | `api_key` | `api_key` or `max_subscription` |
| `ANTHROPIC_API_KEY` | | Required when `BACKFLOW_AUTH_MODE=api_key` |
| `OPENAI_API_KEY` | | Required for the `codex` harness |
| `CLAUDE_CREDENTIALS_PATH` | | Path to `~/.claude/` for `max_subscription` mode |
| `GITHUB_TOKEN` | | Required for private repo access and PR creation |

### EC2 Capacity and Sizing

| Variable | Default | Description |
|----------|---------|-------------|
| `AWS_REGION` | `us-east-1` | AWS region |
| `BACKFLOW_INSTANCE_TYPE` | `m7g.xlarge` | EC2 instance type |
| `BACKFLOW_LAUNCH_TEMPLATE_ID` | | Launch template created by `make setup-aws` |
| `BACKFLOW_MAX_INSTANCES` | `5` | Max EC2 instances |
| `BACKFLOW_CONTAINERS_PER_INSTANCE` | `1` | Max containers per instance |
| `BACKFLOW_CONTAINER_CPUS` | `2` | CPU cores reserved per container |
| `BACKFLOW_CONTAINER_MEMORY_GB` | `8` | Memory reserved per container |

In local mode, `BACKFLOW_CONTAINERS_PER_INSTANCE` must stay at or below `6`.

### Task Defaults

| Variable | Default | Description |
|----------|---------|-------------|
| `BACKFLOW_DEFAULT_HARNESS` | `claude_code` | Default task harness |
| `BACKFLOW_DEFAULT_MODEL` | `claude-sonnet-4-6` | Default Claude Code model |
| `BACKFLOW_DEFAULT_CODEX_MODEL` | `gpt-5.4` | Default Codex model |
| `BACKFLOW_DEFAULT_EFFORT` | `high` | Default effort level |
| `BACKFLOW_DEFAULT_MAX_BUDGET` | `10` | Default max budget in USD |
| `BACKFLOW_DEFAULT_MAX_RUNTIME_MIN` | `30` | Default max runtime in minutes |
| `BACKFLOW_DEFAULT_MAX_TURNS` | `200` | Default max conversation turns |

### Optional Integrations

| Variable | Default | Description |
|----------|---------|-------------|
| `BACKFLOW_S3_BUCKET` | | Upload saved agent output to S3 |
| `BACKFLOW_WEBHOOK_URL` | | Send task lifecycle events to an HTTP endpoint |
| `BACKFLOW_WEBHOOK_EVENTS` | all | Comma-separated event allowlist |

Webhook events:

- `task.created`
- `task.running`
- `task.completed`
- `task.failed`
- `task.needs_input`
- `task.interrupted`

## Additional Docs

- `docs/schema.md`: SQLite schema, statuses, and migration notes
- `docs/sizing.md`: instance sizing and cost reference
- `docs/file-reference.md`: repository file map
