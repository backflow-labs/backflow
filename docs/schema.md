# Database Schema

Backflow uses SQLite in WAL mode with foreign keys enabled. The schema is auto-migrated on startup via `internal/store/sqlite.go:migrate()` using `CREATE TABLE IF NOT EXISTS`.

Connection string flags: `_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on`

## Tables

### `tasks`

Tracks agent tasks from creation through completion.

| Column | Type | Default | Description |
|--------|------|---------|-------------|
| `id` | `TEXT` | — | Primary key. ULID with `bf_` prefix (e.g. `bf_01ABC...`). |
| `status` | `TEXT` | `'pending'` | Current task status. See [Task statuses](#task-statuses). |
| `repo_url` | `TEXT` | — | Git repository URL to clone. Required. |
| `branch` | `TEXT` | `''` | Branch to check out before running the agent. |
| `target_branch` | `TEXT` | `''` | Base branch for the PR (e.g. `main`). |
| `prompt` | `TEXT` | — | The prompt sent to Claude Code. Required. |
| `context` | `TEXT` | `''` | Additional context appended to the prompt. |
| `model` | `TEXT` | `''` | Claude model to use (e.g. `claude-sonnet-4-6`). |
| `effort` | `TEXT` | `''` | Agent effort level: `low`, `medium`, `high`, or `xhigh`. |
| `max_budget_usd` | `REAL` | `0` | Maximum spend in USD. 0 = unlimited. |
| `max_runtime_min` | `INTEGER` | `0` | Maximum runtime in minutes. 0 = unlimited. |
| `max_turns` | `INTEGER` | `0` | Maximum agent conversation turns. 0 = unlimited. |
| `create_pr` | `INTEGER` | `0` | Boolean (0/1). Whether to create a PR on completion. |
| `self_review` | `INTEGER` | `0` | Boolean (0/1). Whether the agent self-reviews before finishing. |
| `pr_title` | `TEXT` | `''` | Pull request title (set by agent on completion). |
| `pr_body` | `TEXT` | `''` | Pull request body (set by agent on completion). |
| `pr_url` | `TEXT` | `''` | URL of the created PR (set by agent on completion). |
| `allowed_tools` | `TEXT` | `'[]'` | JSON array of allowed Claude Code tool names. |
| `claude_md` | `TEXT` | `''` | Custom CLAUDE.md content injected into the agent container. |
| `env_vars` | `TEXT` | `'{}'` | JSON object of environment variables passed to the container. |
| `instance_id` | `TEXT` | `''` | EC2 instance ID where the agent is running. |
| `container_id` | `TEXT` | `''` | Docker container ID on the assigned instance. |
| `retry_count` | `INTEGER` | `0` | Number of times this task has been retried (e.g. after spot interruption). |
| `cost_usd` | `REAL` | `0` | Accumulated cost in USD. |
| `error` | `TEXT` | `''` | Error message if the task failed. |
| `created_at` | `TEXT` | — | RFC 3339 timestamp. Set on creation. |
| `updated_at` | `TEXT` | — | RFC 3339 timestamp. Updated on every write. |
| `started_at` | `TEXT` | `NULL` | RFC 3339 timestamp. Set when the agent container starts. |
| `completed_at` | `TEXT` | `NULL` | RFC 3339 timestamp. Set on terminal status. |

**Indexes:**

- `idx_tasks_status` — on `status`
- `idx_tasks_created` — on `created_at`

### `instances`

Tracks EC2 spot instances managed by the orchestrator.

| Column | Type | Default | Description |
|--------|------|---------|-------------|
| `instance_id` | `TEXT` | — | Primary key. AWS EC2 instance ID. |
| `instance_type` | `TEXT` | — | EC2 instance type (e.g. `m5.xlarge`). |
| `availability_zone` | `TEXT` | `''` | AWS availability zone. |
| `private_ip` | `TEXT` | `''` | Private IP address within the VPC. |
| `status` | `TEXT` | `'pending'` | Current instance status. See [Instance statuses](#instance-statuses). |
| `max_containers` | `INTEGER` | `4` | Maximum number of agent containers this instance can run. |
| `running_containers` | `INTEGER` | `0` | Current number of running agent containers. |
| `created_at` | `TEXT` | — | RFC 3339 timestamp. Set on creation. |
| `updated_at` | `TEXT` | — | RFC 3339 timestamp. Updated on every write. |

**Indexes:**

- `idx_instances_status` — on `status`

## Status Enums

### Task statuses

```
pending → provisioning → running → completed
                                  → failed
                                  → interrupted
                                  → cancelled
```

| Status | Description |
|--------|-------------|
| `pending` | Waiting for an available instance. |
| `provisioning` | Instance assigned, container starting. |
| `running` | Agent container is executing. |
| `completed` | Agent finished successfully. Terminal. |
| `failed` | Agent or container errored. Terminal. |
| `interrupted` | Spot instance was reclaimed mid-run. Task may be re-queued. |
| `cancelled` | Cancelled via API. Terminal. |

### Instance statuses

```
pending → running → draining → terminated
```

| Status | Description |
|--------|-------------|
| `pending` | Instance launched, waiting for SSM + Docker readiness. |
| `running` | Ready to accept agent containers. |
| `draining` | Spot interruption detected; running tasks are being re-queued. |
| `terminated` | Instance terminated and cleaned up. |
