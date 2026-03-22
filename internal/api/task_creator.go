package api

import (
	"context"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/backflow-labs/backflow/internal/config"
	"github.com/backflow-labs/backflow/internal/models"
	"github.com/backflow-labs/backflow/internal/notify"
	"github.com/backflow-labs/backflow/internal/store"
)

// NewTask validates the request, applies config defaults, persists the task, and emits
// a task.created event. Validation errors are returned as-is with user-friendly messages.
// Store errors are wrapped with "failed to create task".
func NewTask(ctx context.Context, req *models.CreateTaskRequest, s store.Store, cfg *config.Config, bus notify.Emitter) (*models.Task, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	taskMode := req.TaskMode
	if taskMode == "" {
		taskMode = models.TaskModeCode
	}

	task := &models.Task{
		ID:             "bf_" + ulid.Make().String(),
		Status:         models.TaskStatusPending,
		TaskMode:       taskMode,
		Harness:        models.Harness(req.Harness),
		RepoURL:        req.RepoURL,
		Branch:         req.Branch,
		TargetBranch:   req.TargetBranch,
		ReviewPRURL:    req.ReviewPRURL,
		ReviewPRNumber: req.ReviewPRNumber,
		Prompt:         req.Prompt,
		Context:        req.Context,
		Model:          req.Model,
		Effort:         req.Effort,
		MaxBudgetUSD:   req.MaxBudgetUSD,
		MaxRuntimeMin:  req.MaxRuntimeMin,
		MaxTurns:       req.MaxTurns,
		PRTitle:        req.PRTitle,
		PRBody:         req.PRBody,
		AllowedTools:   req.AllowedTools,
		ClaudeMD:       req.ClaudeMD,
		EnvVars:        req.EnvVars,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	cfg.TaskDefaults(taskMode).Apply(task, &config.BoolOverrides{
		CreatePR:        req.CreatePR,
		SelfReview:      req.SelfReview,
		SaveAgentOutput: req.SaveAgentOutput,
	})

	if err := s.CreateTask(ctx, task); err != nil {
		return nil, fmt.Errorf("failed to create task: %w", err)
	}

	if bus != nil {
		bus.Emit(notify.NewEvent(notify.EventTaskCreated, task))
	}

	return task, nil
}
