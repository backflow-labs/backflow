package notify

import (
	"fmt"
	"testing"
	"time"
)

type recordingNotifier struct {
	events []Event
}

func (r *recordingNotifier) Notify(e Event) error {
	r.events = append(r.events, e)
	return nil
}

type failingNotifier struct {
	err error
}

func (f *failingNotifier) Notify(Event) error {
	return f.err
}

func TestMultiNotifier_FansOut(t *testing.T) {
	a := &recordingNotifier{}
	b := &recordingNotifier{}
	m := NewMultiNotifier(a, b)

	event := Event{
		Type:      EventTaskCreated,
		TaskID:    "bf_test123",
		Timestamp: time.Now(),
	}

	if err := m.Notify(event); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(a.events) != 1 {
		t.Fatalf("expected 1 event in a, got %d", len(a.events))
	}
	if len(b.events) != 1 {
		t.Fatalf("expected 1 event in b, got %d", len(b.events))
	}
	if a.events[0].TaskID != "bf_test123" {
		t.Fatalf("expected task ID bf_test123, got %s", a.events[0].TaskID)
	}
}

func TestMultiNotifier_ContinuesOnError(t *testing.T) {
	a := &failingNotifier{err: fmt.Errorf("boom")}
	b := &recordingNotifier{}
	m := NewMultiNotifier(a, b)

	event := Event{Type: EventTaskRunning, TaskID: "bf_test456", Timestamp: time.Now()}
	err := m.Notify(event)
	if err == nil {
		t.Fatal("expected error")
	}
	if len(b.events) != 1 {
		t.Fatalf("expected b still called after a fails, got %d events", len(b.events))
	}
}

func TestMultiNotifier_Empty(t *testing.T) {
	m := NewMultiNotifier()
	if err := m.Notify(Event{Type: EventTaskCompleted}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
