# Database Schema

Backflow uses SQLite in WAL mode with foreign keys enabled. The schema is auto-migrated on startup via `internal/store/sqlite.go:migrate()` using `CREATE TABLE IF NOT EXISTS` statements.

Connection string: `<path>?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on`

## Tables

### `tasks`

Stores Claude Code agent tasks submitted via the REST API.

| Column | Type | Nullable | Default | Description |
|--------|------|----------|---------|-------------|
| `id` | `TEXT` | No | — | **Primary key.** ULID with `bf_` prefix (e.g. `bf_01KKQW82994E87Z99QVEMBN8V0`). |
| `status` | `TEXT` | No | `'pending'` | Task lifecycle state. One of: `pending`, `provisioning`, `running`, `completed`, `failed`, `interrupted`, `cancelled`. |
| `repo_url` | `TEXT` | No | — | Git repository URL to clone (required). |
| `branch` | `TEXT` | No | `''` | Branch to check out before running the agent. |
| `target_branch` | `TEXT` | No | `''` | Base branch for PR creation (e.g. `main`). |
| `prompt` | `TEXT` | No | — | The instruction given to Claude Code (required). |
| `context` | `TEXT` | No | `''` | Additional context appended to the prompt. |
| `model` | `TEXT` | No | `''` | Claude model override (e.g. `claude-sonnet-4-20250514`). |
| `effort` | `TEXT` | No | `''` | Agent effort level. One of: `low`, `medium`, `high`, `xhigh`, or empty for default. |
| `max_budget_usd` | `REAL` | No | `0` | Maximum spend in USD. 0 = unlimited. |
| `max_runtime_min` | `INTEGER` | No | `0` | Maximum wall-clock runtime in minutes. 0 = unlimited. |
| `max_turns` | `INTEGER` | No | `0` | Maximum agent conversation turns. 0 = unlimited. |
| `create_pr` | `INTEGER` | No | `0` | Boolean (0/1). Whether to create a pull request on completion. |
| `self_review` | `INTEGER` | No | `0` | Boolean (0/1). Whether the agent self-reviews before finishing. |
| `pr_title` | `TEXT` | No | `''` | Pull request title (if `create_pr` is set). |
| `pr_body` | `TEXT` | No | `''` | Pull request body/description. |
| `pr_url` | `TEXT` | No | `''` | URL of the created PR (populated after completion). |
| `allowed_tools` | `TEXT` | No | `'[]'` | JSON array of allowed Claude Code tool names. |
| `claude_md` | `TEXT` | No | `''` | Custom CLAUDE.md content injected into the agent container. |
| `env_vars` | `TEXT` | No | `'{}'` | JSON object of environment variables passed to the container. |
| `instance_id` | `TEXT` | No | `''` | EC2 instance ID where the container runs. |
| `container_id` | `TEXT` | No | `''` | Docker container ID on the assigned instance. |
| `retry_count` | `INTEGER` | No | `0` | Number of times this task has been re-queued (e.g. after spot interruption). |
| `cost_usd` | `REAL` | No | `0` | Tracked cost in USD. |
| `error` | `TEXT` | No | `''` | Error message if the task failed. |
| `created_at` | `TEXT` | No | — | RFC 3339 timestamp. When the task was created. |
| `updated_at` | `TEXT` | No | — | RFC 3339 timestamp. Last modification time. |
| `started_at` | `TEXT` | **Yes** | `NULL` | RFC 3339 timestamp. When the agent container started. |
| `completed_at` | `TEXT` | **Yes** | `NULL` | RFC 3339 timestamp. When the task reached a terminal state. |

**Indexes:**
- `idx_tasks_status` on `status` — used by the orchestrator to find pending/running tasks.
- `idx_tasks_created` on `created_at` — used for ordered listing.

### `instances`

Tracks EC2 spot instances managed by the orchestrator.

| Column | Type | Nullable | Default | Description |
|--------|------|----------|---------|-------------|
| `instance_id` | `TEXT` | No | — | **Primary key.** AWS EC2 instance ID (e.g. `i-0abc123def456`). |
| `instance_type` | `TEXT` | No | — | EC2 instance type (e.g. `c6g.2xlarge`). |
| `availability_zone` | `TEXT` | No | `''` | AWS AZ (e.g. `us-east-1a`). |
| `private_ip` | `TEXT` | No | `''` | Instance private IP address. |
| `status` | `TEXT` | No | `'pending'` | Instance lifecycle state. One of: `pending`, `running`, `draining`, `terminated`. |
| `max_containers` | `INTEGER` | No | `4` | Maximum concurrent agent containers on this instance. |
| `running_containers` | `INTEGER` | No | `0` | Current number of running containers. |
| `created_at` | `TEXT` | No | — | RFC 3339 timestamp. When the instance record was created. |
| `updated_at` | `TEXT` | No | — | RFC 3339 timestamp. Last modification time. |

**Indexes:**
- `idx_instances_status` on `status` — used to find running/pending instances for task dispatch.

## Status Lifecycles

### Task statuses

```
pending → provisioning → running → completed
                                  → failed
                                  → interrupted → (re-queued as pending)
         (any non-terminal)      → cancelled
```

Terminal states: `completed`, `failed`, `cancelled`.

### Instance statuses

```
pending → running → draining → terminated
                  → terminated
```

## Notes

- All columns are `NOT NULL` unless marked as **Nullable** — the only nullable columns are `tasks.started_at` and `tasks.completed_at`.
- All timestamps are stored as RFC 3339 strings, not SQLite datetime types.
- Booleans (`create_pr`, `self_review`) are stored as integers (0/1).
- JSON fields (`allowed_tools`, `env_vars`) are stored as serialized TEXT.
- Schema changes are applied idempotently in `migrate()` — new columns use `ALTER TABLE ... ADD COLUMN` with `IF NOT EXISTS` semantics.
