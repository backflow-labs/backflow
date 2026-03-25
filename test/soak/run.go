package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sync"
	"time"
)

type config struct {
	apiURL       string
	duration     time.Duration
	short        bool
	taskInterval time.Duration
}

func parseConfig(args []string) (config, error) {
	fs := flag.NewFlagSet("soak", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var cfg config
	fs.StringVar(&cfg.apiURL, "api-url", "http://localhost:8080", "Backflow API base URL")
	fs.DurationVar(&cfg.duration, "duration", defaultDuration, "total soak duration")
	fs.BoolVar(&cfg.short, "short", false, "run a short soak window")
	fs.DurationVar(&cfg.taskInterval, "task-interval", 30*time.Second, "interval between task submissions")

	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if cfg.short {
		cfg.duration = shortDuration
	}
	if cfg.apiURL == "" {
		return config{}, fmt.Errorf("--api-url is required")
	}
	if cfg.taskInterval <= 0 {
		return config{}, fmt.Errorf("--task-interval must be positive")
	}
	return cfg, nil
}

func run(args []string) int {
	cfg, err := parseConfig(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	report, err := execute(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	summary := Analyze(*report)
	fmt.Print(summary.String())
	if summary.Failed() {
		return 1
	}
	return 0
}

func execute(cfg config) (*Report, error) {
	pid, err := discoverPID(cfg.apiURL)
	if err != nil {
		return nil, err
	}

	client := newClient(cfg.apiURL)
	startedAt := time.Now().UTC()
	report := &Report{StartedAt: startedAt}

	initial, err := collectSample(context.Background(), client, pid, nil)
	if err != nil {
		return nil, err
	}
	report.Samples = append(report.Samples, initial)

	runCtx, cancel := context.WithTimeout(context.Background(), cfg.duration+drainGrace)
	defer cancel()

	var (
		mu      sync.Mutex
		taskIDs []string
		wg      sync.WaitGroup
		runErr  error
	)

	setErr := func(err error) {
		if err == nil {
			return
		}
		mu.Lock()
		if runErr == nil {
			runErr = err
			cancel()
		}
		mu.Unlock()
	}

	snapshotIDs := func() []string {
		mu.Lock()
		defer mu.Unlock()
		ids := make([]string, len(taskIDs))
		copy(ids, taskIDs)
		return ids
	}

	wg.Add(2)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(cfg.taskInterval)
		defer ticker.Stop()
		deadline := startedAt.Add(cfg.duration)
		seq := 0

		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				if time.Now().After(deadline) {
					return
				}
				seq++
				id, err := client.CreateTask(runCtx, seq)
				if err != nil {
					setErr(fmt.Errorf("create task %d: %w", seq, err))
					return
				}
				mu.Lock()
				taskIDs = append(taskIDs, id)
				mu.Unlock()
			}
		}
	}()

	go func() {
		defer wg.Done()
		ticker := time.NewTicker(sampleInterval)
		defer ticker.Stop()

		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				sample, err := collectSample(runCtx, client, pid, snapshotIDs())
				if err != nil {
					setErr(fmt.Errorf("collect sample: %w", err))
					return
				}
				mu.Lock()
				report.Samples = append(report.Samples, sample)
				mu.Unlock()
			}
		}
	}()

	wg.Wait()
	mu.Lock()
	err = runErr
	idsCopy := make([]string, len(taskIDs))
	copy(idsCopy, taskIDs)
	mu.Unlock()
	if err != nil {
		return nil, err
	}

	finalCtx, cancelFinal := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelFinal()
	final, err := collectSample(finalCtx, client, pid, idsCopy)
	if err != nil {
		return nil, err
	}
	report.Samples = append(report.Samples, final)
	report.FinishedAt = time.Now().UTC()
	report.Submitted = len(idsCopy)
	return report, nil
}

func collectSample(ctx context.Context, client *Client, pid int, ids []string) (Sample, error) {
	stats, err := client.DebugStats(ctx)
	if err != nil {
		return Sample{}, err
	}

	rss, err := readRSSKB(pid)
	if err != nil {
		return Sample{}, err
	}

	containers, err := countLingeringContainers()
	if err != nil {
		return Sample{}, err
	}

	tasks, err := sampleTaskCounts(ctx, client, ids)
	if err != nil {
		return Sample{}, err
	}

	return Sample{
		At:                  time.Now().UTC(),
		Stats:               stats,
		RSSKB:               rss,
		LingeringContainers: containers,
		Tasks:               tasks,
	}, nil
}
