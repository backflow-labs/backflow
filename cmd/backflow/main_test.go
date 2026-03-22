package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/backflow-labs/backflow/internal/config"
)

func TestLogConfiguredNotificationChannels(t *testing.T) {
	var buf bytes.Buffer
	orig := log.Logger
	log.Logger = zerolog.New(&buf)
	t.Cleanup(func() {
		log.Logger = orig
	})

	cfg := &config.Config{
		SlackWebhookURL: "https://hooks.slack.com/services/test",
	}

	logConfiguredNotificationChannels(cfg)

	out := buf.String()
	if !strings.Contains(out, "slack notifications enabled") {
		t.Fatalf("log output missing Slack enabled message: %s", out)
	}
	if strings.Contains(out, cfg.SlackWebhookURL) {
		t.Fatalf("log output leaked Slack URL: %s", out)
	}
}
