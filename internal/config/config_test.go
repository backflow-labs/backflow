package config

import (
	"os"
	"strings"
	"testing"
)

func TestLoad_MissingDatabaseURL(t *testing.T) {
	// Set minimum env vars to pass earlier validations
	os.Setenv("ANTHROPIC_API_KEY", "test-key")
	os.Setenv("BACKFLOW_DATABASE_URL", "")
	defer os.Unsetenv("ANTHROPIC_API_KEY")
	defer os.Unsetenv("BACKFLOW_DATABASE_URL")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error when BACKFLOW_DATABASE_URL is empty, got nil")
	}

	want := "BACKFLOW_DATABASE_URL"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error should mention %q, got: %s", want, err.Error())
	}
}
