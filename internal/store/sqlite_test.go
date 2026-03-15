package store

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/backflow-labs/backflow/internal/models"
)

func testStore(t *testing.T) *SQLiteStore {
	t.Helper()
	f, err := os.CreateTemp("", "backflow-test-*.db")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })

	s, err := NewSQLite(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestTaskCRUD(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	task := &models.Task{
		ID:           "bf_TEST001",
		Status:       models.TaskStatusPending,
		TaskMode:     models.TaskModeCode,
		RepoURL:      "https://github.com/test/repo",
		Branch:       "backflow/test",
		TargetBranch: "main",
		Prompt:       "Fix the bug",
		Model:        "claude-sonnet-4-6",
		MaxBudgetUSD: 10.0,
		MaxTurns:     200,
		CreatePR:     true,
		PRTitle:      "Fix bug",
		AllowedTools: []string{"Read", "Write"},
		EnvVars:      map[string]string{"FOO": "bar"},
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	// Create
	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// Get
	got, err := s.GetTask(ctx, "bf_TEST001")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got == nil {
		t.Fatal("GetTask returned nil")
	}
	if got.RepoURL != task.RepoURL {
		t.Errorf("RepoURL = %q, want %q", got.RepoURL, task.RepoURL)
	}
	if got.Prompt != task.Prompt {
		t.Errorf("Prompt = %q, want %q", got.Prompt, task.Prompt)
	}
	if got.TaskMode != models.TaskModeCode {
		t.Errorf("TaskMode = %q, want %q", got.TaskMode, models.TaskModeCode)
	}
	if !got.CreatePR {
		t.Error("CreatePR should be true")
	}
	if len(got.AllowedTools) != 2 {
		t.Errorf("AllowedTools len = %d, want 2", len(got.AllowedTools))
	}
	if got.EnvVars["FOO"] != "bar" {
		t.Errorf("EnvVars[FOO] = %q, want %q", got.EnvVars["FOO"], "bar")
	}

	// Update
	got.Status = models.TaskStatusRunning
	startedAt := now.Add(time.Minute)
	got.StartedAt = &startedAt
	if err := s.UpdateTask(ctx, got); err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}
	got2, _ := s.GetTask(ctx, "bf_TEST001")
	if got2.Status != models.TaskStatusRunning {
		t.Errorf("Status = %q, want %q", got2.Status, models.TaskStatusRunning)
	}

	// List
	tasks, err := s.ListTasks(ctx, TaskFilter{Limit: 10})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Errorf("ListTasks len = %d, want 1", len(tasks))
	}

	// List with filter
	pending := models.TaskStatusPending
	tasks, _ = s.ListTasks(ctx, TaskFilter{Status: &pending})
	if len(tasks) != 0 {
		t.Errorf("ListTasks(pending) len = %d, want 0", len(tasks))
	}

	// Delete
	if err := s.DeleteTask(ctx, "bf_TEST001"); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}
	got3, _ := s.GetTask(ctx, "bf_TEST001")
	if got3 != nil {
		t.Error("expected nil after delete")
	}
}

func TestGetTaskNotFound(t *testing.T) {
	s := testStore(t)
	got, err := s.GetTask(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Error("expected nil for nonexistent task")
	}
}

func TestInstanceCRUD(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	inst := &models.Instance{
		InstanceID:        "i-test123",
		InstanceType:      "m7g.xlarge",
		AvailabilityZone:  "us-east-1a",
		PrivateIP:         "10.0.1.5",
		Status:            models.InstanceStatusRunning,
		MaxContainers:     4,
		RunningContainers: 0,
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	if err := s.CreateInstance(ctx, inst); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}

	got, err := s.GetInstance(ctx, "i-test123")
	if err != nil {
		t.Fatalf("GetInstance: %v", err)
	}
	if got.InstanceType != "m7g.xlarge" {
		t.Errorf("InstanceType = %q, want m7g.xlarge", got.InstanceType)
	}
	if got.MaxContainers != 4 {
		t.Errorf("MaxContainers = %d, want 4", got.MaxContainers)
	}

	// Update
	got.RunningContainers = 2
	if err := s.UpdateInstance(ctx, got); err != nil {
		t.Fatalf("UpdateInstance: %v", err)
	}

	// List
	running := models.InstanceStatusRunning
	instances, err := s.ListInstances(ctx, &running)
	if err != nil {
		t.Fatalf("ListInstances: %v", err)
	}
	if len(instances) != 1 {
		t.Errorf("ListInstances len = %d, want 1", len(instances))
	}
	if instances[0].RunningContainers != 2 {
		t.Errorf("RunningContainers = %d, want 2", instances[0].RunningContainers)
	}
}

func TestReviewTaskCRUD(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	task := &models.Task{
		ID:             "bf_REVIEW01",
		Status:         models.TaskStatusPending,
		TaskMode:       models.TaskModeReview,
		RepoURL:        "https://github.com/test/repo",
		ReviewPRNumber: 42,
		Prompt:         "Focus on security",
		Model:          "claude-sonnet-4-6",
		MaxBudgetUSD:   5.0,
		MaxTurns:       50,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	got, err := s.GetTask(ctx, "bf_REVIEW01")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got == nil {
		t.Fatal("GetTask returned nil")
	}
	if got.TaskMode != models.TaskModeReview {
		t.Errorf("TaskMode = %q, want %q", got.TaskMode, models.TaskModeReview)
	}
	if got.ReviewPRNumber != 42 {
		t.Errorf("ReviewPRNumber = %d, want 42", got.ReviewPRNumber)
	}
	if got.Prompt != "Focus on security" {
		t.Errorf("Prompt = %q, want %q", got.Prompt, "Focus on security")
	}
}

func TestSchemaIncludesAllTaskColumns(t *testing.T) {
	s := testStore(t)

	rows, err := s.db.Query(`PRAGMA table_info(tasks)`)
	if err != nil {
		t.Fatalf("PRAGMA table_info(tasks): %v", err)
	}
	defer rows.Close()

	var (
		cid        int
		name       string
		columnType string
		notNull    int
		defaultVal any
		pk         int
	)
	var columns []string
	for rows.Next() {
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &pk); err != nil {
			t.Fatalf("scan PRAGMA row: %v", err)
		}
		columns = append(columns, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate PRAGMA rows: %v", err)
	}

	expected := []string{
		"id",
		"status",
		"task_mode",
		"repo_url",
		"branch",
		"target_branch",
		"review_pr_number",
		"prompt",
		"context",
		"model",
		"effort",
		"max_budget_usd",
		"max_runtime_min",
		"max_turns",
		"create_pr",
		"self_review",
		"pr_title",
		"pr_body",
		"pr_url",
		"allowed_tools",
		"claude_md",
		"env_vars",
		"instance_id",
		"container_id",
		"retry_count",
		"cost_usd",
		"error",
		"created_at",
		"updated_at",
		"started_at",
		"completed_at",
	}

	if len(columns) != len(expected) {
		t.Fatalf("tasks column count = %d, want %d (%v)", len(columns), len(expected), columns)
	}
	for i := range expected {
		if columns[i] != expected[i] {
			t.Fatalf("tasks column %d = %q, want %q; full schema: %v", i, columns[i], expected[i], columns)
		}
	}

	var createSQL string
	if err := s.db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'tasks'`).Scan(&createSQL); err != nil {
		t.Fatalf("read tasks schema from sqlite_master: %v", err)
	}
	for _, column := range []string{"task_mode", "review_pr_number"} {
		if !containsColumnDDL(createSQL, column) {
			t.Fatalf("tasks create statement is missing %q: %s", column, createSQL)
		}
	}
}

func containsColumnDDL(createSQL, column string) bool {
	return strings.Contains(createSQL, fmt.Sprintf("%s ", column))
}
