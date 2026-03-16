package messaging

import (
	"context"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/backflow-labs/backflow/internal/config"
	"github.com/backflow-labs/backflow/internal/models"
	"github.com/backflow-labs/backflow/internal/store"
)

// --- minimal mock store ---

type mockStore struct {
	senders map[string]*models.AllowedSender
	tasks   []*models.Task
}

func (m *mockStore) GetAllowedSender(_ context.Context, channelType, address string) (*models.AllowedSender, error) {
	return m.senders[channelType+":"+address], nil
}

func (m *mockStore) CreateTask(_ context.Context, task *models.Task) error {
	m.tasks = append(m.tasks, task)
	return nil
}

// Unused Store methods — satisfy the interface.
func (m *mockStore) GetTask(context.Context, string) (*models.Task, error)                          { return nil, nil }
func (m *mockStore) ListTasks(context.Context, store.TaskFilter) ([]*models.Task, error)            { return nil, nil }
func (m *mockStore) UpdateTask(context.Context, *models.Task) error                                 { return nil }
func (m *mockStore) DeleteTask(context.Context, string) error                                       { return nil }
func (m *mockStore) CreateInstance(context.Context, *models.Instance) error                         { return nil }
func (m *mockStore) GetInstance(context.Context, string) (*models.Instance, error)                  { return nil, nil }
func (m *mockStore) ListInstances(context.Context, *models.InstanceStatus) ([]*models.Instance, error) { return nil, nil }
func (m *mockStore) UpdateInstance(context.Context, *models.Instance) error                         { return nil }
func (m *mockStore) Close() error                                                                   { return nil }

func newTestConfig() *config.Config {
	return &config.Config{
		DefaultHarness: "claude_code",
		DefaultModel:   "claude-sonnet-4-6",
		DefaultEffort:  "high",
	}
}

func postForm(handler http.HandlerFunc, values url.Values) *httptest.ResponseRecorder {
	body := values.Encode()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/sms/inbound", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

func TestInboundHandler_AllowedSender(t *testing.T) {
	db := &mockStore{
		senders: map[string]*models.AllowedSender{
			"sms:+15551234567": {
				ChannelType: "sms",
				Address:     "+15551234567",
				DefaultRepo: "https://github.com/backflow-labs/backflow",
				Enabled:     true,
			},
		},
	}
	handler := InboundHandler(db, newTestConfig(), NoopMessenger{})

	w := postForm(handler, url.Values{
		"From": {"+15551234567"},
		"Body": {"Fix the flaky test"},
	})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// Verify task was created
	if len(db.tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(db.tasks))
	}
	task := db.tasks[0]
	if task.RepoURL != "https://github.com/backflow-labs/backflow" {
		t.Errorf("repo = %q, want default", task.RepoURL)
	}
	if task.Prompt != "Fix the flaky test" {
		t.Errorf("prompt = %q", task.Prompt)
	}
	if task.ReplyChannel != "sms:+15551234567" {
		t.Errorf("reply_channel = %q", task.ReplyChannel)
	}
	if !task.CreatePR || !task.SelfReview {
		t.Error("expected create_pr and self_review to be true")
	}

	// Verify TwiML response
	var resp twiMLResponse
	if err := xml.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid TwiML: %v", err)
	}
	if resp.Message == nil {
		t.Fatal("expected TwiML message")
	}
	if !strings.Contains(resp.Message.Body, "Task created") {
		t.Errorf("unexpected response: %q", resp.Message.Body)
	}
}

func TestInboundHandler_RejectedSender(t *testing.T) {
	db := &mockStore{
		senders: map[string]*models.AllowedSender{},
	}
	handler := InboundHandler(db, newTestConfig(), NoopMessenger{})

	w := postForm(handler, url.Values{
		"From": {"+15559999999"},
		"Body": {"hack the planet"},
	})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(db.tasks) != 0 {
		t.Fatal("expected no tasks created for rejected sender")
	}

	var resp twiMLResponse
	xml.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Message == nil || !strings.Contains(resp.Message.Body, "not authorized") {
		t.Errorf("expected rejection message, got %v", resp.Message)
	}
}

func TestInboundHandler_DisabledSender(t *testing.T) {
	db := &mockStore{
		senders: map[string]*models.AllowedSender{
			"sms:+15551234567": {
				ChannelType: "sms",
				Address:     "+15551234567",
				Enabled:     false,
			},
		},
	}
	handler := InboundHandler(db, newTestConfig(), NoopMessenger{})

	w := postForm(handler, url.Values{
		"From": {"+15551234567"},
		"Body": {"Fix the test"},
	})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(db.tasks) != 0 {
		t.Fatal("expected no tasks created for disabled sender")
	}
}

func TestInboundHandler_WithExplicitRepo(t *testing.T) {
	db := &mockStore{
		senders: map[string]*models.AllowedSender{
			"sms:+15551234567": {
				ChannelType: "sms",
				Address:     "+15551234567",
				Enabled:     true,
			},
		},
	}
	handler := InboundHandler(db, newTestConfig(), NoopMessenger{})

	w := postForm(handler, url.Values{
		"From": {"+15551234567"},
		"Body": {"github.com/org/repo Fix the bug"},
	})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(db.tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(db.tasks))
	}
	if db.tasks[0].RepoURL != "https://github.com/org/repo" {
		t.Errorf("repo = %q", db.tasks[0].RepoURL)
	}
}

func TestInboundHandler_MissingFields(t *testing.T) {
	db := &mockStore{senders: map[string]*models.AllowedSender{}}
	handler := InboundHandler(db, newTestConfig(), NoopMessenger{})

	w := postForm(handler, url.Values{
		"From": {"+15551234567"},
	})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(db.tasks) != 0 {
		t.Fatal("expected no tasks created")
	}
}
