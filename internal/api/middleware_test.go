package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/backflow-labs/backflow/internal/config"
)

func TestRestrictAPI_BlocksAllAPIEndpoints(t *testing.T) {
	cfg := &config.Config{RestrictAPI: true}
	router := NewServer(&mockStore{}, cfg, noopLogFetcher{}, noopEmitter{}, noopDebugStatsProvider{})

	paths := []struct {
		method string
		path   string
	}{
		{"GET", "/api/v1/health"},
		{"GET", "/api/v1/tasks"},
		{"POST", "/api/v1/tasks"},
		{"GET", "/api/v1/tasks/bf_test123"},
		{"DELETE", "/api/v1/tasks/bf_test123"},
		{"GET", "/api/v1/tasks/bf_test123/logs"},
	}

	for _, tc := range paths {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		if rr.Code != http.StatusForbidden {
			t.Errorf("%s %s: got status %d, want %d", tc.method, tc.path, rr.Code, http.StatusForbidden)
		}

		var resp envelope
		json.NewDecoder(rr.Body).Decode(&resp)
		if resp.Error == "" {
			t.Errorf("%s %s: expected error message in response body", tc.method, tc.path)
		}
	}
}

func TestRestrictAPI_RootHealthStillAccessible(t *testing.T) {
	cfg := &config.Config{RestrictAPI: true}
	router := NewServer(&mockStore{}, cfg, noopLogFetcher{}, noopEmitter{}, noopDebugStatsProvider{})

	req := httptest.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("GET /health: got status %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestRestrictAPI_Disabled_AllowsAPIHealth(t *testing.T) {
	cfg := &config.Config{RestrictAPI: false}
	router := NewServer(&mockStore{}, cfg, noopLogFetcher{}, noopEmitter{}, noopDebugStatsProvider{})

	req := httptest.NewRequest("GET", "/api/v1/health", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("GET /api/v1/health: got status %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestRestrictAPI_DoesNotBlockDebugStats(t *testing.T) {
	cfg := &config.Config{RestrictAPI: true}
	router := NewServer(
		&mockStore{},
		cfg,
		noopLogFetcher{},
		noopEmitter{},
		staticDebugStatsProvider{stats: DebugStats{}},
	)

	req := httptest.NewRequest("GET", "/debug/stats", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("GET /debug/stats: got status %d, want %d", rr.Code, http.StatusOK)
	}
}
