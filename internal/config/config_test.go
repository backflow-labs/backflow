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
}
