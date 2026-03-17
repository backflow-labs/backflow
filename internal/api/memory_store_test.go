package api

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/backflow-labs/backflow/internal/models"
	"github.com/backflow-labs/backflow/internal/store"
)

type memoryStore struct {
	mu        sync.Mutex
	tasks     map[string]*models.Task
	instances map[string]*models.Instance
	senders   map[string]*models.AllowedSender
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		tasks:     make(map[string]*models.Task),
		instances: make(map[string]*models.Instance),
		senders:   make(map[string]*models.AllowedSender),
	}
}

func (s *memoryStore) CreateTask(_ context.Context, task *models.Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[task.ID] = cloneTask(task)
	return nil
}

func (s *memoryStore) GetTask(_ context.Context, id string) (*models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return cloneTask(task), nil
}

func (s *memoryStore) ListTasks(_ context.Context, filter store.TaskFilter) ([]*models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tasks := make([]*models.Task, 0, len(s.tasks))
	for _, task := range s.tasks {
		if filter.Status != nil && task.Status != *filter.Status {
			continue
		}
		tasks = append(tasks, cloneTask(task))
	}

	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].CreatedAt.Before(tasks[j].CreatedAt)
	})

	if filter.Offset > 0 {
		if filter.Offset >= len(tasks) {
			return []*models.Task{}, nil
		}
		tasks = tasks[filter.Offset:]
	}
	if filter.Limit > 0 && filter.Limit < len(tasks) {
		tasks = tasks[:filter.Limit]
	}

	return tasks, nil
}

func (s *memoryStore) DeleteTask(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tasks, id)
	return nil
}

func (s *memoryStore) UpdateTaskStatus(_ context.Context, id string, status models.TaskStatus, taskErr string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[id]
	if !ok {
		return nil
	}
	task.Status = status
	task.Error = taskErr
	task.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *memoryStore) AssignTask(_ context.Context, id string, instanceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[id]
	if !ok {
		return nil
	}
	task.Status = models.TaskStatusProvisioning
	task.InstanceID = instanceID
	task.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *memoryStore) StartTask(_ context.Context, id string, containerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[id]
	if !ok {
		return nil
	}
	now := time.Now().UTC()
	task.Status = models.TaskStatusRunning
	task.ContainerID = containerID
	task.StartedAt = &now
	task.UpdatedAt = now
	return nil
}

func (s *memoryStore) CompleteTask(_ context.Context, id string, result store.TaskResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[id]
	if !ok {
		return nil
	}
	now := time.Now().UTC()
	task.Status = result.Status
	task.Error = result.Error
	task.PRURL = result.PRURL
	task.OutputURL = result.OutputURL
	task.CostUSD = result.CostUSD
	task.ElapsedTimeSec = result.ElapsedTimeSec
	task.CompletedAt = &now
	task.UpdatedAt = now
	return nil
}

func (s *memoryStore) RequeueTask(_ context.Context, id string, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[id]
	if !ok {
		return nil
	}
	now := time.Now().UTC()
	task.Status = models.TaskStatusPending
	task.InstanceID = ""
	task.ContainerID = ""
	task.StartedAt = nil
	task.RetryCount++
	task.Error = reason
	task.UpdatedAt = now
	return nil
}

func (s *memoryStore) CancelTask(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[id]
	if !ok {
		return nil
	}
	now := time.Now().UTC()
	task.Status = models.TaskStatusCancelled
	task.CompletedAt = &now
	task.UpdatedAt = now
	return nil
}

func (s *memoryStore) ClearTaskAssignment(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[id]
	if !ok {
		return nil
	}
	task.InstanceID = ""
	task.ContainerID = ""
	task.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *memoryStore) CreateInstance(_ context.Context, inst *models.Instance) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.instances[inst.InstanceID] = cloneInstance(inst)
	return nil
}

func (s *memoryStore) GetInstance(_ context.Context, id string) (*models.Instance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	inst, ok := s.instances[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return cloneInstance(inst), nil
}

func (s *memoryStore) ListInstances(_ context.Context, status *models.InstanceStatus) ([]*models.Instance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	instances := make([]*models.Instance, 0, len(s.instances))
	for _, inst := range s.instances {
		if status != nil && inst.Status != *status {
			continue
		}
		instances = append(instances, cloneInstance(inst))
	}

	sort.Slice(instances, func(i, j int) bool {
		return instances[i].CreatedAt.Before(instances[j].CreatedAt)
	})

	return instances, nil
}

func (s *memoryStore) UpdateInstanceStatus(_ context.Context, id string, status models.InstanceStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	inst, ok := s.instances[id]
	if !ok {
		return nil
	}
	inst.Status = status
	if status == models.InstanceStatusTerminated {
		inst.RunningContainers = 0
	}
	inst.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *memoryStore) IncrementRunningContainers(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	inst, ok := s.instances[id]
	if !ok {
		return nil
	}
	inst.RunningContainers++
	inst.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *memoryStore) DecrementRunningContainers(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	inst, ok := s.instances[id]
	if !ok {
		return nil
	}
	if inst.RunningContainers > 0 {
		inst.RunningContainers--
	}
	inst.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *memoryStore) UpdateInstanceDetails(_ context.Context, id string, privateIP, az string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	inst, ok := s.instances[id]
	if !ok {
		return nil
	}
	inst.PrivateIP = privateIP
	inst.AvailabilityZone = az
	inst.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *memoryStore) ResetRunningContainers(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	inst, ok := s.instances[id]
	if !ok {
		return nil
	}
	inst.RunningContainers = 0
	inst.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *memoryStore) GetAllowedSender(_ context.Context, channelType, address string) (*models.AllowedSender, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sender, ok := s.senders[senderKey(channelType, address)]
	if !ok {
		return nil, store.ErrNotFound
	}
	return cloneAllowedSender(sender), nil
}

func (s *memoryStore) CreateAllowedSender(_ context.Context, sender *models.AllowedSender) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.senders[senderKey(sender.ChannelType, sender.Address)] = cloneAllowedSender(sender)
	return nil
}

func (s *memoryStore) WithTx(_ context.Context, fn func(store.Store) error) error {
	return fn(s)
}

func (s *memoryStore) Close() error {
	return nil
}

func cloneTask(task *models.Task) *models.Task {
	if task == nil {
		return nil
	}
	cloned := *task
	if task.AllowedTools != nil {
		cloned.AllowedTools = append([]string(nil), task.AllowedTools...)
	}
	if task.EnvVars != nil {
		cloned.EnvVars = make(map[string]string, len(task.EnvVars))
		for key, value := range task.EnvVars {
			cloned.EnvVars[key] = value
		}
	}
	if task.StartedAt != nil {
		startedAt := *task.StartedAt
		cloned.StartedAt = &startedAt
	}
	if task.CompletedAt != nil {
		completedAt := *task.CompletedAt
		cloned.CompletedAt = &completedAt
	}
	return &cloned
}

func cloneInstance(inst *models.Instance) *models.Instance {
	if inst == nil {
		return nil
	}
	cloned := *inst
	return &cloned
}

func cloneAllowedSender(sender *models.AllowedSender) *models.AllowedSender {
	if sender == nil {
		return nil
	}
	cloned := *sender
	return &cloned
}

func senderKey(channelType, address string) string {
	return channelType + ":" + address
}
