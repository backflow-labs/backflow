# Backflow

Background agent orchestrator that runs Claude Code in ephemeral Docker containers on AWS EC2 spot instances. POST a task (repo + prompt), get back a branch with commits and optionally a PR.

## Quickstart

```bash
# Prerequisites: Go 1.24+, AWS CLI (authenticated), Docker, jq, sqlite3

cp .env.example .env
# Edit .env with your ANTHROPIC_API_KEY, GITHUB_TOKEN, and BACKFLOW_LAUNCH_TEMPLATE_ID

make run
```

Server starts on `localhost:8080`.

### First-time AWS setup

```bash
make setup-aws
```

Creates ECR repo, IAM role, security group, and launch template. Grab the `BACKFLOW_LAUNCH_TEMPLATE_ID` from the output and put it in `.env`.

Then build and push the agent image:

```bash
make docker-deploy
# If docker needs sudo: make docker-deploy DOCKER="sudo docker"
```

## Submitting tasks

```bash
# Simple
./scripts/create-task.sh https://github.com/org/repo "Fix the login bug"

# With PR creation
./scripts/create-task.sh https://github.com/org/repo "Fix the login bug" --pr

# Full options
./scripts/create-task.sh https://github.com/org/repo "Add unit tests" \
  --pr --pr-title "Add tests" \
  --budget 15 --model claude-sonnet-4-6 \
  --branch my-feature --target-branch develop \
  --context "Focus on the auth module" \
  --claude-md "Always use table-driven tests" \
  --env "GOPRIVATE=github.com/org/*"
```

Or hit the API directly:

```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{"repo_url": "https://github.com/org/repo", "prompt": "Fix the bug", "create_pr": true}'
```

## Monitoring

```bash
# Dump all tasks and instances from the database
make db-status

# Get task details
curl http://localhost:8080/api/v1/tasks/{id}

# Stream container logs
curl http://localhost:8080/api/v1/tasks/{id}/logs?tail=100

# Shell into an agent VM
aws ssm start-session --target i-0abc...
```

## Development

```bash
make build      # Compile to bin/backflow
make test       # go test ./... -v -count=1
make lint       # go vet ./...
make deps       # go mod tidy
make db-status  # Dump SQLite state
```

Run a single test:

```bash
go test ./internal/store/ -run TestCreateTask -v
```

Build the agent image locally (no push):

```bash
make docker-build-local
```

## API

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/v1/tasks` | Create a task |
| `GET` | `/api/v1/tasks` | List tasks (`?status=`, `?limit=`, `?offset=`) |
| `GET` | `/api/v1/tasks/{id}` | Get a task |
| `DELETE` | `/api/v1/tasks/{id}` | Cancel a task |
| `GET` | `/api/v1/tasks/{id}/logs` | Container logs (`?tail=100`) |
| `GET` | `/api/v1/health` | Health check |

## Architecture

```
Client ──▶ REST API (:8080, chi router)
               │
               ▼
          SQLite (WAL)
               │
               ▼
         Orchestrator (5s poll loop)
          ├── Scaler ── launch/terminate EC2 spot instances
          ├── Docker ── run containers via SSM
          └── Spot ──── detect interruptions, re-queue tasks
               │
               ▼ SSM
         EC2 Spot Instance
          └── Docker container (backflow-agent)
               ├── Clone repo
               ├── Run Claude Code
               ├── Commit + push
               └── Create PR
```

**Task lifecycle:** `pending` → `provisioning` → `running` → `completed` | `failed` | `interrupted` | `cancelled`

**Instance lifecycle:** `pending` → `running` → idle 5 min → `terminated`. Spot interruptions re-queue affected tasks.

## Auth modes

- **`api_key`** (default) — Anthropic API key. Multiple concurrent agents. Pay per token.
- **`max_subscription`** — Claude Max subscription credentials. One agent at a time. Flat rate.

## Webhooks

Set `BACKFLOW_WEBHOOK_URL` in `.env` to receive POST notifications:

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

Events: `task.created`, `task.running`, `task.completed`, `task.failed`, `task.needs_input`, `task.interrupted`

Filter with `BACKFLOW_WEBHOOK_EVENTS=task.completed,task.failed`.

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `BACKFLOW_AUTH_MODE` | `api_key` | `api_key` or `max_subscription` |
| `ANTHROPIC_API_KEY` | | Required for `api_key` mode |
| `CLAUDE_CREDENTIALS_PATH` | | Path to `~/.claude/` for `max_subscription` mode |
| `GITHUB_TOKEN` | | Cloning private repos and creating PRs |
| `BACKFLOW_LISTEN_ADDR` | `:8080` | Server listen address |
| `BACKFLOW_DB_PATH` | `backflow.db` | SQLite database path |
| `AWS_REGION` | `us-east-1` | AWS region |
| `BACKFLOW_INSTANCE_TYPE` | `t4g.medium` | EC2 instance type |
| `BACKFLOW_LAUNCH_TEMPLATE_ID` | | Launch template from `make setup-aws` |
| `BACKFLOW_MAX_INSTANCES` | `5` | Max EC2 instances |
| `BACKFLOW_CONTAINERS_PER_INSTANCE` | `4` | Max containers per instance |
| `BACKFLOW_DEFAULT_MODEL` | `claude-sonnet-4-6` | Default Claude model |
| `BACKFLOW_DEFAULT_MAX_BUDGET` | `10` | Default max budget (USD) |
| `BACKFLOW_DEFAULT_MAX_RUNTIME_MIN` | `30` | Default max runtime (minutes) |
| `BACKFLOW_DEFAULT_MAX_TURNS` | `200` | Default max conversation turns |
| `BACKFLOW_WEBHOOK_URL` | | Webhook endpoint |
| `BACKFLOW_WEBHOOK_EVENTS` | all | Comma-separated event filter |
| `BACKFLOW_POLL_INTERVAL_SEC` | `5` | Orchestrator poll interval |
| `DOCKER` | `docker` | Docker command (`sudo docker` if needed) |
