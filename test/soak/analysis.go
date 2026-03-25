package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/backflow-labs/backflow/internal/api"
)

type Sample struct {
	At                  time.Time
	Stats               api.DebugStats
	RSSKB               int
	LingeringContainers int
	Tasks               taskCounts
}

type Report struct {
	StartedAt  time.Time
	FinishedAt time.Time
	Samples    []Sample
	Submitted  int
}

type Summary struct {
	Duration               time.Duration
	Submitted              int
	Completed              int
	FailedCount            int
	ErrorRate              float64
	ThroughputPerMinute    float64
	BaselineRSSKB          int
	MaxRSSKB               int
	RSSGrowthRatio         float64
	BaselineContainers     int
	MaxContainers          int
	MaxAcquiredConnections int32
	MaxPoolConnections     int32
	Violations             []string
}

func Analyze(report Report) Summary {
	summary := Summary{
		Submitted: report.Submitted,
	}
	if len(report.Samples) == 0 {
		summary.Violations = append(summary.Violations, "no samples collected")
		return summary
	}

	start := report.StartedAt
	if start.IsZero() {
		start = report.Samples[0].At
	}
	finish := report.FinishedAt
	if finish.IsZero() {
		finish = report.Samples[len(report.Samples)-1].At
	}
	duration := finish.Sub(start)
	if duration < 0 {
		duration = 0
	}
	summary.Duration = duration

	baseline := report.Samples[0]
	summary.BaselineRSSKB = baseline.RSSKB
	summary.BaselineContainers = baseline.LingeringContainers

	maxRSS := baseline.RSSKB
	maxContainers := baseline.LingeringContainers
	var maxAcquired int32
	var maxPool int32
	saturationStreak := 0

	for _, sample := range report.Samples {
		if sample.RSSKB > maxRSS {
			maxRSS = sample.RSSKB
		}
		if sample.LingeringContainers > maxContainers {
			maxContainers = sample.LingeringContainers
		}
		if sample.Stats.PGXPool.Acquired > maxAcquired {
			maxAcquired = sample.Stats.PGXPool.Acquired
		}
		if sample.Stats.PGXPool.MaxConnections > maxPool {
			maxPool = sample.Stats.PGXPool.MaxConnections
		}

		if sample.Stats.PGXPool.MaxConnections > 0 &&
			sample.Stats.PGXPool.Acquired >= sample.Stats.PGXPool.MaxConnections &&
			sample.Stats.PGXPool.Idle == 0 {
			saturationStreak++
		} else {
			saturationStreak = 0
		}
	}

	summary.MaxRSSKB = maxRSS
	summary.MaxContainers = maxContainers
	summary.MaxAcquiredConnections = maxAcquired
	summary.MaxPoolConnections = maxPool

	if summary.BaselineRSSKB <= 0 {
		summary.BaselineRSSKB = 1
	}
	summary.RSSGrowthRatio = float64(maxRSS) / float64(summary.BaselineRSSKB)

	last := report.Samples[len(report.Samples)-1]
	summary.Completed = last.Tasks.Completed
	summary.FailedCount = last.Tasks.Failed

	if duration > 0 {
		summary.ErrorRate = float64(summary.FailedCount) / float64(max(1, report.Submitted))
		summary.ThroughputPerMinute = float64(summary.Completed) / duration.Minutes()
	}

	if summary.RSSGrowthRatio > 2.0 {
		summary.Violations = append(summary.Violations, fmt.Sprintf("rss grew to %.2fx baseline", summary.RSSGrowthRatio))
	}
	if saturationStreak >= 3 {
		summary.Violations = append(summary.Violations, "pgxpool stayed saturated for 3 consecutive samples")
	}
	if maxContainers-summary.BaselineContainers > 2 {
		summary.Violations = append(summary.Violations, fmt.Sprintf("lingering containers grew from %d to %d", summary.BaselineContainers, maxContainers))
	}
	if last.Tasks.nonTerminal() > 0 {
		summary.Violations = append(summary.Violations, fmt.Sprintf("%d tasks still non-terminal at end of run", last.Tasks.nonTerminal()))
	}

	return summary
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (s Summary) Failed() bool {
	return len(s.Violations) > 0
}

func (s Summary) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "duration=%s submitted=%d completed=%d failed=%d error_rate=%.2f throughput=%.2f/min\n",
		s.Duration.Truncate(time.Second), s.Submitted, s.Completed, s.FailedCount, s.ErrorRate, s.ThroughputPerMinute)
	fmt.Fprintf(&b, "rss_kb baseline=%d max=%d growth=%.2fx\n", s.BaselineRSSKB, s.MaxRSSKB, s.RSSGrowthRatio)
	fmt.Fprintf(&b, "containers baseline=%d max=%d\n", s.BaselineContainers, s.MaxContainers)
	fmt.Fprintf(&b, "pgxpool acquired_max=%d max_conns=%d\n", s.MaxAcquiredConnections, s.MaxPoolConnections)
	if len(s.Violations) == 0 {
		b.WriteString("status=pass\n")
	} else {
		b.WriteString("status=fail\n")
		for _, v := range s.Violations {
			fmt.Fprintf(&b, "- %s\n", v)
		}
	}
	return b.String()
}
