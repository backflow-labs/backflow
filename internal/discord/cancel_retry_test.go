package discord

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/backflow-labs/backflow/internal/models"
)

// helpers for building tasks in specific states

func runningTask(id string) *models.Task {
	now := time.Now().UTC()
	return &models.Task{
		ID:        id,
		Status:    models.TaskStatusRunning,
		RepoURL:   "https://github.com/test/repo",
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func failedTask(id string) *models.Task {
	now := time.Now().UTC()
	return &models.Task{
		ID:        id,
		Status:    models.TaskStatusFailed,
		RepoURL:   "https://github.com/test/repo",
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// memberBody injects a "member" field with the given roles into an interaction JSON string.
func memberBody(interactionJSON string, roles ...string) string {
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(interactionJSON), &m); err != nil {
		panic(err)
	}
	roleSlice := make([]interface{}, len(roles))
	for i, r := range roles {
		roleSlice[i] = r
	}
	m["member"] = map[string]interface{}{
		"roles": roleSlice,
	}
	b, _ := json.Marshal(m)
	return string(b)
}

// --- /backflow cancel command ---

func TestCancelCommand_Authorized(t *testing.T) {
	pub, priv := testKeyPair(t)
	s := &fakeTaskStore{
		tasks: map[string]*models.Task{"bf_run1": runningTask("bf_run1")},
	}
	var cancelled []string
	cancelFn := CancelTaskFunc(func(id string) error {
		cancelled = append(cancelled, id)
		return nil
	})
	handler := InteractionHandler(pub, s, HandlerActions{CancelTask: cancelFn})

	body := `{"type":2,"data":{"name":"backflow","options":[{"name":"cancel","type":1,"options":[{"name":"task_id","type":3,"value":"bf_run1"}]}]}}`
	rr := postInteraction(handler, priv, body)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	var resp ChannelMessageResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.Contains(resp.Data.Content, "bf_run1") {
		t.Errorf("content = %q, want task ID in response", resp.Data.Content)
	}
	if resp.Data.Flags != FlagEphemeral {
		t.Errorf("flags = %d, want %d (ephemeral)", resp.Data.Flags, FlagEphemeral)
	}
	if len(cancelled) != 1 || cancelled[0] != "bf_run1" {
		t.Errorf("cancelled = %v, want [bf_run1]", cancelled)
	}
}

func TestCancelCommand_Unauthorized(t *testing.T) {
	pub, priv := testKeyPair(t)
	s := &fakeTaskStore{
		tasks: map[string]*models.Task{"bf_run1": runningTask("bf_run1")},
	}
	var cancelled []string
	cancelFn := CancelTaskFunc(func(id string) error {
		cancelled = append(cancelled, id)
		return nil
	})
	handler := InteractionHandler(pub, s, HandlerActions{
		CancelTask:   cancelFn,
		AllowedRoles: []string{"admin-role"},
	})

	body := memberBody(
		`{"type":2,"data":{"name":"backflow","options":[{"name":"cancel","type":1,"options":[{"name":"task_id","type":3,"value":"bf_run1"}]}]}}`,
		"viewer-role",
	)
	rr := postInteraction(handler, priv, body)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	var resp ChannelMessageResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.Contains(strings.ToLower(resp.Data.Content), "permission") {
		t.Errorf("content = %q, want permission denied message", resp.Data.Content)
	}
	if resp.Data.Flags != FlagEphemeral {
		t.Errorf("flags = %d, want %d (ephemeral)", resp.Data.Flags, FlagEphemeral)
	}
	if len(cancelled) != 0 {
		t.Errorf("cancel should not have been called, got %v", cancelled)
	}
}

func TestCancelCommand_MissingTaskID(t *testing.T) {
	pub, priv := testKeyPair(t)
	cancelFn := CancelTaskFunc(func(id string) error { return nil })
	handler := InteractionHandler(pub, nil, HandlerActions{CancelTask: cancelFn})

	body := `{"type":2,"data":{"name":"backflow","options":[{"name":"cancel","type":1,"options":[]}]}}`
	rr := postInteraction(handler, priv, body)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	var resp ChannelMessageResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.Contains(resp.Data.Content, "task_id") {
		t.Errorf("content = %q, want task_id error", resp.Data.Content)
	}
}

func TestCancelCommand_CancelFails(t *testing.T) {
	pub, priv := testKeyPair(t)
	s := &fakeTaskStore{
		tasks: map[string]*models.Task{"bf_run1": runningTask("bf_run1")},
	}
	cancelFn := CancelTaskFunc(func(id string) error {
		return errCancelNotAllowed
	})
	handler := InteractionHandler(pub, s, HandlerActions{CancelTask: cancelFn})

	body := `{"type":2,"data":{"name":"backflow","options":[{"name":"cancel","type":1,"options":[{"name":"task_id","type":3,"value":"bf_run1"}]}]}}`
	rr := postInteraction(handler, priv, body)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	var resp ChannelMessageResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.Contains(resp.Data.Content, "Failed to cancel") {
		t.Errorf("content = %q, want failure message", resp.Data.Content)
	}
}

func TestCancelCommand_NilCancelFunc(t *testing.T) {
	pub, priv := testKeyPair(t)
	handler := InteractionHandler(pub, nil, HandlerActions{})

	body := `{"type":2,"data":{"name":"backflow","options":[{"name":"cancel","type":1,"options":[{"name":"task_id","type":3,"value":"bf_run1"}]}]}}`
	rr := postInteraction(handler, priv, body)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	var resp ChannelMessageResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.Contains(resp.Data.Content, "unavailable") {
		t.Errorf("content = %q, want unavailable message", resp.Data.Content)
	}
}

// --- /backflow retry command ---

func TestRetryCommand_Authorized(t *testing.T) {
	pub, priv := testKeyPair(t)
	s := &fakeTaskStore{
		tasks: map[string]*models.Task{"bf_fail1": failedTask("bf_fail1")},
	}
	var retried []string
	retryFn := RetryTaskFunc(func(id string) error {
		retried = append(retried, id)
		return nil
	})
	handler := InteractionHandler(pub, s, HandlerActions{RetryTask: retryFn})

	body := `{"type":2,"data":{"name":"backflow","options":[{"name":"retry","type":1,"options":[{"name":"task_id","type":3,"value":"bf_fail1"}]}]}}`
	rr := postInteraction(handler, priv, body)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	var resp ChannelMessageResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.Contains(resp.Data.Content, "bf_fail1") {
		t.Errorf("content = %q, want task ID in response", resp.Data.Content)
	}
	if resp.Data.Flags != FlagEphemeral {
		t.Errorf("flags = %d, want %d (ephemeral)", resp.Data.Flags, FlagEphemeral)
	}
	if len(retried) != 1 || retried[0] != "bf_fail1" {
		t.Errorf("retried = %v, want [bf_fail1]", retried)
	}
}

func TestRetryCommand_Unauthorized(t *testing.T) {
	pub, priv := testKeyPair(t)
	s := &fakeTaskStore{
		tasks: map[string]*models.Task{"bf_fail1": failedTask("bf_fail1")},
	}
	var retried []string
	retryFn := RetryTaskFunc(func(id string) error {
		retried = append(retried, id)
		return nil
	})
	handler := InteractionHandler(pub, s, HandlerActions{
		RetryTask:    retryFn,
		AllowedRoles: []string{"admin-role"},
	})

	body := memberBody(
		`{"type":2,"data":{"name":"backflow","options":[{"name":"retry","type":1,"options":[{"name":"task_id","type":3,"value":"bf_fail1"}]}]}}`,
		"viewer-role",
	)
	rr := postInteraction(handler, priv, body)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	var resp ChannelMessageResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.Contains(strings.ToLower(resp.Data.Content), "permission") {
		t.Errorf("content = %q, want permission denied message", resp.Data.Content)
	}
	if resp.Data.Flags != FlagEphemeral {
		t.Errorf("flags = %d, want %d (ephemeral)", resp.Data.Flags, FlagEphemeral)
	}
	if len(retried) != 0 {
		t.Errorf("retry should not have been called, got %v", retried)
	}
}

func TestRetryCommand_RetryFails(t *testing.T) {
	pub, priv := testKeyPair(t)
	s := &fakeTaskStore{
		tasks: map[string]*models.Task{"bf_fail1": failedTask("bf_fail1")},
	}
	retryFn := RetryTaskFunc(func(id string) error {
		return errRetryNotAllowed
	})
	handler := InteractionHandler(pub, s, HandlerActions{RetryTask: retryFn})

	body := `{"type":2,"data":{"name":"backflow","options":[{"name":"retry","type":1,"options":[{"name":"task_id","type":3,"value":"bf_fail1"}]}]}}`
	rr := postInteraction(handler, priv, body)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	var resp ChannelMessageResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.Contains(resp.Data.Content, "Failed to retry") {
		t.Errorf("content = %q, want failure message", resp.Data.Content)
	}
}

func TestRetryCommand_NilRetryFunc(t *testing.T) {
	pub, priv := testKeyPair(t)
	handler := InteractionHandler(pub, nil, HandlerActions{})

	body := `{"type":2,"data":{"name":"backflow","options":[{"name":"retry","type":1,"options":[{"name":"task_id","type":3,"value":"bf_fail1"}]}]}}`
	rr := postInteraction(handler, priv, body)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	var resp ChannelMessageResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.Contains(resp.Data.Content, "unavailable") {
		t.Errorf("content = %q, want unavailable message", resp.Data.Content)
	}
}

// --- Button (message component) interactions ---

func TestButtonCancel_Authorized(t *testing.T) {
	pub, priv := testKeyPair(t)
	var cancelled []string
	cancelFn := CancelTaskFunc(func(id string) error {
		cancelled = append(cancelled, id)
		return nil
	})
	handler := InteractionHandler(pub, nil, HandlerActions{CancelTask: cancelFn})

	body := `{"type":3,"data":{"custom_id":"bf_cancel:bf_run1","component_type":2}}`
	rr := postInteraction(handler, priv, body)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	var resp ChannelMessageResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.Contains(resp.Data.Content, "bf_run1") {
		t.Errorf("content = %q, want task ID", resp.Data.Content)
	}
	if resp.Data.Flags != FlagEphemeral {
		t.Errorf("flags = %d, want %d (ephemeral)", resp.Data.Flags, FlagEphemeral)
	}
	if len(cancelled) != 1 || cancelled[0] != "bf_run1" {
		t.Errorf("cancelled = %v, want [bf_run1]", cancelled)
	}
}

func TestButtonRetry_Authorized(t *testing.T) {
	pub, priv := testKeyPair(t)
	var retried []string
	retryFn := RetryTaskFunc(func(id string) error {
		retried = append(retried, id)
		return nil
	})
	handler := InteractionHandler(pub, nil, HandlerActions{RetryTask: retryFn})

	body := `{"type":3,"data":{"custom_id":"bf_retry:bf_fail1","component_type":2}}`
	rr := postInteraction(handler, priv, body)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	var resp ChannelMessageResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.Contains(resp.Data.Content, "bf_fail1") {
		t.Errorf("content = %q, want task ID", resp.Data.Content)
	}
	if resp.Data.Flags != FlagEphemeral {
		t.Errorf("flags = %d, want %d (ephemeral)", resp.Data.Flags, FlagEphemeral)
	}
	if len(retried) != 1 || retried[0] != "bf_fail1" {
		t.Errorf("retried = %v, want [bf_fail1]", retried)
	}
}

func TestButtonAction_Unauthorized(t *testing.T) {
	pub, priv := testKeyPair(t)
	var cancelled []string
	cancelFn := CancelTaskFunc(func(id string) error {
		cancelled = append(cancelled, id)
		return nil
	})
	handler := InteractionHandler(pub, nil, HandlerActions{
		CancelTask:   cancelFn,
		AllowedRoles: []string{"admin-role"},
	})

	body := memberBody(
		`{"type":3,"data":{"custom_id":"bf_cancel:bf_run1","component_type":2}}`,
		"viewer-role",
	)
	rr := postInteraction(handler, priv, body)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	var resp ChannelMessageResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.Contains(strings.ToLower(resp.Data.Content), "permission") {
		t.Errorf("content = %q, want permission denied message", resp.Data.Content)
	}
	if resp.Data.Flags != FlagEphemeral {
		t.Errorf("flags = %d, want %d (ephemeral)", resp.Data.Flags, FlagEphemeral)
	}
	if len(cancelled) != 0 {
		t.Errorf("cancel should not have been called, got %v", cancelled)
	}
}

func TestButtonAction_AllowedRole(t *testing.T) {
	pub, priv := testKeyPair(t)
	var cancelled []string
	cancelFn := CancelTaskFunc(func(id string) error {
		cancelled = append(cancelled, id)
		return nil
	})
	handler := InteractionHandler(pub, nil, HandlerActions{
		CancelTask:   cancelFn,
		AllowedRoles: []string{"admin-role", "ops-role"},
	})

	// User has one of the allowed roles
	body := memberBody(
		`{"type":3,"data":{"custom_id":"bf_cancel:bf_run1","component_type":2}}`,
		"viewer-role", "ops-role",
	)
	rr := postInteraction(handler, priv, body)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if len(cancelled) != 1 {
		t.Errorf("expected cancel to be called once, got %v", cancelled)
	}
}

func TestButtonCancel_ActionFails(t *testing.T) {
	pub, priv := testKeyPair(t)
	cancelFn := CancelTaskFunc(func(id string) error {
		return errCancelNotAllowed
	})
	handler := InteractionHandler(pub, nil, HandlerActions{CancelTask: cancelFn})

	body := `{"type":3,"data":{"custom_id":"bf_cancel:bf_run1","component_type":2}}`
	rr := postInteraction(handler, priv, body)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	var resp ChannelMessageResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.Contains(resp.Data.Content, "Failed to cancel") {
		t.Errorf("content = %q, want failure message", resp.Data.Content)
	}
	if resp.Data.Flags != FlagEphemeral {
		t.Errorf("flags = %d, want %d (ephemeral)", resp.Data.Flags, FlagEphemeral)
	}
}

func TestButtonUnknownCustomID(t *testing.T) {
	pub, priv := testKeyPair(t)
	handler := InteractionHandler(pub, nil, HandlerActions{})

	body := `{"type":3,"data":{"custom_id":"unknown_action","component_type":2}}`
	rr := postInteraction(handler, priv, body)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}

// --- RegisterCommands includes cancel and retry ---

func TestRegisterCommands_IncludesCancelAndRetry(t *testing.T) {
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"id":"123","name":"backflow"}]`))
	}))
	defer server.Close()

	if err := RegisterCommands(server.URL, "app-123", "token-abc"); err != nil {
		t.Fatalf("RegisterCommands: %v", err)
	}

	var commands []slashCommand
	if err := json.Unmarshal(gotBody, &commands); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}

	opts := commands[0].Options
	hasCancel, hasRetry := false, false
	for _, opt := range opts {
		switch opt.Name {
		case "cancel":
			hasCancel = true
		case "retry":
			hasRetry = true
		}
	}
	if !hasCancel {
		t.Error("commands missing 'cancel' subcommand")
	}
	if !hasRetry {
		t.Error("commands missing 'retry' subcommand")
	}
}

// sentinel errors used by tests
var (
	errCancelNotAllowed = fmt.Errorf("task is not in a cancellable state")
	errRetryNotAllowed  = fmt.Errorf("task is not in a retryable state")
)
