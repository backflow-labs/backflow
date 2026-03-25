package main

import (
	"testing"
	"time"

	"github.com/backflow-labs/backflow/internal/api"
)

func TestAnalyzePassesOnStableSeries(t *testing.T) {
	start := time.Now().Add(-3 * time.Minute)
	report := Report{
		StartedAt:  start,
		FinishedAt: start.Add(3 * time.Minute),
		Submitted:  6,
		Samples: []Sample{
			{
				At:                  start,
				Stats:               api.DebugStats{PGXPool: api.DebugPoolStats{Acquired: 1, Idle: 4, Total: 5, MaxConnections: 5}},
				RSSKB:               1000,
				LingeringContainers: 0,
				Tasks:               taskCounts{Completed: 0},
			},
			{
				At:                  start.Add(time.Minute),
				Stats:               api.DebugStats{PGXPool: api.DebugPoolStats{Acquired: 2, Idle: 3, Total: 5, MaxConnections: 5}},
				RSSKB:               1200,
				LingeringContainers: 1,
				Tasks:               taskCounts{Completed: 3},
			},
			{
				At:                  start.Add(2 * time.Minute),
				Stats:               api.DebugStats{PGXPool: api.DebugPoolStats{Acquired: 1, Idle: 4, Total: 5, MaxConnections: 5}},
				RSSKB:               1150,
				LingeringContainers: 0,
				Tasks:               taskCounts{Completed: 6},
			},
		},
	}

	summary := Analyze(report)
	if summary.Failed() {
		t.Fatalf("expected pass, got violations: %v", summary.Violations)
	}
	if summary.Completed != 6 {
		t.Fatalf("completed = %d, want 6", summary.Completed)
	}
}

func TestAnalyzeFailsOnRSSGrowth(t *testing.T) {
	report := Report{
		StartedAt:  time.Now().Add(-2 * time.Minute),
		FinishedAt: time.Now(),
		Submitted:  2,
		Samples: []Sample{
			{At: time.Now().Add(-2 * time.Minute), RSSKB: 1000},
			{At: time.Now(), RSSKB: 2501},
		},
	}

	summary := Analyze(report)
	if !summary.Failed() {
		t.Fatal("expected RSS growth to fail")
	}
}

func TestAnalyzeFailsOnPoolSaturation(t *testing.T) {
	report := Report{
		StartedAt:  time.Now().Add(-3 * time.Minute),
		FinishedAt: time.Now(),
		Submitted:  3,
		Samples: []Sample{
			{Stats: api.DebugStats{PGXPool: api.DebugPoolStats{Acquired: 2, Idle: 0, MaxConnections: 2}}},
			{Stats: api.DebugStats{PGXPool: api.DebugPoolStats{Acquired: 2, Idle: 0, MaxConnections: 2}}},
			{Stats: api.DebugStats{PGXPool: api.DebugPoolStats{Acquired: 2, Idle: 0, MaxConnections: 2}}},
		},
	}

	summary := Analyze(report)
	if !summary.Failed() {
		t.Fatal("expected pool saturation to fail")
	}
}

func TestAnalyzeFailsOnContainerAccumulation(t *testing.T) {
	report := Report{
		StartedAt:  time.Now().Add(-3 * time.Minute),
		FinishedAt: time.Now(),
		Submitted:  3,
		Samples: []Sample{
			{LingeringContainers: 0, Tasks: taskCounts{Completed: 0}},
			{LingeringContainers: 2, Tasks: taskCounts{Completed: 1}},
			{LingeringContainers: 4, Tasks: taskCounts{Completed: 2}},
		},
	}

	summary := Analyze(report)
	if !summary.Failed() {
		t.Fatal("expected container accumulation to fail")
	}
}
