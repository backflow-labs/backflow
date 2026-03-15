# CLAUDE.md

## What This Is

Backflow is a Go service that runs coding agents (Claude Code or Codex) in ephemeral containers. Tasks come in via REST API; the orchestrator provisions infrastructure, runs agents, and cleans up.

## Commands

```bash
make build              # Build to bin/backflow
make run                # Build + run (sources .env)
make test               # go test ./... -v -count=1
make lint               # go vet ./...
make deps               # go mod tidy
make clean              # Remove bin/ directory
make db-status          # Dump SQLite state
make docker-build       # Buildx multi-platform (amd64+arm64) image
make docker-build-local # Single-architecture build
make docker-push        # Tag + push to ECR (requires REGISTRY=<ecr-uri>)
make docker-deploy      # Full ECR pipeline: login, buildx, push
make setup-aws          # Create AWS infrastructure
```

Single test: `go test ./internal/store/ -run TestCreateTask -v`

## Architecture

Two goroutines: chi REST API on `:8080` + polling orchestrator (5s default). Three operating modes: `ec2` (default, spot instances), `local` (Docker on local machine), and `fargate` (one ECS task per Backflow task, no instance management).

**Flow:** Client → API → SQLite → Orchestrator → Docker on EC2 via SSM, local Docker, or ECS/Fargate → Webhooks.

### API endpoints (`/api/v1`)

- `GET /health` — Health check
- `POST /tasks` — Create task
- `GET /tasks` — List tasks (query params: `status`, `limit`, `offset`)
- `GET /tasks/{id}` — Get task
- `DELETE /tasks/{id}` — Cancel task (sets status to `cancelled`)
- `GET /tasks/{id}/logs` — Stream container logs

### Key modules (`internal/`)

- **api/** — chi router, handlers, JSON responses, `LogFetcher` interface
- **orchestrator/** — Poll loop (`orchestrator.go`), EC2 scaling (`ec2.go`, `scaler.go`), Docker via SSM (`docker.go`), Fargate ECS/CloudWatch runner (`fargate.go`), spot interruption handling (`spot.go`), local mode (`local.go`)
- **store/** — `Store` interface + SQLite (WAL mode, auto-migrated)
- **models/** — `Task` and `Instance` structs with status enums
- **config/** — Env-var config (`BACKFLOW_*` prefix), three modes (`ec2`/`local`/`fargate`)
- **notify/** — `Notifier` interface, `WebhookNotifier` (HTTP POST, 3 retries, event filtering), `NoopNotifier`

### Agent container (`docker/`)

Node.js 20 image with Claude Code CLI + git + gh. `entrypoint.sh`: clone → checkout → inject CLAUDE.md → run agent (with retry up to 3 attempts) → commit → push → create PR → optional self-review. Supports two harnesses: `claude_code` (default, `--output-format stream-json`) and `codex` (`--full-auto --quiet`). Writes `status.json` for Docker-based modes and emits a `BACKFLOW_STATUS_JSON:` line for Fargate log parsing.

### Statuses

- **Task:** `pending` → `provisioning` → `running` → `completed` | `failed` | `interrupted` | `cancelled` | `recovering` → `pending` | `running` | `completed` | `failed`
- **Instance:** `pending` → `running` → `draining` → `terminated`

### Webhook events

`task.created`, `task.running`, `task.completed`, `task.failed`, `task.needs_input`, `task.interrupted`, `task.recovering`

## Harnesses

- **`claude_code`** (default) — Claude Code CLI. Requires `ANTHROPIC_API_KEY` or Max subscription credentials.
- **`codex`** — OpenAI Codex CLI. Requires `OPENAI_API_KEY`. Defaults to `gpt-5.4` model.

Configured per-task via the `harness` field or globally via `BACKFLOW_DEFAULT_HARNESS`.

PR comments include actual cost for `claude_code` (extracted from `total_cost_usd` in stream-json output). Codex CLI doesn't report cost in dollars — only raw token counts via `--json` — so cost is omitted for `codex` harness runs.

## Auth modes

- **`api_key`** — Anthropic API key via `ANTHROPIC_API_KEY`, concurrent agents (max_instances × containers_per_instance)
- **`max_subscription`** — Claude Max credentials via `CLAUDE_CREDENTIALS_PATH` volume mount, serial (one agent at a time)

`max_subscription` is not supported in `fargate` mode. Initial Fargate support assumes API-key auth only.

## Fargate mode

Set `BACKFLOW_MODE=fargate` to run each Backflow task as a standalone ECS task. Capacity is tracked through a synthetic `fargate` instance in SQLite; there are no EC2 instances to launch or drain.

Required env vars:

- `BACKFLOW_ECS_CLUSTER`
- `BACKFLOW_ECS_TASK_DEFINITION`
- `BACKFLOW_ECS_SUBNETS` (comma-separated)
- `BACKFLOW_CLOUDWATCH_LOG_GROUP`

Optional Fargate env vars:

- `BACKFLOW_ECS_SECURITY_GROUPS` (comma-separated)
- `BACKFLOW_ECS_LAUNCH_TYPE` (`FARGATE` or `FARGATE_SPOT`, default `FARGATE_SPOT`)
- `BACKFLOW_ECS_CONTAINER_NAME` (default `backflow-agent`)
- `BACKFLOW_ECS_LOG_STREAM_PREFIX` (default `ecs`)
- `BACKFLOW_MAX_CONCURRENT_TASKS` (default `5`)

ECS prerequisites:

- ECS cluster with Fargate enabled and, if using `FARGATE_SPOT`, Fargate capacity providers associated to the cluster
- Task definition whose main container name matches `BACKFLOW_ECS_CONTAINER_NAME`
- Task definition configured with the `awslogs` log driver writing into `BACKFLOW_CLOUDWATCH_LOG_GROUP`
- Subnets and security groups in the same VPC, with egress for git/GitHub/API traffic
- IAM execution/task roles allowing image pull, log delivery, and whatever repository/API access the agent needs

## Design patterns

- Interface abstractions (`Store`, `Notifier`, `LogFetcher`) for testability
- Polling over events for simplicity
- SSM instead of SSH (no key management) in EC2 mode; direct Docker exec in local mode
- ULID task IDs with `bf_` prefix
- Zerolog structured logging
- Spot interruption detection with automatic task re-queuing

## Database

SQLite with WAL mode. Schema auto-migrates on startup via `CREATE TABLE IF NOT EXISTS` in `internal/store/sqlite.go:migrate()`. No separate migration files — add new columns with `ALTER TABLE` idempotently in the same function.

## Documentation

Additional docs in `docs/`:
- `schema.md` — SQLite database schema reference
- `file-reference.md` — Codebase file reference guide
