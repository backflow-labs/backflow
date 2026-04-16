package api

import (
	"context"

	"github.com/backflow-labs/backflow/internal/config"
	"github.com/backflow-labs/backflow/internal/models"
	"github.com/backflow-labs/backflow/internal/notify"
	"github.com/backflow-labs/backflow/internal/store"
	"github.com/backflow-labs/backflow/internal/taskcreate"
)

var ErrStoreFailure = taskcreate.ErrStoreFailure

func NewTask(ctx context.Context, req *models.CreateTaskRequest, s store.Store, cfg *config.Config, bus notify.Emitter) (*models.Task, error) {
	task, err := taskcreate.NewTask(ctx, req, s, cfg)
	if err != nil {
		return nil, err
	}
	if bus != nil {
		bus.Emit(notify.NewEvent(notify.EventTaskCreated, task))
	}
	return task, nil
}

func NewReadTask(ctx context.Context, req *models.CreateTaskRequest, s store.Store, cfg *config.Config, bus notify.Emitter) (*models.Task, error) {
	task, err := taskcreate.NewReadTask(ctx, req, s, cfg)
	if err != nil {
		return nil, err
	}
	if bus != nil {
		bus.Emit(notify.NewEvent(notify.EventTaskCreated, task))
	}
	return task, nil
}
