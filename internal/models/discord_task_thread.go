package models

import "time"

// DiscordTaskThread stores the Discord message/thread pair for a task.
type DiscordTaskThread struct {
	TaskID        string    `json:"task_id"`
	RootMessageID string    `json:"root_message_id"`
	ThreadID      string    `json:"thread_id"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}
