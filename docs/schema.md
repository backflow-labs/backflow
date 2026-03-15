# Database Schema

SQLite database with WAL mode, busy timeout of 5000ms, and foreign keys enabled. Schema is auto-migrated on startup via `internal/store/sqlite.go:migrate()`.

## Tables

### `tasks`

Tracks Claude Code agent tasks from creation through completion.

| Column | Type | Default | Description |
|--------|------|---------|-------------|
| `id` | `TEXT` | — | **Primary key.** ULID with `bf_` prefix (e.g. `bf_01KKQW82994E87Z99QVEMBN8V0`). |
| `status` | `TEXT` | `'pending'` | Task lifecycle state. One of: `pending`, `provisioning`, `running`, `completed`, `failed`, `interrupted`, `cancelled`. |
| `repo_url` | `TEXT` | — | Git repository URL to clone (required). |
| `branch` | `TEXT` | `''` | Branch to check out before running the agent. |
| `target_branch` | `TEXT` | `''` | Base branch for the PR (e.g. `main`). |
| `prompt` | `TEXT` | — | The instruction sent to Claude Code (required). |
| `context` | `TEXT` | `''` | Additional context appended to the prompt. |
| `model` | `TEXT` | `''` | Claude model to use (e.g. `claude-sonnet-4-20250514`). Empty uses the agent default. |
| `effort` | `TEXT` | `''` | Agent effort level. One of: `low`, `medium`, `high`, `xhigh`, or empty. |
| `max_budget_usd` | `REAL` | `0` | Maximum spend in USD. `0` means unlimited. |
| `max_runtime_min` | `INTEGER` | `0` | Maximum runtime in minutes. `0` means unlimited. |
| `max_turns` | `INTEGER` | `0` | Maximum agent conversation turns. `0` means unlimited. |
| `create_pr` | `INTEGER` | `0` | Boolean (0/1). Whether to create a GitHub PR on completion. |
| `self_review` | `INTEGER` | `0` | Boolean (0/1). Whether the agent should self-review before finishing. |
| `pr_title` | `TEXT` | `''` | Title for the created PR. |
| `pr_body` | `TEXT` | `''` | Body/description for the created PR. |
| `pr_url` | `TEXT` | `''` | URL of the PR after creation. |
| `allowed_tools` | `TEXT` | `'[]'` | JSON array of tool names the agent is allowed to use. |
| `claude_md` | `TEXT` | `''` | Custom CLAUDE.md content injected into the agent workspace. |
| `env_vars` | `TEXT` | `'{}'` | JSON object of environment variables passed to the agent container. |
| `instance_id` | `TEXT` | `''` | EC2 instance ID where the agent container runs. |
| `container_id` | `TEXT` | `''` | Docker container ID for this task. |
| `retry_count` | `INTEGER` | `0` | Number of times this task has been retried (e.g. after spot interruption). |
| `cost_usd` | `REAL` | `0` | Accumulated cost in USD. |
| `error` | `TEXT` | `''` | Error message if the task failed. |
| `created_at` | `TEXT` | — | RFC 3339 timestamp. When the task was created. |
| `updated_at` | `TEXT` | — | RFC 3339 timestamp. Last modification time. |
| `started_at` | `TEXT` | `NULL` | RFC 3339 timestamp. When the agent started running. NULL if not yet started. |
| `completed_at` | `TEXT` | `NULL` | RFC 3339 timestamp. When the task reached a terminal state. NULL if still active. |

**Indexes:**
- `idx_tasks_status` on `status` — used by the orchestrator to find pending/running tasks.
- `idx_tasks_created` on `created_at` — used for ordered listing.

### `instances`

Tracks EC2 spot instances provisioned by the orchestrator.

| Column | Type | Default | Description |
|--------|------|---------|-------------|
| `instance_id` | `TEXT` | — | **Primary key.** AWS EC2 instance ID (e.g. `i-0abc123def456`). |
| `instance_type` | `TEXT` | — | EC2 instance type (e.g. `m7g.xlarge`). |
| `availability_zone` | `TEXT` | `''` | AWS availability zone (e.g. `us-east-1a`). |
| `private_ip` | `TEXT` | `''` | Private IP address within the VPC. |
| `status` | `TEXT` | `'pending'` | Instance lifecycle state. One of: `pending`, `running`, `draining`, `terminated`. |
| `max_containers` | `INTEGER` | `4` | Maximum number of agent containers this instance can run. |
| `running_containers` | `INTEGER` | `0` | Current number of running agent containers. |
| `created_at` | `TEXT` | — | RFC 3339 timestamp. When the instance record was created. |
| `updated_at` | `TEXT` | — | RFC 3339 timestamp. Last modification time. |

**Indexes:**
- `idx_instances_status` on `status` — used by the scaler to find running/pending instances.

## Status Lifecycles

### Task statuses

```
pending → provisioning → running → completed
                                  → failed
                                  → interrupted (spot termination, re-queued to pending)
              (any) ──────────────→ cancelled
```

Terminal states: `completed`, `failed`, `cancelled`.

### Instance statuses

```
pending → running → draining → terminated
```

## Notes

- All timestamps are stored as RFC 3339 strings, not SQLite datetime types.
- Booleans (`create_pr`, `self_review`) are stored as integers (0/1).
- `allowed_tools` and `env_vars` are stored as JSON-encoded strings.
- Schema changes are applied idempotently in `migrate()` using `CREATE TABLE IF NOT EXISTS` and `CREATE INDEX IF NOT EXISTS`. New columns should use `ALTER TABLE ... ADD COLUMN` in the same function.
