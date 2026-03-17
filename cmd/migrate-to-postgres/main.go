package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	dbmigrations "github.com/backflow-labs/backflow/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // registers "pgx" for goose
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

const batchSize = 250

const taskInsertSQL = `
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
	) VALUES (
		$1, $2, $3, $4, $5, $6, $7,
		$8, $9,
		$10, $11,
		$12, $13, $14, $15, $16,
		$17, $18, $19, $20, $21, $22, $23,
		$24, $25, $26,
		$27, $28, $29, $30, $31, $32,
		$33,
		$34, $35, $36, $37
	) ON CONFLICT (id) DO NOTHING
`

const instanceInsertSQL = `
	INSERT INTO instances (
		instance_id, instance_type, availability_zone, private_ip, status,
		max_containers, running_containers, created_at, updated_at
	) VALUES (
		$1, $2, $3, $4, $5,
		$6, $7, $8, $9
	) ON CONFLICT (instance_id) DO NOTHING
`

const allowedSenderInsertSQL = `
	INSERT INTO allowed_senders (
		channel_type, address, default_repo, enabled, created_at
	) VALUES (
		$1, $2, $3, $4, $5
	) ON CONFLICT (channel_type, address) DO NOTHING
`

const taskSelectSQL = `
	SELECT
		id, status, task_mode, harness, repo_url, branch, target_branch,
		review_pr_url, review_pr_number,
		prompt, context,
		model, effort, max_budget_usd, max_runtime_min, max_turns,
		create_pr, self_review, save_agent_output, pr_title, pr_body, pr_url, output_url,
		allowed_tools, claude_md, env_vars,
		instance_id, container_id, retry_count, cost_usd, elapsed_time_sec, error,
		reply_channel,
		created_at, updated_at, started_at, completed_at
	FROM tasks
	ORDER BY created_at ASC
`

const instanceSelectSQL = `
	SELECT
		instance_id, instance_type, availability_zone, private_ip, status,
		max_containers, running_containers, created_at, updated_at
	FROM instances
	ORDER BY created_at ASC
`

const allowedSenderSelectSQL = `
	SELECT
		channel_type, address, default_repo, enabled, created_at
	FROM allowed_senders
	ORDER BY created_at ASC
`

var gooseFSMu sync.Mutex

type config struct {
	SQLitePath  string
	PostgresURL string
}

type tableResult struct {
	Name          string
	ReadCount     int
	InsertedCount int
}

type sqliteTaskRow struct {
	ID              string
	Status          string
	TaskMode        string
	Harness         string
	RepoURL         string
	Branch          string
	TargetBranch    string
	ReviewPRURL     string
	ReviewPRNumber  int
	Prompt          string
	Context         string
	Model           string
	Effort          string
	MaxBudgetUSD    float64
	MaxRuntimeMin   int
	MaxTurns        int
	CreatePR        int
	SelfReview      int
	SaveAgentOutput int
	PRTitle         string
	PRBody          string
	PRURL           string
	OutputURL       string
	AllowedTools    string
	ClaudeMD        string
	EnvVars         string
	InstanceID      string
	ContainerID     string
	RetryCount      int
	CostUSD         float64
	ElapsedTimeSec  int
	Error           string
	ReplyChannel    string
	CreatedAt       string
	UpdatedAt       string
	StartedAt       sql.NullString
	CompletedAt     sql.NullString
}

type sqliteInstanceRow struct {
	InstanceID        string
	InstanceType      string
	AvailabilityZone  string
	PrivateIP         string
	Status            string
	MaxContainers     int
	RunningContainers int
	CreatedAt         string
	UpdatedAt         string
}

type sqliteAllowedSenderRow struct {
	ChannelType string
	Address     string
	DefaultRepo string
	Enabled     int
	CreatedAt   string
}

func main() {
	logger := log.New(os.Stderr, "", log.LstdFlags|log.LUTC)

	cfg, err := loadConfigFromEnv()
	if err != nil {
		logger.Printf("configuration error: %v", err)
		os.Exit(1)
	}

	if _, err := run(context.Background(), cfg, logger); err != nil {
		logger.Printf("migration failed: %v", err)
		os.Exit(1)
	}
}

func loadConfigFromEnv() (config, error) {
	cfg := config{
		SQLitePath:  strings.TrimSpace(os.Getenv("BACKFLOW_DB_PATH")),
		PostgresURL: strings.TrimSpace(os.Getenv("BACKFLOW_DATABASE_URL")),
	}
	if cfg.SQLitePath == "" {
		return config{}, errors.New("BACKFLOW_DB_PATH is required")
	}
	if cfg.PostgresURL == "" {
		return config{}, errors.New("BACKFLOW_DATABASE_URL is required")
	}
	return cfg, nil
}

func run(ctx context.Context, cfg config, logger *log.Logger) ([]tableResult, error) {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	if err := ensureSQLiteSourceExists(cfg.SQLitePath); err != nil {
		return nil, err
	}

	logger.Printf("migrating SQLite database %q into Postgres", cfg.SQLitePath)

	if err := applyPostgresMigrations(cfg.PostgresURL); err != nil {
		return nil, fmt.Errorf("apply postgres migrations: %w", err)
	}

	sqliteDB, err := sql.Open("sqlite", cfg.SQLitePath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	defer sqliteDB.Close()

	if err := sqliteDB.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	pool, err := pgxpool.New(ctx, cfg.PostgresURL)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin postgres transaction: %w", err)
	}
	defer tx.Rollback(ctx) // nolint:errcheck

	results := make([]tableResult, 0, 3)

	tasksResult, err := migrateTasks(ctx, sqliteDB, tx)
	if err != nil {
		return nil, err
	}
	logger.Printf("table=%s read=%d inserted=%d", tasksResult.Name, tasksResult.ReadCount, tasksResult.InsertedCount)
	results = append(results, tasksResult)

	instancesResult, err := migrateInstances(ctx, sqliteDB, tx)
	if err != nil {
		return nil, err
	}
	logger.Printf("table=%s read=%d inserted=%d", instancesResult.Name, instancesResult.ReadCount, instancesResult.InsertedCount)
	results = append(results, instancesResult)

	sendersResult, err := migrateAllowedSenders(ctx, sqliteDB, tx)
	if err != nil {
		return nil, err
	}
	logger.Printf("table=%s read=%d inserted=%d", sendersResult.Name, sendersResult.ReadCount, sendersResult.InsertedCount)
	results = append(results, sendersResult)

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit postgres transaction: %w", err)
	}

	logger.Printf("migration complete")
	return results, nil
}

func ensureSQLiteSourceExists(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat sqlite source %q: %w", path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("sqlite source %q is a directory", path)
	}
	return nil
}

func applyPostgresMigrations(databaseURL string) error {
	gooseFSMu.Lock()
	defer gooseFSMu.Unlock()

	goose.SetBaseFS(dbmigrations.Files)
	defer goose.SetBaseFS(nil)

	gooseDB, err := goose.OpenDBWithDriver("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("open goose db: %w", err)
	}
	defer gooseDB.Close()

	if err := goose.Up(gooseDB, "."); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}

func migrateTasks(ctx context.Context, sqliteDB *sql.DB, tx pgx.Tx) (tableResult, error) {
	rows, err := sqliteDB.QueryContext(ctx, taskSelectSQL)
	if err != nil {
		return tableResult{}, fmt.Errorf("query sqlite tasks: %w", err)
	}
	defer rows.Close()

	result := tableResult{Name: "tasks"}
	batch := &pgx.Batch{}
	queued := 0

	for rows.Next() {
		var row sqliteTaskRow
		if err := rows.Scan(
			&row.ID, &row.Status, &row.TaskMode, &row.Harness, &row.RepoURL, &row.Branch, &row.TargetBranch,
			&row.ReviewPRURL, &row.ReviewPRNumber,
			&row.Prompt, &row.Context,
			&row.Model, &row.Effort, &row.MaxBudgetUSD, &row.MaxRuntimeMin, &row.MaxTurns,
			&row.CreatePR, &row.SelfReview, &row.SaveAgentOutput, &row.PRTitle, &row.PRBody, &row.PRURL, &row.OutputURL,
			&row.AllowedTools, &row.ClaudeMD, &row.EnvVars,
			&row.InstanceID, &row.ContainerID, &row.RetryCount, &row.CostUSD, &row.ElapsedTimeSec, &row.Error,
			&row.ReplyChannel,
			&row.CreatedAt, &row.UpdatedAt, &row.StartedAt, &row.CompletedAt,
		); err != nil {
			return tableResult{}, fmt.Errorf("scan sqlite task: %w", err)
		}

		args, err := row.insertArgs()
		if err != nil {
			return tableResult{}, fmt.Errorf("prepare task %q: %w", row.ID, err)
		}

		batch.Queue(taskInsertSQL, args...)
		queued++
		result.ReadCount++

		if queued == batchSize {
			inserted, err := flushBatch(ctx, tx, batch, queued)
			if err != nil {
				return tableResult{}, fmt.Errorf("insert postgres tasks: %w", err)
			}
			result.InsertedCount += inserted
			batch = &pgx.Batch{}
			queued = 0
		}
	}
	if err := rows.Err(); err != nil {
		return tableResult{}, fmt.Errorf("iterate sqlite tasks: %w", err)
	}

	inserted, err := flushBatch(ctx, tx, batch, queued)
	if err != nil {
		return tableResult{}, fmt.Errorf("insert postgres tasks: %w", err)
	}
	result.InsertedCount += inserted

	return result, nil
}

func migrateInstances(ctx context.Context, sqliteDB *sql.DB, tx pgx.Tx) (tableResult, error) {
	rows, err := sqliteDB.QueryContext(ctx, instanceSelectSQL)
	if err != nil {
		return tableResult{}, fmt.Errorf("query sqlite instances: %w", err)
	}
	defer rows.Close()

	result := tableResult{Name: "instances"}
	batch := &pgx.Batch{}
	queued := 0

	for rows.Next() {
		var row sqliteInstanceRow
		if err := rows.Scan(
			&row.InstanceID, &row.InstanceType, &row.AvailabilityZone, &row.PrivateIP, &row.Status,
			&row.MaxContainers, &row.RunningContainers, &row.CreatedAt, &row.UpdatedAt,
		); err != nil {
			return tableResult{}, fmt.Errorf("scan sqlite instance: %w", err)
		}

		args, err := row.insertArgs()
		if err != nil {
			return tableResult{}, fmt.Errorf("prepare instance %q: %w", row.InstanceID, err)
		}

		batch.Queue(instanceInsertSQL, args...)
		queued++
		result.ReadCount++

		if queued == batchSize {
			inserted, err := flushBatch(ctx, tx, batch, queued)
			if err != nil {
				return tableResult{}, fmt.Errorf("insert postgres instances: %w", err)
			}
			result.InsertedCount += inserted
			batch = &pgx.Batch{}
			queued = 0
		}
	}
	if err := rows.Err(); err != nil {
		return tableResult{}, fmt.Errorf("iterate sqlite instances: %w", err)
	}

	inserted, err := flushBatch(ctx, tx, batch, queued)
	if err != nil {
		return tableResult{}, fmt.Errorf("insert postgres instances: %w", err)
	}
	result.InsertedCount += inserted

	return result, nil
}

func migrateAllowedSenders(ctx context.Context, sqliteDB *sql.DB, tx pgx.Tx) (tableResult, error) {
	rows, err := sqliteDB.QueryContext(ctx, allowedSenderSelectSQL)
	if err != nil {
		return tableResult{}, fmt.Errorf("query sqlite allowed_senders: %w", err)
	}
	defer rows.Close()

	result := tableResult{Name: "allowed_senders"}
	batch := &pgx.Batch{}
	queued := 0

	for rows.Next() {
		var row sqliteAllowedSenderRow
		if err := rows.Scan(&row.ChannelType, &row.Address, &row.DefaultRepo, &row.Enabled, &row.CreatedAt); err != nil {
			return tableResult{}, fmt.Errorf("scan sqlite allowed sender: %w", err)
		}

		args, err := row.insertArgs()
		if err != nil {
			return tableResult{}, fmt.Errorf("prepare allowed sender %q/%q: %w", row.ChannelType, row.Address, err)
		}

		batch.Queue(allowedSenderInsertSQL, args...)
		queued++
		result.ReadCount++

		if queued == batchSize {
			inserted, err := flushBatch(ctx, tx, batch, queued)
			if err != nil {
				return tableResult{}, fmt.Errorf("insert postgres allowed_senders: %w", err)
			}
			result.InsertedCount += inserted
			batch = &pgx.Batch{}
			queued = 0
		}
	}
	if err := rows.Err(); err != nil {
		return tableResult{}, fmt.Errorf("iterate sqlite allowed_senders: %w", err)
	}

	inserted, err := flushBatch(ctx, tx, batch, queued)
	if err != nil {
		return tableResult{}, fmt.Errorf("insert postgres allowed_senders: %w", err)
	}
	result.InsertedCount += inserted

	return result, nil
}

func flushBatch(ctx context.Context, tx pgx.Tx, batch *pgx.Batch, queued int) (int, error) {
	if queued == 0 {
		return 0, nil
	}

	results := tx.SendBatch(ctx, batch)
	inserted := 0
	for i := 0; i < queued; i++ {
		tag, err := results.Exec()
		if err != nil {
			results.Close() // nolint:errcheck
			return inserted, err
		}
		inserted += int(tag.RowsAffected())
	}
	if err := results.Close(); err != nil {
		return inserted, err
	}
	return inserted, nil
}

func (r sqliteTaskRow) insertArgs() ([]any, error) {
	createdAt, err := parseTimestamp(r.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("created_at: %w", err)
	}
	updatedAt, err := parseTimestamp(r.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("updated_at: %w", err)
	}
	startedAt, err := parseNullableTimestamp(r.StartedAt)
	if err != nil {
		return nil, fmt.Errorf("started_at: %w", err)
	}
	completedAt, err := parseNullableTimestamp(r.CompletedAt)
	if err != nil {
		return nil, fmt.Errorf("completed_at: %w", err)
	}

	return []any{
		r.ID, r.Status, r.TaskMode, r.Harness, r.RepoURL, r.Branch, r.TargetBranch,
		r.ReviewPRURL, r.ReviewPRNumber,
		r.Prompt, r.Context,
		r.Model, r.Effort, r.MaxBudgetUSD, r.MaxRuntimeMin, r.MaxTurns,
		intToBool(r.CreatePR), intToBool(r.SelfReview), intToBool(r.SaveAgentOutput),
		r.PRTitle, r.PRBody, r.PRURL, r.OutputURL,
		jsonValue(r.AllowedTools, "[]"), r.ClaudeMD, jsonValue(r.EnvVars, "{}"),
		r.InstanceID, r.ContainerID, r.RetryCount, r.CostUSD, r.ElapsedTimeSec, r.Error,
		r.ReplyChannel,
		createdAt, updatedAt, startedAt, completedAt,
	}, nil
}

func (r sqliteInstanceRow) insertArgs() ([]any, error) {
	createdAt, err := parseTimestamp(r.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("created_at: %w", err)
	}
	updatedAt, err := parseTimestamp(r.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("updated_at: %w", err)
	}

	return []any{
		r.InstanceID, r.InstanceType, r.AvailabilityZone, r.PrivateIP, r.Status,
		r.MaxContainers, r.RunningContainers, createdAt, updatedAt,
	}, nil
}

func (r sqliteAllowedSenderRow) insertArgs() ([]any, error) {
	createdAt, err := parseTimestamp(r.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("created_at: %w", err)
	}

	return []any{
		r.ChannelType, r.Address, r.DefaultRepo, intToBool(r.Enabled), createdAt,
	}, nil
}

func parseTimestamp(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, errors.New("empty timestamp")
	}

	ts, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, err
	}
	return ts, nil
}

func parseNullableTimestamp(value sql.NullString) (*time.Time, error) {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return nil, nil
	}
	ts, err := parseTimestamp(value.String)
	if err != nil {
		return nil, err
	}
	return &ts, nil
}

func jsonValue(value string, fallback string) json.RawMessage {
	if strings.TrimSpace(value) == "" {
		value = fallback
	}
	return json.RawMessage(value)
}

func intToBool(value int) bool {
	return value != 0
}
