package notify

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/backflow-labs/backflow/internal/messaging"
	"github.com/backflow-labs/backflow/internal/models"
	"github.com/backflow-labs/backflow/internal/store"
)

// --- test helpers ---

type recordingNotifier struct {
	events []Event
}

func (n *recordingNotifier) Notify(e Event) error {
	n.events = append(n.events, e)
	return nil
}

type recordingMessenger struct {
	messages []messaging.OutboundMessage
	mu       sync.Mutex
}

func (m *recordingMessenger) Send(_ context.Context, msg messaging.OutboundMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, msg)
	return nil
}

type stubStore struct {
	tasks map[string]*models.Task
}

func (s *stubStore) GetTask(_ context.Context, id string) (*models.Task, error) {
	return s.tasks[id], nil
}

// Unused Store methods
func (s *stubStore) CreateTask(context.Context, *models.Task) error                                    { return nil }
func (s *stubStore) ListTasks(context.Context, store.TaskFilter) ([]*models.Task, error)               { return nil, nil }
func (s *stubStore) UpdateTask(context.Context, *models.Task) error                                    { return nil }
func (s *stubStore) DeleteTask(context.Context, string) error                                          { return nil }
func (s *stubStore) CreateInstance(context.Context, *models.Instance) error                            { return nil }
func (s *stubStore) GetInstance(context.Context, string) (*models.Instance, error)                     { return nil, nil }
func (s *stubStore) ListInstances(context.Context, *models.InstanceStatus) ([]*models.Instance, error) { return nil, nil }
func (s *stubStore) UpdateInstance(context.Context, *models.Instance) error                            { return nil }
func (s *stubStore) GetAllowedSender(context.Context, string, string) (*models.AllowedSender, error)  { return nil, nil }
func (s *stubStore) Close() error                                                                      { return nil }

// --- tests ---

func TestMessagingNotifier_DelegatesToInner(t *testing.T) {
	inner := &recordingNotifier{}
	m := &recordingMessenger{}
	s := &stubStore{tasks: map[string]*models.Task{}}

	mn := NewMessagingNotifier(inner, m, s, nil)
	event := Event{Type: EventTaskCompleted, TaskID: "bf_123", Timestamp: time.Now()}
	mn.Notify(event)

	if len(inner.events) != 1 {
		t.Fatalf("expected inner to receive 1 event, got %d", len(inner.events))
	}
	if inner.events[0].TaskID != "bf_123" {
		t.Errorf("inner event task_id = %q", inner.events[0].TaskID)
	}
}

func TestMessagingNotifier_SendsSMSForReplyChannel(t *testing.T) {
	inner := &recordingNotifier{}
	m := &recordingMessenger{}
	s := &stubStore{tasks: map[string]*models.Task{
		"bf_123": {
			ID:           "bf_123",
			ReplyChannel: "sms:+15551234567",
		},
	}}

	mn := NewMessagingNotifier(inner, m, s, nil)
	event := Event{Type: EventTaskCompleted, TaskID: "bf_123", PRURL: "https://github.com/org/repo/pull/42", Timestamp: time.Now()}
	mn.Notify(event)

	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.messages) != 1 {
		t.Fatalf("expected 1 SMS, got %d", len(m.messages))
	}
	if m.messages[0].Channel.Address != "+15551234567" {
		t.Errorf("address = %q", m.messages[0].Channel.Address)
	}
	if m.messages[0].Channel.Type != messaging.ChannelSMS {
		t.Errorf("channel type = %q", m.messages[0].Channel.Type)
	}
}

func TestMessagingNotifier_SkipsWithoutReplyChannel(t *testing.T) {
	inner := &recordingNotifier{}
	m := &recordingMessenger{}
	s := &stubStore{tasks: map[string]*models.Task{
		"bf_456": {ID: "bf_456", ReplyChannel: ""},
	}}

	mn := NewMessagingNotifier(inner, m, s, nil)
	mn.Notify(Event{Type: EventTaskCompleted, TaskID: "bf_456", Timestamp: time.Now()})

	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.messages) != 0 {
		t.Fatalf("expected no SMS for task without reply channel, got %d", len(m.messages))
	}
}

func TestMessagingNotifier_EventFilter(t *testing.T) {
	inner := &recordingNotifier{}
	m := &recordingMessenger{}
	s := &stubStore{tasks: map[string]*models.Task{
		"bf_789": {ID: "bf_789", ReplyChannel: "sms:+15551234567"},
	}}

	// Only send SMS for task.failed events
	mn := NewMessagingNotifier(inner, m, s, []string{"task.failed"})

	// task.completed should not trigger SMS
	mn.Notify(Event{Type: EventTaskCompleted, TaskID: "bf_789", Timestamp: time.Now()})
	m.mu.Lock()
	if len(m.messages) != 0 {
		t.Fatalf("expected no SMS for filtered event, got %d", len(m.messages))
	}
	m.mu.Unlock()

	// But inner notifier should still have been called
	if len(inner.events) != 1 {
		t.Fatalf("inner should always be called, got %d events", len(inner.events))
	}

	// task.failed should trigger SMS
	mn.Notify(Event{Type: EventTaskFailed, TaskID: "bf_789", Message: "something broke", Timestamp: time.Now()})
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.messages) != 1 {
		t.Fatalf("expected 1 SMS for matching event, got %d", len(m.messages))
	}
}

func TestFormatEventMessage(t *testing.T) {
	tests := []struct {
		name     string
		event    Event
		contains string
	}{
		{
			name:     "completed with PR",
			event:    Event{Type: EventTaskCompleted, TaskID: "bf_1", PRURL: "https://github.com/org/repo/pull/1"},
			contains: "PR: https://github.com/org/repo/pull/1",
		},
		{
			name:     "completed without PR",
			event:    Event{Type: EventTaskCompleted, TaskID: "bf_1"},
			contains: "completed",
		},
		{
			name:     "failed with message",
			event:    Event{Type: EventTaskFailed, TaskID: "bf_1", Message: "container exited 1"},
			contains: "container exited 1",
		},
		{
			name:     "running",
			event:    Event{Type: EventTaskRunning, TaskID: "bf_1"},
			contains: "running",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := formatEventMessage(tt.event)
			if msg == "" {
				t.Fatal("expected non-empty message")
			}
			if !contains(msg, tt.contains) {
				t.Errorf("message %q does not contain %q", msg, tt.contains)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || containsSubstring(s, substr))
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
