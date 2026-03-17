# Database Schema

Backflow uses PostgreSQL. The schema is defined in [migrations/001_initial_schema.sql](../migrations/001_initial_schema.sql) and applied automatically on startup via goose.

Connection string: `BACKFLOW_DATABASE_URL`

## Tables

### `tasks`

Stores submitted agent work.

- Primary key: `id` (`TEXT`)
- Status and mode fields: `status`, `task_mode`, `harness`
- Repo and PR fields: `repo_url`, `branch`, `target_branch`, `review_pr_url`, `review_pr_number`, `pr_title`, `pr_body`, `pr_url`
- Execution config: `prompt`, `context`, `model`, `effort`, `max_budget_usd`, `max_runtime_min`, `max_turns`, `create_pr`, `self_review`, `save_agent_output`
- Agent output metadata: `output_url`, `cost_usd`, `elapsed_time_sec`, `error`
- JSON fields: `allowed_tools` (`JSONB`), `env_vars` (`JSONB`)
- Assignment fields: `instance_id`, `container_id`, `retry_count`, `reply_channel`
- Timestamps: `created_at`, `updated_at`, `started_at`, `completed_at` (`TIMESTAMPTZ`)

Indexes:

- `idx_tasks_status` on `status`
- `idx_tasks_created` on `created_at`

### `instances`

Tracks orchestration capacity.

- Primary key: `instance_id` (`TEXT`)
- Metadata: `instance_type`, `availability_zone`, `private_ip`
- Capacity fields: `status`, `max_containers`, `running_containers`
- Timestamps: `created_at`, `updated_at` (`TIMESTAMPTZ`)

Indexes:

- `idx_instances_status` on `status`

### `allowed_senders`

Pre-registered messaging senders allowed to create tasks.

- Composite primary key: `channel_type`, `address`
- Metadata: `default_repo`, `enabled`
- Timestamp: `created_at` (`TIMESTAMPTZ`)

## Type Mapping

- Timestamps use `TIMESTAMPTZ`
- Booleans use `BOOLEAN`
- Structured fields use `JSONB`
- Floating point values use `DOUBLE PRECISION`

## Status Lifecycles

### Task statuses

```
pending -> provisioning -> running -> completed
                                 -> failed
                                 -> interrupted -> (re-queued as pending)
        (any non-terminal)      -> cancelled
running/provisioning -> recovering -> running
                                 -> completed/failed
                                 -> pending
```

Terminal states: `completed`, `failed`, `cancelled`.

### Instance statuses

```
pending -> running -> draining -> terminated
                 -> terminated
```
