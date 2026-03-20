package notify

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/backflow-labs/backflow/internal/messaging"
)

// --- test helpers ---

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

type failingMessenger struct {
	err error
}

func (m *failingMessenger) Send(context.Context, messaging.OutboundMessage) error {
	return m.err
}

// --- tests ---

func TestMessagingNotifier_SendsSMSFromEventReplyChannel(t *testing.T) {
	m := &recordingMessenger{}
	mn := NewMessagingNotifier(m, nil)

	event := Event{
		Type:         EventTaskCompleted,
		TaskID:       "bf_123",
		ReplyChannel: "sms:+15551234567",
		PRURL:        "https://github.com/org/repo/pull/42",
		Timestamp:    time.Now(),
	}
	err := mn.Notify(event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

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
	m := &recordingMessenger{}
	mn := NewMessagingNotifier(m, nil)

	mn.Notify(Event{Type: EventTaskCompleted, TaskID: "bf_456", Timestamp: time.Now()})

	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.messages) != 0 {
		t.Fatalf("expected no SMS for event without reply channel, got %d", len(m.messages))
	}
}

func TestMessagingNotifier_EventFilter(t *testing.T) {
	m := &recordingMessenger{}
	mn := NewMessagingNotifier(m, []string{"task.failed"})

	// task.completed should not trigger SMS
	mn.Notify(Event{Type: EventTaskCompleted, TaskID: "bf_789", ReplyChannel: "sms:+15551234567", Timestamp: time.Now()})
	m.mu.Lock()
	if len(m.messages) != 0 {
		t.Fatalf("expected no SMS for filtered event, got %d", len(m.messages))
	}
	m.mu.Unlock()

	// task.failed should trigger SMS
	mn.Notify(Event{Type: EventTaskFailed, TaskID: "bf_789", ReplyChannel: "sms:+15551234567", Message: "something broke", Timestamp: time.Now()})
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.messages) != 1 {
		t.Fatalf("expected 1 SMS for matching event, got %d", len(m.messages))
	}
}

func TestMessagingNotifier_ErrorDoesNotPropagate(t *testing.T) {
	m := &failingMessenger{err: errors.New("twilio down")}
	mn := NewMessagingNotifier(m, nil)

	err := mn.Notify(Event{Type: EventTaskCompleted, TaskID: "bf_err", ReplyChannel: "sms:+15551234567", Timestamp: time.Now()})
	if err != nil {
		t.Fatalf("messenger error should not propagate, got: %v", err)
	}
}

func TestMessagingNotifier_InvalidReplyChannelSkips(t *testing.T) {
	m := &recordingMessenger{}
	mn := NewMessagingNotifier(m, nil)

	// "sms" without address should be skipped
	mn.Notify(Event{Type: EventTaskCompleted, TaskID: "bf_bad", ReplyChannel: "sms", Timestamp: time.Now()})

	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.messages) != 0 {
		t.Fatalf("expected no SMS for invalid reply channel, got %d", len(m.messages))
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
			if !strings.Contains(msg, tt.contains) {
				t.Errorf("message %q does not contain %q", msg, tt.contains)
			}
		})
	}
}
