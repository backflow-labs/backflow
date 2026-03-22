package notify

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSlackNotifier_NotifySendsTextPayload(t *testing.T) {
	var gotBody string
	var gotErr error

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			gotErr = err
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		gotBody = string(raw)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	notifier := NewSlackNotifier(server.URL, nil)
	event := Event{
		Type:      EventTaskCompleted,
		TaskID:    "bf_123",
		PRURL:     "https://github.com/org/repo/pull/42",
		RepoURL:   "https://github.com/org/repo",
		Timestamp: time.Now().UTC(),
	}

	if err := notifier.Notify(event); err != nil {
		t.Fatalf("Notify() error = %v", err)
	}
	if gotErr != nil {
		t.Fatalf("read request body: %v", gotErr)
	}

	var payload struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(gotBody), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Text == "" {
		t.Fatal("expected slack payload text")
	}
	if !strings.Contains(payload.Text, "bf_123") {
		t.Fatalf("payload text %q does not mention task id", payload.Text)
	}
	if !strings.Contains(payload.Text, "completed") {
		t.Fatalf("payload text %q does not mention completion", payload.Text)
	}
	if !strings.Contains(payload.Text, "Repo: https://github.com/org/repo") {
		t.Fatalf("payload text %q does not include repo url", payload.Text)
	}
}

func TestSlackNotifier_EventFilter(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	notifier := NewSlackNotifier(server.URL, []string{"task.failed"})

	if err := notifier.Notify(Event{Type: EventTaskCompleted, TaskID: "bf_123", Timestamp: time.Now().UTC()}); err != nil {
		t.Fatalf("Notify(completed) error = %v", err)
	}
	if calls != 0 {
		t.Fatalf("expected no request for filtered event, got %d", calls)
	}

	if err := notifier.Notify(Event{Type: EventTaskFailed, TaskID: "bf_123", Timestamp: time.Now().UTC()}); err != nil {
		t.Fatalf("Notify(failed) error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected one request for matching event, got %d", calls)
	}
}

func TestSlackNotifier_Name(t *testing.T) {
	if got := NewSlackNotifier("https://hooks.slack.com/services/test", nil).Name(); got != "slack" {
		t.Fatalf("Name() = %q, want %q", got, "slack")
	}
}
