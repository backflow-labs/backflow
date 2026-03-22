package taskbuilder

import (
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/backflow-labs/backflow/internal/config"
	"github.com/backflow-labs/backflow/internal/models"
)

// Build constructs a pending task from a validated create-task request.
func Build(cfg *config.Config, req models.CreateTaskRequest, now time.Time) *models.Task {
	harness := models.Harness(withDefault(req.Harness, cfg.DefaultHarness))

	return &models.Task{
		ID:              "bf_" + ulid.Make().String(),
		Status:          models.TaskStatusPending,
		TaskMode:        withDefault(req.TaskMode, models.TaskModeCode),
		Harness:         harness,
		RepoURL:         req.RepoURL,
		Branch:          req.Branch,
		TargetBranch:    req.TargetBranch,
		ReviewPRURL:     req.ReviewPRURL,
		ReviewPRNumber:  req.ReviewPRNumber,
		Prompt:          req.Prompt,
		Context:         req.Context,
		Model:           defaultModel(cfg, harness, req.Model),
		Effort:          withDefault(req.Effort, cfg.DefaultEffort),
		MaxBudgetUSD:    withDefaultFloat(req.MaxBudgetUSD, cfg.DefaultMaxBudget),
		MaxRuntimeMin:   withDefaultInt(req.MaxRuntimeMin, int(cfg.DefaultMaxRuntime.Minutes())),
		MaxTurns:        withDefaultInt(req.MaxTurns, cfg.DefaultMaxTurns),
		CreatePR:        req.CreatePR,
		SelfReview:      req.SelfReview,
		SaveAgentOutput: req.SaveAgentOutput == nil || *req.SaveAgentOutput,
		PRTitle:         req.PRTitle,
		PRBody:          req.PRBody,
		AllowedTools:    req.AllowedTools,
		ClaudeMD:        req.ClaudeMD,
		EnvVars:         req.EnvVars,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

func defaultModel(cfg *config.Config, harness models.Harness, requested string) string {
	if requested != "" {
		return requested
	}
	if harness == models.HarnessCodex {
		return cfg.DefaultCodexModel
	}
	return cfg.DefaultClaudeModel
}

func withDefault(val, fallback string) string {
	if val == "" {
		return fallback
	}
	return val
}

func withDefaultFloat(val, fallback float64) float64 {
	if val == 0 {
		return fallback
	}
	return val
}

func withDefaultInt(val, fallback int) int {
	if val == 0 {
		return fallback
	}
	return val
}
