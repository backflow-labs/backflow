package orchestrator

import (
	"context"
	"testing"

	"github.com/backflow-labs/backflow/internal/models"
)

func TestFindAvailableInstance_ReturnsInstanceWithCapacity(t *testing.T) {
	s := newMockStore()
	s.CreateInstance(context.Background(), &models.Instance{
		InstanceID:        "i-full",
		Status:            models.InstanceStatusRunning,
		MaxContainers:     2,
		RunningContainers: 2,
	})
	s.CreateInstance(context.Background(), &models.Instance{
		InstanceID:        "i-avail",
		Status:            models.InstanceStatusRunning,
		MaxContainers:     4,
		RunningContainers: 1,
	})

	o := newTestOrchestrator(s, &mockNotifier{})

	inst, err := o.findAvailableInstance(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inst.InstanceID != "i-avail" {
		t.Errorf("instance = %q, want i-avail", inst.InstanceID)
	}
}

func TestFindAvailableInstance_NoCapacity(t *testing.T) {
	s := newMockStore()
	s.CreateInstance(context.Background(), &models.Instance{
		InstanceID:        "i-full",
		Status:            models.InstanceStatusRunning,
		MaxContainers:     2,
		RunningContainers: 2,
	})

	o := newTestOrchestrator(s, &mockNotifier{})

	_, err := o.findAvailableInstance(context.Background())
	if err != errNoCapacity {
		t.Errorf("expected errNoCapacity, got %v", err)
	}
}

func TestFindAvailableInstance_IgnoresNonRunning(t *testing.T) {
	s := newMockStore()
	s.CreateInstance(context.Background(), &models.Instance{
		InstanceID:        "i-terminated",
		Status:            models.InstanceStatusTerminated,
		MaxContainers:     4,
		RunningContainers: 0,
	})

	o := newTestOrchestrator(s, &mockNotifier{})

	_, err := o.findAvailableInstance(context.Background())
	if err != errNoCapacity {
		t.Errorf("expected errNoCapacity for terminated instance, got %v", err)
	}
}

func TestFindAvailableInstance_EmptyStore(t *testing.T) {
	s := newMockStore()
	o := newTestOrchestrator(s, &mockNotifier{})

	_, err := o.findAvailableInstance(context.Background())
	if err != errNoCapacity {
		t.Errorf("expected errNoCapacity for empty store, got %v", err)
	}
}

func TestReleaseSlot(t *testing.T) {
	s := newMockStore()
	s.CreateInstance(context.Background(), &models.Instance{
		InstanceID:        "local",
		Status:            models.InstanceStatusRunning,
		MaxContainers:     4,
		RunningContainers: 2,
	})

	o := newTestOrchestrator(s, &mockNotifier{})
	o.running = 2

	task := &models.Task{InstanceID: "local"}
	o.releaseSlot(context.Background(), task)

	if o.running != 1 {
		t.Errorf("running = %d, want 1", o.running)
	}

	inst, _ := s.GetInstance(context.Background(), "local")
	if inst.RunningContainers != 1 {
		t.Errorf("RunningContainers = %d, want 1", inst.RunningContainers)
	}
}

func TestReleaseSlot_PreventsNegativeContainers(t *testing.T) {
	s := newMockStore()
	s.CreateInstance(context.Background(), &models.Instance{
		InstanceID:        "local",
		Status:            models.InstanceStatusRunning,
		MaxContainers:     4,
		RunningContainers: 0,
	})

	o := newTestOrchestrator(s, &mockNotifier{})
	o.running = 1

	task := &models.Task{InstanceID: "local"}
	o.releaseSlot(context.Background(), task)

	inst, _ := s.GetInstance(context.Background(), "local")
	if inst.RunningContainers != 0 {
		t.Errorf("RunningContainers = %d, want 0 (should not go negative)", inst.RunningContainers)
	}
}

func TestMarkInstanceTerminated(t *testing.T) {
	s := newMockStore()
	s.CreateInstance(context.Background(), &models.Instance{
		InstanceID:        "i-abc",
		Status:            models.InstanceStatusRunning,
		MaxContainers:     4,
		RunningContainers: 2,
	})

	o := newTestOrchestrator(s, &mockNotifier{})

	o.markInstanceTerminated(context.Background(), "i-abc")

	inst, _ := s.GetInstance(context.Background(), "i-abc")
	if inst.Status != models.InstanceStatusTerminated {
		t.Errorf("status = %q, want terminated", inst.Status)
	}
	if inst.RunningContainers != 0 {
		t.Errorf("RunningContainers = %d, want 0", inst.RunningContainers)
	}
}

func TestMarkInstanceTerminated_AlreadyTerminated(t *testing.T) {
	s := newMockStore()
	s.CreateInstance(context.Background(), &models.Instance{
		InstanceID:        "i-abc",
		Status:            models.InstanceStatusTerminated,
		MaxContainers:     4,
		RunningContainers: 0,
	})

	o := newTestOrchestrator(s, &mockNotifier{})

	// Should be a no-op, not panic
	o.markInstanceTerminated(context.Background(), "i-abc")

	inst, _ := s.GetInstance(context.Background(), "i-abc")
	if inst.Status != models.InstanceStatusTerminated {
		t.Errorf("status = %q, want terminated", inst.Status)
	}
}

func TestMarkInstanceTerminated_EmptyID(t *testing.T) {
	o := newTestOrchestrator(newMockStore(), &mockNotifier{})
	// Should not panic
	o.markInstanceTerminated(context.Background(), "")
}
