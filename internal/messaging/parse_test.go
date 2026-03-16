package messaging

import (
	"testing"
)

func TestParseTaskFromSMS(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		defaultRepo string
		wantRepo    string
		wantPrompt  string
		wantErr     bool
	}{
		{
			name:       "full URL with prompt",
			body:       "https://github.com/backflow-labs/backflow Fix the flaky test",
			wantRepo:   "https://github.com/backflow-labs/backflow",
			wantPrompt: "Fix the flaky test",
		},
		{
			name:       "short URL with prompt",
			body:       "github.com/backflow-labs/backflow Fix the flaky test",
			wantRepo:   "https://github.com/backflow-labs/backflow",
			wantPrompt: "Fix the flaky test",
		},
		{
			name:        "prompt only with default repo",
			body:        "Fix the flaky test",
			defaultRepo: "https://github.com/backflow-labs/backflow",
			wantRepo:    "https://github.com/backflow-labs/backflow",
			wantPrompt:  "Fix the flaky test",
		},
		{
			name:    "prompt only without default repo",
			body:    "Fix the flaky test",
			wantErr: true,
		},
		{
			name:    "empty message",
			body:    "",
			wantErr: true,
		},
		{
			name:    "whitespace only",
			body:    "   ",
			wantErr: true,
		},
		{
			name:    "URL without prompt",
			body:    "github.com/backflow-labs/backflow",
			wantErr: true,
		},
		{
			name:       "URL with extra whitespace",
			body:       "  github.com/backflow-labs/backflow   Fix the flaky test  ",
			wantRepo:   "https://github.com/backflow-labs/backflow",
			wantPrompt: "Fix the flaky test",
		},
		{
			name:        "multi-word prompt with default repo",
			body:        "Refactor the orchestrator to use interfaces for all external dependencies",
			defaultRepo: "https://github.com/example/repo",
			wantRepo:    "https://github.com/example/repo",
			wantPrompt:  "Refactor the orchestrator to use interfaces for all external dependencies",
		},
		{
			name:       "http URL",
			body:       "http://github.com/org/repo Add a README",
			wantRepo:   "http://github.com/org/repo",
			wantPrompt: "Add a README",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, prompt, err := ParseTaskFromSMS(tt.body, tt.defaultRepo)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseTaskFromSMS() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if repo != tt.wantRepo {
				t.Errorf("repo = %q, want %q", repo, tt.wantRepo)
			}
			if prompt != tt.wantPrompt {
				t.Errorf("prompt = %q, want %q", prompt, tt.wantPrompt)
			}
		})
	}
}
