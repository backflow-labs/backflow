package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSetupLogger_StderrOnly(t *testing.T) {
	logger, closer, err := setupLogger("")
	if err != nil {
		t.Fatalf("setupLogger(\"\") returned error: %v", err)
	}
	if closer != nil {
		t.Error("closer should be nil when no log file is specified")
	}
	// Logger should be usable
	logger.Info().Msg("test message")
}

func TestSetupLogger_WithFile(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "test.log")

	logger, closer, err := setupLogger(logPath)
	if err != nil {
		t.Fatalf("setupLogger(%q) returned error: %v", logPath, err)
	}
	if closer == nil {
		t.Fatal("closer should not be nil when log file is specified")
	}
	logger.Info().Msg("hello from test")

	// Close to flush, then verify file has content
	closer.Close()

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}
	if len(data) == 0 {
		t.Error("log file is empty, expected content")
	}
}

func TestSetupLogger_CreatesParentDirs(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "nested", "dir", "test.log")

	_, closer, err := setupLogger(logPath)
	if err != nil {
		t.Fatalf("setupLogger(%q) returned error: %v", logPath, err)
	}
	if closer == nil {
		t.Fatal("closer should not be nil")
	}
	closer.Close()

	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Errorf("log file was not created at %s", logPath)
	}
}
