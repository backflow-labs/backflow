package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/backflow-labs/backflow/internal/config"
	"github.com/backflow-labs/backflow/internal/models"
	"github.com/backflow-labs/backflow/internal/notify"
)

func TestMonitorCancelled_DecrementsRunning(t *testing.T) {
	s := newMockStore()
	now := time.Now().UTC()

	s.CreateInstance(context.Background(), &models.Instance{
		InstanceID:        "local",
		Status:            models.InstanceStatusRunning,
		MaxContainers:     4,
		RunningContainers: 1,
	})

	s.CreateTask(context.Background(), &models.Task{
		ID:          "bf_cancel_run",
		Status:      models.TaskStatusCancelled,
		InstanceID:  "local",
		ContainerID: "abc123",
		StartedAt:   &now,
		CompletedAt: &now,
	})

	n := &mockNotifier{}
	o := newTestOrchestrator(s, n)
	o.running = 1

	o.monitorCancelled(context.Background())

	if o.running != 0 {
		t.Errorf("running = %d, want 0", o.running)
	}

	task, _ := s.GetTask(context.Background(), "bf_cancel_run")
	if task.ContainerID != "" {
		t.Errorf("containerID = %q, want empty (should be cleared after cleanup)", task.ContainerID)
	}

	inst, _ := s.GetInstance(context.Background(), "local")
	if inst.RunningContainers != 0 {
		t.Errorf("RunningContainers = %d, want 0", inst.RunningContainers)
	}
}

func TestMonitorCancelled_IgnoresWithoutContainer(t *testing.T) {
	s := newMockStore()
	now := time.Now().UTC()

	s.CreateTask(context.Background(), &models.Task{
		ID:          "bf_cancel_prov",
		Status:      models.TaskStatusCancelled,
		CompletedAt: &now,
	})

	n := &mockNotifier{}
	o := newTestOrchestrator(s, n)
	o.running = 0

	o.monitorCancelled(context.Background())

	if o.running != 0 {
		t.Errorf("running = %d, want 0", o.running)
	}
}

func TestMonitorCancelled_RecoveringTaskCancelled(t *testing.T) {
	s := newMockStore()
	now := time.Now().UTC()

	s.CreateInstance(context.Background(), &models.Instance{
		InstanceID:        "local",
		Status:            models.InstanceStatusRunning,
		MaxContainers:     4,
		RunningContainers: 1,
	})

	s.CreateTask(context.Background(), &models.Task{
		ID:          "bf_cancel_recov",
		Status:      models.TaskStatusCancelled,
		InstanceID:  "local",
		ContainerID: "def456",
		StartedAt:   &now,
		CompletedAt: &now,
	})

	n := &mockNotifier{}
	o := newTestOrchestrator(s, n)
	o.running = 1

	o.monitorCancelled(context.Background())

	if o.running != 0 {
		t.Errorf("running = %d, want 0", o.running)
	}

	task, _ := s.GetTask(context.Background(), "bf_cancel_recov")
	if task.ContainerID != "" {
		t.Errorf("containerID = %q, want empty", task.ContainerID)
	}
}

func TestHandleCompletion_Success(t *testing.T) {
	s := newMockStore()
	now := time.Now().UTC()

	s.CreateInstance(context.Background(), &models.Instance{
		InstanceID:        "local",
		Status:            models.InstanceStatusRunning,
		MaxContainers:     4,
		RunningContainers: 1,
	})

	s.CreateTask(context.Background(), &models.Task{
		ID:          "bf_ok",
		Status:      models.TaskStatusRunning,
		RepoURL:     "https://github.com/test/repo",
		Prompt:      "do something",
		InstanceID:  "local",
		ContainerID: "cont1",
		StartedAt:   &now,
	})

	n := &mockNotifier{}
	o := newTestOrchestrator(s, n)
	o.running = 1

	task, _ := s.GetTask(context.Background(), "bf_ok")
	o.handleCompletion(context.Background(), task, ContainerStatus{
		Done:     true,
		ExitCode: 0,
		PRURL:    "https://github.com/test/repo/pull/1",
	})

	task, _ = s.GetTask(context.Background(), "bf_ok")
	if task.Status != models.TaskStatusCompleted {
		t.Errorf("status = %q, want completed", task.Status)
	}
	if task.PRURL != "https://github.com/test/repo/pull/1" {
		t.Errorf("PRURL = %q, want PR URL", task.PRURL)
	}
	if task.CompletedAt == nil {
		t.Error("CompletedAt should be set")
	}
	if o.running != 0 {
		t.Errorf("running = %d, want 0", o.running)
	}

	inst, _ := s.GetInstance(context.Background(), "local")
	if inst.RunningContainers != 0 {
		t.Errorf("RunningContainers = %d, want 0", inst.RunningContainers)
	}

	types := n.eventTypes()
	if len(types) != 1 || types[0] != notify.EventTaskCompleted {
		t.Errorf("expected [task.completed], got %v", types)
	}
}

func TestHandleCompletion_NeedsInput(t *testing.T) {
	s := newMockStore()
	now := time.Now().UTC()

	s.CreateInstance(context.Background(), newLocalInstance())

	s.CreateTask(context.Background(), &models.Task{
		ID:          "bf_input",
		Status:      models.TaskStatusRunning,
		RepoURL:     "https://github.com/test/repo",
		Prompt:      "do something",
		InstanceID:  "local",
		ContainerID: "cont1",
		StartedAt:   &now,
	})

	n := &mockNotifier{}
	o := newTestOrchestrator(s, n)
	o.running = 1

	task, _ := s.GetTask(context.Background(), "bf_input")
	o.handleCompletion(context.Background(), task, ContainerStatus{
		Done:       true,
		ExitCode:   1,
		NeedsInput: true,
		Question:   "What is the database password?",
	})

	task, _ = s.GetTask(context.Background(), "bf_input")
	if task.Status != models.TaskStatusFailed {
		t.Errorf("status = %q, want failed", task.Status)
	}
	if task.Error != "agent needs input" {
		t.Errorf("error = %q, want 'agent needs input'", task.Error)
	}
	if o.running != 0 {
		t.Errorf("running = %d, want 0", o.running)
	}

	types := n.eventTypes()
	if len(types) != 1 || types[0] != notify.EventTaskNeedsInput {
		t.Errorf("expected [task.needs_input], got %v", types)
	}
}

func TestHandleCompletion_Failure(t *testing.T) {
	s := newMockStore()
	now := time.Now().UTC()

	s.CreateInstance(context.Background(), newLocalInstance())

	s.CreateTask(context.Background(), &models.Task{
		ID:          "bf_fail",
		Status:      models.TaskStatusRunning,
		RepoURL:     "https://github.com/test/repo",
		Prompt:      "do something",
		InstanceID:  "local",
		ContainerID: "cont1",
		StartedAt:   &now,
	})

	n := &mockNotifier{}
	o := newTestOrchestrator(s, n)
	o.running = 1

	task, _ := s.GetTask(context.Background(), "bf_fail")
	o.handleCompletion(context.Background(), task, ContainerStatus{
		Done:     true,
		ExitCode: 1,
		Error:    "something went wrong",
	})

	task, _ = s.GetTask(context.Background(), "bf_fail")
	if task.Status != models.TaskStatusFailed {
		t.Errorf("status = %q, want failed", task.Status)
	}
	if task.Error != "something went wrong" {
		t.Errorf("error = %q, want 'something went wrong'", task.Error)
	}

	types := n.eventTypes()
	if len(types) != 1 || types[0] != notify.EventTaskFailed {
		t.Errorf("expected [task.failed], got %v", types)
	}
}

func TestKillTask(t *testing.T) {
	s := newMockStore()
	now := time.Now().UTC()

	s.CreateInstance(context.Background(), &models.Instance{
		InstanceID:        "local",
		Status:            models.InstanceStatusRunning,
		MaxContainers:     4,
		RunningContainers: 1,
	})

	s.CreateTask(context.Background(), &models.Task{
		ID:          "bf_kill",
		Status:      models.TaskStatusRunning,
		RepoURL:     "https://github.com/test/repo",
		Prompt:      "long running task",
		InstanceID:  "local",
		ContainerID: "cont_kill",
		StartedAt:   &now,
	})

	n := &mockNotifier{}
	o := newTestOrchestrator(s, n)
	o.running = 1

	task, _ := s.GetTask(context.Background(), "bf_kill")
	o.killTask(context.Background(), task, "exceeded max runtime")

	task, _ = s.GetTask(context.Background(), "bf_kill")
	if task.Status != models.TaskStatusFailed {
		t.Errorf("status = %q, want failed", task.Status)
	}
	if task.Error != "exceeded max runtime" {
		t.Errorf("error = %q, want 'exceeded max runtime'", task.Error)
	}
	if task.CompletedAt == nil {
		t.Error("CompletedAt should be set")
	}
	if o.running != 0 {
		t.Errorf("running = %d, want 0", o.running)
	}

	inst, _ := s.GetInstance(context.Background(), "local")
	if inst.RunningContainers != 0 {
		t.Errorf("RunningContainers = %d, want 0", inst.RunningContainers)
	}

	types := n.eventTypes()
	if len(types) != 1 || types[0] != notify.EventTaskFailed {
		t.Errorf("expected [task.failed], got %v", types)
	}
}

func TestRequeueTask_EC2Mode(t *testing.T) {
	s := newMockStore()
	now := time.Now().UTC()

	s.CreateInstance(context.Background(), &models.Instance{
		InstanceID:        "i-abc",
		Status:            models.InstanceStatusRunning,
		MaxContainers:     4,
		RunningContainers: 1,
	})

	s.CreateTask(context.Background(), &models.Task{
		ID:          "bf_requeue",
		Status:      models.TaskStatusRunning,
		RepoURL:     "https://github.com/test/repo",
		Prompt:      "requeue me",
		InstanceID:  "i-abc",
		ContainerID: "cont_rq",
		StartedAt:   &now,
	})

	n := &mockNotifier{}
	o := newTestOrchestrator(s, n, func(o *Orchestrator) {
		o.config.Mode = config.ModeEC2
	})
	o.running = 1

	task, _ := s.GetTask(context.Background(), "bf_requeue")
	o.requeueTask(context.Background(), task, "instance terminated")

	task, _ = s.GetTask(context.Background(), "bf_requeue")
	if task.Status != models.TaskStatusPending {
		t.Errorf("status = %q, want pending", task.Status)
	}
	if task.InstanceID != "" {
		t.Errorf("instanceID = %q, want empty", task.InstanceID)
	}
	if task.ContainerID != "" {
		t.Errorf("containerID = %q, want empty", task.ContainerID)
	}
	if task.StartedAt != nil {
		t.Error("StartedAt should be nil")
	}
	if task.RetryCount != 1 {
		t.Errorf("retry count = %d, want 1", task.RetryCount)
	}
	if o.running != 0 {
		t.Errorf("running = %d, want 0", o.running)
	}

	// Instance should be marked terminated in EC2 mode
	inst, _ := s.GetInstance(context.Background(), "i-abc")
	if inst.Status != models.InstanceStatusTerminated {
		t.Errorf("instance status = %q, want terminated", inst.Status)
	}
}

func TestRequeueTask_LocalMode(t *testing.T) {
	s := newMockStore()
	now := time.Now().UTC()

	s.CreateInstance(context.Background(), &models.Instance{
		InstanceID:        "local",
		Status:            models.InstanceStatusRunning,
		MaxContainers:     4,
		RunningContainers: 1,
	})

	s.CreateTask(context.Background(), &models.Task{
		ID:          "bf_requeue_local",
		Status:      models.TaskStatusRunning,
		RepoURL:     "https://github.com/test/repo",
		Prompt:      "requeue me",
		InstanceID:  "local",
		ContainerID: "cont_rq",
		StartedAt:   &now,
	})

	n := &mockNotifier{}
	o := newTestOrchestrator(s, n) // defaults to ModeLocal
	o.running = 1

	task, _ := s.GetTask(context.Background(), "bf_requeue_local")
	o.requeueTask(context.Background(), task, "container gone")

	task, _ = s.GetTask(context.Background(), "bf_requeue_local")
	if task.Status != models.TaskStatusPending {
		t.Errorf("status = %q, want pending", task.Status)
	}
	if o.running != 0 {
		t.Errorf("running = %d, want 0", o.running)
	}

	// Instance should NOT be terminated in local mode
	inst, _ := s.GetInstance(context.Background(), "local")
	if inst.Status != models.InstanceStatusRunning {
		t.Errorf("instance status = %q, want running (local mode should not terminate)", inst.Status)
	}
}

func TestIsTimedOut(t *testing.T) {
	o := newTestOrchestrator(newMockStore(), &mockNotifier{})

	// No StartedAt — not timed out
	task := &models.Task{MaxRuntimeMin: 10}
	if o.isTimedOut(task) {
		t.Error("task without StartedAt should not be timed out")
	}

	// MaxRuntimeMin = 0 — no timeout
	now := time.Now().UTC()
	task = &models.Task{StartedAt: &now, MaxRuntimeMin: 0}
	if o.isTimedOut(task) {
		t.Error("task with MaxRuntimeMin=0 should not be timed out")
	}

	// Recently started — not timed out
	task = &models.Task{StartedAt: &now, MaxRuntimeMin: 10}
	if o.isTimedOut(task) {
		t.Error("recently started task should not be timed out")
	}

	// Started long ago — timed out
	past := time.Now().UTC().Add(-20 * time.Minute)
	task = &models.Task{StartedAt: &past, MaxRuntimeMin: 10}
	if !o.isTimedOut(task) {
		t.Error("task past deadline should be timed out")
	}
}
