package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/backflow-labs/backflow/internal/models"
)

func testPostgresStore(t *testing.T) *PostgresStore {
	t.Helper()
	ctx := context.Background()

	connStr := SetupTestDB(t)
	s, err := NewPostgres(ctx, connStr)
	if err != nil {
		t.Fatalf("NewPostgres: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestTaskCRUD(t *testing.T) {
	s := testPostgresStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)

	task := &models.Task{
		ID:           "bf_TEST001",
		Status:       models.TaskStatusPending,
		TaskMode:     models.TaskModeCode,
		Harness:      models.HarnessClaudeCode,
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
	if got.Harness != models.HarnessClaudeCode {
		t.Errorf("Harness = %q, want %q", got.Harness, models.HarnessClaudeCode)
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

	// Update via named methods
	if err := s.StartTask(ctx, "bf_TEST001", "container-1"); err != nil {
		t.Fatalf("StartTask: %v", err)
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
	got3, err := s.GetTask(ctx, "bf_TEST001")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
	if got3 != nil {
		t.Error("expected nil after delete")
	}
}

func TestGetTaskNotFound(t *testing.T) {
	s := testPostgresStore(t)
	got, err := s.GetTask(context.Background(), "nonexistent")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if got != nil {
		t.Error("expected nil for nonexistent task")
	}
}

func TestGetInstanceNotFound(t *testing.T) {
	s := testPostgresStore(t)
	got, err := s.GetInstance(context.Background(), "nonexistent")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if got != nil {
		t.Error("expected nil for nonexistent instance")
	}
}

func TestInstanceCRUD(t *testing.T) {
	s := testPostgresStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)

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

	// Update via named methods
	s.IncrementRunningContainers(ctx, "i-test123")
	s.IncrementRunningContainers(ctx, "i-test123")

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
	s := testPostgresStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)

	task := &models.Task{
		ID:             "bf_REVIEW01",
		Status:         models.TaskStatusPending,
		TaskMode:       models.TaskModeReview,
		RepoURL:        "https://github.com/test/repo",
		ReviewPRURL:    "https://github.com/test/repo/pull/42",
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
	if got.ReviewPRURL != "https://github.com/test/repo/pull/42" {
		t.Errorf("ReviewPRURL = %q, want %q", got.ReviewPRURL, "https://github.com/test/repo/pull/42")
	}
	if got.ReviewPRNumber != 42 {
		t.Errorf("ReviewPRNumber = %d, want 42", got.ReviewPRNumber)
	}
	if got.Prompt != "Focus on security" {
		t.Errorf("Prompt = %q, want %q", got.Prompt, "Focus on security")
	}
}

// --- Named update method tests ---

func createTestTask(t *testing.T, s *PostgresStore) *models.Task {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	task := &models.Task{
		ID:        "bf_TEST001",
		Status:    models.TaskStatusPending,
		TaskMode:  models.TaskModeCode,
		Harness:   models.HarnessClaudeCode,
		RepoURL:   "https://github.com/test/repo",
		Branch:    "backflow/test",
		Prompt:    "Fix the bug",
		Model:     "claude-sonnet-4-6",
		CreatePR:  true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	return task
}

func createTestInstance(t *testing.T, s *PostgresStore) *models.Instance {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
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
	return inst
}

func TestUpdateTaskStatus(t *testing.T) {
	s := testPostgresStore(t)
	ctx := context.Background()
	task := createTestTask(t, s)

	if err := s.UpdateTaskStatus(ctx, task.ID, models.TaskStatusFailed, "something broke"); err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}

	got, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != models.TaskStatusFailed {
		t.Errorf("Status = %q, want %q", got.Status, models.TaskStatusFailed)
	}
	if got.Error != "something broke" {
		t.Errorf("Error = %q, want %q", got.Error, "something broke")
	}
	// Verify other fields aren't clobbered
	if got.Prompt != "Fix the bug" {
		t.Errorf("Prompt was clobbered: %q", got.Prompt)
	}
	if !got.CreatePR {
		t.Error("CreatePR was clobbered")
	}
}

func TestAssignTask(t *testing.T) {
	s := testPostgresStore(t)
	ctx := context.Background()
	createTestTask(t, s)

	if err := s.AssignTask(ctx, "bf_TEST001", "i-abc123"); err != nil {
		t.Fatalf("AssignTask: %v", err)
	}

	got, err := s.GetTask(ctx, "bf_TEST001")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != models.TaskStatusProvisioning {
		t.Errorf("Status = %q, want %q", got.Status, models.TaskStatusProvisioning)
	}
	if got.InstanceID != "i-abc123" {
		t.Errorf("InstanceID = %q, want %q", got.InstanceID, "i-abc123")
	}
	if got.Prompt != "Fix the bug" {
		t.Errorf("Prompt was clobbered: %q", got.Prompt)
	}
}

func TestStartTask(t *testing.T) {
	s := testPostgresStore(t)
	ctx := context.Background()
	createTestTask(t, s)

	if err := s.StartTask(ctx, "bf_TEST001", "container-abc"); err != nil {
		t.Fatalf("StartTask: %v", err)
	}

	got, err := s.GetTask(ctx, "bf_TEST001")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != models.TaskStatusRunning {
		t.Errorf("Status = %q, want %q", got.Status, models.TaskStatusRunning)
	}
	if got.ContainerID != "container-abc" {
		t.Errorf("ContainerID = %q, want %q", got.ContainerID, "container-abc")
	}
	if got.StartedAt == nil {
		t.Fatal("StartedAt should be set")
	}
	if time.Since(*got.StartedAt) > 5*time.Second {
		t.Errorf("StartedAt too old: %v", got.StartedAt)
	}
}

func TestCompleteTask(t *testing.T) {
	s := testPostgresStore(t)
	ctx := context.Background()
	createTestTask(t, s)

	result := TaskResult{
		Status:         models.TaskStatusCompleted,
		Error:          "",
		PRURL:          "https://github.com/test/repo/pull/1",
		OutputURL:      "s3://bucket/output.log",
		CostUSD:        1.23,
		ElapsedTimeSec: 120,
	}
	if err := s.CompleteTask(ctx, "bf_TEST001", result); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}

	got, err := s.GetTask(ctx, "bf_TEST001")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != models.TaskStatusCompleted {
		t.Errorf("Status = %q, want %q", got.Status, models.TaskStatusCompleted)
	}
	if got.PRURL != "https://github.com/test/repo/pull/1" {
		t.Errorf("PRURL = %q", got.PRURL)
	}
	if got.OutputURL != "s3://bucket/output.log" {
		t.Errorf("OutputURL = %q", got.OutputURL)
	}
	if got.CostUSD != 1.23 {
		t.Errorf("CostUSD = %f, want 1.23", got.CostUSD)
	}
	if got.ElapsedTimeSec != 120 {
		t.Errorf("ElapsedTimeSec = %d, want 120", got.ElapsedTimeSec)
	}
	if got.CompletedAt == nil {
		t.Fatal("CompletedAt should be set")
	}
	if got.Prompt != "Fix the bug" {
		t.Errorf("Prompt was clobbered: %q", got.Prompt)
	}
}

func TestRequeueTask(t *testing.T) {
	s := testPostgresStore(t)
	ctx := context.Background()
	task := createTestTask(t, s)

	// Set task to running with instance/container first
	s.AssignTask(ctx, task.ID, "i-abc123")
	s.StartTask(ctx, task.ID, "container-abc")

	if err := s.RequeueTask(ctx, task.ID, "instance terminated"); err != nil {
		t.Fatalf("RequeueTask: %v", err)
	}

	got, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != models.TaskStatusPending {
		t.Errorf("Status = %q, want %q", got.Status, models.TaskStatusPending)
	}
	if got.InstanceID != "" {
		t.Errorf("InstanceID should be cleared, got %q", got.InstanceID)
	}
	if got.ContainerID != "" {
		t.Errorf("ContainerID should be cleared, got %q", got.ContainerID)
	}
	if got.StartedAt != nil {
		t.Error("StartedAt should be cleared")
	}
	if got.RetryCount != 1 {
		t.Errorf("RetryCount = %d, want 1", got.RetryCount)
	}
	if got.Error == "" {
		t.Error("Error should contain the reason")
	}
}

func TestCancelTask(t *testing.T) {
	s := testPostgresStore(t)
	ctx := context.Background()
	createTestTask(t, s)

	if err := s.CancelTask(ctx, "bf_TEST001"); err != nil {
		t.Fatalf("CancelTask: %v", err)
	}

	got, err := s.GetTask(ctx, "bf_TEST001")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != models.TaskStatusCancelled {
		t.Errorf("Status = %q, want %q", got.Status, models.TaskStatusCancelled)
	}
	if got.CompletedAt == nil {
		t.Fatal("CompletedAt should be set")
	}
}

func TestClearTaskAssignment(t *testing.T) {
	s := testPostgresStore(t)
	ctx := context.Background()
	createTestTask(t, s)

	s.AssignTask(ctx, "bf_TEST001", "i-abc123")
	s.StartTask(ctx, "bf_TEST001", "container-abc")

	if err := s.ClearTaskAssignment(ctx, "bf_TEST001"); err != nil {
		t.Fatalf("ClearTaskAssignment: %v", err)
	}

	got, err := s.GetTask(ctx, "bf_TEST001")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.InstanceID != "" {
		t.Errorf("InstanceID should be cleared, got %q", got.InstanceID)
	}
	if got.ContainerID != "" {
		t.Errorf("ContainerID should be cleared, got %q", got.ContainerID)
	}
}

func TestUpdateInstanceStatus(t *testing.T) {
	s := testPostgresStore(t)
	ctx := context.Background()
	createTestInstance(t, s)

	if err := s.UpdateInstanceStatus(ctx, "i-test123", models.InstanceStatusTerminated); err != nil {
		t.Fatalf("UpdateInstanceStatus: %v", err)
	}

	got, _ := s.GetInstance(ctx, "i-test123")
	if got.Status != models.InstanceStatusTerminated {
		t.Errorf("Status = %q, want %q", got.Status, models.InstanceStatusTerminated)
	}
	if got.RunningContainers != 0 {
		t.Errorf("RunningContainers = %d, want 0 (should zero on terminate)", got.RunningContainers)
	}
	if got.InstanceType != "m7g.xlarge" {
		t.Errorf("InstanceType was clobbered: %q", got.InstanceType)
	}
}

func TestIncrementDecrementRunningContainers(t *testing.T) {
	s := testPostgresStore(t)
	ctx := context.Background()
	createTestInstance(t, s)

	if err := s.IncrementRunningContainers(ctx, "i-test123"); err != nil {
		t.Fatalf("IncrementRunningContainers: %v", err)
	}
	got, _ := s.GetInstance(ctx, "i-test123")
	if got.RunningContainers != 1 {
		t.Errorf("RunningContainers = %d, want 1", got.RunningContainers)
	}

	s.IncrementRunningContainers(ctx, "i-test123")
	got, _ = s.GetInstance(ctx, "i-test123")
	if got.RunningContainers != 2 {
		t.Errorf("RunningContainers = %d, want 2", got.RunningContainers)
	}

	if err := s.DecrementRunningContainers(ctx, "i-test123"); err != nil {
		t.Fatalf("DecrementRunningContainers: %v", err)
	}
	got, _ = s.GetInstance(ctx, "i-test123")
	if got.RunningContainers != 1 {
		t.Errorf("RunningContainers = %d, want 1", got.RunningContainers)
	}

	// Decrement to zero, then once more — should floor at 0
	s.DecrementRunningContainers(ctx, "i-test123")
	s.DecrementRunningContainers(ctx, "i-test123")
	got, _ = s.GetInstance(ctx, "i-test123")
	if got.RunningContainers != 0 {
		t.Errorf("RunningContainers = %d, want 0 (should floor at zero)", got.RunningContainers)
	}
}

func TestUpdateInstanceDetails(t *testing.T) {
	s := testPostgresStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	inst := &models.Instance{
		InstanceID:   "i-new",
		InstanceType: "m7g.xlarge",
		Status:       models.InstanceStatusPending,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	s.CreateInstance(ctx, inst)

	if err := s.UpdateInstanceDetails(ctx, "i-new", "10.0.1.99", "us-west-2b"); err != nil {
		t.Fatalf("UpdateInstanceDetails: %v", err)
	}

	got, _ := s.GetInstance(ctx, "i-new")
	if got.PrivateIP != "10.0.1.99" {
		t.Errorf("PrivateIP = %q, want 10.0.1.99", got.PrivateIP)
	}
	if got.AvailabilityZone != "us-west-2b" {
		t.Errorf("AvailabilityZone = %q, want us-west-2b", got.AvailabilityZone)
	}
}

func TestResetRunningContainers(t *testing.T) {
	s := testPostgresStore(t)
	ctx := context.Background()
	inst := createTestInstance(t, s)

	s.IncrementRunningContainers(ctx, inst.InstanceID)
	s.IncrementRunningContainers(ctx, inst.InstanceID)

	if err := s.ResetRunningContainers(ctx, inst.InstanceID); err != nil {
		t.Fatalf("ResetRunningContainers: %v", err)
	}

	got, _ := s.GetInstance(ctx, inst.InstanceID)
	if got.RunningContainers != 0 {
		t.Errorf("RunningContainers = %d, want 0", got.RunningContainers)
	}
}

func TestCreateAllowedSender(t *testing.T) {
	s := testPostgresStore(t)
	ctx := context.Background()

	sender := &models.AllowedSender{
		ChannelType: "sms",
		Address:     "+15551234567",
		DefaultRepo: "https://github.com/test/repo",
		Enabled:     true,
		CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
	}

	if err := s.CreateAllowedSender(ctx, sender); err != nil {
		t.Fatalf("CreateAllowedSender: %v", err)
	}

	got, err := s.GetAllowedSender(ctx, "sms", "+15551234567")
	if err != nil {
		t.Fatalf("GetAllowedSender: %v", err)
	}
	if got.ChannelType != "sms" {
		t.Errorf("ChannelType = %q, want sms", got.ChannelType)
	}
	if got.Address != "+15551234567" {
		t.Errorf("Address = %q", got.Address)
	}
	if got.DefaultRepo != "https://github.com/test/repo" {
		t.Errorf("DefaultRepo = %q", got.DefaultRepo)
	}
	if !got.Enabled {
		t.Error("Enabled should be true")
	}
}

func TestGetAllowedSenderNotFound(t *testing.T) {
	s := testPostgresStore(t)
	got, err := s.GetAllowedSender(context.Background(), "sms", "+10000000000")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if got != nil {
		t.Error("expected nil for nonexistent sender")
	}
}

func TestWithTx_Commit(t *testing.T) {
	s := testPostgresStore(t)
	ctx := context.Background()
	createTestTask(t, s)
	createTestInstance(t, s)

	err := s.WithTx(ctx, func(tx Store) error {
		if err := tx.AssignTask(ctx, "bf_TEST001", "i-test123"); err != nil {
			return err
		}
		return tx.IncrementRunningContainers(ctx, "i-test123")
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}

	task, _ := s.GetTask(ctx, "bf_TEST001")
	if task.Status != models.TaskStatusProvisioning {
		t.Errorf("Status = %q, want provisioning", task.Status)
	}
	inst, _ := s.GetInstance(ctx, "i-test123")
	if inst.RunningContainers != 1 {
		t.Errorf("RunningContainers = %d, want 1", inst.RunningContainers)
	}
}

func TestWithTx_Rollback(t *testing.T) {
	s := testPostgresStore(t)
	ctx := context.Background()
	createTestTask(t, s)
	createTestInstance(t, s)

	err := s.WithTx(ctx, func(tx Store) error {
		tx.AssignTask(ctx, "bf_TEST001", "i-test123")
		tx.IncrementRunningContainers(ctx, "i-test123")
		return fmt.Errorf("something failed")
	})
	if err == nil {
		t.Fatal("expected error from WithTx")
	}

	// Both should be rolled back
	task, _ := s.GetTask(ctx, "bf_TEST001")
	if task.Status != models.TaskStatusPending {
		t.Errorf("Status = %q, want pending (should have rolled back)", task.Status)
	}
	inst, _ := s.GetInstance(ctx, "i-test123")
	if inst.RunningContainers != 0 {
		t.Errorf("RunningContainers = %d, want 0 (should have rolled back)", inst.RunningContainers)
	}
}
