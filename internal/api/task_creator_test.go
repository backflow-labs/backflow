package api

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/backflow-labs/backflow/internal/config"
	"github.com/backflow-labs/backflow/internal/models"
	"github.com/backflow-labs/backflow/internal/notify"
	"github.com/backflow-labs/backflow/internal/store"
)

// mockStore implements store.Store for unit tests that need a failing CreateTask.
type mockStore struct {
	store.Store
	createErr error
}

func (m *mockStore) CreateTask(_ context.Context, _ *models.Task) error {
	return m.createErr
}

func (m *mockStore) HasAPIKeys(_ context.Context) (bool, error) {
	return false, nil
}

func (m *mockStore) GetAPIKeyByHash(_ context.Context, _ string) (*models.APIKey, error) {
	return nil, store.ErrNotFound
}

func (m *mockStore) CreateAPIKey(_ context.Context, _ *models.APIKey) error {
	return nil
}

func TestNewTask_StoreError_ReturnsErrStoreFailure(t *testing.T) {
	cfg := &config.Config{}
	s := &mockStore{createErr: fmt.Errorf("connection refused")}
	req := &models.CreateTaskRequest{
		Prompt: "Fix bug",
	}

	_, err := NewTask(context.Background(), req, s, cfg, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrStoreFailure) {
		t.Errorf("error = %v, want errors.Is(err, ErrStoreFailure)", err)
	}
}

func TestNewTask_ValidationError_NotStoreFailure(t *testing.T) {
	cfg := &config.Config{}
	s := &mockStore{}
	req := &models.CreateTaskRequest{
		Prompt: "",
	}

	_, err := NewTask(context.Background(), req, s, cfg, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if errors.Is(err, ErrStoreFailure) {
		t.Errorf("validation error should not match ErrStoreFailure, got: %v", err)
	}
}

type capturingEmitter2 struct {
	events []notify.Event
}

func (c *capturingEmitter2) Emit(e notify.Event) { c.events = append(c.events, e) }
