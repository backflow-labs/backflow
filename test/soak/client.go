package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/backflow-labs/backflow/internal/api"
)

const (
	fakeAgentImage    = "backflow-fake-agent:test"
	sampleInterval    = 60 * time.Second
	defaultDuration   = time.Hour
	shortDuration     = 12 * time.Minute
	drainGrace        = 10 * time.Minute
	taskSubmitTimeout = 10 * time.Second
)

type Client struct {
	baseURL string
	http    *http.Client
}

func newClient(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: taskSubmitTimeout},
	}
}

func (c *Client) CreateTask(ctx context.Context, seq int) (string, error) {
	body, err := json.Marshal(map[string]any{
		"prompt":            fmt.Sprintf("soak task %d", seq),
		"create_pr":         false,
		"self_review":       false,
		"save_agent_output": false,
		"env_vars": map[string]string{
			"FAKE_OUTCOME": "success",
		},
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/tasks", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("create task: status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var env struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return "", err
	}
	if env.Error != "" {
		return "", fmt.Errorf("create task: %s", env.Error)
	}
	if env.Data.ID == "" {
		return "", fmt.Errorf("create task: empty task id")
	}
	return env.Data.ID, nil
}

func (c *Client) GetTask(ctx context.Context, id string) (taskState, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/tasks/"+id, nil)
	if err != nil {
		return taskState{}, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return taskState{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return taskState{}, fmt.Errorf("get task %s: status %d: %s", id, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var env struct {
		Data  taskState `json:"data"`
		Error string    `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return taskState{}, err
	}
	if env.Error != "" {
		return taskState{}, fmt.Errorf("get task %s: %s", id, env.Error)
	}
	return env.Data, nil
}

func (c *Client) DebugStats(ctx context.Context) (api.DebugStats, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/debug/stats", nil)
	if err != nil {
		return api.DebugStats{}, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return api.DebugStats{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return api.DebugStats{}, fmt.Errorf("debug stats: status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var env struct {
		Data  api.DebugStats `json:"data"`
		Error string         `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return api.DebugStats{}, err
	}
	if env.Error != "" {
		return api.DebugStats{}, fmt.Errorf("debug stats: %s", env.Error)
	}
	return env.Data, nil
}

type taskState struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type taskCounts struct {
	Pending      int
	Provisioning int
	Running      int
	Completed    int
	Failed       int
	Interrupted  int
	Cancelled    int
	Recovering   int
}

func (c taskCounts) nonTerminal() int {
	return c.Pending + c.Provisioning + c.Running + c.Interrupted + c.Recovering
}

func (c taskCounts) total() int {
	return c.Pending + c.Provisioning + c.Running + c.Completed + c.Failed + c.Interrupted + c.Cancelled + c.Recovering
}

func sampleTaskCounts(ctx context.Context, client *Client, ids []string) (taskCounts, error) {
	var counts taskCounts
	for _, id := range ids {
		task, err := client.GetTask(ctx, id)
		if err != nil {
			return taskCounts{}, err
		}
		switch task.Status {
		case "pending":
			counts.Pending++
		case "provisioning":
			counts.Provisioning++
		case "running":
			counts.Running++
		case "completed":
			counts.Completed++
		case "failed":
			counts.Failed++
		case "interrupted":
			counts.Interrupted++
		case "cancelled":
			counts.Cancelled++
		case "recovering":
			counts.Recovering++
		default:
			return taskCounts{}, fmt.Errorf("task %s: unknown status %q", id, task.Status)
		}
	}
	return counts, nil
}

func discoverPID(apiURL string) (int, error) {
	u, err := url.Parse(apiURL)
	if err != nil {
		return 0, err
	}
	port := u.Port()
	if port == "" {
		return 0, fmt.Errorf("api-url %q must include a port", apiURL)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "lsof", "-ti", "tcp:"+port, "-sTCP:LISTEN").Output()
	if err != nil {
		return 0, fmt.Errorf("discover backflow pid for port %s: %w", port, err)
	}

	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return 0, fmt.Errorf("discover backflow pid for port %s: no listener found", port)
	}

	pid, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0, fmt.Errorf("parse backflow pid %q: %w", fields[0], err)
	}
	return pid, nil
}

func readRSSKB(pid int) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "ps", "-o", "rss=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, fmt.Errorf("read rss for pid %d: %w", pid, err)
	}

	value := strings.TrimSpace(string(out))
	if value == "" {
		return 0, fmt.Errorf("read rss for pid %d: empty output", pid)
	}

	rss, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("parse rss %q for pid %d: %w", value, pid, err)
	}
	return rss, nil
}

func countLingeringContainers() (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "docker", "ps", "-a", "--filter", "ancestor="+fakeAgentImage, "--format", "{{.ID}}").Output()
	if err != nil {
		return 0, fmt.Errorf("count lingering containers: %w", err)
	}

	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return 0, nil
	}
	return len(strings.Split(trimmed, "\n")), nil
}
