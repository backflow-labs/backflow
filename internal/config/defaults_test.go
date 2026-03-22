package config

import (
	"testing"
	"time"

	"github.com/backflow-labs/backflow/internal/models"
)

func testConfig() *Config {
	return &Config{
		DefaultHarness:     "codex",
		DefaultClaudeModel: "claude-sonnet-4-6",
		DefaultCodexModel:  "gpt-5.4-mini",
		DefaultEffort:      "medium",
		DefaultMaxBudget:   10.0,
		DefaultMaxRuntime:  30 * time.Minute,
		DefaultMaxTurns:    200,
		DefaultCreatePR:    true,
		DefaultSelfReview:  false,
		DefaultSaveOutput:  true,
	}
}

func TestTaskDefaults_CodeMode(t *testing.T) {
	cfg := testConfig()
	d := cfg.TaskDefaults(models.TaskModeCode)

	if d.Harness != "codex" {
		t.Errorf("Harness = %q, want %q", d.Harness, "codex")
	}
	if d.CodexModel != "gpt-5.4-mini" {
		t.Errorf("CodexModel = %q, want %q", d.CodexModel, "gpt-5.4-mini")
	}
	if d.ClaudeModel != "claude-sonnet-4-6" {
		t.Errorf("ClaudeModel = %q, want %q", d.ClaudeModel, "claude-sonnet-4-6")
	}
	if d.Effort != "medium" {
		t.Errorf("Effort = %q, want %q", d.Effort, "medium")
	}
	if d.MaxBudgetUSD != 10.0 {
		t.Errorf("MaxBudgetUSD = %v, want %v", d.MaxBudgetUSD, 10.0)
	}
	if d.MaxRuntimeMin != 30 {
		t.Errorf("MaxRuntimeMin = %d, want %d", d.MaxRuntimeMin, 30)
	}
	if d.MaxTurns != 200 {
		t.Errorf("MaxTurns = %d, want %d", d.MaxTurns, 200)
	}
	if !d.CreatePR {
		t.Error("CreatePR = false, want true in code mode")
	}
	if d.SelfReview {
		t.Error("SelfReview = true, want false")
	}
	if !d.SaveAgentOutput {
		t.Error("SaveAgentOutput = false, want true")
	}
}

func TestTaskDefaults_ReviewMode(t *testing.T) {
	cfg := testConfig()
	d := cfg.TaskDefaults(models.TaskModeReview)

	if d.CreatePR {
		t.Error("CreatePR = true, want false in review mode")
	}
	// Other defaults unchanged
	if d.Harness != "codex" {
		t.Errorf("Harness = %q, want %q", d.Harness, "codex")
	}
	if !d.SaveAgentOutput {
		t.Error("SaveAgentOutput = false, want true")
	}
}

func TestApply_FillsZeroValues(t *testing.T) {
	cfg := testConfig()
	d := cfg.TaskDefaults(models.TaskModeCode)
	task := &models.Task{}

	d.Apply(task, nil)

	if task.Harness != models.HarnessCodex {
		t.Errorf("Harness = %q, want %q", task.Harness, models.HarnessCodex)
	}
	if task.Model != "gpt-5.4-mini" {
		t.Errorf("Model = %q, want %q", task.Model, "gpt-5.4-mini")
	}
	if task.Effort != "medium" {
		t.Errorf("Effort = %q, want %q", task.Effort, "medium")
	}
	if task.MaxBudgetUSD != 10.0 {
		t.Errorf("MaxBudgetUSD = %v, want %v", task.MaxBudgetUSD, 10.0)
	}
	if task.MaxRuntimeMin != 30 {
		t.Errorf("MaxRuntimeMin = %d, want %d", task.MaxRuntimeMin, 30)
	}
	if task.MaxTurns != 200 {
		t.Errorf("MaxTurns = %d, want %d", task.MaxTurns, 200)
	}
	if !task.CreatePR {
		t.Error("CreatePR = false, want true")
	}
	if task.SelfReview {
		t.Error("SelfReview = true, want false")
	}
	if !task.SaveAgentOutput {
		t.Error("SaveAgentOutput = false, want true")
	}
}

func TestApply_PreservesExplicitValues(t *testing.T) {
	cfg := testConfig()
	d := cfg.TaskDefaults(models.TaskModeCode)
	task := &models.Task{
		Harness:       models.HarnessClaudeCode,
		Model:         "claude-opus-4-6",
		Effort:        "high",
		MaxBudgetUSD:  25.0,
		MaxRuntimeMin: 60,
		MaxTurns:      500,
	}

	d.Apply(task, nil)

	if task.Harness != models.HarnessClaudeCode {
		t.Errorf("Harness = %q, want %q", task.Harness, models.HarnessClaudeCode)
	}
	if task.Model != "claude-opus-4-6" {
		t.Errorf("Model = %q, want %q", task.Model, "claude-opus-4-6")
	}
	if task.Effort != "high" {
		t.Errorf("Effort = %q, want %q", task.Effort, "high")
	}
	if task.MaxBudgetUSD != 25.0 {
		t.Errorf("MaxBudgetUSD = %v, want %v", task.MaxBudgetUSD, 25.0)
	}
	if task.MaxRuntimeMin != 60 {
		t.Errorf("MaxRuntimeMin = %d, want %d", task.MaxRuntimeMin, 60)
	}
	if task.MaxTurns != 500 {
		t.Errorf("MaxTurns = %d, want %d", task.MaxTurns, 500)
	}
}

func TestApply_BoolOverrides_Nil(t *testing.T) {
	cfg := testConfig()
	d := cfg.TaskDefaults(models.TaskModeCode)
	task := &models.Task{}

	d.Apply(task, nil)

	if !task.CreatePR {
		t.Error("CreatePR = false, want true (default)")
	}
	if task.SelfReview {
		t.Error("SelfReview = true, want false (default)")
	}
	if !task.SaveAgentOutput {
		t.Error("SaveAgentOutput = false, want true (default)")
	}
}

func boolPtr(v bool) *bool { return &v }

func TestApply_BoolOverrides_ExplicitFalse(t *testing.T) {
	cfg := testConfig()
	d := cfg.TaskDefaults(models.TaskModeCode)
	task := &models.Task{}

	d.Apply(task, &BoolOverrides{
		CreatePR:        boolPtr(false),
		SaveAgentOutput: boolPtr(false),
	})

	if task.CreatePR {
		t.Error("CreatePR = true, want false (explicit override)")
	}
	if task.SelfReview {
		t.Error("SelfReview = true, want false (default, no override)")
	}
	if task.SaveAgentOutput {
		t.Error("SaveAgentOutput = true, want false (explicit override)")
	}
}

func TestApply_HarnessModelCoupling(t *testing.T) {
	cfg := testConfig()

	// Default harness is codex → model should be codex model
	d := cfg.TaskDefaults(models.TaskModeCode)
	task := &models.Task{}
	d.Apply(task, nil)
	if task.Model != "gpt-5.4-mini" {
		t.Errorf("Model = %q, want %q for codex harness", task.Model, "gpt-5.4-mini")
	}

	// Claude harness → claude model
	cfg.DefaultHarness = "claude_code"
	d = cfg.TaskDefaults(models.TaskModeCode)
	task = &models.Task{}
	d.Apply(task, nil)
	if task.Model != "claude-sonnet-4-6" {
		t.Errorf("Model = %q, want %q for claude harness", task.Model, "claude-sonnet-4-6")
	}
}

func TestApply_CallerOverridesHarness(t *testing.T) {
	cfg := testConfig() // default harness is codex
	d := cfg.TaskDefaults(models.TaskModeCode)

	// Task explicitly sets claude_code harness; model should follow
	task := &models.Task{Harness: models.HarnessClaudeCode}
	d.Apply(task, nil)

	if task.Model != "claude-sonnet-4-6" {
		t.Errorf("Model = %q, want %q when caller overrides harness to claude_code", task.Model, "claude-sonnet-4-6")
	}

	// Task explicitly sets codex harness with claude default config
	cfg.DefaultHarness = "claude_code"
	d = cfg.TaskDefaults(models.TaskModeCode)
	task = &models.Task{Harness: models.HarnessCodex}
	d.Apply(task, nil)

	if task.Model != "gpt-5.4-mini" {
		t.Errorf("Model = %q, want %q when caller overrides harness to codex", task.Model, "gpt-5.4-mini")
	}
}
