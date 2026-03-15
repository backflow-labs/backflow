# Database Schema

Backflow uses SQLite in WAL mode with busy timeout (5s) and foreign keys enabled. The schema auto-migrates on startup via `internal/store/sqlite.go:migrate()` — there are no separate migration files.

Connection string: `<path>?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on`

## Tables

### `tasks`

Stores Claude Code agent tasks submitted via the API.

| Column | Type | Default | Description |
|--------|------|---------|-------------|
| `id` | `TEXT` | — | **Primary key.** ULID with `bf_` prefix (e.g. `bf_01KKRNQCFTZJXT5XX77X1JM364`). |
| `status` | `TEXT` | `'pending'` | Task lifecycle status. See [Task statuses](#task-statuses). |
| `repo_url` | `TEXT` | — | Git repository URL to clone (required). |
| `branch` | `TEXT` | `''` | Branch to check out before running the agent. |
| `target_branch` | `TEXT` | `''` | Base branch for PR creation (e.g. `main`). |
| `prompt` | `TEXT` | — | The prompt/instructions sent to Claude Code (required). |
| `context` | `TEXT` | `''` | Additional context appended to the prompt. |
| `model` | `TEXT` | `''` | Claude model override (e.g. `claude-sonnet-4-6`). |
| `effort` | `TEXT` | `''` | Agent effort level: `low`, `medium`, `high`, or `xhigh`. |
| `max_budget_usd` | `REAL` | `0` | Maximum spend in USD (0 = unlimited). |
| `max_runtime_min` | `INTEGER` | `0` | Maximum runtime in minutes (0 = unlimited). |
| `max_turns` | `INTEGER` | `0` | Maximum conversation turns (0 = unlimited). |
| `create_pr` | `INTEGER` | `0` | Boolean (0/1). Whether to create a GitHub PR on completion. |
| `self_review` | `INTEGER` | `0` | Boolean (0/1). Whether the agent self-reviews before finishing. |
| `pr_title` | `TEXT` | `''` | Title for the created PR. |
| `pr_body` | `TEXT` | `''` | Body/description for the created PR. |
| `pr_url` | `TEXT` | `''` | URL of the created PR (populated after creation). |
| `allowed_tools` | `TEXT` | `'[]'` | JSON array of allowed Claude Code tool names. |
| `claude_md` | `TEXT` | `''` | Custom CLAUDE.md content injected into the agent container. |
| `env_vars` | `TEXT` | `'{}'` | JSON object of environment variables passed to the container. |
| `instance_id` | `TEXT` | `''` | EC2 instance ID where this task's container runs. |
| `container_id` | `TEXT` | `''` | Docker container ID on the assigned instance. |
| `retry_count` | `INTEGER` | `0` | Number of times this task has been retried (e.g. after spot interruption). |
| `cost_usd` | `REAL` | `0` | Actual cost incurred so far in USD. |
| `error` | `TEXT` | `''` | Error message if the task failed. |
| `created_at` | `TEXT` | — | RFC 3339 timestamp. When the task was created. |
| `updated_at` | `TEXT` | — | RFC 3339 timestamp. Last modification time. |
| `started_at` | `TEXT` | `NULL` | RFC 3339 timestamp. When the agent container started. Nullable. |
| `completed_at` | `TEXT` | `NULL` | RFC 3339 timestamp. When the task reached a terminal state. Nullable. |

### `instances`

Tracks EC2 spot instances provisioned by the orchestrator.

| Column | Type | Default | Description |
|--------|------|---------|-------------|
| `instance_id` | `TEXT` | — | **Primary key.** AWS EC2 instance ID (e.g. `i-0abc123`). |
| `instance_type` | `TEXT` | — | EC2 instance type (e.g. `m5.xlarge`). |
| `availability_zone` | `TEXT` | `''` | AWS availability zone (e.g. `us-east-1a`). |
| `private_ip` | `TEXT` | `''` | Private IP address within the VPC. |
| `status` | `TEXT` | `'pending'` | Instance lifecycle status. See [Instance statuses](#instance-statuses). |
| `max_containers` | `INTEGER` | `4` | Maximum number of agent containers this instance can run. |
| `running_containers` | `INTEGER` | `0` | Current number of running containers. |
| `created_at` | `TEXT` | — | RFC 3339 timestamp. When the instance record was created. |
| `updated_at` | `TEXT` | — | RFC 3339 timestamp. Last modification time. |

## Indexes

| Index | Table | Column(s) |
|-------|-------|-----------|
| `idx_tasks_status` | `tasks` | `status` |
| `idx_tasks_created` | `tasks` | `created_at` |
| `idx_instances_status` | `instances` | `status` |

## Status Enums

### Task statuses

| Status | Description |
|--------|-------------|
| `pending` | Task created, waiting for an available instance. |
| `provisioning` | Instance assigned, container being started. |
| `running` | Agent container is actively running. |
| `completed` | Agent finished successfully. Terminal state. |
| `failed` | Agent encountered an error. Terminal state. |
| `interrupted` | Task interrupted by spot instance termination. Re-queued as `pending` with incremented `retry_count`. |
| `cancelled` | Task cancelled via API. Terminal state. |

### Instance statuses

| Status | Description |
|--------|-------------|
| `pending` | Instance launched, waiting for SSM + Docker readiness. |
| `running` | Instance is ready and accepting containers. |
| `draining` | Spot interruption detected. Running tasks are being re-queued. |
| `terminated` | Instance has been terminated. |

## Notes

- **Booleans** are stored as `INTEGER` (0/1) since SQLite has no native boolean type.
- **Timestamps** are stored as `TEXT` in RFC 3339 format. `started_at` and `completed_at` are nullable; all others are required.
- **JSON columns** (`allowed_tools`, `env_vars`) are stored as serialized JSON strings. Defaults are `'[]'` and `'{}'` respectively.
- **Schema migrations** are idempotent `CREATE TABLE IF NOT EXISTS` / `CREATE INDEX IF NOT EXISTS` statements in `internal/store/sqlite.go:migrate()`. New columns should be added via `ALTER TABLE` in the same function.
