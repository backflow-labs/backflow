# CLAUDE.md

## What This Is

Backflow is a Go service that runs coding agents (Claude Code or Codex) in ephemeral containers. Tasks come in via REST API; the orchestrator provisions infrastructure, runs agents, and cleans up.

Three task modes: `code` (default: clone → code → commit → PR), `review` (PR review with inline comments), and `read` (fetch a URL, summarize it via a reader agent, embed the TL;DR, store the result in the `readings` table for later similarity search).

## HANDOFF.md

`HANDOFF.md` at the repo root captures cross-PR tradeoffs and decisions that aren't obvious from the diff alone — what was deferred, what was unblocked for future issues, and why an alternative was rejected.

- **Before writing a plan:** read `HANDOFF.md` and weigh any notes that apply to the current task. Prior decisions may constrain or inform the approach.
- **When writing a plan:** add brief notes to `HANDOFF.md` about explicit tradeoffs decided for this change — especially any decisions the user expressed directly (e.g. "add Force now vs defer to #175"). Record the decision, the alternatives considered, and the consequence for downstream work. Keep each entry tight; this file is a ledger, not a design doc. Items should be limited to forward-looking constraints or explicit deferrals that a subsequent issue will need to know about.

## Commands

```bash
make build              # Build to bin/backflow
make run                # Build + run (sources .env, refreshes AWS creds if needed)
make test               # Unit/integration tests (excludes blackbox; see make test-blackbox)
make lint               # go vet ./...
make test-schema        # Schemathesis fuzz tests against OpenAPI spec (requires docker, goose, schemathesis)
make test-blackbox      # Black-box integration test (builds fake agent, spins up server + DB)
make test-soak          # Soak test (10 min short mode; warns before truncating tasks DB)
make test-fake-agent    # Unit tests for the fake agent Docker image
make test-reader-scripts         # Reader-agent shell script tests
make test-reader-status-writer   # Reader-agent status writer test
make test-docker-status-writer   # Agent-container status writer test
make docker-fake-agent-build  # Build fake agent image for testing
make deps               # go mod tidy
make clean              # Remove bin/ directory
make cloudflared-setup  # Create cloudflared tunnel, DNS route, and config (one-time)
make tunnel             # Start cloudflared tunnel → $BACKFLOW_DOMAIN → localhost:8080
make deploy-site        # Deploy site/ to Cloudflare Pages (backflow-site) via wrangler
make db-running         # Show running tasks (also: db-pending, db-completed, db-failed, etc.)
make docker-agent-build       # Buildx multi-platform agent image (amd64+arm64)
make docker-agent-build-local # Single-architecture agent build
make docker-agent-push        # Tag + push agent to ECR (requires REGISTRY=<ecr-uri>)
make docker-agent-deploy      # Full agent ECR pipeline: login, buildx, push
make docker-server-build       # Buildx multi-platform server image (amd64+arm64)
make docker-server-build-local # Single-architecture server build
make docker-server-deploy      # Full server ECR pipeline: login, buildx, push
make docker-reader-build       # Buildx multi-platform reader image (amd64+arm64)
make docker-reader-build-local # Single-architecture reader build
make docker-reader-push        # Tag + push reader to ECR (requires REGISTRY=<ecr-uri>)
make docker-reader-deploy      # Full reader ECR pipeline: login, buildx, push
make setup-aws          # Create AWS infrastructure
make restore-env        # Copy ~/dev/etc/backflow/.env → ./.env
make backup-env         # Copy ./.env → ~/dev/etc/backflow/.env
goose -dir migrations status # Show pending/applied migrations
goose -dir migrations up     # Apply the next migration(s)
goose -dir migrations down   # Roll back the last migration
```

Single test: `go test ./internal/store/ -run TestCreateTask -v`

## Architecture

Two goroutines: chi REST API on `:8080` + polling orchestrator (5s default). Three operating modes: `ec2` (default, spot instances), `local` (Docker on local machine), and `fargate` (one ECS task per Backflow task, no instance management).

**Flow:** Client → API → PostgreSQL → Orchestrator → Docker on EC2 via SSM, local Docker, or ECS/Fargate → Webhooks.

### API endpoints

- `GET /health` — Health check (root-level, always accessible; used by Fly.io)
- `GET /debug/stats` — Operational stats: PID, uptime, running tasks, pool metrics (outside `/api/v1/`, bearer-auth protected when API keys are configured)
- `GET /api/v1/health` — Health check (under API prefix; bearer-auth protected when API keys are configured; blocked when `BACKFLOW_RESTRICT_API=true`)
- `POST /tasks` — Create task
- `GET /tasks` — List tasks (query params: `status`, `limit`, `offset`)
- `GET /tasks/{id}` — Get task
- `DELETE /tasks/{id}` — Cancel task (sets status to `cancelled`)
- `POST /tasks/{id}/retry` — Retry a failed/interrupted/cancelled task (atomic, gated by `ready_for_retry` and user retry cap)
- `GET /tasks/{id}/logs` — Stream container logs
- `POST /webhooks/discord` — Discord interaction endpoint (signature-verified)
- `POST /webhooks/sms/inbound` — Twilio inbound SMS webhook

### Key modules (`internal/`)

- **api/** — chi router, HTTP handlers, JSON responses, `LogFetcher` interface. The `POST /tasks` handler calls `taskcreate.NewTask`; `CancelTask` and `RetryTask` shared action helpers are used by both REST handlers and Discord.
- **taskcreate/** — Canonical task-creation layer (`NewTask`, `NewReadTask`, `ErrStoreFailure`). Validates the request, applies config defaults, persists the task, and (when a non-nil `notify.Emitter` is passed) emits `task.created`. All three entry points (REST, Discord, SMS) go through this package, so every successfully created task produces exactly one `task.created` event — this is a structural invariant, pinned by tests.
- **orchestrator/** — Poll loop (`orchestrator.go`), dispatch (`dispatch.go`), monitoring (`monitor.go`, including `handleReadingCompletion` for read-mode tasks), recovery (`recovery.go`), local mode (`local.go`). Subpackages: `docker/` (Docker container management via SSM or local exec), `ec2/` (EC2 lifecycle, auto-scaler, spot interruption handler), `fargate/` (ECS/Fargate runner, CloudWatch log parsing), `s3/` (agent output upload)
- **store/** — `Store` interface + PostgreSQL (`pgxpool`, goose migrations). Includes `UpsertReading` / `GetReadingByURL` for the `readings` table.
- **models/** — `Task`, `Instance`, `AllowedSender`, `DiscordInstall`, and `Reading` (+ `Connection`) structs with status enums. `Task.AgentImage` records which Docker image the orchestrator used (read tasks get `ReaderImage`, others get the default agent image). `FindFirstURL` / `InferReviewMode` auto-detect review mode when a prompt's first URL is a GitHub PR URL.
- **embeddings/** — Thin `Embedder` interface (`Embed(ctx, text) ([]float32, error)`) with an `OpenAIEmbedder` HTTP client (no SDK). Used by the orchestrator to embed a reading's final TL;DR before writing the `readings` row.
- **discord/** — Discord interaction handler (Ed25519 signature verification, PING/PONG, interaction routing, `/backflow create` modal for task creation, `/backflow read <url> [force]` for reading-mode task creation, `/backflow cancel` and `/backflow retry` commands, button click handling). `HandlerActions` struct groups callback functions and role-based authorization config.
- **config/** — Env-var config (`BACKFLOW_*` prefix), three modes (`ec2`/`local`/`fargate`). `RestrictAPI` blocks all `/api/v1/*` endpoints when `BACKFLOW_RESTRICT_API=true` (used in Fly.io deployment). `BACKFLOW_API_KEY` enables single-token API auth; otherwise `api_keys` in Postgres can back authenticated API/debug requests. `TaskDefaults(taskMode)` returns resolved defaults — for `read` mode it swaps in `ReaderImage` plus the `BACKFLOW_DEFAULT_READ_MAX_*` caps. `Apply(task, overrides)` fills zero-value fields using `*bool` overrides (nil = use default, non-nil = use pointed value)
- **notify/** — `Notifier` interface, `WebhookNotifier` (HTTP POST, 3 retries, event filtering), `DiscordNotifier` (lifecycle messages in channel + per-task threads), `NoopNotifier`, `EventBus` (async fan-out delivery via buffered channel), `NewEvent` constructor with `EventOption` functional options (including `WithReading` for read-mode completion events). `Event` carries `TaskMode` plus optional reading fields (`TLDR`, `NoveltyVerdict`, `Tags`, `Connections`) populated only for read-task completion events. `notify/` intentionally imports no transport packages — transport-specific notifiers live alongside their transport code (e.g. `messaging.MessagingNotifier`) to avoid an import cycle back through `taskcreate`.
- **messaging/** — `Messenger` interface, `TwilioMessenger` (outbound SMS with retries + backoff), `InboundHandler` (Twilio-signature-verified webhook that calls `taskcreate.NewTask` directly and takes a `notify.Emitter` so event emission is the same invariant as every other create path). `parseReadCommand` extracts `Read <https-url>` from an inbound SMS body; the URL is validated by `discord.ValidateReadURL`. `MessagingNotifier` subscribes to the event bus and renders outbound SMS via `formatEventMessage` (read-mode completions render as TLDR + tags; other events use generic status copy).
- **debug/** — `/debug/stats` handler: PID, uptime, running task count, pgxpool metrics

### Fake agent (`test/blackbox/fake-agent/`)

Minimal Alpine image used by black-box and soak tests. Reads `FAKE_OUTCOME` env var to simulate outcomes: `success`, `slow_success`, `fail`, `needs_input`, `timeout`, `crash`. Writes `status.json` and emits `BACKFLOW_STATUS_JSON:` just like the real agent. Does not create `container_output.log` (soak tasks set `save_agent_output: false`).

### Soak test (`test/soak/`)

Long-running resource leak detector. Submits tasks at intervals, collects RSS, pool stats, and container counts, then analyzes for memory growth and container accumulation. Run via `make test-soak` (10-min short mode). Truncates the tasks table and prunes stale containers at start and end. The wrapper script (`scripts/test-soak.sh`) warns before truncating and asks for confirmation.

### Agent container (`docker/agent/`)

Node.js 24 image with Claude Code CLI + Codex CLI + git + gh. `entrypoint.sh`: clone → checkout → inject CLAUDE.md → run agent (with retry up to 3 attempts) → commit → push → create PR → optional self-review. Supports two harnesses: `claude_code` (`--output-format stream-json`, `--max-turns`) and `codex` (`exec --dangerously-bypass-approvals-and-sandbox`). Both harnesses work in code and review modes. Writes `status.json` for Docker-based modes and emits a `BACKFLOW_STATUS_JSON:` line for Fargate log parsing.

### Statuses

- **Task:** `pending` → `provisioning` → `running` → `completed` | `failed` | `interrupted` | `cancelled` | `recovering` → `pending` | `running` | `completed` | `failed`
- **Instance:** `pending` → `running` → `draining` → `terminated`

### Webhook events

`task.created`, `task.running`, `task.completed`, `task.failed`, `task.needs_input`, `task.interrupted`, `task.recovering`, `task.cancelled`, `task.retry`

### Discord integration

When `BACKFLOW_DISCORD_APP_ID` is set, Backflow enables the Discord integration:

Required env vars:

- `BACKFLOW_DISCORD_APP_ID` — Discord application ID
- `BACKFLOW_DISCORD_PUBLIC_KEY` — Ed25519 public key for interaction verification
- `BACKFLOW_DISCORD_BOT_TOKEN` — Bot token for API calls
- `BACKFLOW_DISCORD_GUILD_ID` — Target server ID
- `BACKFLOW_DISCORD_CHANNEL_ID` — Target channel ID

Optional env vars:

- `BACKFLOW_DISCORD_ALLOWED_ROLES` (comma-separated role IDs for mutation authorization)
- `BACKFLOW_DISCORD_EVENTS` (comma-separated event filter; nil = all events)

At startup, Backflow persists the install config to the `discord_installs` table, registers the `/backflow` slash command (with `create`, `status`, `list`, `cancel`, `retry`, and `read` subcommands) via the Discord API, mounts the interaction handler at `/webhooks/discord`, and subscribes a `DiscordNotifier` to the event bus. The `/backflow create` subcommand opens a modal dialog for task creation. The `/backflow read <url> [force]` subcommand creates a `task_mode=read` task for the given URL; the optional `force` flag bypasses the exact-URL duplicate check and upserts the existing reading on completion. The `/backflow cancel <task_id>` and `/backflow retry <task_id>` subcommands cancel or retry tasks respectively. All mutation commands enforce role-based permissions via `BACKFLOW_DISCORD_ALLOWED_ROLES`. The notifier creates a channel message on the first event for each task, then posts subsequent events as replies in a per-task thread. Thread messages include Cancel buttons for active tasks and Retry buttons for failed/interrupted tasks (cancelled tasks show a Retry button only after container cleanup completes).

## Reading mode

When a task's `task_mode` is `read`, the orchestrator selects `BACKFLOW_READER_IMAGE` instead of the default agent image. The reader container fetches the URL in the prompt, drafts a summary, and emits structured JSON (url/title/tldr/tags/connections/novelty_verdict/etc.) to `status.json` (and a `BACKFLOW_STATUS_JSON:` log line for Fargate).

**At dispatch** (before the reader container launches), the orchestrator looks up the URL via `store.GetReadingByURL`. If the row already exists and `!task.Force`, the task is marked `failed` with `"reading already exists for url X (id=Y); resubmit with force=true to overwrite"` and no container is started. This avoids spending reader-container minutes and LLM tokens on a URL that's already captured — and means the orchestrator, not the agent, is the source of truth for duplicate detection. The in-container `read-lookup.sh` script still exists as a best-effort hint during the agent's session but is no longer authoritative. If the DB lookup itself errors, dispatch fails through the generic error path and the task is marked failed with the DB error.

On completion, the orchestrator's `handleReadingCompletion` helper (in `internal/orchestrator/monitor.go`) runs synchronously:

1. Parses the reading-specific fields off `ContainerStatus`.
2. If the agent's `novelty_verdict` is `"duplicate"` and `!task.Force`, short-circuits with no write (agent noticed a dup mid-run; preserve the existing row).
3. Calls `embeddings.Embedder.Embed(ctx, tldr)` to embed the final TL;DR (re-embedded by the orchestrator, not reused from the agent — the agent's draft TL;DR can be refined after similarity lookup).
4. Writes the row via `store.UpsertReading`.
5. Emits `task.completed` with `WithReading(tldr, verdict, tags, connections)`.

If the embedding API call, the DB write, or the agent output fails the reading-mode contract, the task is marked `failed` rather than silently `completed`, and `task.failed` is emitted. Contract failures include:

- `embedder` is nil (no `OPENAI_API_KEY` configured).
- Agent reported an empty `url` (both `status.URL` and `task.Prompt` empty).
- Agent returned an **empty or whitespace-only TLDR** on a fresh (non-duplicate) or forced read — empty TLDR means the summary actually failed (paywall, crashed agent, parser mismatch), so surfacing it as a failure prevents the downstream SMS/Discord notifiers from sending a misleading "Task bf_xxx completed." reply for a page the user can't read. The duplicate short-circuit carves out an exception: an empty TLDR on a duplicate is acceptable because the existing reading carries the user-facing content.

The reading agent image and reader-side shell scripts live in `docker/reader/`:

- `reader-entrypoint.sh` — Image entrypoint: runs the harness, extracts JSON via `reader_helpers.sh`, writes `status.json` via `status_writer.sh`, emits `BACKFLOW_STATUS_JSON:` for Fargate.
- `read-embed.sh` — Embeds text via OpenAI `text-embedding-3-small`. Used by the agent to embed a draft TL;DR for similarity search.
- `read-similar.sh` — Semantic similarity search: embeds input text, calls the `reader.match_readings` RPC via PostgREST.
- `read-lookup.sh` — Exact-URL duplicate check via PostgREST.
- `reader_helpers.sh` — JSON extraction helpers (pulls the first JSON object from the agent transcript).
- `status_writer.sh` — Shared helper for writing `status.json` and the Fargate `BACKFLOW_STATUS_JSON:` log line.

See `docs/supabase-setup.md` for the PostgREST-backed similarity-search path the agent uses during its session.

Reading-mode env vars:

- `BACKFLOW_READER_IMAGE` — Docker image for reading-mode containers
- `BACKFLOW_DEFAULT_READ_MAX_BUDGET` / `BACKFLOW_DEFAULT_READ_MAX_RUNTIME_SEC` / `BACKFLOW_DEFAULT_READ_MAX_TURNS` — Tighter defaults applied by `TaskDefaults("read")`
- `OPENAI_API_KEY` — Required for the orchestrator's embeddings client (and for the reader container's `read-embed.sh`)
- `SUPABASE_URL` / `SUPABASE_ANON_KEY` — Passed to reader containers for PostgREST similarity search (see `docs/supabase-setup.md`)

The `tasks` table carries a `force` boolean column. `CreateTaskRequest.Force` is honored across all creation paths (REST `POST /tasks`, Discord `/backflow read ... force:true`, `taskcreate.NewReadTask`). SMS read commands do not yet accept a force flag — see `HANDOFF.md` #199 for why and how to wire it.

## Harnesses

- **`claude_code`** — Claude Code CLI. Requires `ANTHROPIC_API_KEY` or Max subscription credentials.
- **`codex`** — OpenAI Codex CLI. Requires `OPENAI_API_KEY`.

Configured per-task via the `harness` field or globally via `BACKFLOW_DEFAULT_HARNESS`.

PR comments include actual cost for `claude_code` (extracted from `total_cost_usd` in stream-json output). Codex CLI doesn't report cost in dollars — only raw token counts via `--json` — so cost is omitted for `codex` harness runs.

## API auth

- `BACKFLOW_API_KEY` — Optional single bearer token for API and debug access in small deployments
- `api_keys` — Postgres-backed bearer tokens with named scopes (`tasks:read`, `tasks:write`, `health:read`, `stats:read`) and optional expiration

When API keys are configured, bearer auth applies to `/api/v1/*` and `/debug/stats`. Root `/health` and webhook endpoints remain public.

## Fargate mode

Set `BACKFLOW_MODE=fargate` to run each Backflow task as a standalone ECS task. Capacity is tracked through a synthetic `fargate` instance in PostgreSQL; there are no EC2 instances to launch or drain.

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
- `BACKFLOW_ECS_ASSIGN_PUBLIC_IP` (`true` or `false`, default `true`; set to `false` for private subnets with NAT)
- `BACKFLOW_MAX_CONCURRENT_TASKS` (default `5`)

ECS prerequisites:

- ECS cluster with Fargate enabled and, if using `FARGATE_SPOT`, Fargate capacity providers associated to the cluster
- Task definition whose main container name matches `BACKFLOW_ECS_CONTAINER_NAME`
- Task definition configured with the `awslogs` log driver writing into `BACKFLOW_CLOUDWATCH_LOG_GROUP`
- Subnets and security groups in the same VPC, with egress for git/GitHub/API traffic
- IAM execution/task roles allowing image pull, log delivery, and whatever repository/API access the agent needs

## Fly.io deployment

Production app: `backflow` (`fly.toml`). Auto-deploys on push to main via `.github/workflows/ci.yml`. `BACKFLOW_RESTRICT_API=true` blocks all `/api/v1/*` endpoints; webhook paths and `/health` are unaffected.

AWS credentials for ECS/S3/CloudWatch are provided via the `backflow-fly` IAM user (created by `make setup-aws`). See `docs/fly-setup.md` for deployment steps.

## Documentation guidelines

Do not record default values for config or env vars in documentation. Defaults change frequently and docs drift silently. Instead, point to the source (`internal/config/config.go`) or say "see config for current defaults."

## Input validation

Environment variable keys passed via the `env_vars` field must match POSIX naming rules (`^[A-Za-z_][A-Za-z0-9_]*$`) and must not override reserved system keys (e.g. `ANTHROPIC_API_KEY`, `GITHUB_TOKEN`, `TASK_ID`, `SUPABASE_URL`, `SUPABASE_ANON_KEY`). See `reservedEnvVarKeys` in `internal/models/task.go` for the full list. All user-supplied text fields are validated to reject null bytes (PostgreSQL text columns reject them).

In Docker mode, secrets (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `GITHUB_TOKEN`) are passed via `--env-file` rather than inline in the command string. In Fargate mode, reserved keys are blocked at validation time.

## Design patterns

- Interface abstractions (`Store`, `Notifier`, `LogFetcher`) for testability
- Polling over events for simplicity
- SSM instead of SSH (no key management) in EC2 mode; direct Docker exec in local mode
- ULID task IDs with `bf_` prefix
- Zerolog structured logging
- Spot interruption detection with automatic task re-queuing

## Database

PostgreSQL via Supabase (session pooler). Tables: `tasks`, `instances`, `allowed_senders`, `api_keys`, `readings`, `discord_installs`, `discord_task_threads`. See `docs/schema.md` for the full column-level schema. Migrations are managed by [goose](https://github.com/pressly/goose) and live in `migrations/`. The store implementation is in `internal/store/postgres.go` using `pgxpool`. Set `BACKFLOW_DATABASE_URL` to the Supabase session pooler connection string.

Migration workflow:

```bash
goose -dir migrations status
goose -dir migrations up
goose -dir migrations down
```

Create new migrations in `migrations/` with the next numeric prefix, `-- +goose Up`, and `-- +goose Down`.

## Documentation

Additional docs in `docs/`:
- `ROADMAP.md` — Product roadmap with phased implementation plan
- `schema.md` — Database schema (tables, columns, indexes, status lifecycles)
- `discord-setup.md` — Discord bot creation, server install, and Backflow configuration
- `sms-setup.md` — Twilio SMS setup and allowed sender configuration
- `sizing.md` — EC2 instance sizing and container density guide
- `setup-ci.md` — GitHub Actions CI/CD setup for agent image builds
- `fly-setup.md` — Fly.io deployment setup and configuration
- `supabase-setup.md` — Supabase project setup, `readings` table, `reader` schema for PostgREST, and the publishable-key model used by the reader container
