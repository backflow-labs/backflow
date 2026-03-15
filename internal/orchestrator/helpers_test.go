package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/backflow-labs/backflow/internal/config"
	"github.com/backflow-labs/backflow/internal/models"
	"github.com/backflow-labs/backflow/internal/notify"
	"github.com/backflow-labs/backflow/internal/store"
)

// --- Mock store ---

type mockStore struct {
	tasks     map[string]*models.Task
	instances map[string]*models.Instance
	mu        sync.Mutex
}

func newMockStore() *mockStore {
	return &mockStore{
		tasks:     make(map[string]*models.Task),
		instances: make(map[string]*models.Instance),
	}
}

func (s *mockStore) CreateTask(_ context.Context, task *models.Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[task.ID] = task
	return nil
}

func (s *mockStore) GetTask(_ context.Context, id string) (*models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return nil, nil
	}
	return t, nil
}

func (s *mockStore) ListTasks(_ context.Context, filter store.TaskFilter) ([]*models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []*models.Task
	for _, t := range s.tasks {
		if filter.Status != nil && t.Status != *filter.Status {
			continue
		}
		result = append(result, t)
	}
	if filter.Limit > 0 && len(result) > filter.Limit {
		result = result[:filter.Limit]
	}
	return result, nil
}

func (s *mockStore) UpdateTask(_ context.Context, task *models.Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[task.ID] = task
	return nil
}

func (s *mockStore) DeleteTask(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tasks, id)
	return nil
}

func (s *mockStore) CreateInstance(_ context.Context, inst *models.Instance) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.instances[inst.InstanceID] = inst
	return nil
}

func (s *mockStore) GetInstance(_ context.Context, id string) (*models.Instance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i, ok := s.instances[id]
	if !ok {
		return nil, nil
	}
	return i, nil
}

func (s *mockStore) ListInstances(_ context.Context, status *models.InstanceStatus) ([]*models.Instance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []*models.Instance
	for _, i := range s.instances {
		if status != nil && i.Status != *status {
			continue
		}
		result = append(result, i)
	}
	return result, nil
}

func (s *mockStore) UpdateInstance(_ context.Context, inst *models.Instance) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.instances[inst.InstanceID] = inst
	return nil
}

func (s *mockStore) Close() error { return nil }

// --- Mock notifier ---

type mockNotifier struct {
	events []notify.Event
	mu     sync.Mutex
}

func (n *mockNotifier) Notify(e notify.Event) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.events = append(n.events, e)
	return nil
}

func (n *mockNotifier) eventTypes() []notify.EventType {
	n.mu.Lock()
	defer n.mu.Unlock()
	var types []notify.EventType
	for _, e := range n.events {
		types = append(types, e.Type)
	}
	return types
}

// --- Mock docker manager ---

type mockDockerManager struct {
	inspectResults map[string]ContainerStatus
	inspectErrors  map[string]error
}

func (m *mockDockerManager) inspect(instanceID, containerID string) (ContainerStatus, error) {
	key := instanceID + "/" + containerID
	if err, ok := m.inspectErrors[key]; ok {
		return ContainerStatus{}, err
	}
	if status, ok := m.inspectResults[key]; ok {
		return status, nil
	}
	return ContainerStatus{}, fmt.Errorf("unknown container %s", key)
}

// --- Test orchestrator constructor ---

func newTestOrchestrator(s store.Store, n notify.Notifier) *Orchestrator {
	cfg := &config.Config{
		Mode:              config.ModeLocal,
		AuthMode:          config.AuthModeAPIKey,
		ContainersPerInst: 4,
		PollInterval:      5 * time.Second,
	}
	return &Orchestrator{
		store:           s,
		config:          cfg,
		notifier:        n,
		docker:          NewDockerManager(cfg),
		scaler:          localScaler{},
		stopCh:          make(chan struct{}),
		inspectFailures: make(map[string]int),
	}
}

// newLocalInstance creates a standard local instance for tests.
func newLocalInstance() *models.Instance {
	return &models.Instance{
		InstanceID:        "local",
		Status:            models.InstanceStatusRunning,
		MaxContainers:     4,
		RunningContainers: 0,
	}
}
