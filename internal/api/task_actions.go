package api

import (
	"context"
	"fmt"

	"github.com/backflow-labs/backflow/internal/models"
	"github.com/backflow-labs/backflow/internal/notify"
	"github.com/backflow-labs/backflow/internal/store"
)

// CancelTask validates the task is in a cancellable state, cancels it in the store,
// and emits a task.cancelled event. This is the shared implementation used by both
// the REST API handler and the Discord interaction handler.
func CancelTask(ctx context.Context, taskID string, s store.Store, bus notify.Emitter) error {
	task, err := s.GetTask(ctx, taskID)
	if err != nil {
		return err
	}

	switch task.Status {
	case models.TaskStatusPending, models.TaskStatusProvisioning, models.TaskStatusRunning, models.TaskStatusRecovering:
		if err := s.CancelTask(ctx, taskID); err != nil {
			return err
		}
		if bus != nil {
			bus.Emit(notify.NewEvent(notify.EventTaskCancelled, task))
		}
		return nil
	default:
		return fmt.Errorf("task %s cannot be cancelled (status: %s)", taskID, task.Status)
	}
}

// RetryTask validates the task is in a retryable state and requeues it.
// This is the shared implementation used by both the REST API handler
// and the Discord interaction handler.
func RetryTask(ctx context.Context, taskID string, s store.Store) error {
	task, err := s.GetTask(ctx, taskID)
	if err != nil {
		return err
	}

	if task.ContainerID != "" {
		return fmt.Errorf("task %s is still being cleaned up, try again shortly", taskID)
	}

	switch task.Status {
	case models.TaskStatusFailed, models.TaskStatusInterrupted, models.TaskStatusCancelled:
		return s.RequeueTask(ctx, taskID, "user_retry")
	default:
		return fmt.Errorf("task %s cannot be retried (status: %s)", taskID, task.Status)
	}
}
