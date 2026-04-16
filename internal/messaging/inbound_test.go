package messaging

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
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
	s, ok := m.senders[channelType+":"+address]
	if !ok {
		return nil, store.ErrNotFound
	}
	return s, nil
}

func (m *mockStore) CreateTask(_ context.Context, task *models.Task) error {
	m.tasks = append(m.tasks, task)
	return nil
}

func (m *mockStore) HasAPIKeys(context.Context) (bool, error) { return false, nil }
func (m *mockStore) GetAPIKeyByHash(context.Context, string) (*models.APIKey, error) {
	return nil, store.ErrNotFound
}
func (m *mockStore) CreateAPIKey(context.Context, *models.APIKey) error { return nil }

// Unused Store methods — satisfy the interface.
func (m *mockStore) GetTask(context.Context, string) (*models.Task, error) {
	return nil, store.ErrNotFound
}
func (m *mockStore) ListTasks(context.Context, store.TaskFilter) ([]*models.Task, error) {
	return nil, nil
}
func (m *mockStore) DeleteTask(context.Context, string) error               { return nil }
func (m *mockStore) CreateInstance(context.Context, *models.Instance) error { return nil }
func (m *mockStore) GetInstance(context.Context, string) (*models.Instance, error) {
	return nil, store.ErrNotFound
}
func (m *mockStore) ListInstances(context.Context, *models.InstanceStatus) ([]*models.Instance, error) {
	return nil, nil
}
func (m *mockStore) UpdateTaskStatus(context.Context, string, models.TaskStatus, string) error {
	return nil
}
func (m *mockStore) AssignTask(context.Context, string, string) error { return nil }
func (m *mockStore) StartTask(context.Context, string, string) error  { return nil }
func (m *mockStore) CompleteTask(context.Context, string, store.TaskResult) error {
	return nil
}
func (m *mockStore) RequeueTask(context.Context, string, string) error { return nil }
func (m *mockStore) CancelTask(context.Context, string) error          { return nil }
func (m *mockStore) ClearTaskAssignment(context.Context, string) error { return nil }
func (m *mockStore) MarkReadyForRetry(context.Context, string) error   { return nil }
func (m *mockStore) RetryTask(context.Context, string, int) error      { return nil }
func (m *mockStore) UpdateInstanceStatus(context.Context, string, models.InstanceStatus) error {
	return nil
}
func (m *mockStore) IncrementRunningContainers(context.Context, string) error { return nil }
func (m *mockStore) DecrementRunningContainers(context.Context, string) error { return nil }
func (m *mockStore) UpdateInstanceDetails(context.Context, string, string, string) error {
	return nil
}
func (m *mockStore) ResetRunningContainers(context.Context, string) error { return nil }
func (m *mockStore) CreateAllowedSender(context.Context, *models.AllowedSender) error {
	return nil
}
func (m *mockStore) UpsertDiscordInstall(context.Context, *models.DiscordInstall) error { return nil }
func (m *mockStore) GetDiscordInstall(context.Context, string) (*models.DiscordInstall, error) {
	return nil, store.ErrNotFound
}
func (m *mockStore) DeleteDiscordInstall(context.Context, string) error { return nil }
func (m *mockStore) UpsertDiscordTaskThread(context.Context, *models.DiscordTaskThread) error {
	return nil
}
func (m *mockStore) GetDiscordTaskThread(context.Context, string) (*models.DiscordTaskThread, error) {
	return nil, store.ErrNotFound
}
func (m *mockStore) CreateReading(context.Context, *models.Reading) error       { return nil }
func (m *mockStore) UpsertReading(context.Context, *models.Reading) error       { return nil }
func (m *mockStore) WithTx(_ context.Context, fn func(store.Store) error) error { return fn(m) }
func (m *mockStore) Close() error                                               { return nil }

const testAuthToken = "test-auth-token"

func newTestConfig() *config.Config {
	return &config.Config{
		TwilioAuthToken:    testAuthToken,
		DefaultHarness:     "claude_code",
		DefaultClaudeModel: "claude-sonnet-4-6",
		DefaultEffort:      "medium",
		DefaultCreatePR:    true,
		DefaultSaveOutput:  true,
	}
}

// postForm sends a signed POST to the handler, simulating a legitimate Twilio request.
func postForm(handler http.HandlerFunc, values url.Values) *httptest.ResponseRecorder {
	body := values.Encode()
	reqURL := "http://example.com/webhooks/sms/inbound"
	req := httptest.NewRequest(http.MethodPost, reqURL, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	sig := signRequest(testAuthToken, reqURL, values)
	req.Header.Set("X-Twilio-Signature", sig)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

// postUnsignedForm sends an unsigned POST, simulating a forged request.
func postUnsignedForm(handler http.HandlerFunc, values url.Values) *httptest.ResponseRecorder {
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

				Enabled: true,
			},
		},
	}
	handler := InboundHandler(db, newTestConfig(), NoopMessenger{}, nil)

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
	if task.Prompt != "Fix the flaky test" {
		t.Errorf("prompt = %q, want %q", task.Prompt, "Fix the flaky test")
	}
	if task.TaskMode != models.TaskModeAuto {
		t.Errorf("task_mode = %q, want %q", task.TaskMode, models.TaskModeAuto)
	}
	if task.RepoURL != "" {
		t.Errorf("repo_url = %q, want empty", task.RepoURL)
	}
	if task.ReplyChannel != "sms:+15551234567" {
		t.Errorf("reply_channel = %q", task.ReplyChannel)
	}
	if !task.CreatePR || task.SelfReview {
		t.Error("expected create_pr true and self_review false")
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
	if strings.Contains(resp.Message.Body, "Repo:") {
		t.Errorf("response should not contain Repo line: %q", resp.Message.Body)
	}
	if !strings.HasSuffix(resp.Message.Body, "\n"+UnsubscribeFooter) {
		t.Errorf("response missing unsubscribe footer on its own line: %q", resp.Message.Body)
	}
}

func TestInboundHandler_ReadCommandCreatesReadTask(t *testing.T) {
	db := &mockStore{
		senders: map[string]*models.AllowedSender{
			"sms:+15551234567": {
				ChannelType: "sms",
				Address:     "+15551234567",
				Enabled:     true,
			},
		},
	}
	cfg := newTestConfig()
	cfg.ReaderImage = "reader:latest"
	handler := InboundHandler(db, cfg, NoopMessenger{}, nil)

	w := postForm(handler, url.Values{
		"From": {"+15551234567"},
		"Body": {"Read https://example.com/article trailing words"},
	})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(db.tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(db.tasks))
	}
	task := db.tasks[0]
	if task.TaskMode != models.TaskModeRead {
		t.Fatalf("task_mode = %q, want %q", task.TaskMode, models.TaskModeRead)
	}
	if task.Prompt != "https://example.com/article" {
		t.Fatalf("prompt = %q, want first URL only", task.Prompt)
	}
	if task.CreatePR {
		t.Fatal("CreatePR = true, want false for read mode")
	}

	var resp twiMLResponse
	if err := xml.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid TwiML: %v", err)
	}
	if resp.Message == nil {
		t.Fatal("expected TwiML message")
	}
	if !strings.Contains(resp.Message.Body, "Reading https://example.com/article...") {
		t.Fatalf("unexpected response: %q", resp.Message.Body)
	}
}

func TestInboundHandler_InvalidReadCommandDoesNotCreateTask(t *testing.T) {
	db := &mockStore{
		senders: map[string]*models.AllowedSender{
			"sms:+15551234567": {
				ChannelType: "sms",
				Address:     "+15551234567",
				Enabled:     true,
			},
		},
	}
	cfg := newTestConfig()
	cfg.ReaderImage = "reader:latest"
	handler := InboundHandler(db, cfg, NoopMessenger{}, nil)

	w := postForm(handler, url.Values{
		"From": {"+15551234567"},
		"Body": {"Read http://example.com/article"},
	})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(db.tasks) != 0 {
		t.Fatalf("expected no tasks, got %d", len(db.tasks))
	}

	var resp twiMLResponse
	if err := xml.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid TwiML: %v", err)
	}
	if resp.Message == nil {
		t.Fatal("expected TwiML message")
	}
	if !strings.Contains(resp.Message.Body, "invalid read command") {
		t.Fatalf("unexpected response: %q", resp.Message.Body)
	}
}

func TestInboundHandler_RejectedSender(t *testing.T) {
	db := &mockStore{
		senders: map[string]*models.AllowedSender{},
	}
	handler := InboundHandler(db, newTestConfig(), NoopMessenger{}, nil)

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
	if resp.Message != nil && !strings.HasSuffix(resp.Message.Body, "\n"+UnsubscribeFooter) {
		t.Errorf("rejection response missing unsubscribe footer: %q", resp.Message.Body)
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
	handler := InboundHandler(db, newTestConfig(), NoopMessenger{}, nil)

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
	handler := InboundHandler(db, newTestConfig(), NoopMessenger{}, nil)

	w := postForm(handler, url.Values{
		"From": {"+15551234567"},
		"Body": {"https://github.com/test/repo fix the auth bug"},
	})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(db.tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(db.tasks))
	}
	task := db.tasks[0]
	if task.Prompt != "https://github.com/test/repo fix the auth bug" {
		t.Errorf("prompt = %q, want raw body", task.Prompt)
	}
	if task.TaskMode != models.TaskModeAuto {
		t.Errorf("task_mode = %q, want %q", task.TaskMode, models.TaskModeAuto)
	}
	if task.RepoURL != "" {
		t.Errorf("repo_url = %q, want empty", task.RepoURL)
	}
}

func TestInboundHandler_MissingFields(t *testing.T) {
	db := &mockStore{senders: map[string]*models.AllowedSender{}}
	handler := InboundHandler(db, newTestConfig(), NoopMessenger{}, nil)

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

func TestInboundHandler_WhitespaceOnlyBody(t *testing.T) {
	db := &mockStore{
		senders: map[string]*models.AllowedSender{
			"sms:+15551234567": {
				ChannelType: "sms",
				Address:     "+15551234567",
				Enabled:     true,
			},
		},
	}
	handler := InboundHandler(db, newTestConfig(), NoopMessenger{}, nil)

	w := postForm(handler, url.Values{
		"From": {"+15551234567"},
		"Body": {"   "},
	})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(db.tasks) != 0 {
		t.Fatal("expected no tasks created for whitespace-only body")
	}
}

func TestInboundHandler_AutoDetectsReviewMode(t *testing.T) {
	db := &mockStore{
		senders: map[string]*models.AllowedSender{
			"sms:+15551234567": {
				ChannelType: "sms",
				Address:     "+15551234567",

				Enabled: true,
			},
		},
	}
	handler := InboundHandler(db, newTestConfig(), NoopMessenger{}, nil)

	w := postForm(handler, url.Values{
		"From": {"+15551234567"},
		"Body": {"Review https://github.com/backflow-labs/backflow/pull/115"},
	})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(db.tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(db.tasks))
	}
	task := db.tasks[0]
	// Handler no longer auto-detects review mode; everything is "auto"
	if task.TaskMode != models.TaskModeAuto {
		t.Errorf("task_mode = %q, want %q", task.TaskMode, models.TaskModeAuto)
	}
	if task.Prompt != "Review https://github.com/backflow-labs/backflow/pull/115" {
		t.Errorf("prompt = %q, want raw body", task.Prompt)
	}
}

func TestInboundHandler_ReviewModePRURLOnly(t *testing.T) {
	db := &mockStore{
		senders: map[string]*models.AllowedSender{
			"sms:+15551234567": {
				ChannelType: "sms",
				Address:     "+15551234567",

				Enabled: true,
			},
		},
	}
	handler := InboundHandler(db, newTestConfig(), NoopMessenger{}, nil)

	// SMS body is just a PR URL — handler forwards it as-is, no review detection.
	w := postForm(handler, url.Values{
		"From": {"+15551234567"},
		"Body": {"https://github.com/backflow-labs/backflow/pull/115"},
	})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(db.tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(db.tasks))
	}
	task := db.tasks[0]
	if task.TaskMode != models.TaskModeAuto {
		t.Errorf("task_mode = %q, want %q", task.TaskMode, models.TaskModeAuto)
	}
	if task.Prompt != "https://github.com/backflow-labs/backflow/pull/115" {
		t.Errorf("prompt = %q, want raw body", task.Prompt)
	}
}

func TestInboundHandler_TaskDefaults(t *testing.T) {
	db := &mockStore{
		senders: map[string]*models.AllowedSender{
			"sms:+15551234567": {
				ChannelType: "sms",
				Address:     "+15551234567",

				Enabled: true,
			},
		},
	}
	handler := InboundHandler(db, newTestConfig(), NoopMessenger{}, nil)

	w := postForm(handler, url.Values{
		"From": {"+15551234567"},
		"Body": {"Fix the flaky test"},
	})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(db.tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(db.tasks))
	}
	task := db.tasks[0]

	// Verify defaults applied from config
	if task.Harness != "claude_code" && task.Harness != "codex" {
		t.Errorf("Harness = %q, want claude_code or codex", task.Harness)
	}
	if task.Model == "" {
		t.Error("Model is empty, want non-empty default")
	}
	if task.Effort != "medium" {
		t.Errorf("Effort = %q, want %q", task.Effort, "medium")
	}
	if !task.CreatePR {
		t.Error("CreatePR = false, want true")
	}
	if task.SelfReview {
		t.Error("SelfReview = true, want false")
	}
	if !task.SaveAgentOutput {
		t.Error("SaveAgentOutput = false, want true")
	}
}

// --- Twilio signature validation tests ---

// signRequest computes a valid X-Twilio-Signature for the given URL and params.
func signRequest(authToken, reqURL string, params url.Values) string {
	s := reqURL
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		s += k + params.Get(k)
	}
	mac := hmac.New(sha1.New, []byte(authToken))
	mac.Write([]byte(s))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func TestValidateTwilioSignature(t *testing.T) {
	token := "test-auth-token"
	reqURL := "https://example.com/webhooks/sms/inbound"
	params := url.Values{"From": {"+15551234567"}, "Body": {"Fix the test"}}

	validSig := signRequest(token, reqURL, params)

	if !validateTwilioSignature(token, reqURL, validSig, params) {
		t.Fatal("expected valid signature to pass")
	}
	if validateTwilioSignature(token, reqURL, "invalidsig", params) {
		t.Fatal("expected invalid signature to fail")
	}
	if validateTwilioSignature(token, reqURL, "", params) {
		t.Fatal("expected empty signature to fail")
	}
	if validateTwilioSignature("wrong-token", reqURL, validSig, params) {
		t.Fatal("expected wrong token to fail")
	}
}

func TestInboundHandler_RejectsInvalidSignature(t *testing.T) {
	db := &mockStore{
		senders: map[string]*models.AllowedSender{
			"sms:+15551234567": {
				ChannelType: "sms",
				Address:     "+15551234567",

				Enabled: true,
			},
		},
	}
	handler := InboundHandler(db, newTestConfig(), NoopMessenger{}, nil)

	// Request with no signature header — must be rejected
	w := postUnsignedForm(handler, url.Values{
		"From": {"+15551234567"},
		"Body": {"Fix the test"},
	})

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
	if len(db.tasks) != 0 {
		t.Fatal("expected no tasks created for unsigned request")
	}
}

func TestInboundHandler_AcceptsValidSignature(t *testing.T) {
	db := &mockStore{
		senders: map[string]*models.AllowedSender{
			"sms:+15551234567": {
				ChannelType: "sms",
				Address:     "+15551234567",

				Enabled: true,
			},
		},
	}
	handler := InboundHandler(db, newTestConfig(), NoopMessenger{}, nil)

	// postForm signs automatically — should succeed
	w := postForm(handler, url.Values{
		"From": {"+15551234567"},
		"Body": {"Fix the test"},
	})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(db.tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(db.tasks))
	}
}

// TestParseReadCommand pins the contract of parseReadCommand directly, not
// through the handler. The return value is (url, isReadCommand, err):
//   - isReadCommand=false means "not a read command" — caller should treat the
//     body as a regular code/review prompt. err is always nil in this branch.
//   - isReadCommand=true, err!=nil means "read command, but malformed" — caller
//     should surface the error to the user.
//   - isReadCommand=true, err=nil means "valid read command" — use url.
func TestParseReadCommand(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		wantURL      string
		wantIsRead   bool
		wantErr      bool
		errSubstring string // optional: substring expected in the error message
		urlMustEqual bool   // when true, wantURL must match exactly (default true for valid cases)
		notRelevant  bool   // when true, wantURL is ignored (used for !isRead and err cases)
	}{
		// ─── Not a read command ─────────────────────────────────────
		{
			name:        "empty body",
			body:        "",
			wantIsRead:  false,
			notRelevant: true,
		},
		{
			name:        "whitespace-only body",
			body:        "   \t  ",
			wantIsRead:  false,
			notRelevant: true,
		},
		{
			name:        "plain code prompt",
			body:        "Fix the login bug",
			wantIsRead:  false,
			notRelevant: true,
		},
		{
			name:        "URL without Read prefix",
			body:        "https://example.com/article",
			wantIsRead:  false,
			notRelevant: true,
		},
		{
			name:        "Read not the first word",
			body:        "Please Read https://example.com",
			wantIsRead:  false,
			notRelevant: true,
		},
		{
			name:        "all-caps READ not accepted",
			body:        "READ https://example.com",
			wantIsRead:  false,
			notRelevant: true,
		},
		{
			name:        "PascalCase ReadMe not accepted",
			body:        "ReadMe https://example.com",
			wantIsRead:  false,
			notRelevant: true,
		},

		// ─── Read command, valid URL ───────────────────────────────
		{
			name:         "capital Read with plain URL",
			body:         "Read https://example.com",
			wantURL:      "https://example.com",
			wantIsRead:   true,
			urlMustEqual: true,
		},
		{
			name:         "lowercase read with plain URL",
			body:         "read https://example.com",
			wantURL:      "https://example.com",
			wantIsRead:   true,
			urlMustEqual: true,
		},
		{
			name:         "URL with path",
			body:         "Read https://example.com/articles/2026/go-generics",
			wantURL:      "https://example.com/articles/2026/go-generics",
			wantIsRead:   true,
			urlMustEqual: true,
		},
		{
			name:         "URL with single query param",
			body:         "Read https://example.com/a?b=c",
			wantURL:      "https://example.com/a?b=c",
			wantIsRead:   true,
			urlMustEqual: true,
		},
		{
			name:         "URL with multiple query params",
			body:         "Read https://example.com/search?q=golang&page=2&sort=date",
			wantURL:      "https://example.com/search?q=golang&page=2&sort=date",
			wantIsRead:   true,
			urlMustEqual: true,
		},
		{
			name:         "URL with fragment",
			body:         "Read https://example.com/doc#section-3",
			wantURL:      "https://example.com/doc#section-3",
			wantIsRead:   true,
			urlMustEqual: true,
		},
		{
			name:         "URL with query and fragment",
			body:         "Read https://example.com/a/b?x=1&y=2#frag",
			wantURL:      "https://example.com/a/b?x=1&y=2#frag",
			wantIsRead:   true,
			urlMustEqual: true,
		},
		{
			name:         "commentary before URL is ignored",
			body:         "Read this article https://example.com/post",
			wantURL:      "https://example.com/post",
			wantIsRead:   true,
			urlMustEqual: true,
		},
		{
			name:         "commentary after URL is ignored (whitespace-separated)",
			body:         "Read https://example.com/post please",
			wantURL:      "https://example.com/post",
			wantIsRead:   true,
			urlMustEqual: true,
		},
		{
			name:         "multiple URLs — only the first is used",
			body:         "Read https://first.example.com https://second.example.com",
			wantURL:      "https://first.example.com",
			wantIsRead:   true,
			urlMustEqual: true,
		},
		{
			name:         "leading/trailing whitespace in body",
			body:         "   Read   https://example.com/post   ",
			wantURL:      "https://example.com/post",
			wantIsRead:   true,
			urlMustEqual: true,
		},
		{
			name:         "CRLF-separated commentary after URL",
			body:         "Read https://example.com\r\n-- sent from my phone",
			wantURL:      "https://example.com",
			wantIsRead:   true,
			urlMustEqual: true,
		},

		// ─── Read command, trailing-punctuation gotchas ─────────────
		// These pin current behavior: the URL extractor uses \S+, so trailing
		// punctuation glued to the URL (no space) gets captured. ValidateReadURL
		// then accepts it because url.Parse treats trailing '.' / ')' as part
		// of the path. This is a known limitation; fixing it belongs in
		// ValidateReadURL or the regex, not here.
		{
			name:         "trailing period is captured into URL (known limitation)",
			body:         "Read https://example.com/article.",
			wantURL:      "https://example.com/article.",
			wantIsRead:   true,
			urlMustEqual: true,
		},
		{
			// The regex `https?://\S+` starts matching at 'h', so the leading
			// '(' is not captured. The trailing ')' is \S and IS captured.
			name:         "trailing paren is captured into URL (known limitation)",
			body:         "Read (https://example.com/post)",
			wantURL:      "https://example.com/post)",
			wantIsRead:   true,
			urlMustEqual: true,
		},

		// ─── Read command, missing or invalid URL ──────────────────
		{
			name:         "Read with no URL",
			body:         "Read this article",
			wantIsRead:   true,
			wantErr:      true,
			errSubstring: "https URL",
			notRelevant:  true,
		},
		{
			name:         "Read alone",
			body:         "Read",
			wantIsRead:   true,
			wantErr:      true,
			errSubstring: "https URL",
			notRelevant:  true,
		},
		{
			name:         "Read with trailing spaces only",
			body:         "Read   ",
			wantIsRead:   true,
			wantErr:      true,
			errSubstring: "https URL",
			notRelevant:  true,
		},
		{
			name:         "Read with ftp URL (not matched by regex)",
			body:         "Read ftp://example.com/file",
			wantIsRead:   true,
			wantErr:      true,
			errSubstring: "https URL",
			notRelevant:  true,
		},
		{
			name:         "Read with http URL — matched but rejected by ValidateReadURL",
			body:         "Read http://example.com",
			wantIsRead:   true,
			wantErr:      true,
			errSubstring: "https",
			notRelevant:  true,
		},
		{
			name:        "Read with scheme but no host",
			body:        "Read https://",
			wantIsRead:  true,
			wantErr:     true,
			notRelevant: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotURL, gotIsRead, gotErr := parseReadCommand(tc.body)

			if gotIsRead != tc.wantIsRead {
				t.Errorf("isReadCommand = %v, want %v (body=%q)", gotIsRead, tc.wantIsRead, tc.body)
			}

			if tc.wantErr {
				if gotErr == nil {
					t.Fatalf("expected error, got nil (body=%q)", tc.body)
				}
				if tc.errSubstring != "" && !strings.Contains(gotErr.Error(), tc.errSubstring) {
					t.Errorf("error = %q, want substring %q", gotErr.Error(), tc.errSubstring)
				}
				return
			}

			if gotErr != nil {
				t.Fatalf("unexpected error: %v (body=%q)", gotErr, tc.body)
			}

			if tc.notRelevant {
				return
			}

			if tc.urlMustEqual && gotURL != tc.wantURL {
				t.Errorf("url = %q, want %q (body=%q)", gotURL, tc.wantURL, tc.body)
			}
		})
	}
}
