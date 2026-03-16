# Backflow

Background agent orchestrator that runs coding agents (Claude Code or Codex) in ephemeral Docker containers on AWS EC2 spot instances. POST a task (repo + prompt), get back a branch with commits and optionally a PR.

## Prerequisites

- Go 1.24+
- Docker
- AWS CLI configured with credentials (for EC2 mode)
- `sqlite3` CLI (optional, for `make db-status`)

## Quick Start

```bash
cp .env.example .env
# Edit .env — set ANTHROPIC_API_KEY and GITHUB_TOKEN at minimum

make build          # Compile to bin/backflow
make run            # Build + run (auto-sources .env)
```

Server starts on `http://localhost:8080`. Set `BACKFLOW_MODE=local` in `.env` to skip EC2 provisioning and run containers on the local Docker daemon.

## Submitting Tasks

```bash
# Via helper script
./scripts/create-task.sh https://github.com/org/repo "Fix the login bug" --pr

# With full options
./scripts/create-task.sh https://github.com/org/repo "Add unit tests" \
  --pr --pr-title "Add tests" \
  --budget 15 --model claude-sonnet-4-6 \
  --branch my-feature --target-branch develop \
  --context "Focus on the auth module" \
  --claude-md "Always use table-driven tests" \
  --env "GOPRIVATE=github.com/org/*"
```

Or call the API directly:

```bash
# Claude Code (default harness)
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{"repo_url": "https://github.com/org/repo", "prompt": "Fix the bug", "create_pr": true}'

# Codex (requires OPENAI_API_KEY)
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{"repo_url": "https://github.com/org/repo", "prompt": "Fix the bug", "harness": "codex", "create_pr": true}'
```

## API Reference

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/v1/tasks` | Create a task |
| `GET` | `/api/v1/tasks` | List tasks (`?status=`, `?limit=`, `?offset=`) |
| `GET` | `/api/v1/tasks/{id}` | Get task details |
| `DELETE` | `/api/v1/tasks/{id}` | Cancel a task |
| `GET` | `/api/v1/tasks/{id}/logs` | Stream container logs (`?tail=100`) |
| `GET` | `/api/v1/health` | Health check |

## Task Lifecycle

```
pending → provisioning → running → completed | failed | cancelled
                                 ↘ interrupted → recovering → pending (re-queued)
```

Spot interruptions are detected automatically and affected tasks are re-queued.

## Harnesses

Tasks can run with different agent CLIs via the `harness` field:

- **`claude_code`** (default) — Claude Code CLI with `--output-format stream-json`, retry logic, and structured result parsing. Requires `ANTHROPIC_API_KEY`.
- **`codex`** — OpenAI Codex CLI with `--full-auto --quiet` mode. Requires `OPENAI_API_KEY`.

Set `BACKFLOW_DEFAULT_HARNESS` to change the default, or specify per-task in the API request.

## Auth Modes

- **`api_key`** (default) — Anthropic API key. Supports multiple concurrent agents. Pay per token.
- **`max_subscription`** — Claude Max subscription credentials via `CLAUDE_CREDENTIALS_PATH`. One agent at a time. Flat rate.

## Webhooks

Set `BACKFLOW_WEBHOOK_URL` in `.env` to receive HTTP POST notifications:

```json
{
  "event": "task.completed",
  "task_id": "bf_01KK...",
  "repo_url": "https://github.com/org/repo",
  "prompt": "Fix the bug",
  "agent_log_tail": "last 20 lines...",
  "timestamp": "2026-03-13T22:00:00Z"
}
```

Events: `task.created`, `task.running`, `task.completed`, `task.failed`, `task.needs_input`, `task.interrupted`, `task.recovering`

Filter with `BACKFLOW_WEBHOOK_EVENTS=task.completed,task.failed`.

## AWS Setup (EC2 Mode)

```bash
make setup-aws          # Create ECR repo, IAM role, security group, launch template
```

Copy the `BACKFLOW_LAUNCH_TEMPLATE_ID` from the output into `.env`.

```bash
make docker-deploy      # Build multi-arch image (amd64 + arm64) and push to ECR
```

See [docs/sizing.md](docs/sizing.md) for instance type recommendations and container density calculations.

## Database

SQLite in WAL mode, configured via `BACKFLOW_DB_PATH` (default: `backflow.db`). Schema auto-migrates on startup — no separate migration files.

```bash
make db-status                          # Dump all tasks and instances
sqlite3 backflow.db ".schema"           # Show schema
```

To reset, delete `backflow.db`. It will be recreated on next startup.

## Testing

```bash
make test                               # Run all tests
make lint                               # go vet

go test ./internal/store/ -run TestCreateTask -v   # Single test
go test ./internal/api/ -v -count=1                # Single package
```

Tests use temporary SQLite databases that are cleaned up automatically.

## Configuration

All configuration is via environment variables or `.env` file. See [`.env.example`](.env.example) for the full list with descriptions.

Key variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `BACKFLOW_MODE` | `ec2` | `ec2` or `local` |
| `BACKFLOW_AUTH_MODE` | `api_key` | `api_key` or `max_subscription` |
| `ANTHROPIC_API_KEY` | | Required for `api_key` auth mode |
| `OPENAI_API_KEY` | | Required for `codex` harness |
| `GITHUB_TOKEN` | | For cloning private repos and creating PRs |
| `BACKFLOW_LAUNCH_TEMPLATE_ID` | | From `make setup-aws` (EC2 mode only) |
| `BACKFLOW_MAX_INSTANCES` | `5` | Max EC2 instances |
| `BACKFLOW_DEFAULT_HARNESS` | `claude_code` | `claude_code` or `codex` |
| `BACKFLOW_DEFAULT_MODEL` | `claude-sonnet-4-6` | Default model for Claude Code |
| `BACKFLOW_DEFAULT_MAX_BUDGET` | `10` | Default budget in USD |
| `BACKFLOW_WEBHOOK_URL` | | Webhook endpoint |
| `BACKFLOW_S3_BUCKET` | | S3 bucket for agent output storage (optional) |
