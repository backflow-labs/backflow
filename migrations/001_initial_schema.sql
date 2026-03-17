-- +goose Up
CREATE TABLE IF NOT EXISTS tasks (
    id                TEXT PRIMARY KEY,
    status            TEXT NOT NULL DEFAULT 'pending',
    task_mode         TEXT NOT NULL DEFAULT 'code',
    harness           TEXT NOT NULL DEFAULT 'claude_code',
    repo_url          TEXT NOT NULL,
    branch            TEXT NOT NULL DEFAULT '',
    target_branch     TEXT NOT NULL DEFAULT '',
    review_pr_url     TEXT NOT NULL DEFAULT '',
    review_pr_number  INTEGER NOT NULL DEFAULT 0,
    prompt            TEXT NOT NULL,
    context           TEXT NOT NULL DEFAULT '',
    model             TEXT NOT NULL DEFAULT '',
    effort            TEXT NOT NULL DEFAULT '',
    max_budget_usd    DOUBLE PRECISION NOT NULL DEFAULT 0,
    max_runtime_min   INTEGER NOT NULL DEFAULT 0,
    max_turns         INTEGER NOT NULL DEFAULT 0,
    create_pr         BOOLEAN NOT NULL DEFAULT FALSE,
    self_review       BOOLEAN NOT NULL DEFAULT FALSE,
    save_agent_output BOOLEAN NOT NULL DEFAULT TRUE,
    pr_title          TEXT NOT NULL DEFAULT '',
    pr_body           TEXT NOT NULL DEFAULT '',
    pr_url            TEXT NOT NULL DEFAULT '',
    output_url        TEXT NOT NULL DEFAULT '',
    allowed_tools     JSONB NOT NULL DEFAULT '[]'::jsonb,
    claude_md         TEXT NOT NULL DEFAULT '',
    env_vars          JSONB NOT NULL DEFAULT '{}'::jsonb,
    instance_id       TEXT NOT NULL DEFAULT '',
    container_id      TEXT NOT NULL DEFAULT '',
    retry_count       INTEGER NOT NULL DEFAULT 0,
    cost_usd          DOUBLE PRECISION NOT NULL DEFAULT 0,
    elapsed_time_sec  INTEGER NOT NULL DEFAULT 0,
    error             TEXT NOT NULL DEFAULT '',
    reply_channel     TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL,
    updated_at        TIMESTAMPTZ NOT NULL,
    started_at        TIMESTAMPTZ,
    completed_at      TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_created ON tasks(created_at);

CREATE TABLE IF NOT EXISTS instances (
    instance_id        TEXT PRIMARY KEY,
    instance_type      TEXT NOT NULL,
    availability_zone  TEXT NOT NULL DEFAULT '',
    private_ip         TEXT NOT NULL DEFAULT '',
    status             TEXT NOT NULL DEFAULT 'pending',
    max_containers     INTEGER NOT NULL DEFAULT 4,
    running_containers INTEGER NOT NULL DEFAULT 0,
    created_at         TIMESTAMPTZ NOT NULL,
    updated_at         TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_instances_status ON instances(status);

CREATE TABLE IF NOT EXISTS allowed_senders (
    channel_type TEXT NOT NULL,
    address      TEXT NOT NULL,
    default_repo TEXT NOT NULL DEFAULT '',
    enabled      BOOLEAN NOT NULL DEFAULT TRUE,
    created_at   TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (channel_type, address)
);

-- +goose Down
DROP TABLE IF EXISTS allowed_senders;
DROP INDEX IF EXISTS idx_instances_status;
DROP TABLE IF EXISTS instances;
DROP INDEX IF EXISTS idx_tasks_created;
DROP INDEX IF EXISTS idx_tasks_status;
DROP TABLE IF EXISTS tasks;
