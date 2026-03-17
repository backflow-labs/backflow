# Database Schema

Backflow uses Postgres. The schema lives in [`migrations/001_initial_schema.sql`](/home/agent/workspace/migrations/001_initial_schema.sql) and is applied on startup with goose.

Connection string: `postgres://user:password@host:5432/database?sslmode=disable`

## Tables

### `tasks`

Stores agent tasks submitted via the REST API.

| Column | Type | Default | Description |
|--------|------|---------|-------------|
| `id` | `TEXT` | — | **Primary key.** ULID with `bf_` prefix (e.g. `bf_01KKQW82994E87Z99QVEMBN8V0`). |
| `status` | `TEXT` | `'pending'` | Task lifecycle state. One of: `pending`, `provisioning`, `running`, `completed`, `failed`, `interrupted`, `cancelled`, `recovering`. |
| `task_mode` | `TEXT` | `'code'` | Task mode. `code` (default) or `review` (PR review). |
| `harness` | `TEXT` | `'claude_code'` | Agent CLI harness. `claude_code` (default) or `codex`. |
| `repo_url` | `TEXT` | — | Git repository URL to clone (required). |
| `branch` | `TEXT` | `''` | Branch to check out before running the agent. |
| `target_branch` | `TEXT` | `''` | Base branch for PR creation (e.g. `main`). |
| `review_pr_url` | `TEXT` | `''` | Full URL of the pull request under review. |
| `review_pr_number` | `INTEGER` | `0` | PR number to review (used when `task_mode` is `review`). |
| `prompt` | `TEXT` | — | The instruction given to the agent (required). |
| `context` | `TEXT` | `''` | Additional context appended to the prompt. |
| `model` | `TEXT` | `''` | Model override (e.g. `claude-sonnet-4-6`, `gpt-5.4`). |
| `effort` | `TEXT` | `''` | Agent effort level. One of: `low`, `medium`, `high`, `xhigh`, or empty for default. |
| `max_budget_usd` | `DOUBLE PRECISION` | `0` | Maximum spend in USD. 0 = unlimited. |
| `max_runtime_min` | `INTEGER` | `0` | Maximum wall-clock runtime in minutes. 0 = unlimited. |
| `max_turns` | `INTEGER` | `0` | Maximum agent conversation turns. 0 = unlimited. |
| `create_pr` | `BOOLEAN` | `FALSE` | Whether to create a pull request on completion. |
| `self_review` | `BOOLEAN` | `FALSE` | Whether the agent self-reviews before finishing. |
| `save_agent_output` | `BOOLEAN` | `TRUE` | Whether to persist agent output artifacts when available. |
| `pr_title` | `TEXT` | `''` | Pull request title (if `create_pr` is set). |
| `pr_body` | `TEXT` | `''` | Pull request body/description. |
| `pr_url` | `TEXT` | `''` | URL of the created PR (populated after completion). |
| `output_url` | `TEXT` | `''` | Artifact/log URL captured on completion. |
| `allowed_tools` | `JSONB` | `'[]'` | JSON array of allowed tool names. |
| `claude_md` | `TEXT` | `''` | Custom CLAUDE.md content injected into the agent container. |
| `env_vars` | `JSONB` | `'{}'` | JSON object of environment variables passed to the container. |
| `instance_id` | `TEXT` | `''` | EC2 instance ID where the container runs. |
| `container_id` | `TEXT` | `''` | Docker container ID on the assigned instance. |
| `retry_count` | `INTEGER` | `0` | Number of times this task has been re-queued (e.g. after spot interruption). |
| `cost_usd` | `DOUBLE PRECISION` | `0` | Tracked cost in USD. |
| `elapsed_time_sec` | `INTEGER` | `0` | Task runtime captured on completion. |
| `error` | `TEXT` | `''` | Error message if the task failed. |
| `reply_channel` | `TEXT` | `''` | Messaging reply channel (e.g. `sms:+15551234567`). Set when task is created via SMS. |
| `created_at` | `TIMESTAMPTZ` | — | When the task was created. |
| `updated_at` | `TIMESTAMPTZ` | — | Last modification time. |
| `started_at` | `TIMESTAMPTZ` | `NULL` | When the agent container started. Nullable. |
| `completed_at` | `TIMESTAMPTZ` | `NULL` | When the task reached a terminal state. Nullable. |

**Indexes:**
- `idx_tasks_status` on `status` — used by the orchestrator to find pending/running tasks.
- `idx_tasks_created` on `created_at` — used for ordered listing.

### `instances`

Tracks EC2 spot instances managed by the orchestrator.

| Column | Type | Default | Description |
|--------|------|---------|-------------|
| `instance_id` | `TEXT` | — | **Primary key.** AWS EC2 instance ID (e.g. `i-0abc123def456`). |
| `instance_type` | `TEXT` | — | EC2 instance type (e.g. `c6g.2xlarge`). |
| `availability_zone` | `TEXT` | `''` | AWS AZ (e.g. `us-east-1a`). |
| `private_ip` | `TEXT` | `''` | Instance private IP address. |
| `status` | `TEXT` | `'pending'` | Instance lifecycle state. One of: `pending`, `running`, `draining`, `terminated`. |
| `max_containers` | `INTEGER` | `4` | Maximum concurrent agent containers on this instance. |
| `running_containers` | `INTEGER` | `0` | Current number of running containers. |
| `created_at` | `TIMESTAMPTZ` | — | When the instance record was created. |
| `updated_at` | `TIMESTAMPTZ` | — | Last modification time. |

**Indexes:**
- `idx_instances_status` on `status` — used to find running/pending instances for task dispatch.

## Status Lifecycles

### Task statuses

```
pending → provisioning → running → completed
                                  → failed
                                  → interrupted → (re-queued as pending)
         (any non-terminal)      → cancelled
running/provisioning → recovering → running (container still alive)
                                  → completed/failed (container exited)
                                  → pending (re-queued, container/instance gone)
```

Terminal states: `completed`, `failed`, `cancelled`.

The `recovering` status is set on startup for tasks orphaned by a server restart. The orchestrator inspects their containers and resolves them on each tick.

### Instance statuses

```
pending → running → draining → terminated
                  → terminated
```

### `allowed_senders`

Pre-registered senders authorized to create tasks via messaging (e.g. SMS).

| Column | Type | Default | Description |
|--------|------|---------|-------------|
| `channel_type` | `TEXT` | — | **Composite PK.** Messaging channel type (e.g. `sms`). |
| `address` | `TEXT` | — | **Composite PK.** Sender address (e.g. `+15551234567`). |
| `default_repo` | `TEXT` | `''` | Default repo URL when sender omits it from the message. |
| `enabled` | `BOOLEAN` | `TRUE` | Whether this sender is allowed to create tasks. |
| `created_at` | `TIMESTAMPTZ` | — | When the sender was registered. |

**Primary key:** `(channel_type, address)`

## Notes

- All timestamps are stored as native `TIMESTAMPTZ`.
- Booleans (`create_pr`, `self_review`, `save_agent_output`, `enabled`) are stored as native `BOOLEAN`.
- JSON fields (`allowed_tools`, `env_vars`) are stored as `JSONB`.
- Schema changes are applied with new goose migration files rather than ad hoc in-code `ALTER TABLE` statements.
