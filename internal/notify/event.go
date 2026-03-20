package notify

import (
	"strings"
	"time"

	"github.com/backflow-labs/backflow/internal/models"
)

// EventOption is a functional option for NewEvent.
type EventOption func(*Event)

// WithContainerStatus sets fields that come from container inspection.
func WithContainerStatus(prURL, message, agentLogTail string) EventOption {
	return func(e *Event) {
		e.PRURL = prURL
		e.Message = message
		e.AgentLogTail = agentLogTail
	}
}

// NewEvent constructs an Event from a task, populating core fields.
func NewEvent(eventType EventType, task *models.Task, opts ...EventOption) Event {
	e := Event{
		Type:         eventType,
		TaskID:       task.ID,
		RepoURL:      task.RepoURL,
		Prompt:       task.Prompt,
		ReplyChannel: redactChannel(task.ReplyChannel),
		Timestamp:    time.Now().UTC(),
	}
	for _, opt := range opts {
		opt(&e)
	}
	return e
}

// redactChannel strips the value after ":" from a reply channel,
// keeping only the channel type (e.g. "sms:+15551234567" → "sms").
func redactChannel(ch string) string {
	if idx := strings.Index(ch, ":"); idx >= 0 {
		return ch[:idx]
	}
	return ch
}
