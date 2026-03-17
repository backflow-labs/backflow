package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/backflow-labs/backflow/internal/models"
)

const taskColumns = `
	id, status, task_mode, harness, repo_url, branch, target_branch,
	review_pr_url, review_pr_number,
	prompt, context,
	model, effort, max_budget_usd, max_runtime_min, max_turns,
	create_pr, self_review, save_agent_output, pr_title, pr_body, pr_url, output_url,
	allowed_tools, claude_md, env_vars,
	instance_id, container_id, retry_count, cost_usd, elapsed_time_sec, error,
	reply_channel,
	created_at, updated_at, started_at, completed_at
`

type querier interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type PostgresStore struct {
	db   querier
	pool *pgxpool.Pool
}

func NewPostgres(ctx context.Context, databaseURL string) (*PostgresStore, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse postgres config: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open postgres pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return &PostgresStore{
		db:   pool,
		pool: pool,
	}, nil
}

func (s *PostgresStore) Close() error {
	if s.pool == nil {
		return fmt.Errorf("cannot close a transaction store")
	}
	s.pool.Close()
	return nil
}

func (s *PostgresStore) WithTx(ctx context.Context, fn func(Store) error) error {
	if s.pool == nil {
		return fmt.Errorf("nested transactions are not supported")
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	txStore := &PostgresStore{db: tx}
	if err := fn(txStore); err != nil {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			return fmt.Errorf("rollback tx: %v (original error: %w)", rollbackErr, err)
		}
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func (s *PostgresStore) CreateTask(ctx context.Context, task *models.Task) error {
	_, err := s.db.Exec(ctx, `
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
			$24::jsonb, $25, $26::jsonb,
			$27, $28, $29, $30, $31, $32,
			$33,
			$34, $35, $36, $37
		)`,
		task.ID, task.Status, task.TaskMode, task.Harness, task.RepoURL, task.Branch, task.TargetBranch,
		task.ReviewPRURL, task.ReviewPRNumber,
		task.Prompt, task.Context,
		task.Model, task.Effort, task.MaxBudgetUSD, task.MaxRuntimeMin, task.MaxTurns,
		task.CreatePR, task.SelfReview, task.SaveAgentOutput, task.PRTitle, task.PRBody, task.PRURL, task.OutputURL,
		task.AllowedToolsJSON(), task.ClaudeMD, task.EnvVarsJSON(),
		task.InstanceID, task.ContainerID, task.RetryCount, task.CostUSD, task.ElapsedTimeSec, task.Error,
		task.ReplyChannel,
		task.CreatedAt, task.UpdatedAt, timeArg(task.StartedAt), timeArg(task.CompletedAt),
	)
	return err
}

func (s *PostgresStore) GetTask(ctx context.Context, id string) (*models.Task, error) {
	row := s.db.QueryRow(ctx, "SELECT "+taskColumns+" FROM tasks WHERE id = $1", id)
	return scanTask(row)
}

func (s *PostgresStore) ListTasks(ctx context.Context, filter TaskFilter) ([]*models.Task, error) {
	query := "SELECT " + taskColumns + " FROM tasks"
	args := make([]any, 0, 3)
	var where []string

	if filter.Status != nil {
		args = append(args, string(*filter.Status))
		where = append(where, fmt.Sprintf("status = $%d", len(args)))
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY created_at ASC"
	if filter.Limit > 0 {
		args = append(args, filter.Limit)
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	if filter.Offset > 0 {
		args = append(args, filter.Offset)
		query += fmt.Sprintf(" OFFSET $%d", len(args))
	}

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []*models.Task
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func (s *PostgresStore) DeleteTask(ctx context.Context, id string) error {
	_, err := s.db.Exec(ctx, "DELETE FROM tasks WHERE id = $1", id)
	return err
}

func (s *PostgresStore) UpdateTaskStatus(ctx context.Context, id string, status models.TaskStatus, taskErr string) error {
	_, err := s.db.Exec(ctx,
		"UPDATE tasks SET status = $1, error = $2, updated_at = $3 WHERE id = $4",
		status, taskErr, time.Now().UTC(), id,
	)
	return err
}

func (s *PostgresStore) AssignTask(ctx context.Context, id string, instanceID string) error {
	_, err := s.db.Exec(ctx,
		"UPDATE tasks SET status = $1, instance_id = $2, updated_at = $3 WHERE id = $4",
		models.TaskStatusProvisioning, instanceID, time.Now().UTC(), id,
	)
	return err
}

func (s *PostgresStore) StartTask(ctx context.Context, id string, containerID string) error {
	now := time.Now().UTC()
	_, err := s.db.Exec(ctx,
		"UPDATE tasks SET status = $1, container_id = $2, started_at = $3, updated_at = $4 WHERE id = $5",
		models.TaskStatusRunning, containerID, now, now, id,
	)
	return err
}

func (s *PostgresStore) CompleteTask(ctx context.Context, id string, result TaskResult) error {
	now := time.Now().UTC()
	_, err := s.db.Exec(ctx, `
		UPDATE tasks
		SET status = $1, error = $2, pr_url = $3, output_url = $4, cost_usd = $5,
		    elapsed_time_sec = $6, completed_at = $7, updated_at = $8
		WHERE id = $9`,
		result.Status, result.Error, result.PRURL, result.OutputURL, result.CostUSD,
		result.ElapsedTimeSec, now, now, id,
	)
	return err
}

func (s *PostgresStore) RequeueTask(ctx context.Context, id string, reason string) error {
	now := time.Now().UTC()
	_, err := s.db.Exec(ctx, `
		UPDATE tasks
		SET status = $1, instance_id = '', container_id = '', started_at = NULL,
		    retry_count = retry_count + 1, error = $2, updated_at = $3
		WHERE id = $4`,
		models.TaskStatusPending, "re-queued: "+reason+" at "+now.Format(time.RFC3339), now, id,
	)
	return err
}

func (s *PostgresStore) CancelTask(ctx context.Context, id string) error {
	now := time.Now().UTC()
	_, err := s.db.Exec(ctx,
		"UPDATE tasks SET status = $1, completed_at = $2, updated_at = $3 WHERE id = $4",
		models.TaskStatusCancelled, now, now, id,
	)
	return err
}

func (s *PostgresStore) ClearTaskAssignment(ctx context.Context, id string) error {
	_, err := s.db.Exec(ctx,
		"UPDATE tasks SET instance_id = '', container_id = '', updated_at = $1 WHERE id = $2",
		time.Now().UTC(), id,
	)
	return err
}

func (s *PostgresStore) CreateInstance(ctx context.Context, inst *models.Instance) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO instances (
			instance_id, instance_type, availability_zone, private_ip, status,
			max_containers, running_containers, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		inst.InstanceID, inst.InstanceType, inst.AvailabilityZone, inst.PrivateIP, inst.Status,
		inst.MaxContainers, inst.RunningContainers, inst.CreatedAt, inst.UpdatedAt,
	)
	return err
}

func (s *PostgresStore) GetInstance(ctx context.Context, id string) (*models.Instance, error) {
	row := s.db.QueryRow(ctx, `
		SELECT instance_id, instance_type, availability_zone, private_ip, status,
		       max_containers, running_containers, created_at, updated_at
		FROM instances
		WHERE instance_id = $1`, id)
	return scanInstance(row)
}

func (s *PostgresStore) ListInstances(ctx context.Context, status *models.InstanceStatus) ([]*models.Instance, error) {
	query := `
		SELECT instance_id, instance_type, availability_zone, private_ip, status,
		       max_containers, running_containers, created_at, updated_at
		FROM instances`
	args := make([]any, 0, 1)
	if status != nil {
		args = append(args, string(*status))
		query += " WHERE status = $1"
	}
	query += " ORDER BY created_at ASC"

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var instances []*models.Instance
	for rows.Next() {
		inst, err := scanInstance(rows)
		if err != nil {
			return nil, err
		}
		instances = append(instances, inst)
	}
	return instances, rows.Err()
}

func (s *PostgresStore) UpdateInstanceStatus(ctx context.Context, id string, status models.InstanceStatus) error {
	query := "UPDATE instances SET status = $1, updated_at = $2 WHERE instance_id = $3"
	args := []any{status, time.Now().UTC(), id}
	if status == models.InstanceStatusTerminated {
		query = "UPDATE instances SET status = $1, running_containers = 0, updated_at = $2 WHERE instance_id = $3"
	}

	_, err := s.db.Exec(ctx, query, args...)
	return err
}

func (s *PostgresStore) IncrementRunningContainers(ctx context.Context, id string) error {
	_, err := s.db.Exec(ctx,
		"UPDATE instances SET running_containers = running_containers + 1, updated_at = $1 WHERE instance_id = $2",
		time.Now().UTC(), id,
	)
	return err
}

func (s *PostgresStore) DecrementRunningContainers(ctx context.Context, id string) error {
	_, err := s.db.Exec(ctx,
		"UPDATE instances SET running_containers = GREATEST(running_containers - 1, 0), updated_at = $1 WHERE instance_id = $2",
		time.Now().UTC(), id,
	)
	return err
}

func (s *PostgresStore) UpdateInstanceDetails(ctx context.Context, id string, privateIP, az string) error {
	_, err := s.db.Exec(ctx,
		"UPDATE instances SET private_ip = $1, availability_zone = $2, updated_at = $3 WHERE instance_id = $4",
		privateIP, az, time.Now().UTC(), id,
	)
	return err
}

func (s *PostgresStore) ResetRunningContainers(ctx context.Context, id string) error {
	_, err := s.db.Exec(ctx,
		"UPDATE instances SET running_containers = 0, updated_at = $1 WHERE instance_id = $2",
		time.Now().UTC(), id,
	)
	return err
}

func (s *PostgresStore) GetAllowedSender(ctx context.Context, channelType, address string) (*models.AllowedSender, error) {
	row := s.db.QueryRow(ctx, `
		SELECT channel_type, address, default_repo, enabled, created_at
		FROM allowed_senders
		WHERE channel_type = $1 AND address = $2`,
		channelType, address,
	)

	var sender models.AllowedSender
	err := row.Scan(&sender.ChannelType, &sender.Address, &sender.DefaultRepo, &sender.Enabled, &sender.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &sender, nil
}

func (s *PostgresStore) CreateAllowedSender(ctx context.Context, sender *models.AllowedSender) error {
	_, err := s.db.Exec(ctx,
		`INSERT INTO allowed_senders (channel_type, address, default_repo, enabled, created_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		sender.ChannelType, sender.Address, sender.DefaultRepo, sender.Enabled, sender.CreatedAt,
	)
	return err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanTask(row scanner) (*models.Task, error) {
	var task models.Task
	var allowedToolsJSON []byte
	var envVarsJSON []byte
	var startedAt pgtype.Timestamptz
	var completedAt pgtype.Timestamptz

	err := row.Scan(
		&task.ID, &task.Status, &task.TaskMode, &task.Harness, &task.RepoURL, &task.Branch, &task.TargetBranch,
		&task.ReviewPRURL, &task.ReviewPRNumber,
		&task.Prompt, &task.Context,
		&task.Model, &task.Effort, &task.MaxBudgetUSD, &task.MaxRuntimeMin, &task.MaxTurns,
		&task.CreatePR, &task.SelfReview, &task.SaveAgentOutput, &task.PRTitle, &task.PRBody, &task.PRURL, &task.OutputURL,
		&allowedToolsJSON, &task.ClaudeMD, &envVarsJSON,
		&task.InstanceID, &task.ContainerID, &task.RetryCount, &task.CostUSD, &task.ElapsedTimeSec, &task.Error,
		&task.ReplyChannel,
		&task.CreatedAt, &task.UpdatedAt, &startedAt, &completedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	if len(allowedToolsJSON) > 0 {
		if err := json.Unmarshal(allowedToolsJSON, &task.AllowedTools); err != nil {
			return nil, fmt.Errorf("unmarshal allowed tools: %w", err)
		}
	}
	if len(envVarsJSON) > 0 {
		if err := json.Unmarshal(envVarsJSON, &task.EnvVars); err != nil {
			return nil, fmt.Errorf("unmarshal env vars: %w", err)
		}
	}
	if startedAt.Valid {
		started := startedAt.Time.UTC()
		task.StartedAt = &started
	}
	if completedAt.Valid {
		completed := completedAt.Time.UTC()
		task.CompletedAt = &completed
	}

	task.CreatedAt = task.CreatedAt.UTC()
	task.UpdatedAt = task.UpdatedAt.UTC()
	return &task, nil
}

func scanInstance(row scanner) (*models.Instance, error) {
	var inst models.Instance
	err := row.Scan(
		&inst.InstanceID, &inst.InstanceType, &inst.AvailabilityZone,
		&inst.PrivateIP, &inst.Status, &inst.MaxContainers,
		&inst.RunningContainers, &inst.CreatedAt, &inst.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	inst.CreatedAt = inst.CreatedAt.UTC()
	inst.UpdatedAt = inst.UpdatedAt.UTC()
	return &inst, nil
}

func timeArg(t *time.Time) any {
	if t == nil {
		return nil
	}
	return *t
}
