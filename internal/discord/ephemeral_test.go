package discord

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// TestModalSubmitErrors_AreEphemeral verifies that all error responses from
// modal submit use the ephemeral flag (64) so only the submitting user sees them.
func TestModalSubmitErrors_AreEphemeral(t *testing.T) {
	pub, priv := testKeyPair(t)

	tests := []struct {
		name      string
		createFn  CreateTaskFunc
		fields    map[string]string
		wantInMsg string
	}{
		{
			name:      "missing repo_url",
			createFn:  fakeCreateTask(fakeTask(), nil),
			fields:    map[string]string{fieldPrompt: "Add tests"},
			wantInMsg: "repo_url is required",
		},
		{
			name:      "missing prompt",
			createFn:  fakeCreateTask(fakeTask(), nil),
			fields:    map[string]string{fieldRepoURL: "https://github.com/owner/repo"},
			wantInMsg: "prompt is required",
		},
		{
			name:     "invalid budget",
			createFn: fakeCreateTask(fakeTask(), nil),
			fields: map[string]string{
				fieldRepoURL:   "https://github.com/owner/repo",
				fieldPrompt:    "Add tests",
				fieldBudgetUSD: "abc",
			},
			wantInMsg: "Invalid budget",
		},
		{
			name:     "nil creator",
			createFn: nil,
			fields: map[string]string{
				fieldRepoURL: "https://github.com/owner/repo",
				fieldPrompt:  "Add tests",
			},
			wantInMsg: "unavailable",
		},
		{
			name:     "creator error",
			createFn: fakeCreateTask(nil, fmt.Errorf("db down")),
			fields: map[string]string{
				fieldRepoURL: "https://github.com/owner/repo",
				fieldPrompt:  "Add tests",
			},
			wantInMsg: "Failed to create task",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			handler := InteractionHandler(pub, nil, tc.createFn, nil, nil, nil)
			customID := modalIDCreate
			body := buildModalSubmitBody(customID, tc.fields)
			rr := postInteraction(handler, priv, body)

			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
			}

			var raw map[string]json.RawMessage
			if err := json.NewDecoder(rr.Body).Decode(&raw); err != nil {
				t.Fatalf("decode: %v", err)
			}
			var data struct {
				Flags int `json:"flags"`
			}
			if err := json.Unmarshal(raw["data"], &data); err != nil {
				t.Fatalf("decode data: %v", err)
			}
			if data.Flags != FlagEphemeral {
				t.Errorf("flags = %d, want %d (ephemeral)", data.Flags, FlagEphemeral)
			}
		})
	}
}

// TestModalSubmitSuccess_NotEphemeral verifies that successful task creation
// responses are visible to the whole channel (no ephemeral flag).
func TestModalSubmitSuccess_NotEphemeral(t *testing.T) {
	pub, priv := testKeyPair(t)
	handler := InteractionHandler(pub, nil, fakeCreateTask(fakeTask(), nil), nil, nil, nil)

	customID := modalIDCreate
	body := buildModalSubmitBody(customID, map[string]string{
		fieldRepoURL: "https://github.com/owner/repo",
		fieldPrompt:  "Add tests",
	})
	rr := postInteraction(handler, priv, body)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	var raw map[string]json.RawMessage
	if err := json.NewDecoder(rr.Body).Decode(&raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var data struct {
		Flags int `json:"flags"`
	}
	if err := json.Unmarshal(raw["data"], &data); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if data.Flags != 0 {
		t.Errorf("flags = %d, want 0 (not ephemeral for success)", data.Flags)
	}
}
