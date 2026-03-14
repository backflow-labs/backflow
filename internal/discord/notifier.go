package discord

import (
	"context"
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog/log"

	"github.com/backflow-labs/backflow/internal/notify"
)

// DiscordNotifier implements notify.Notifier by posting embeds to Discord threads.
type DiscordNotifier struct {
	bot *Bot
}

func (n *DiscordNotifier) Notify(event notify.Event) error {
	info, ok := n.bot.threads.Load(event.TaskID)
	if !ok {
		// Task wasn't created via Discord — skip
		return nil
	}
	ti := info.(*ThreadInfo)

	embed := formatEvent(event)

	_, err := n.bot.session.ChannelMessageSendEmbed(ti.ThreadID, embed)
	if err != nil {
		log.Error().Err(err).Str("task_id", event.TaskID).Str("thread_id", ti.ThreadID).Msg("failed to send discord embed")
		return fmt.Errorf("discord send: %w", err)
	}

	// Handle needs_input: register thread for Q&A
	if event.Type == notify.EventTaskNeedsInput {
		n.bot.waitingFor.Store(ti.ThreadID, ti)
		log.Info().Str("task_id", event.TaskID).Str("thread_id", ti.ThreadID).Msg("awaiting user reply in discord thread")
	}

	// Handle completion: fetch PR URL from store and update embed if available
	if event.Type == notify.EventTaskCompleted {
		task, err := n.bot.store.GetTask(context.Background(), event.TaskID)
		if err == nil && task != nil && task.PRURL != "" && event.Message == "" {
			_, _ = n.bot.session.ChannelMessageSendEmbed(ti.ThreadID, &discordgo.MessageEmbed{
				Title:       "Pull Request",
				Description: task.PRURL,
				Color:       colorGreen,
			})
		}
	}

	return nil
}
