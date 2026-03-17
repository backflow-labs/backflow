package main

import (
	"bytes"
	"context"
	"database/sql"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/backflow-labs/backflow/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	_ "modernc.org/sqlite"
)

const legacySQLiteSchema = `
CREATE TABLE tasks (
	id               TEXT PRIMARY KEY,
	status           TEXT NOT NULL DEFAULT 'pending',
	task_mode        TEXT NOT NULL DEFAULT 'code',
	harness          TEXT NOT NULL DEFAULT 'claude_code',
	repo_url         TEXT NOT NULL,
	branch           TEXT NOT NULL DEFAULT '',
	target_branch    TEXT NOT NULL DEFAULT '',
	review_pr_url    TEXT NOT NULL DEFAULT '',
	review_pr_number INTEGER NOT NULL DEFAULT 0,
	prompt           TEXT NOT NULL,
	context          TEXT NOT NULL DEFAULT '',
	model            TEXT NOT NULL DEFAULT '',
	effort           TEXT NOT NULL DEFAULT '',
	max_budget_usd   REAL NOT NULL DEFAULT 0,
	max_runtime_min  INTEGER NOT NULL DEFAULT 0,
	max_turns        INTEGER NOT NULL DEFAULT 0,
	create_pr        INTEGER NOT NULL DEFAULT 0,
	self_review      INTEGER NOT NULL DEFAULT 0,
	save_agent_output INTEGER NOT NULL DEFAULT 1,
	pr_title         TEXT NOT NULL DEFAULT '',
	pr_body          TEXT NOT NULL DEFAULT '',
	pr_url           TEXT NOT NULL DEFAULT '',
	output_url       TEXT NOT NULL DEFAULT '',
	allowed_tools    TEXT NOT NULL DEFAULT '[]',
	claude_md        TEXT NOT NULL DEFAULT '',
	env_vars         TEXT NOT NULL DEFAULT '{}',
	instance_id      TEXT NOT NULL DEFAULT '',
	container_id     TEXT NOT NULL DEFAULT '',
	retry_count      INTEGER NOT NULL DEFAULT 0,
	cost_usd         REAL NOT NULL DEFAULT 0,
	elapsed_time_sec INTEGER NOT NULL DEFAULT 0,
	error            TEXT NOT NULL DEFAULT '',
	reply_channel    TEXT NOT NULL DEFAULT '',
	created_at       TEXT NOT NULL,
	updated_at       TEXT NOT NULL,
	started_at       TEXT,
	completed_at     TEXT
);

CREATE TABLE instances (
	instance_id        TEXT PRIMARY KEY,
	instance_type      TEXT NOT NULL,
	availability_zone  TEXT NOT NULL DEFAULT '',
	private_ip         TEXT NOT NULL DEFAULT '',
	status             TEXT NOT NULL DEFAULT 'pending',
	max_containers     INTEGER NOT NULL DEFAULT 4,
	running_containers INTEGER NOT NULL DEFAULT 0,
	created_at         TEXT NOT NULL,
	updated_at         TEXT NOT NULL
);

CREATE TABLE allowed_senders (
	channel_type TEXT NOT NULL,
	address      TEXT NOT NULL,
	default_repo TEXT NOT NULL DEFAULT '',
	enabled      INTEGER NOT NULL DEFAULT 1,
	created_at   TEXT NOT NULL,
	PRIMARY KEY (channel_type, address)
);
`

var (
	sharedConnStr string
	truncatePool  *pgxpool.Pool
)

func TestMain(m *testing.M) {
	ctx := context.Background()

	pgContainer, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("backflow_test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		log.Fatalf("start postgres container: %v", err)
	}

	sharedConnStr, err = pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		log.Fatalf("get connection string: %v", err)
	}

	if err := applyPostgresMigrations(sharedConnStr); err != nil {
		log.Fatalf("apply migrations: %v", err)
	}

	truncatePool, err = pgxpool.New(ctx, sharedConnStr)
	if err != nil {
		log.Fatalf("truncate pool: %v", err)
	}

	code := m.Run()

	truncatePool.Close()
	pgContainer.Terminate(ctx)
	os.Exit(code)
}

func TestRunMigratesSQLiteDataToPostgres(t *testing.T) {
	resetTargetDB(t)
	sqlitePath := writeLegacySQLiteFixture(t)

	var logs bytes.Buffer
	results, err := run(context.Background(), config{
		SQLitePath:  sqlitePath,
		PostgresURL: sharedConnStr,
	}, log.New(&logs, "", 0))
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	assertTableResult(t, results, "tasks", 2, 2)
	assertTableResult(t, results, "instances", 1, 1)
	assertTableResult(t, results, "allowed_senders", 1, 1)

	logOutput := logs.String()
	for _, want := range []string{
		`table=tasks read=2 inserted=2`,
		`table=instances read=1 inserted=1`,
		`table=allowed_senders read=1 inserted=1`,
		`migration complete`,
	} {
		if !strings.Contains(logOutput, want) {
			t.Fatalf("log output missing %q: %s", want, logOutput)
		}
	}

	s := testStore(t)
	ctx := context.Background()

	taskOne, err := s.GetTask(ctx, "bf_migrate_001")
	if err != nil {
		t.Fatalf("GetTask(taskOne): %v", err)
	}
	taskOneCreated := time.Date(2025, 12, 25, 14, 0, 0, 0, time.FixedZone("PST", -8*60*60))
	taskOneUpdated := taskOneCreated.Add(3 * time.Minute)
	taskOneStarted := taskOneCreated.Add(5 * time.Minute)
	taskOneCompleted := taskOneCreated.Add(37 * time.Minute)
	if !taskOne.CreatedAt.Equal(taskOneCreated) {
		t.Fatalf("taskOne.CreatedAt = %v, want %v", taskOne.CreatedAt, taskOneCreated)
	}
	if !taskOne.UpdatedAt.Equal(taskOneUpdated) {
		t.Fatalf("taskOne.UpdatedAt = %v, want %v", taskOne.UpdatedAt, taskOneUpdated)
	}
	if taskOne.StartedAt == nil || !taskOne.StartedAt.Equal(taskOneStarted) {
		t.Fatalf("taskOne.StartedAt = %v, want %v", taskOne.StartedAt, taskOneStarted)
	}
	if taskOne.CompletedAt == nil || !taskOne.CompletedAt.Equal(taskOneCompleted) {
		t.Fatalf("taskOne.CompletedAt = %v, want %v", taskOne.CompletedAt, taskOneCompleted)
	}
	if taskOne.Status != "completed" || taskOne.TaskMode != "code" || taskOne.Harness != "claude_code" {
		t.Fatalf("unexpected taskOne identity fields: %+v", taskOne)
	}
	if taskOne.ReviewPRNumber != 42 || taskOne.ReviewPRURL != "https://github.com/acme/backflow/pull/42" {
		t.Fatalf("unexpected review metadata: %+v", taskOne)
	}
	if !taskOne.CreatePR || taskOne.SelfReview || !taskOne.SaveAgentOutput {
		t.Fatalf("unexpected boolean fields on taskOne: %+v", taskOne)
	}
	if taskOne.MaxBudgetUSD != 12.75 || taskOne.CostUSD != 3.5 {
		t.Fatalf("unexpected float fields on taskOne: %+v", taskOne)
	}
	if taskOne.ElapsedTimeSec != 1800 || taskOne.MaxRuntimeMin != 45 || taskOne.MaxTurns != 150 {
		t.Fatalf("unexpected integer fields on taskOne: %+v", taskOne)
	}
	if taskOne.ReplyChannel != "sms:+15551234567" || taskOne.OutputURL != "s3://bucket/output/one.json" {
		t.Fatalf("unexpected output fields on taskOne: %+v", taskOne)
	}
	if len(taskOne.AllowedTools) != 3 || taskOne.AllowedTools[0] != "Read" || taskOne.AllowedTools[2] != "Bash" {
		t.Fatalf("unexpected allowed tools on taskOne: %+v", taskOne.AllowedTools)
	}
	if taskOne.EnvVars["FOO"] != "bar" || taskOne.EnvVars["BAZ"] != "qux" {
		t.Fatalf("unexpected env vars on taskOne: %+v", taskOne.EnvVars)
	}

	taskTwo, err := s.GetTask(ctx, "bf_migrate_002")
	if err != nil {
		t.Fatalf("GetTask(taskTwo): %v", err)
	}
	taskTwoCreated := time.Date(2026, 1, 2, 9, 30, 0, 0, time.UTC)
	taskTwoUpdated := taskTwoCreated.Add(2 * time.Minute)
	if !taskTwo.CreatedAt.Equal(taskTwoCreated) || !taskTwo.UpdatedAt.Equal(taskTwoUpdated) {
		t.Fatalf("unexpected taskTwo timestamps: %+v", taskTwo)
	}
	if taskTwo.StartedAt != nil || taskTwo.CompletedAt != nil {
		t.Fatalf("expected nil nullable timestamps on taskTwo: %+v", taskTwo)
	}
	if taskTwo.TaskMode != "review" || taskTwo.Harness != "codex" {
		t.Fatalf("unexpected taskTwo mode fields: %+v", taskTwo)
	}
	if taskTwo.CreatePR || !taskTwo.SelfReview || taskTwo.SaveAgentOutput {
		t.Fatalf("unexpected taskTwo boolean fields: %+v", taskTwo)
	}
	if len(taskTwo.AllowedTools) != 0 || len(taskTwo.EnvVars) != 0 {
		t.Fatalf("expected empty JSON fields on taskTwo: tools=%v env=%v", taskTwo.AllowedTools, taskTwo.EnvVars)
	}

	instance, err := s.GetInstance(ctx, "i-migrate-001")
	if err != nil {
		t.Fatalf("GetInstance: %v", err)
	}
	instanceCreated := time.Date(2025, 12, 24, 10, 0, 0, 0, time.FixedZone("MST", -7*60*60))
	instanceUpdated := instanceCreated.Add(15 * time.Minute)
	if !instance.CreatedAt.Equal(instanceCreated) || !instance.UpdatedAt.Equal(instanceUpdated) {
		t.Fatalf("unexpected instance timestamps: %+v", instance)
	}
	if instance.InstanceType != "c7g.large" || instance.MaxContainers != 6 || instance.RunningContainers != 2 {
		t.Fatalf("unexpected instance fields: %+v", instance)
	}

	sender, err := s.GetAllowedSender(ctx, "sms", "+15551234567")
	if err != nil {
		t.Fatalf("GetAllowedSender: %v", err)
	}
	senderCreated := time.Date(2026, 1, 1, 8, 15, 0, 0, time.FixedZone("CST", -6*60*60))
	if !sender.CreatedAt.Equal(senderCreated) {
		t.Fatalf("sender.CreatedAt = %v, want %v", sender.CreatedAt, senderCreated)
	}
	if sender.Enabled || sender.DefaultRepo != "https://github.com/acme/legacy" {
		t.Fatalf("unexpected sender fields: %+v", sender)
	}
}

func TestRunIsIdempotent(t *testing.T) {
	resetTargetDB(t)
	sqlitePath := writeLegacySQLiteFixture(t)
	ctx := context.Background()

	first, err := run(ctx, config{
		SQLitePath:  sqlitePath,
		PostgresURL: sharedConnStr,
	}, log.New(bytes.NewBuffer(nil), "", 0))
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	second, err := run(ctx, config{
		SQLitePath:  sqlitePath,
		PostgresURL: sharedConnStr,
	}, log.New(bytes.NewBuffer(nil), "", 0))
	if err != nil {
		t.Fatalf("second run: %v", err)
	}

	assertTableResult(t, first, "tasks", 2, 2)
	assertTableResult(t, first, "instances", 1, 1)
	assertTableResult(t, first, "allowed_senders", 1, 1)

	assertTableResult(t, second, "tasks", 2, 0)
	assertTableResult(t, second, "instances", 1, 0)
	assertTableResult(t, second, "allowed_senders", 1, 0)

	assertRowCount(t, "tasks", 2)
	assertRowCount(t, "instances", 1)
	assertRowCount(t, "allowed_senders", 1)
}

func testStore(t *testing.T) *store.PostgresStore {
	t.Helper()

	_, thisFile, _, _ := runtime.Caller(0)
	migrationsDir := filepath.Join(filepath.Dir(thisFile), "..", "..", "migrations")

	s, err := store.NewPostgres(context.Background(), sharedConnStr, migrationsDir)
	if err != nil {
		t.Fatalf("NewPostgres: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func resetTargetDB(t *testing.T) {
	t.Helper()

	if _, err := truncatePool.Exec(context.Background(), "TRUNCATE tasks, instances, allowed_senders CASCADE"); err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

func writeLegacySQLiteFixture(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "backflow.sqlite")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite fixture: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(legacySQLiteSchema); err != nil {
		t.Fatalf("create sqlite schema: %v", err)
	}

	taskOneCreated := time.Date(2025, 12, 25, 14, 0, 0, 0, time.FixedZone("PST", -8*60*60))
	taskOneUpdated := taskOneCreated.Add(3 * time.Minute)
	taskOneStarted := taskOneCreated.Add(5 * time.Minute)
	taskOneCompleted := taskOneCreated.Add(37 * time.Minute)
	if _, err := db.Exec(`
		INSERT INTO tasks (
			id, status, task_mode, harness, repo_url, branch, target_branch,
			review_pr_url, review_pr_number,
			prompt, context,
			model, effort, max_budget_usd, max_runtime_min, max_turns,
			create_pr, self_review, save_agent_output, pr_title, pr_body, pr_url, output_url,
			allowed_tools, claude_md, env_vars,
			instance_id, container_id, retry_count, cost_usd, elapsed_time_sec, error,
			reply_channel,
			created_at, updated_at, started_at, completed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		"bf_migrate_001", "completed", "code", "claude_code", "https://github.com/acme/backflow", "backflow/task-one", "main",
		"https://github.com/acme/backflow/pull/42", 42,
		"Ship the fix", "Focus on the orchestrator path",
		"claude-sonnet-4-6", "high", 12.75, 45, 150,
		1, 0, 1, "Ship the fix", "Implements the fix", "https://github.com/acme/backflow/pull/42", "s3://bucket/output/one.json",
		`["Read","Write","Bash"]`, "Prefer small diffs", `{"FOO":"bar","BAZ":"qux"}`,
		"i-migrate-001", "ctr-001", 2, 3.5, 1800, "",
		"sms:+15551234567",
		taskOneCreated.Format(time.RFC3339), taskOneUpdated.Format(time.RFC3339), taskOneStarted.Format(time.RFC3339), taskOneCompleted.Format(time.RFC3339),
	); err != nil {
		t.Fatalf("insert sqlite task one: %v", err)
	}

	taskTwoCreated := time.Date(2026, 1, 2, 9, 30, 0, 0, time.UTC)
	taskTwoUpdated := taskTwoCreated.Add(2 * time.Minute)
	if _, err := db.Exec(`
		INSERT INTO tasks (
			id, status, task_mode, harness, repo_url, branch, target_branch,
			review_pr_url, review_pr_number,
			prompt, context,
			model, effort, max_budget_usd, max_runtime_min, max_turns,
			create_pr, self_review, save_agent_output, pr_title, pr_body, pr_url, output_url,
			allowed_tools, claude_md, env_vars,
			instance_id, container_id, retry_count, cost_usd, elapsed_time_sec, error,
			reply_channel,
			created_at, updated_at, started_at, completed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		"bf_migrate_002", "failed", "review", "codex", "https://github.com/acme/backflow", "backflow/review-two", "main",
		"https://github.com/acme/backflow/pull/77", 77,
		"Review the PR", "Focus on regressions",
		"gpt-5.4", "medium", 0.5, 10, 20,
		0, 1, 0, "", "", "", "",
		`[]`, "", `{}`,
		"", "", 0, 0.25, 95, "timed out while posting comments",
		"",
		taskTwoCreated.Format(time.RFC3339), taskTwoUpdated.Format(time.RFC3339), nil, nil,
	); err != nil {
		t.Fatalf("insert sqlite task two: %v", err)
	}

	instanceCreated := time.Date(2025, 12, 24, 10, 0, 0, 0, time.FixedZone("MST", -7*60*60))
	instanceUpdated := instanceCreated.Add(15 * time.Minute)
	if _, err := db.Exec(`
		INSERT INTO instances (
			instance_id, instance_type, availability_zone, private_ip, status,
			max_containers, running_containers, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		"i-migrate-001", "c7g.large", "us-east-1b", "10.0.0.12", "running",
		6, 2, instanceCreated.Format(time.RFC3339), instanceUpdated.Format(time.RFC3339),
	); err != nil {
		t.Fatalf("insert sqlite instance: %v", err)
	}

	senderCreated := time.Date(2026, 1, 1, 8, 15, 0, 0, time.FixedZone("CST", -6*60*60))
	if _, err := db.Exec(`
		INSERT INTO allowed_senders (
			channel_type, address, default_repo, enabled, created_at
		) VALUES (?, ?, ?, ?, ?)
	`,
		"sms", "+15551234567", "https://github.com/acme/legacy", 0, senderCreated.Format(time.RFC3339),
	); err != nil {
		t.Fatalf("insert sqlite allowed sender: %v", err)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("close sqlite fixture: %v", err)
	}

	return dbPath
}

func assertTableResult(t *testing.T, results []tableResult, name string, readCount int, insertedCount int) {
	t.Helper()

	for _, result := range results {
		if result.Name != name {
			continue
		}
		if result.ReadCount != readCount || result.InsertedCount != insertedCount {
			t.Fatalf("result[%s] = %+v, want read=%d inserted=%d", name, result, readCount, insertedCount)
		}
		return
	}

	t.Fatalf("missing table result for %s: %+v", name, results)
}

func assertRowCount(t *testing.T, table string, want int) {
	t.Helper()

	var got int
	if err := truncatePool.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&got); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if got != want {
		t.Fatalf("row count for %s = %d, want %d", table, got, want)
	}
}
