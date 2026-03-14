package discord

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/backflow-labs/backflow/internal/notify"
)

const (
	colorBlue   = 0x3498db
	colorYellow = 0xf39c12
	colorGreen  = 0x2ecc71
	colorRed    = 0xe74c3c
	colorOrange = 0xe67e22
)

func formatEvent(event notify.Event) *discordgo.MessageEmbed {
	switch event.Type {
	case notify.EventTaskCreated:
		return &discordgo.MessageEmbed{
			Title:       "Task Queued",
			Description: fmt.Sprintf("**Task:** `%s`\n**Repo:** %s", event.TaskID, event.RepoURL),
			Color:       colorBlue,
			Fields: []*discordgo.MessageEmbedField{
				{Name: "Prompt", Value: truncate(event.Prompt, 1024)},
			},
		}

	case notify.EventTaskRunning:
		return &discordgo.MessageEmbed{
			Title:       "Agent Working",
			Description: fmt.Sprintf("Task `%s` is now running.", event.TaskID),
			Color:       colorYellow,
		}

	case notify.EventTaskCompleted:
		desc := fmt.Sprintf("Task `%s` completed.", event.TaskID)
		if event.Message != "" {
			desc += fmt.Sprintf("\n**PR:** %s", event.Message)
		}
		return &discordgo.MessageEmbed{
			Title:       "Done!",
			Description: desc,
			Color:       colorGreen,
		}

	case notify.EventTaskFailed:
		desc := fmt.Sprintf("Task `%s` failed.", event.TaskID)
		fields := []*discordgo.MessageEmbedField{}
		if event.Message != "" {
			fields = append(fields, &discordgo.MessageEmbedField{
				Name:  "Error",
				Value: truncate(event.Message, 1024),
			})
		}
		if event.AgentLogTail != "" {
			fields = append(fields, &discordgo.MessageEmbedField{
				Name:  "Log Tail",
				Value: "```\n" + truncate(event.AgentLogTail, 1000) + "\n```",
			})
		}
		return &discordgo.MessageEmbed{
			Title:       "Task Failed",
			Description: desc,
			Color:       colorRed,
			Fields:      fields,
		}

	case notify.EventTaskNeedsInput:
		return &discordgo.MessageEmbed{
			Title:       "Agent Has a Question",
			Description: truncate(event.Message, 2000),
			Color:       colorOrange,
			Footer:      &discordgo.MessageEmbedFooter{Text: "Reply in this thread to answer"},
		}

	case notify.EventTaskInterrupted:
		return &discordgo.MessageEmbed{
			Title:       "Task Interrupted",
			Description: fmt.Sprintf("Task `%s` was interrupted. Re-queuing...", event.TaskID),
			Color:       colorRed,
		}

	default:
		return &discordgo.MessageEmbed{
			Title:       string(event.Type),
			Description: event.Message,
			Color:       colorBlue,
		}
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
