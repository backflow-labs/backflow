# Backflow

Backflow is a background agent orchestrator for repository tasks. It accepts coding or review jobs over HTTP, runs agent CLIs inside isolated Docker containers, and can push a branch and open a pull request when the work is done.

Backflow supports:

- `code` tasks that clone a repo, make changes, commit, push, and optionally open a PR
- `review` tasks that inspect an existing pull request and post feedback with `gh`
- local execution on the host Docker daemon
- EC2 spot-based execution for burst capacity
- SQLite-backed task tracking
- optional webhooks and optional S3 upload for agent output

## Quickstart

### Prerequisites

- Go 1.24+
- Docker
- GitHub CLI (`gh`)
- `jq`
- Git
- Anthropic API key when `BACKFLOW_AUTH_MODE=api_key`
- GitHub token for private repo access and PR creation

Optional:

- OpenAI API key for the `codex` harness
- AWS CLI for EC2 mode
- `sqlite3` CLI for `make db-status`

### Local setup

```bash
cp .env.example .env
```

Edit `.env` with the values you need. For a local-only setup, set:

```bash
BACKFLOW_MODE=local
ANTHROPIC_API_KEY=...
GITHUB_TOKEN=...
```

If you want to run Codex tasks, also set:

```bash
OPENAI_API_KEY=...
```

### Run the server

```bash
make build
make run
```

The API listens on `http://localhost:8080`.

## How It Works

1. A client submits a task over the REST API or via a helper script.
2. Backflow writes the task to SQLite and the orchestrator picks it up on the next poll interval.
3. The orchestrator starts an agent container either locally or on an EC2 spot instance.
4. The container clones the target repository, runs the selected harness, writes a `status.json` result, and optionally creates a PR.
5. Backflow updates task state, exposes logs over the API, and can send webhook notifications or upload agent output to S3.

## Execution Modes

### `local`

Use `BACKFLOW_MODE=local` to run containers on the local Docker daemon. This is the fastest way to develop on Backflow and avoids AWS provisioning entirely.

### `ec2`

Use `BACKFLOW_MODE=ec2` to dispatch work onto EC2 spot instances. Backflow manages capacity, waits for SSM and Docker readiness, and re-queues work when spot interruptions happen.

## Harnesses and Auth

### Harnesses

- `claude_code`: default API harness
- `codex`: OpenAI Codex CLI harness

The API defaults to `claude_code`. The helper script [`scripts/create-task.sh`](scripts/create-task.sh) currently defaults to `codex`, so set `--harness` explicitly if you want different behavior.
Review tasks currently run through the Claude-based review flow.

### Auth modes

- `api_key`: uses `ANTHROPIC_API_KEY`
- `max_subscription`: uses cached Claude credentials from `CLAUDE_CREDENTIALS_PATH`

When `BACKFLOW_AUTH_MODE=max_subscription`, Backflow only runs one task at a time.

## Submitting Tasks

### Helper scripts

Create a code task:

```bash
./scripts/create-task.sh https://github.com/org/repo "Fix the login bug" \
  --harness claude_code \
  --pr-title "Fix login bug"
```

Create a code task from a plan file:

```bash
./scripts/create-task.sh https://github.com/org/repo --plan prompts.md \
  --harness codex \
  --target-branch main \
  --self-review
```

Review an existing PR:

```bash
./scripts/review-pr.sh https://github.com/org/repo 42 \
  --prompt "Focus on correctness and regression risk"
```

### API examples

Create a code task:

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

Create a review task:

```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "task_mode": "review",
    "repo_url": "https://github.com/org/repo",
    "review_pr_number": 42,
    "prompt": "Focus on security and error handling"
  }'
```

Useful fields on create:

- `task_mode`: `code` or `review`
- `harness`: `claude_code` or `codex`
- `branch`: working branch name to create
- `target_branch`: PR base branch
- `model`, `effort`, `max_budget_usd`, `max_runtime_min`, `max_turns`
- `create_pr`, `self_review`
- `save_agent_output`: defaults to `true`
- `pr_title`, `pr_body`
- `context`, `claude_md`, `allowed_tools`, `env_vars`

## Monitoring and Operations

Development commands:

```bash
make test
make lint
make db-status
```

Check health:

```bash
curl http://localhost:8080/api/v1/health
```

List tasks:

```bash
curl http://localhost:8080/api/v1/tasks
curl "http://localhost:8080/api/v1/tasks?status=running&limit=20&offset=0"
```

Inspect one task:

```bash
curl http://localhost:8080/api/v1/tasks/{id}
```

Fetch logs:

```bash
curl http://localhost:8080/api/v1/tasks/{id}/logs
curl "http://localhost:8080/api/v1/tasks/{id}/logs?tail=100"
```

Cancel or delete a task:

```bash
curl -X DELETE http://localhost:8080/api/v1/tasks/{id}
```

Inspect local database state:

```bash
make db-status
sqlite3 backflow.db ".schema"
```

Task lifecycle:

```text
pending -> provisioning -> running -> completed
                                 -> failed
                                 -> interrupted
                                 -> cancelled
running/provisioning -> recovering -> running/completed/failed/pending
```

## AWS Setup

For EC2 mode you need AWS credentials configured locally, plus a launch template and ECR repository.

Create the AWS-side infrastructure:

```bash
make setup-aws
```

That script creates the ECR repository, IAM role, security group, and launch template. Copy the resulting `BACKFLOW_LAUNCH_TEMPLATE_ID` into `.env`.

Build and push the agent image:

```bash
make docker-deploy
```

If your Docker setup requires sudo:

```bash
make docker-deploy DOCKER="sudo docker"
```

To build the agent image locally without pushing:

```bash
make docker-build-local
```

## Webhooks and Output Storage

Set `BACKFLOW_WEBHOOK_URL` to receive task lifecycle events. You can filter them with `BACKFLOW_WEBHOOK_EVENTS`.

Example webhook payload:

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

Set `BACKFLOW_S3_BUCKET` to upload agent output. Per-task uploads are controlled by `save_agent_output`, which defaults to `true`.

## Configuration

Backflow is configured entirely through environment variables. Start with [`.env.example`](.env.example).

Common settings:

| Variable | Default | Notes |
| --- | --- | --- |
| `BACKFLOW_MODE` | `ec2` | `local` or `ec2` |
| `BACKFLOW_AUTH_MODE` | `api_key` | `api_key` or `max_subscription` |
| `ANTHROPIC_API_KEY` | | Required in `api_key` mode |
| `OPENAI_API_KEY` | | Required for `codex` tasks |
| `CLAUDE_CREDENTIALS_PATH` | | Used in `max_subscription` mode |
| `GITHUB_TOKEN` | | Used for repo access and PR creation |
| `BACKFLOW_LISTEN_ADDR` | `:8080` | API bind address |
| `BACKFLOW_DB_PATH` | `backflow.db` | SQLite path |
| `BACKFLOW_POLL_INTERVAL_SEC` | `5` | Orchestrator poll interval |
| `BACKFLOW_DEFAULT_HARNESS` | `claude_code` | API default harness |
| `BACKFLOW_DEFAULT_MODEL` | `claude-sonnet-4-6` | Default Claude model |
| `BACKFLOW_DEFAULT_CODEX_MODEL` | `gpt-5.4` | Default Codex model |
| `BACKFLOW_DEFAULT_EFFORT` | `high` | Default reasoning effort |
| `BACKFLOW_DEFAULT_MAX_BUDGET` | `10` | USD |
| `BACKFLOW_DEFAULT_MAX_RUNTIME_MIN` | `30` | Minutes |
| `BACKFLOW_DEFAULT_MAX_TURNS` | `200` | Conversation turns |
| `BACKFLOW_CONTAINER_CPUS` | `2` | Per-container CPU allocation |
| `BACKFLOW_CONTAINER_MEMORY_GB` | `8` | Per-container RAM allocation |
| `BACKFLOW_S3_BUCKET` | | Optional agent output bucket |
| `BACKFLOW_WEBHOOK_URL` | | Optional webhook endpoint |
| `BACKFLOW_WEBHOOK_EVENTS` | all | Comma-separated event filter |

EC2-specific settings:

| Variable | Default | Notes |
| --- | --- | --- |
| `AWS_REGION` | `us-east-1` | AWS region |
| `BACKFLOW_INSTANCE_TYPE` | `m7g.xlarge` | Instance type |
| `BACKFLOW_AMI` | | Optional AMI override |
| `BACKFLOW_LAUNCH_TEMPLATE_ID` | | Launch template from `make setup-aws` |
| `BACKFLOW_MAX_INSTANCES` | `5` | Max EC2 instances |
| `BACKFLOW_CONTAINERS_PER_INSTANCE` | `1` | Task slots per instance |

## Reference Docs

- [docs/file-reference.md](docs/file-reference.md): repository map
- [docs/schema.md](docs/schema.md): SQLite schema and task lifecycle details
- [docs/sizing.md](docs/sizing.md): instance sizing and cost guidance
