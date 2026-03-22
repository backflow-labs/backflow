package discord

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/backflow-labs/backflow/internal/models"
)

// Discord component type constants.
const (
	ComponentTypeActionRow = 1
	ComponentTypeTextInput = 4
)

// TextInput styles.
const (
	TextInputStyleShort     = 1
	TextInputStyleParagraph = 2
)

// ResponseTypeModal is the Discord interaction response type for opening a modal.
const ResponseTypeModal = 9

// Modal field custom_id constants.
const (
	modalIDCreate  = "backflow_create"
	fieldRepoURL   = "repo_url"
	fieldPrompt    = "prompt"
	fieldBranch    = "branch"
	fieldHarness   = "harness"
	fieldBudgetUSD = "budget_usd"
)

// CreateTaskFunc creates a new Backflow task from a request.
// It should validate the request, persist it, and emit a task.created event.
type CreateTaskFunc func(ctx context.Context, req *models.CreateTaskRequest) (*models.Task, error)

// ModalResponse is the Discord response that opens a modal dialog.
type ModalResponse struct {
	Type int       `json:"type"`
	Data ModalData `json:"data"`
}

// ModalData describes the modal to show the user.
type ModalData struct {
	CustomID   string      `json:"custom_id"`
	Title      string      `json:"title"`
	Components []ActionRow `json:"components"`
}

// ActionRow wraps one or more components in a row container.
type ActionRow struct {
	Type       int         `json:"type"`
	Components []TextInput `json:"components"`
}

// TextInput is a single text field inside a modal action row.
// The Value field is populated when parsing modal submit data.
type TextInput struct {
	Type        int    `json:"type"`
	CustomID    string `json:"custom_id"`
	Label       string `json:"label,omitempty"`
	Style       int    `json:"style,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	MaxLength   int    `json:"max_length,omitempty"`
	Value       string `json:"value,omitempty"`
}

// ModalSubmitData is the parsed data from an MODAL_SUBMIT interaction.
type ModalSubmitData struct {
	CustomID   string      `json:"custom_id"`
	Components []ActionRow `json:"components"`
}

// encodeCreateCustomID encodes optional slash-command options into the modal custom_id
// so they survive the round-trip to the modal submit handler.
// Format: "backflow_create:{target_branch}:{runtime_min}"
func encodeCreateCustomID(targetBranch string, runtimeMin int) string {
	return fmt.Sprintf("%s:%s:%d", modalIDCreate, targetBranch, runtimeMin)
}

// decodeCreateCustomID parses the custom_id set when the modal was opened.
func decodeCreateCustomID(customID string) (targetBranch string, runtimeMin int) {
	parts := strings.SplitN(customID, ":", 3)
	if len(parts) < 3 {
		return "", 0
	}
	targetBranch = parts[1]
	runtimeMin, _ = strconv.Atoi(parts[2])
	return targetBranch, runtimeMin
}

// openCreateModal responds with a Discord modal for code task creation.
// targetBranch and runtimeMin are optional values from slash command options
// that are encoded in the modal's custom_id to be retrieved on submit.
func openCreateModal(w http.ResponseWriter, targetBranch string, runtimeMin int) {
	modal := ModalResponse{
		Type: ResponseTypeModal,
		Data: ModalData{
			CustomID: encodeCreateCustomID(targetBranch, runtimeMin),
			Title:    "Create Backflow Task",
			Components: []ActionRow{
				{
					Type: ComponentTypeActionRow,
					Components: []TextInput{{
						Type:        ComponentTypeTextInput,
						CustomID:    fieldRepoURL,
						Label:       "Repository URL",
						Style:       TextInputStyleShort,
						Required:    true,
						Placeholder: "https://github.com/owner/repo",
					}},
				},
				{
					Type: ComponentTypeActionRow,
					Components: []TextInput{{
						Type:        ComponentTypeTextInput,
						CustomID:    fieldPrompt,
						Label:       "Task description",
						Style:       TextInputStyleParagraph,
						Required:    true,
						Placeholder: "Describe what you want the agent to do...",
						MaxLength:   2000,
					}},
				},
				{
					Type: ComponentTypeActionRow,
					Components: []TextInput{{
						Type:        ComponentTypeTextInput,
						CustomID:    fieldBranch,
						Label:       "Branch (optional)",
						Style:       TextInputStyleShort,
						Required:    false,
						Placeholder: "Leave empty for default branch",
					}},
				},
				{
					Type: ComponentTypeActionRow,
					Components: []TextInput{{
						Type:        ComponentTypeTextInput,
						CustomID:    fieldHarness,
						Label:       "Harness (optional)",
						Style:       TextInputStyleShort,
						Required:    false,
						Placeholder: "claude_code or codex",
					}},
				},
				{
					Type: ComponentTypeActionRow,
					Components: []TextInput{{
						Type:        ComponentTypeTextInput,
						CustomID:    fieldBudgetUSD,
						Label:       "Max budget in USD (optional)",
						Style:       TextInputStyleShort,
						Required:    false,
						Placeholder: "e.g. 5.00",
					}},
				},
			},
		},
	}
	respondJSON(w, modal)
}

// handleCreateSubmit processes a modal submit interaction for task creation.
func handleCreateSubmit(ctx context.Context, w http.ResponseWriter, data ModalSubmitData, createTask CreateTaskFunc) {
	// Parse the custom_id to recover slash-command options.
	targetBranch, runtimeMin := decodeCreateCustomID(data.CustomID)

	// Extract modal field values.
	fields := extractModalFields(data.Components)
	repoURL := strings.TrimSpace(fields[fieldRepoURL])
	prompt := strings.TrimSpace(fields[fieldPrompt])
	branch := strings.TrimSpace(fields[fieldBranch])
	harness := strings.TrimSpace(fields[fieldHarness])
	budgetStr := strings.TrimSpace(fields[fieldBudgetUSD])

	// Validate required fields locally before calling CreateTaskFunc.
	if repoURL == "" {
		respondJSON(w, ChannelMessageResponse{
			Type: ResponseTypeChannelMessage,
			Data: MessageData{Content: "repo_url is required.", Flags: FlagEphemeral},
		})
		return
	}
	if prompt == "" {
		respondJSON(w, ChannelMessageResponse{
			Type: ResponseTypeChannelMessage,
			Data: MessageData{Content: "prompt is required.", Flags: FlagEphemeral},
		})
		return
	}

	// Parse optional budget.
	var budgetUSD float64
	if budgetStr != "" {
		var err error
		budgetUSD, err = strconv.ParseFloat(budgetStr, 64)
		if err != nil || budgetUSD < 0 {
			respondJSON(w, ChannelMessageResponse{
				Type: ResponseTypeChannelMessage,
				Data: MessageData{Content: fmt.Sprintf("Invalid budget %q: must be a non-negative number (e.g. 5.00).", budgetStr), Flags: FlagEphemeral},
			})
			return
		}
	}

	req := &models.CreateTaskRequest{
		TaskMode:      models.TaskModeCode,
		RepoURL:       repoURL,
		Prompt:        prompt,
		Branch:        branch,
		TargetBranch:  targetBranch,
		Harness:       harness,
		MaxBudgetUSD:  budgetUSD,
		MaxRuntimeMin: runtimeMin,
	}

	if createTask == nil {
		respondJSON(w, ChannelMessageResponse{
			Type: ResponseTypeChannelMessage,
			Data: MessageData{Content: "Task creation is unavailable right now.", Flags: FlagEphemeral},
		})
		return
	}

	task, err := createTask(ctx, req)
	if err != nil {
		log.Warn().Err(err).Msg("discord: failed to create task from modal")
		respondJSON(w, ChannelMessageResponse{
			Type: ResponseTypeChannelMessage,
			Data: MessageData{Content: fmt.Sprintf("Failed to create task: %s", err.Error()), Flags: FlagEphemeral},
		})
		return
	}

	respondJSON(w, ChannelMessageResponse{
		Type: ResponseTypeChannelMessage,
		Data: MessageData{Content: formatCreatedTask(task)},
	})
}

// extractModalFields walks the action rows returned in a modal submit and
// returns a map of custom_id → value for all TEXT_INPUT components.
func extractModalFields(rows []ActionRow) map[string]string {
	out := make(map[string]string)
	for _, row := range rows {
		if row.Type != ComponentTypeActionRow {
			continue
		}
		for _, comp := range row.Components {
			if comp.Type == ComponentTypeTextInput {
				out[comp.CustomID] = comp.Value
			}
		}
	}
	return out
}

// formatCreatedTask produces a user-facing confirmation for a newly created task.
func formatCreatedTask(task *models.Task) string {
	parts := []string{
		fmt.Sprintf("Task created: **%s**", task.ID),
		fmt.Sprintf("Repo: %s", task.RepoURL),
		fmt.Sprintf("Status: %s", task.Status),
	}
	if task.Harness != "" {
		parts = append(parts, fmt.Sprintf("Harness: %s", task.Harness))
	}
	return strings.Join(parts, "\n")
}
