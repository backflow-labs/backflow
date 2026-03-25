package api

import (
	"runtime"
	"time"

	"github.com/backflow-labs/backflow/internal/store"
)

// RunningCounter exposes the orchestrator's in-memory running task count.
type RunningCounter interface {
	RunningCount() int
}

// PoolStatsProvider exposes pgxpool statistics without leaking the concrete store.
type PoolStatsProvider interface {
	DebugPoolStats() store.PoolStats
}

// DebugStatsProvider returns a snapshot of the process's operational state.
type DebugStatsProvider interface {
	Snapshot() DebugStats
}

// DebugStatsSource aggregates the sources needed by /debug/stats.
type DebugStatsSource struct {
	startedAt time.Time
	running   RunningCounter
	pool      PoolStatsProvider
}

// NewDebugStatsSource creates a provider for the debug stats endpoint.
func NewDebugStatsSource(startedAt time.Time, running RunningCounter, pool PoolStatsProvider) *DebugStatsSource {
	return &DebugStatsSource{
		startedAt: startedAt,
		running:   running,
		pool:      pool,
	}
}

// Snapshot returns a point-in-time view of operational metrics.
func (s *DebugStatsSource) Snapshot() DebugStats {
	now := time.Now().UTC()

	var running int
	if s != nil && s.running != nil {
		running = s.running.RunningCount()
	}

	var pool DebugPoolStats
	if s != nil && s.pool != nil {
		p := s.pool.DebugPoolStats()
		pool = DebugPoolStats{
			Acquired:       p.Acquired,
			Idle:           p.Idle,
			Total:          p.Total,
			MaxConnections: p.MaxConnections,
		}
	}

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	uptime := int64(0)
	if s != nil && !s.startedAt.IsZero() {
		uptime = int64(now.Sub(s.startedAt).Seconds())
		if uptime < 0 {
			uptime = 0
		}
	}

	return DebugStats{
		OrchestratorRunning: running,
		PGXPool:             pool,
		ProcessUptimeSec:    uptime,
		Memory: DebugMemoryStats{
			HeapAlloc: mem.HeapAlloc,
			Sys:       mem.Sys,
		},
	}
}

// DebugStats is the JSON payload returned by GET /debug/stats.
type DebugStats struct {
	OrchestratorRunning int              `json:"orchestrator_running"`
	PGXPool             DebugPoolStats   `json:"pgxpool"`
	ProcessUptimeSec    int64            `json:"process_uptime_sec"`
	Memory              DebugMemoryStats `json:"memory"`
}

// DebugPoolStats contains the pgxpool counters needed for soak monitoring.
type DebugPoolStats struct {
	Acquired       int32 `json:"acquired"`
	Idle           int32 `json:"idle"`
	Total          int32 `json:"total"`
	MaxConnections int32 `json:"max_connections"`
}

// DebugMemoryStats contains the runtime heap metrics needed for soak analysis.
type DebugMemoryStats struct {
	HeapAlloc uint64 `json:"heap_alloc"`
	Sys       uint64 `json:"sys"`
}
