package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/backflow-labs/backflow/internal/config"
	"github.com/backflow-labs/backflow/internal/store"
)

type fakeRunningCounter int

func (f fakeRunningCounter) RunningCount() int { return int(f) }

type fakePoolProvider struct {
	stats store.PoolStats
}

func (f fakePoolProvider) DebugPoolStats() store.PoolStats { return f.stats }

func TestDebugStatsSourceSnapshot(t *testing.T) {
	startedAt := time.Now().Add(-90 * time.Second).UTC()
	source := NewDebugStatsSource(startedAt, fakeRunningCounter(7), fakePoolProvider{
		stats: store.PoolStats{
			Acquired:       2,
			Idle:           3,
			Total:          5,
			MaxConnections: 8,
		},
	})

	stats := source.Snapshot()

	if stats.OrchestratorRunning != 7 {
		t.Fatalf("running = %d, want 7", stats.OrchestratorRunning)
	}
	if stats.PGXPool.Acquired != 2 || stats.PGXPool.Idle != 3 || stats.PGXPool.Total != 5 || stats.PGXPool.MaxConnections != 8 {
		t.Fatalf("pool stats = %+v, want %+v", stats.PGXPool, DebugPoolStats{Acquired: 2, Idle: 3, Total: 5, MaxConnections: 8})
	}
	if stats.ProcessUptimeSec < 90 {
		t.Fatalf("uptime = %d, want >= 90", stats.ProcessUptimeSec)
	}
	if stats.Memory.HeapAlloc == 0 || stats.Memory.Sys == 0 {
		t.Fatalf("memory stats should be non-zero, got %+v", stats.Memory)
	}
}

func TestDebugStatsEndpoint(t *testing.T) {
	stats := DebugStats{
		OrchestratorRunning: 4,
		PGXPool: DebugPoolStats{
			Acquired:       1,
			Idle:           2,
			Total:          3,
			MaxConnections: 6,
		},
		ProcessUptimeSec: 123,
		Memory: DebugMemoryStats{
			HeapAlloc: 1024,
			Sys:       2048,
		},
	}

	router := NewServer(
		&mockStore{},
		&config.Config{AuthMode: config.AuthModeAPIKey},
		noopLogFetcher{},
		noopEmitter{},
		staticDebugStatsProvider{stats: stats},
	)

	req := httptest.NewRequest(http.MethodGet, "/debug/stats", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	checkResponse(t, req, rec)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var env struct {
		Data DebugStats `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&env); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if env.Data != stats {
		t.Fatalf("response = %+v, want %+v", env.Data, stats)
	}

	body := rec.Body.Bytes()
	for _, forbidden := range []string{"database_url", "BACKFLOW_", "webhook", "secret", "token"} {
		if bytes.Contains(bytes.ToLower(body), []byte(strings.ToLower(forbidden))) {
			t.Fatalf("debug stats response unexpectedly contained %q: %s", forbidden, body)
		}
	}
}
