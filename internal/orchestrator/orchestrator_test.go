package orchestrator

import (
	"fmt"
	"testing"
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
