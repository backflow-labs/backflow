# File Reference

Complete mapping of every file in the Backflow repository.

## Root

| File | Description |
|------|-------------|
| `CLAUDE.md` | Project instructions for Claude Code — architecture overview, commands, design patterns |
| `Makefile` | Build, test, lint, Docker, and deployment targets |
| `README.md` | Project documentation — quickstart, API reference, configuration, architecture diagram |
| `.env.example` | Sample environment variables with defaults and comments |
| `.gitignore` | Ignores `bin/`, `.db` files, `.env`, and `mise.toml` |
| `go.mod` | Go module definition (`github.com/backflow-labs/backflow`, Go 1.24.1) with dependencies |
| `go.sum` | Go module checksums |

## `.claude/`

| File | Description |
|------|-------------|
| `settings.local.json` | Local Claude/Codex permission overrides for this workspace |
| `skills/goose-migration.md` | Project skill for generating new goose migration files |

## `cmd/backflow/`

Entry point for the server binary.

| File | Description |
|------|-------------|
| `main.go` | Application entry point. Loads config, connects to PostgreSQL via `store.NewPostgres`, initializes the notifier, orchestrator, and HTTP server, then runs both the orchestrator poll loop and the chi-based API server as concurrent goroutines. Handles graceful shutdown on SIGINT/SIGTERM. |

## `internal/api/`

REST API layer built on the chi router.

| File | Description |
|------|-------------|
| `server.go` | Creates the chi router with middleware (RequestID, RealIP, Logger, Recoverer, JSON content-type) and registers all `/api/v1` routes: health check, and CRUD + logs endpoints for tasks. |
| `handlers.go` | HTTP handler methods for the API. `CreateTask` validates input and writes to the store. `GetTask`, `ListTasks`, `DeleteTask` handle retrieval, listing with filters (status/limit/offset), and cancellation or deletion. `GetTaskLogs` fetches live container logs via the `LogFetcher` interface. `HealthCheck` returns status and auth mode. |
| `handlers_test.go` | Tests for API handlers — health check, create/get task, list tasks, input validation (including harness validation), codex harness default model selection, 404 on missing task, and delete. Uses a testcontainers PostgreSQL instance and `httptest`. |
| `responses.go` | JSON response helpers. Defines the `envelope` struct (`{data, error}`) and `writeJSON`/`writeError` functions used by all handlers. |

## `internal/config/`

Environment-variable-based configuration.

| File | Description |
|------|-------------|
| `config.go` | Defines the `Config` struct with all server settings (mode, auth, AWS, ECS/Fargate, agent defaults, webhooks, DB, polling). `Load()` reads from environment variables with sensible defaults. Supports three modes (`ec2`, `local`, `fargate`), two auth modes (`api_key`, `max_subscription`), and two harnesses (`claude_code`, `codex`) with per-harness default models. Fargate-specific validation (required ECS fields, launch type, max concurrent tasks, no `max_subscription`) runs only when mode is `fargate`. `MaxConcurrent()` computes the concurrency limit based on mode, auth mode, and instance capacity. |

## `internal/models/`

Data structures and status enums.

| File | Description |
|------|-------------|
| `task.go` | `Task` struct with all fields (ID, status, task_mode, harness, repo, branch, prompt, model, effort, budget, runtime, turns, PR info, container/instance IDs, cost, timestamps). `Harness` type (`claude_code`, `codex`). `TaskMode` constants (`code`, `review`). `CreateTaskRequest` struct with `Validate()` for API input. `TaskStatus` enum: `pending`, `provisioning`, `running`, `completed`, `failed`, `interrupted`, `cancelled`, `recovering`. Helper methods `IsTerminal()`, `AllowedToolsJSON()`, `EnvVarsJSON()`. |
| `task_test.go` | Table-driven tests for `CreateTaskRequest.Validate()` (valid input, missing fields, negative budget, harness validation, task mode validation) and `TaskStatus.IsTerminal()` for all status values. |
| `instance.go` | `Instance` struct (instance ID, type, AZ, IP, status, container counts, timestamps). `InstanceStatus` enum: `pending`, `running`, `draining`, `terminated`. |
| `sender.go` | `AllowedSender` struct (channel type, address, default repo, enabled flag, created timestamp). Used for authorizing inbound messaging (e.g. SMS) task creation. |

## `internal/notify/`

Notification system for task lifecycle events.

| File | Description |
|------|-------------|
| `webhook.go` | `Notifier` interface with a `Notify(Event)` method. `NoopNotifier` discards events. `WebhookNotifier` sends HTTP POST requests with JSON payloads, supports event filtering, and retries up to 3 times with backoff. Defines `Event` struct and event types: `task.created`, `task.running`, `task.completed`, `task.failed`, `task.needs_input`, `task.interrupted`, `task.recovering`. |
| `bus.go` | `EventBus` struct with async fan-out event delivery. `NewEventBus()` starts a delivery goroutine reading from a buffered channel (100 events). `Subscribe()` registers `Notifier` implementations. `Emit()` is non-blocking — drops events with a warning when the buffer is full. `Close()` drains pending events before returning (idempotent via `sync.Once`). Subscriber errors are logged but isolated. |
| `event.go` | `NewEvent()` constructor that populates an `Event` from a `Task` (ID, RepoURL, Prompt, redacted ReplyChannel, Timestamp). `EventOption` functional options pattern — `WithContainerStatus(prURL, message, agentLogTail)` sets post-completion fields. `redactChannel()` strips PII from reply channels (e.g. `sms:+15551234567` → `sms`). |
| `messaging.go` | `MessagingNotifier` wraps an inner `Notifier` and additionally sends SMS notifications to task creators who submitted via messaging. Looks up the task's `ReplyChannel` in the store, formats a human-readable message via `formatEventMessage()`, and sends via the `Messenger` interface. Supports event filtering. |
| `bus_test.go` | Tests for `EventBus` and `NewEvent`: fan-out delivery, subscriber isolation (one failure doesn't affect others), async delivery (Emit doesn't block), graceful shutdown (Close drains all events), no-subscribers safety, `NewEvent` field population from task, all event types, and `WithContainerStatus` option. |
| `messaging_test.go` | Tests for `MessagingNotifier`: delegation to inner notifier, error propagation, SMS sending for reply channels, skip without reply channel, event filtering, and `formatEventMessage` output for various event types. |

## `internal/messaging/`

Messaging abstraction layer (SMS via Twilio).

| File | Description |
|------|-------------|
| `messenger.go` | `Messenger` interface with `Send(ctx, OutboundMessage)`. `Channel` struct (type + address), `ChannelType` constants (`sms`). `TwilioMessenger` sends SMS via Twilio's REST API (raw HTTP, no SDK) with 3 retries and exponential backoff. `NoopMessenger` discards messages. |
| `inbound.go` | HTTP handler for inbound Twilio SMS webhooks (`/webhooks/sms/inbound`). Validates the sender against `allowed_senders`, parses the message body for a repo URL and prompt, creates a task with the sender's `reply_channel`, and returns TwiML. |
| `inbound_test.go` | Tests for inbound SMS: authorized sender creates task, unauthorized sender rejected, message parsing, default repo fallback. |
| `parse.go` | Message body parsing — extracts GitHub repo URLs and prompt text from free-form SMS messages. |
| `parse_test.go` | Tests for message parsing edge cases. |
| `twilio.go` | Twilio HTTP client for sending SMS. Constructs form-encoded POST requests to the Twilio Messages API. |

## `internal/orchestrator/`

Core orchestration loop and infrastructure management.

| File | Description |
|------|-------------|
| `orchestrator.go` | Main orchestrator. `New()` initializes sub-components based on mode (EC2, local, or Fargate). `Start()` runs the poll loop on a configurable interval. Each `tick()` delegates to `monitor.go`, `dispatch.go`, `recovery.go`, and `scaler.Evaluate()`. Fargate mode seeds a synthetic `fargate` instance and terminates stale instances from other modes. |
| `dispatch.go` | Task dispatch logic. Finds pending tasks, assigns them to instances with capacity, and starts containers. |
| `dispatch_test.go` | Tests for dispatch logic. |
| `monitor.go` | Running task monitoring. Checks container status, detects timeouts, handles completions/failures, and manages cancellations. |
| `monitor_test.go` | Tests for monitoring logic. |
| `recovery.go` | Startup recovery for orphaned tasks. Detects tasks left in `running`/`provisioning` state after a server restart, marks them as `recovering`, and resolves them by inspecting containers. |
| `recovery_test.go` | Tests for recovery logic. |
| `helpers_test.go` | Shared test helpers for orchestrator tests. |
| `scaler.go` | EC2 instance auto-scaling. Defines the `scaler` interface (`Evaluate`, `RequestScaleUp`). `Scaler` implements it for EC2 mode: launches spot instances when capacity is needed, waits for SSM + Docker readiness before marking instances as running, detects externally terminated instances, and terminates idle instances after 5 minutes. |
| `docker.go` | Container lifecycle management via SSM (EC2 mode) or local shell (local mode). Defines the `dockerClient` interface (`RunAgent`, `InspectContainer`, `StopContainer`, `GetLogs`) and `ContainerStatus` struct. `RunAgent()` builds `docker run` commands with environment variables for task config (including harness and task mode), auth credentials (Anthropic, OpenAI, GitHub). `InspectContainer()` checks container state and reads `status.json`. `StopContainer()` and `GetLogs()` wrap Docker commands. |
| `command.go` | Shell command execution. Routes to local shell or SSM based on mode. `runSSMCommand()` executes commands on remote EC2 instances via AWS SSM `SendCommand`. `isInstanceGone()` detects terminated instances and Fargate Spot interruptions (via `errSpotInterruption` sentinel). |
| `command_test.go` | Tests for `isInstanceGone` (SSM errors, sentinel spot interruption, wrapped errors), `shellEscape`, and `isHexString`. |
| `fargate.go` | Fargate container lifecycle management. Implements `dockerClient` for ECS tasks. `RunAgent()` launches ECS tasks with capacity provider strategy (Fargate Spot with on-demand fallback). `InspectContainer()` maps ECS task states to `ContainerStatus`, fetches CloudWatch logs, and parses `BACKFLOW_STATUS_JSON:` lines for agent completion status. Detects Fargate Spot interruptions via `isSpotInterruptionReason()` matching specific ECS stop reasons. |
| `fargate_test.go` | Tests for Fargate — env var building, ECS task status mapping, status JSON parsing from log events (including `Complete` field), log stream name construction, and `isSpotInterruptionReason` edge cases. |
| `ec2.go` | EC2 API wrapper. `LaunchSpotInstance()` creates one-time spot instances using either a launch template or AMI + instance type. `TerminateInstance()` and `DescribeInstance()` wrap the corresponding EC2 API calls. Lazy-initializes the AWS EC2 client. |
| `local.go` | No-op `localScaler` struct that satisfies the `scaler` interface. Used in local and Fargate modes where no EC2 instances need management. |
| `spot.go` | EC2 Spot interruption handler. `CheckInterruptions()` polls running instances for termination signals. `handleInterruption()` marks the instance as draining and re-queues all running tasks on that instance back to `pending` with an incremented retry count. |

## `internal/store/`

Persistence layer (PostgreSQL via pgxpool).

| File | Description |
|------|-------------|
| `store.go` | `Store` interface defining named update methods for tasks (`CreateTask`, `GetTask`, `ListTasks`, `DeleteTask`, `UpdateTaskStatus`, `AssignTask`, `StartTask`, `CompleteTask`, `RequeueTask`, `CancelTask`, `ClearTaskAssignment`) and instances (`CreateInstance`, `GetInstance`, `ListInstances`, `UpdateInstanceStatus`, `IncrementRunningContainers`, `DecrementRunningContainers`, `UpdateInstanceDetails`, `ResetRunningContainers`), plus `AllowedSender` CRUD, `WithTx`, and `Close()`. `TaskFilter` struct for list queries with status filter, limit, and offset. `TaskResult` struct for task completion. |
| `postgres.go` | PostgreSQL implementation of `Store` using `pgxpool`. `NewPostgres()` connects, pings, and runs goose migrations. Implements all CRUD and named update operations with `$N` placeholders, JSONB serialization for `allowed_tools` and `env_vars`, and `TIMESTAMPTZ` handling. `WithTx` provides transactional execution via `pgx.Tx`. Uses a `querier` interface to abstract over pool and transaction contexts. |
| `postgres_test.go` | Tests for PostgreSQL store using testcontainers — task round-trip (create/get with all fields), transaction commit and rollback, not-found handling, list/delete tasks, all named task updates (status, assign, start, complete, requeue, cancel, clear assignment), instance CRUD and named updates (status, increment/decrement/reset containers, update details), allowed sender CRUD, and review task mode. |

## `migrations/`

Goose SQL migration files.

| File | Description |
|------|-------------|
| `001_initial_schema.sql` | Creates `tasks`, `instances`, and `allowed_senders` tables with indexes. Uses PostgreSQL-native types: `BOOLEAN`, `DOUBLE PRECISION`, `JSONB`, `TIMESTAMPTZ`. Includes `+goose Down` to drop all tables. |

## `docker/`

Agent container image.

| File | Description |
|------|-------------|
| `Dockerfile` | Multi-arch Docker image based on `node:20-slim`. Installs git, curl, jq, Python 3, GitHub CLI, and Claude Code CLI (`@anthropic-ai/claude-code`). Creates an `agent` user, configures git defaults, and copies the entrypoint script. |
| `entrypoint.sh` | Agent lifecycle script run inside each container. Supports two modes: `code` (default) and `review` (PR review). Supports two harnesses: `claude_code` (stream-json output) and `codex` (plain text output). Clones the repo (depth 50), checks out the target branch, creates a working branch, optionally injects CLAUDE.md content, runs the selected harness with retries (up to 3 attempts), parses output for completion/needs-input/error status, writes `status.json` (for Docker-based modes) and emits a `BACKFLOW_STATUS_JSON:` log line (for Fargate log parsing), and optionally creates a PR + self-review. |

## `scripts/`

Operational and development helper scripts.

| File | Description |
|------|-------------|
| `build-agent-image.sh` | Builds and pushes the multi-arch agent Docker image to ECR. Authenticates with ECR, creates a buildx builder, and pushes with `linux/amd64,linux/arm64` platforms. |
| `create-task.sh` | CLI helper to submit tasks via the REST API. Accepts repo URL and prompt as positional args, plus flags for branch, model, effort, budget, runtime, turns, PR options, CLAUDE.md injection, context, and env vars. Builds a JSON payload with `jq` and posts to the API with `curl`. |
| `review-pr.sh` | CLI helper to submit PR review tasks via the REST API. Accepts a PR URL as a positional arg, plus flags for prompt, model, effort, budget, runtime, turns, CLAUDE.md injection, context, and env vars. Builds a JSON payload with `task_mode: "review"` and posts to the API with `curl`. |
| `setup-aws.sh` | One-time AWS infrastructure setup. Creates shared resources (ECR repo, security group, S3 bucket), EC2-mode resources (IAM role with SSM/ECR policies, instance profile, launch template), and Fargate-mode resources (CloudWatch log group, ECS task execution and task roles, ECS cluster with FARGATE/FARGATE_SPOT capacity providers, task definition). Discovers default VPC subnets. Outputs `.env` values for both EC2 and Fargate modes. |
| `user-data.sh` | EC2 instance bootstrap script (run via launch template user-data). Installs Docker and SSM agent, authenticates with ECR using IMDSv2, and pulls the `backflow-agent` image. |
