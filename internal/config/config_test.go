package config

import (
	"strings"
	"testing"
)

func TestLoad_MissingDatabaseURL(t *testing.T) {
	// Set minimum env vars to pass earlier validations
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("BACKFLOW_DATABASE_URL", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error when BACKFLOW_DATABASE_URL is empty, got nil")
	}

	want := "BACKFLOW_DATABASE_URL"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error should mention %q, got: %s", want, err.Error())
	}
}

func TestLoad_DefaultModel(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("BACKFLOW_DATABASE_URL", "postgres://user:pass@localhost:5432/db")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if cfg.DefaultModel != "claude-sonnet-4-6" {
		t.Errorf("DefaultModel = %q, want claude-sonnet-4-6", cfg.DefaultModel)
	}
	if cfg.SlackEvents != nil {
		t.Errorf("SlackEvents = %#v, want nil when unset", cfg.SlackEvents)
	}
	if cfg.DiscordEvents != nil {
		t.Errorf("DiscordEvents = %#v, want nil when unset", cfg.DiscordEvents)
	}
}

func TestLoad_SlackAndDiscordEvents(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("BACKFLOW_DATABASE_URL", "postgres://user:pass@localhost:5432/db")
	t.Setenv("BACKFLOW_SLACK_WEBHOOK_URL", "https://hooks.slack.com/services/test")
	t.Setenv("BACKFLOW_DISCORD_WEBHOOK_URL", "https://discord.com/api/webhooks/test")
	t.Setenv("BACKFLOW_SLACK_EVENTS", "task.created, task.completed ,task.failed")
	t.Setenv("BACKFLOW_DISCORD_EVENTS", "task.running, task.interrupted")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	wantSlack := []string{"task.created", "task.completed", "task.failed"}
	if len(cfg.SlackEvents) != len(wantSlack) {
		t.Fatalf("SlackEvents length = %d, want %d", len(cfg.SlackEvents), len(wantSlack))
	}
	for i := range wantSlack {
		if cfg.SlackEvents[i] != wantSlack[i] {
			t.Fatalf("SlackEvents[%d] = %q, want %q", i, cfg.SlackEvents[i], wantSlack[i])
		}
	}

	wantDiscord := []string{"task.running", "task.interrupted"}
	if len(cfg.DiscordEvents) != len(wantDiscord) {
		t.Fatalf("DiscordEvents length = %d, want %d", len(cfg.DiscordEvents), len(wantDiscord))
	}
	for i := range wantDiscord {
		if cfg.DiscordEvents[i] != wantDiscord[i] {
			t.Fatalf("DiscordEvents[%d] = %q, want %q", i, cfg.DiscordEvents[i], wantDiscord[i])
		}
	}
}
