package notify

import (
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
		ReplyChannel: models.RedactChannel(task.ReplyChannel),
		Timestamp:    time.Now().UTC(),
	}
	for _, opt := range opts {
		opt(&e)
	}
	return e
}

