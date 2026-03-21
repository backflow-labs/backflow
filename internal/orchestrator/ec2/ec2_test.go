package ec2_test

import (
	orchestrator "github.com/backflow-labs/backflow/internal/orchestrator"
	ec2pkg "github.com/backflow-labs/backflow/internal/orchestrator/ec2"
)

// Compile-time check: *Scaler must satisfy orchestrator.Scaler.
var _ orchestrator.Scaler = (*ec2pkg.Scaler)(nil)
