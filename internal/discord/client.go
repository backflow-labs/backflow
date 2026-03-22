package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const discordAPIBase = "https://discord.com/api/v10"

// Client is the small Discord REST surface used by Backflow notifications.
type Client interface {
	CreateMessage(ctx context.Context, channelID string, payload MessagePayload) (*Message, error)
	StartThreadFromMessage(ctx context.Context, channelID, messageID string, payload StartThreadPayload) (*Channel, error)
}

// APIClient talks to the Discord REST API.
type APIClient struct {
	baseURL    string
	botToken   string
	httpClient *http.Client
}

// NewClient creates a Discord API client using the default Discord base URL.
func NewClient(botToken string) *APIClient {
	return NewClientWithBaseURL("", botToken)
}

// NewClientWithBaseURL creates a Discord API client with an override base URL
// for tests.
func NewClientWithBaseURL(baseURL, botToken string) *APIClient {
	if baseURL == "" {
		baseURL = discordAPIBase
	}
	return &APIClient{
		baseURL:  baseURL,
		botToken: botToken,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// AllowedMentions suppresses mention parsing for operational notifications.
type AllowedMentions struct {
	Parse []string `json:"parse"`
}

// Embed represents a Discord embed payload.
type Embed struct {
	Title       string       `json:"title,omitempty"`
	Description string       `json:"description,omitempty"`
	URL         string       `json:"url,omitempty"`
	Color       int          `json:"color,omitempty"`
	Timestamp   string       `json:"timestamp,omitempty"`
	Fields      []EmbedField `json:"fields,omitempty"`
	Footer      *EmbedFooter `json:"footer,omitempty"`
}

// EmbedField is a Discord embed field.
type EmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

// EmbedFooter is a Discord embed footer.
type EmbedFooter struct {
	Text string `json:"text"`
}

// Button style constants.
const (
	ButtonStylePrimary   = 1
	ButtonStyleSecondary = 2
	ButtonStyleSuccess   = 3
	ButtonStyleDanger    = 4
)

// Button is a clickable button component.
type Button struct {
	Type     int    `json:"type"` // always ComponentTypeButton (2)
	Style    int    `json:"style"`
	Label    string `json:"label"`
	CustomID string `json:"custom_id,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
}

// MessageActionRow is an action row containing buttons for use in message payloads.
type MessageActionRow struct {
	Type       int      `json:"type"` // always ComponentTypeActionRow (1)
	Components []Button `json:"components"`
}

// MessagePayload is used for both channel and thread messages.
type MessagePayload struct {
	Content         string             `json:"content,omitempty"`
	Embeds          []Embed            `json:"embeds,omitempty"`
	Components      []MessageActionRow `json:"components,omitempty"`
	AllowedMentions *AllowedMentions   `json:"allowed_mentions,omitempty"`
}

// StartThreadPayload starts a thread from an existing message.
type StartThreadPayload struct {
	Name                string `json:"name"`
	AutoArchiveDuration int    `json:"auto_archive_duration,omitempty"`
}

// Message is the minimal response data needed from Discord.
type Message struct {
	ID string `json:"id"`
}

// Channel is the minimal thread response data needed from Discord.
type Channel struct {
	ID string `json:"id"`
}

func (c *APIClient) CreateMessage(ctx context.Context, channelID string, payload MessagePayload) (*Message, error) {
	var msg Message
	if err := c.doJSON(ctx, http.MethodPost, fmt.Sprintf("/channels/%s/messages", channelID), payload, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

func (c *APIClient) StartThreadFromMessage(ctx context.Context, channelID, messageID string, payload StartThreadPayload) (*Channel, error) {
	var channel Channel
	if payload.AutoArchiveDuration == 0 {
		payload.AutoArchiveDuration = 10080
	}
	if err := c.doJSON(ctx, http.MethodPost, fmt.Sprintf("/channels/%s/messages/%s/threads", channelID, messageID), payload, &channel); err != nil {
		return nil, err
	}
	return &channel, nil
}

func (c *APIClient) doJSON(ctx context.Context, method, path string, body any, out any) error {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return fmt.Errorf("encode request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, &buf)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bot "+c.botToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("discord API request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("discord API returned %d: %s", resp.StatusCode, string(respBody))
	}

	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
