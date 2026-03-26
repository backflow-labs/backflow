package ec2

import (
	"context"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/backflow-labs/backflow/internal/config"
	"github.com/backflow-labs/backflow/internal/models"
	"github.com/backflow-labs/backflow/internal/store"
)

// --- mock ec2Client ---

type mockEC2Client struct {
	launchID  string
	launchErr error

	terminateCalls []string
	terminateErr   error

	describeResult *types.Instance
	describeErr    error
}

func (m *mockEC2Client) LaunchSpotInstance(ctx context.Context) (string, error) {
	return m.launchID, m.launchErr
}

func (m *mockEC2Client) TerminateInstance(ctx context.Context, instanceID string) error {
	m.terminateCalls = append(m.terminateCalls, instanceID)
	return m.terminateErr
}

func (m *mockEC2Client) DescribeInstance(ctx context.Context, instanceID string) (*types.Instance, error) {
	return m.describeResult, m.describeErr
}

// --- mock store (minimal) ---

type mockScalerStore struct {
	store.Store // embed to satisfy interface; panics on unimplemented methods
	instances   []*models.Instance

	createInstanceErr error
	createCalls       int
}

func (m *mockScalerStore) ListInstances(_ context.Context, status *models.InstanceStatus) ([]*models.Instance, error) {
	if status == nil {
		return m.instances, nil
	}
	var result []*models.Instance
	for _, inst := range m.instances {
		if inst.Status == *status {
			result = append(result, inst)
		}
	}
	return result, nil
}

func (m *mockScalerStore) CreateInstance(_ context.Context, inst *models.Instance) error {
	m.createCalls++
	if m.createInstanceErr != nil {
		return m.createInstanceErr
	}
	m.instances = append(m.instances, inst)
	return nil
}

func TestRequestScaleUp_TerminatesOnDBFailure(t *testing.T) {
	ec2mock := &mockEC2Client{launchID: "i-orphan123"}
	db := &mockScalerStore{createInstanceErr: fmt.Errorf("connection refused")}
	cfg := &config.Config{MaxInstances: 3, InstanceType: "c5.xlarge", ContainersPerInst: 2}

	s := &Scaler{store: db, ec2: ec2mock, config: cfg}
	s.RequestScaleUp(context.Background())

	// The DB write failed, so the launched instance must be terminated.
	if len(ec2mock.terminateCalls) != 1 {
		t.Fatalf("TerminateInstance calls = %d, want 1", len(ec2mock.terminateCalls))
	}
	if ec2mock.terminateCalls[0] != "i-orphan123" {
		t.Errorf("terminated instance = %q, want %q", ec2mock.terminateCalls[0], "i-orphan123")
	}
}

func TestRequestScaleUp_Success(t *testing.T) {
	ec2mock := &mockEC2Client{launchID: "i-good456"}
	db := &mockScalerStore{}
	cfg := &config.Config{MaxInstances: 3, InstanceType: "c5.xlarge", ContainersPerInst: 2}

	s := &Scaler{store: db, ec2: ec2mock, config: cfg}
	s.RequestScaleUp(context.Background())

	// Instance should be saved, not terminated.
	if db.createCalls != 1 {
		t.Fatalf("CreateInstance calls = %d, want 1", db.createCalls)
	}
	if len(ec2mock.terminateCalls) != 0 {
		t.Errorf("TerminateInstance calls = %d, want 0", len(ec2mock.terminateCalls))
	}
	if len(db.instances) != 1 || db.instances[0].InstanceID != "i-good456" {
		t.Errorf("saved instance = %v, want i-good456", db.instances)
	}
}

func TestRequestScaleUp_SkipsWhenAtMax(t *testing.T) {
	ec2mock := &mockEC2Client{launchID: "i-shouldnt-launch"}
	db := &mockScalerStore{
		instances: []*models.Instance{
			{InstanceID: "i-1", Status: models.InstanceStatusRunning},
			{InstanceID: "i-2", Status: models.InstanceStatusRunning},
		},
	}
	cfg := &config.Config{MaxInstances: 2}

	s := &Scaler{store: db, ec2: ec2mock, config: cfg}
	s.RequestScaleUp(context.Background())

	// Should not launch because already at max.
	if db.createCalls != 0 {
		t.Errorf("CreateInstance calls = %d, want 0", db.createCalls)
	}
}
