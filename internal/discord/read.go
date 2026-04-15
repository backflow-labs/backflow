package discord

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/backflow-labs/backflow/internal/models"
)

// ValidateReadURL validates that input is a syntactically well-formed absolute
// URL with a scheme and host. It does not check reachability, enforce a scheme
// allowlist, or restrict domains — those are deliberately out of scope.
// The returned string is the trimmed input on success.
func ValidateReadURL(input string) (string, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "", fmt.Errorf("url is required")
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme == "" {
		return "", fmt.Errorf("url must include a scheme (e.g. https://)")
	}
	if u.Host == "" {
		return "", fmt.Errorf("url must include a host")
	}
	return trimmed, nil
}

// handleReadCommand processes a `/backflow read` application-command interaction.
func handleReadCommand(ctx context.Context, w http.ResponseWriter, interaction Interaction, options []CommandOption, actions HandlerActions) {
	if !hasPermission(interaction.Member, actions.AllowedRoles) {
		respondJSON(w, ChannelMessageResponse{
			Type: ResponseTypeChannelMessage,
			Data: MessageData{Content: "You don't have permission to read URLs.", Flags: FlagEphemeral},
		})
		return
	}
	rawURL, err := stringOption(options, "url")
	if err != nil {
		respondJSON(w, ChannelMessageResponse{
			Type: ResponseTypeChannelMessage,
			Data: MessageData{Content: err.Error(), Flags: FlagEphemeral},
		})
		return
	}
	validated, err := ValidateReadURL(rawURL)
	if err != nil {
		respondJSON(w, ChannelMessageResponse{
			Type: ResponseTypeChannelMessage,
			Data: MessageData{Content: fmt.Sprintf("Invalid url: %s", err.Error()), Flags: FlagEphemeral},
		})
		return
	}
	readMode := models.TaskModeRead
	req := &models.CreateTaskRequest{
		Prompt:   validated,
		TaskMode: &readMode,
	}
	if force, ok := boolOption(options, "force"); ok {
		req.Force = &force
	}
	if actions.CreateTask == nil {
		respondJSON(w, ChannelMessageResponse{
			Type: ResponseTypeChannelMessage,
			Data: MessageData{Content: "Task creation is unavailable right now.", Flags: FlagEphemeral},
		})
		return
	}
	if _, err := actions.CreateTask(ctx, req); err != nil {
		log.Warn().Err(err).Msg("discord: failed to create read task")
		respondJSON(w, ChannelMessageResponse{
			Type: ResponseTypeChannelMessage,
			Data: MessageData{Content: fmt.Sprintf("Failed to create reading task: %s", err.Error()), Flags: FlagEphemeral},
		})
		return
	}
	respondJSON(w, ChannelMessageResponse{
		Type: ResponseTypeChannelMessage,
		Data: MessageData{Content: fmt.Sprintf("Reading %s...", validated), Flags: FlagEphemeral},
	})
}
