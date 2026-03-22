package discord

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/backflow-labs/backflow/internal/models"
	"github.com/backflow-labs/backflow/internal/store"
)

type fakeTaskStore struct {
	tasks map[string]*models.Task
	list  []*models.Task
}

func (f *fakeTaskStore) GetTask(ctx context.Context, id string) (*models.Task, error) {
	if task, ok := f.tasks[id]; ok {
		return task, nil
	}
	return nil, store.ErrNotFound
}

func (f *fakeTaskStore) ListTasks(ctx context.Context, filter store.TaskFilter) ([]*models.Task, error) {
	out := make([]*models.Task, 0, len(f.list))
	for _, task := range f.list {
		if filter.Status != nil && task.Status != *filter.Status {
			continue
		}
		out = append(out, task)
	}
	start := filter.Offset
	if start > len(out) {
		start = len(out)
	}
	end := len(out)
	if filter.Limit > 0 && start+filter.Limit < end {
		end = start + filter.Limit
	}
	return out[start:end], nil
}

func testKeyPair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return pub, priv
}

func signRequest(priv ed25519.PrivateKey, timestamp, body string) string {
	msg := []byte(timestamp + body)
	sig := ed25519.Sign(priv, msg)
	return hex.EncodeToString(sig)
}

func postInteraction(handler http.HandlerFunc, priv ed25519.PrivateKey, body string) *httptest.ResponseRecorder {
	timestamp := "1234567890"
	sig := signRequest(priv, timestamp, body)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/discord", strings.NewReader(body))
	req.Header.Set("X-Signature-Ed25519", sig)
	req.Header.Set("X-Signature-Timestamp", timestamp)
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func TestInteractionHandler_Ping(t *testing.T) {
	pub, priv := testKeyPair(t)
	handler := InteractionHandler(pub, nil)

	rr := postInteraction(handler, priv, `{"type":1}`)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	var resp InteractionResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Type != ResponseTypePong {
		t.Errorf("response type = %d, want %d (PONG)", resp.Type, ResponseTypePong)
	}
}

func TestInteractionHandler_InvalidSignature(t *testing.T) {
	pub, _ := testKeyPair(t)
	handler := InteractionHandler(pub, nil)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/discord", strings.NewReader(`{"type":1}`))
	req.Header.Set("X-Signature-Ed25519", "deadbeef")
	req.Header.Set("X-Signature-Timestamp", "1234567890")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestInteractionHandler_MissingHeaders(t *testing.T) {
	pub, _ := testKeyPair(t)
	handler := InteractionHandler(pub, nil)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/discord", strings.NewReader(`{"type":1}`))
	// No signature headers

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestInteractionHandler_BackflowRootCommand(t *testing.T) {
	pub, priv := testKeyPair(t)
	handler := InteractionHandler(pub, nil)

	rr := postInteraction(handler, priv, `{"type":2,"data":{"name":"backflow"}}`)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	var resp ChannelMessageResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Type != ResponseTypeChannelMessage {
		t.Errorf("response type = %d, want %d", resp.Type, ResponseTypeChannelMessage)
	}
	if !strings.Contains(resp.Data.Content, "Use /backflow status or /backflow list") {
		t.Errorf("content = %q, want usage guidance", resp.Data.Content)
	}
}

func TestInteractionHandler_BackflowStatusCommand(t *testing.T) {
	pub, priv := testKeyPair(t)
	store := &fakeTaskStore{
		tasks: map[string]*models.Task{
			"bf_123": {
				ID:          "bf_123",
				Status:      models.TaskStatusRunning,
				RepoURL:     "https://github.com/test/repo",
				PRURL:       "https://github.com/test/repo/pull/42",
				StartedAt:   ptrTime(time.Date(2026, 3, 22, 12, 0, 0, 0, time.UTC)),
				CreatedAt:   time.Now().UTC(),
				UpdatedAt:   time.Now().UTC(),
			},
		},
	}
	handler := InteractionHandler(pub, store)

	body := `{"type":2,"data":{"name":"backflow","options":[{"name":"status","type":1,"options":[{"name":"task_id","type":3,"value":"bf_123"}]}]}}`
	rr := postInteraction(handler, priv, body)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	var resp ChannelMessageResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.Contains(resp.Data.Content, "Task bf_123 is running.") {
		t.Fatalf("content = %q, want task status", resp.Data.Content)
	}
	if !strings.Contains(resp.Data.Content, "https://github.com/test/repo") {
		t.Fatalf("content = %q, want repo URL", resp.Data.Content)
	}
}

func TestInteractionHandler_BackflowListCommand(t *testing.T) {
	pub, priv := testKeyPair(t)
	now := time.Now().UTC()
	store := &fakeTaskStore{
		list: []*models.Task{
			{ID: "bf_1", Status: models.TaskStatusRunning, RepoURL: "https://github.com/test/repo1", CreatedAt: now, UpdatedAt: now},
			{ID: "bf_2", Status: models.TaskStatusRunning, RepoURL: "https://github.com/test/repo2", CreatedAt: now, UpdatedAt: now},
			{ID: "bf_3", Status: models.TaskStatusCompleted, RepoURL: "https://github.com/test/repo3", CreatedAt: now, UpdatedAt: now},
		},
	}
	handler := InteractionHandler(pub, store)

	body := `{"type":2,"data":{"name":"backflow","options":[{"name":"list","type":1,"options":[{"name":"status","type":3,"value":"running"},{"name":"limit","type":4,"value":2},{"name":"offset","type":4,"value":0}]}]}}`
	rr := postInteraction(handler, priv, body)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	var resp ChannelMessageResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.Contains(resp.Data.Content, "Tasks (2 shown") {
		t.Fatalf("content = %q, want task list header", resp.Data.Content)
	}
	if !strings.Contains(resp.Data.Content, "status running") {
		t.Fatalf("content = %q, want status filter in header", resp.Data.Content)
	}
	if !strings.Contains(resp.Data.Content, "bf_1") || !strings.Contains(resp.Data.Content, "bf_2") {
		t.Fatalf("content = %q, want listed task IDs", resp.Data.Content)
	}
	if strings.Contains(resp.Data.Content, "bf_3") {
		t.Fatalf("content = %q, want status filter to exclude bf_3", resp.Data.Content)
	}
}

func TestInteractionHandler_UnknownCommand(t *testing.T) {
	pub, priv := testKeyPair(t)
	handler := InteractionHandler(pub, nil)

	rr := postInteraction(handler, priv, `{"type":2,"data":{"name":"unknown"}}`)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	var resp ChannelMessageResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Type != ResponseTypeChannelMessage {
		t.Errorf("response type = %d, want %d", resp.Type, ResponseTypeChannelMessage)
	}
	if !strings.Contains(resp.Data.Content, "Unknown command") {
		t.Errorf("expected unknown command message, got %q", resp.Data.Content)
	}
}

func TestRegisterCommands(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	var gotBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"id":"123","name":"backflow"}]`))
	}))
	defer server.Close()

	err := RegisterCommands(server.URL, "app-123", "token-abc")
	if err != nil {
		t.Fatalf("RegisterCommands: %v", err)
	}

	if gotMethod != http.MethodPut {
		t.Errorf("method = %q, want PUT", gotMethod)
	}
	wantPath := "/applications/app-123/commands"
	if gotPath != wantPath {
		t.Errorf("path = %q, want %q", gotPath, wantPath)
	}
	if gotAuth != "Bot token-abc" {
		t.Errorf("auth = %q, want %q", gotAuth, "Bot token-abc")
	}

	var commands []slashCommand
	if err := json.Unmarshal(gotBody, &commands); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if len(commands) == 0 {
		t.Fatal("expected at least one command in body")
	}
	if commands[0].Name != "backflow" {
		t.Errorf("command name = %q, want %q", commands[0].Name, "backflow")
	}
	if len(commands[0].Options) != 2 {
		t.Fatalf("options = %v, want 2 subcommands", commands[0].Options)
	}
	if commands[0].Options[0].Name != "status" || commands[0].Options[1].Name != "list" {
		t.Fatalf("subcommands = %v, want status and list", commands[0].Options)
	}
}

func TestInteractionHandler_UnknownType(t *testing.T) {
	pub, priv := testKeyPair(t)
	handler := InteractionHandler(pub, nil)

	rr := postInteraction(handler, priv, `{"type":99}`)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func ptrTime(t time.Time) *time.Time { return &t }

func TestParsePublicKey_Valid(t *testing.T) {
	pub, _ := testKeyPair(t)
	hexKey := hex.EncodeToString(pub)

	parsed, err := ParsePublicKey(hexKey)
	if err != nil {
		t.Fatalf("ParsePublicKey: %v", err)
	}
	if !pub.Equal(parsed) {
		t.Error("parsed key does not match original")
	}
}

func TestParsePublicKey_InvalidHex(t *testing.T) {
	_, err := ParsePublicKey("not-hex!")
	if err == nil {
		t.Fatal("expected error for invalid hex")
	}
}

func TestParsePublicKey_WrongLength(t *testing.T) {
	_, err := ParsePublicKey("abcdef")
	if err == nil {
		t.Fatal("expected error for wrong key length")
	}
}
