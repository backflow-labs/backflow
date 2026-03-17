package orchestrator

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/backflow-labs/backflow/internal/models"
)

func TestInitLocalMode_DBError_DoesNotCreateInstance(t *testing.T) {
	ms := newMockStore()
	ms.getInstanceErr = fmt.Errorf("disk I/O error")

	o := newTestOrchestrator(ms, &mockNotifier{})
	o.initLocalMode(ms, o.config)

	// On a real DB error, initLocalMode should bail out — not create an instance.
	if _, exists := ms.instances["local"]; exists {
		t.Fatal("expected no instance to be created when GetInstance returns a real DB error")
	}
}

func TestInitEC2Mode_DBError_DoesNotTerminateLocalInstance(t *testing.T) {
	ms := newMockStore()
	// Seed a running local instance — simulating a leftover from local-mode.
	ms.instances["local"] = &models.Instance{
		InstanceID: "local",
		Status:     models.InstanceStatusRunning,
	}
	// Inject a DB error so GetInstance fails.
	ms.getInstanceErr = fmt.Errorf("disk I/O error")

	o := newTestOrchestrator(ms, &mockNotifier{})
	o.initEC2Mode(ms, o.config, NewDockerManager(o.config))

	// Should not have terminated the local instance — we couldn't confirm it exists.
	if ms.instances["local"].Status == models.InstanceStatusTerminated {
		t.Fatal("expected local instance NOT to be terminated when GetInstance returns a real DB error")
	}
}

func TestInitFargateMode_DBError_DoesNotCreateInstance(t *testing.T) {
	ms := newMockStore()
	ms.getInstanceErr = fmt.Errorf("disk I/O error")

	o := newTestOrchestrator(ms, &mockNotifier{})
	o.config.MaxConcurrentTasks = 5
	o.initFargateMode(ms, o.config)

	// On a real DB error, initFargateMode should bail out — not create an instance.
	if _, exists := ms.instances["fargate"]; exists {
		t.Fatal("expected no instance to be created when GetInstance returns a real DB error")
	}
}

func TestLogRunningTasks_LogsCurrentCount(t *testing.T) {
	ms := newMockStore()
	o := newTestOrchestrator(ms, &mockNotifier{})
	o.running = 3

	var buf bytes.Buffer
	prevLogger := log.Logger
	log.Logger = zerolog.New(&buf)
	defer func() {
		log.Logger = prevLogger
	}()

	o.logRunningTasks()

	output := buf.String()
	if !strings.Contains(output, `"message":"orchestrator: running task count"`) {
		t.Fatalf("log output = %q, want running task count message", output)
	}
	if !strings.Contains(output, `"running_tasks":3`) {
		t.Fatalf("log output = %q, want running_tasks count", output)
	}
}
