package api

import (
	"context"
	"fmt"
	"testing"

	"github.com/backflow-labs/backflow/internal/models"
	"github.com/backflow-labs/backflow/internal/notify"
	"github.com/backflow-labs/backflow/internal/store"
)

// taskActionStore implements the subset of store.Store needed by CancelTask and RetryTask.
type taskActionStore struct {
	store.Store
	task       *models.Task
	getErr     error
	cancelErr  error
	requeueErr error
	cancelled  []string
	requeued   []string
}

func (s *taskActionStore) GetTask(_ context.Context, id string) (*models.Task, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	if s.task != nil && s.task.ID == id {
		return s.task, nil
	}
	return nil, store.ErrNotFound
}

func (s *taskActionStore) CancelTask(_ context.Context, id string) error {
	s.cancelled = append(s.cancelled, id)
	return s.cancelErr
}

func (s *taskActionStore) RequeueTask(_ context.Context, id, _ string) error {
	s.requeued = append(s.requeued, id)
	return s.requeueErr
}

// --- CancelTask tests ---

func TestCancelTask_RunningTask(t *testing.T) {
	s := &taskActionStore{
		task: &models.Task{ID: "bf_1", Status: models.TaskStatusRunning},
	}
	bus := &capturingEmitter2{}

	err := CancelTask(context.Background(), "bf_1", s, bus)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(s.cancelled) != 1 || s.cancelled[0] != "bf_1" {
		t.Errorf("cancelled = %v, want [bf_1]", s.cancelled)
	}
	if len(bus.events) != 1 || bus.events[0].Type != notify.EventTaskCancelled {
		t.Errorf("events = %v, want one EventTaskCancelled", bus.events)
	}
}

func TestCancelTask_ProvisioningTask(t *testing.T) {
	s := &taskActionStore{
		task: &models.Task{ID: "bf_1", Status: models.TaskStatusProvisioning},
	}
	bus := &capturingEmitter2{}

	err := CancelTask(context.Background(), "bf_1", s, bus)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(s.cancelled) != 1 {
		t.Errorf("cancelled = %v, want [bf_1]", s.cancelled)
	}
}

func TestCancelTask_PendingTask(t *testing.T) {
	s := &taskActionStore{
		task: &models.Task{ID: "bf_1", Status: models.TaskStatusPending},
	}
	bus := &capturingEmitter2{}

	err := CancelTask(context.Background(), "bf_1", s, bus)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(s.cancelled) != 1 {
		t.Errorf("cancelled = %v, want [bf_1]", s.cancelled)
	}
}

func TestCancelTask_RecoveringTask(t *testing.T) {
	s := &taskActionStore{
		task: &models.Task{ID: "bf_1", Status: models.TaskStatusRecovering},
	}
	bus := &capturingEmitter2{}

	err := CancelTask(context.Background(), "bf_1", s, bus)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(s.cancelled) != 1 {
		t.Errorf("cancelled = %v, want [bf_1]", s.cancelled)
	}
}

func TestCancelTask_CompletedTask_ReturnsError(t *testing.T) {
	s := &taskActionStore{
		task: &models.Task{ID: "bf_1", Status: models.TaskStatusCompleted},
	}
	bus := &capturingEmitter2{}

	err := CancelTask(context.Background(), "bf_1", s, bus)
	if err == nil {
		t.Fatal("expected error for completed task")
	}
	if len(s.cancelled) != 0 {
		t.Errorf("should not have called CancelTask on store")
	}
	if len(bus.events) != 0 {
		t.Errorf("should not have emitted events")
	}
}

func TestCancelTask_NotFound(t *testing.T) {
	s := &taskActionStore{getErr: store.ErrNotFound}
	bus := &capturingEmitter2{}

	err := CancelTask(context.Background(), "bf_missing", s, bus)
	if err == nil {
		t.Fatal("expected error for missing task")
	}
}

func TestCancelTask_StoreError(t *testing.T) {
	s := &taskActionStore{
		task:      &models.Task{ID: "bf_1", Status: models.TaskStatusRunning},
		cancelErr: fmt.Errorf("db error"),
	}
	bus := &capturingEmitter2{}

	err := CancelTask(context.Background(), "bf_1", s, bus)
	if err == nil {
		t.Fatal("expected error when store fails")
	}
	if len(bus.events) != 0 {
		t.Errorf("should not emit event on store failure")
	}
}

// --- RetryTask tests ---

func TestRetryTask_FailedTask(t *testing.T) {
	s := &taskActionStore{
		task: &models.Task{ID: "bf_1", Status: models.TaskStatusFailed},
	}

	err := RetryTask(context.Background(), "bf_1", s)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(s.requeued) != 1 || s.requeued[0] != "bf_1" {
		t.Errorf("requeued = %v, want [bf_1]", s.requeued)
	}
}

func TestRetryTask_InterruptedTask(t *testing.T) {
	s := &taskActionStore{
		task: &models.Task{ID: "bf_1", Status: models.TaskStatusInterrupted},
	}

	err := RetryTask(context.Background(), "bf_1", s)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(s.requeued) != 1 {
		t.Errorf("requeued = %v, want [bf_1]", s.requeued)
	}
}

func TestRetryTask_CancelledTask(t *testing.T) {
	s := &taskActionStore{
		task: &models.Task{ID: "bf_1", Status: models.TaskStatusCancelled},
	}

	err := RetryTask(context.Background(), "bf_1", s)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(s.requeued) != 1 {
		t.Errorf("requeued = %v, want [bf_1]", s.requeued)
	}
}

func TestRetryTask_CancelledButContainerStillRunning_ReturnsError(t *testing.T) {
	s := &taskActionStore{
		task: &models.Task{ID: "bf_1", Status: models.TaskStatusCancelled, ContainerID: "arn:aws:ecs:task/123"},
	}

	err := RetryTask(context.Background(), "bf_1", s)
	if err == nil {
		t.Fatal("expected error when container is still assigned")
	}
	if len(s.requeued) != 0 {
		t.Errorf("should not have requeued, got %v", s.requeued)
	}
}

func TestRetryTask_RunningTask_ReturnsError(t *testing.T) {
	s := &taskActionStore{
		task: &models.Task{ID: "bf_1", Status: models.TaskStatusRunning},
	}

	err := RetryTask(context.Background(), "bf_1", s)
	if err == nil {
		t.Fatal("expected error for running task")
	}
	if len(s.requeued) != 0 {
		t.Errorf("should not have requeued")
	}
}

func TestRetryTask_NotFound(t *testing.T) {
	s := &taskActionStore{getErr: store.ErrNotFound}

	err := RetryTask(context.Background(), "bf_missing", s)
	if err == nil {
		t.Fatal("expected error for missing task")
	}
}

func TestRetryTask_StoreError(t *testing.T) {
	s := &taskActionStore{
		task:       &models.Task{ID: "bf_1", Status: models.TaskStatusFailed},
		requeueErr: fmt.Errorf("db error"),
	}

	err := RetryTask(context.Background(), "bf_1", s)
	if err == nil {
		t.Fatal("expected error when store fails")
	}
}
