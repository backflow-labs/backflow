package notify

import (
	"testing"
	"time"
)

func TestDiscordNotifier_FiltersEvents(t *testing.T) {
	n := NewDiscordNotifier([]string{"task.completed", "task.failed"})

	// Matching event — should return nil (no error)
	err := n.Notify(Event{
		Type:      EventTaskCompleted,
		TaskID:    "bf_TEST001",
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Errorf("Notify(completed) = %v, want nil", err)
	}

	// Non-matching event — should also return nil (silently skipped)
	err = n.Notify(Event{
		Type:      EventTaskRunning,
		TaskID:    "bf_TEST001",
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Errorf("Notify(running) = %v, want nil", err)
	}
}

func TestDiscordNotifier_AllEvents(t *testing.T) {
	n := NewDiscordNotifier(nil)

	// Should accept any event type when filter is nil
	for _, et := range []EventType{EventTaskCreated, EventTaskRunning, EventTaskCompleted, EventTaskFailed} {
		if err := n.Notify(Event{Type: et, TaskID: "bf_TEST001", Timestamp: time.Now()}); err != nil {
			t.Errorf("Notify(%s) = %v, want nil", et, err)
		}
	}
}

func TestDiscordNotifier_Name(t *testing.T) {
	n := NewDiscordNotifier(nil)
	if got := n.Name(); got != "discord" {
		t.Errorf("Name() = %q, want %q", got, "discord")
	}
}
