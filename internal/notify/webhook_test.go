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

func TestEvent_MarshalJSONRedactsReplyChannel(t *testing.T) {
	event := Event{
		Type:         EventTaskCompleted,
		TaskID:       "bf_123",
		ReplyChannel: "sms:+15551234567",
		Timestamp:    time.Now().UTC(),
	}

	body, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}

	jsonBody := string(body)
	if strings.Contains(jsonBody, "+15551234567") {
		t.Fatalf("serialized event leaked phone number: %s", jsonBody)
	}
	if !strings.Contains(jsonBody, `"reply_channel":"sms"`) {
		t.Fatalf("serialized event did not redact reply channel: %s", jsonBody)
	}
	if event.ReplyChannel != "sms:+15551234567" {
		t.Fatalf("event ReplyChannel was mutated: %q", event.ReplyChannel)
	}
}

func TestWebhookNotifier_NotifyRedactsReplyChannel(t *testing.T) {
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

	notifier := NewWebhookNotifier(server.URL, nil)
	event := Event{
		Type:         EventTaskCompleted,
		TaskID:       "bf_123",
		ReplyChannel: "sms:+15551234567",
		Timestamp:    time.Now().UTC(),
	}

	if err := notifier.Notify(event); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if gotErr != nil {
		t.Fatalf("read webhook body: %v", gotErr)
	}

	if strings.Contains(gotBody, "+15551234567") {
		t.Fatalf("webhook payload leaked phone number: %s", gotBody)
	}
	if !strings.Contains(gotBody, `"reply_channel":"sms"`) {
		t.Fatalf("webhook payload did not redact reply channel: %s", gotBody)
	}
	if event.ReplyChannel != "sms:+15551234567" {
		t.Fatalf("event ReplyChannel was mutated: %q", event.ReplyChannel)
	}
}
