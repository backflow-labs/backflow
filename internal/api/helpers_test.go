package api

import (
	"context"

	"github.com/backflow-labs/backflow/internal/models"
	"github.com/backflow-labs/backflow/internal/notify"
	"github.com/backflow-labs/backflow/internal/store"
)

type noopLogFetcher struct{}

func (noopLogFetcher) GetLogs(_ context.Context, _, _ string, _ int) (string, error) {
	return "test logs\n", nil
}

type noopEmitter struct{}

func (noopEmitter) Emit(_ notify.Event) {}

// capturingEmitter records emitted events for assertions in tests.
type capturingEmitter struct {
	events []notify.Event
}

func (c *capturingEmitter) Emit(e notify.Event) { c.events = append(c.events, e) }

// mockStore implements store.Store for unit tests that need a failing CreateTask
// or want to inspect the number of CreateTask calls without touching a real DB.
type mockStore struct {
	store.Store
	createErr   error
	createCalls int
}

func (m *mockStore) CreateTask(_ context.Context, _ *models.Task) error {
	m.createCalls++
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
