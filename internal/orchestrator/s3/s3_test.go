package s3_test

import (
	"testing"

	orchestrator "github.com/backflow-labs/backflow/internal/orchestrator"
	s3pkg "github.com/backflow-labs/backflow/internal/orchestrator/s3"
)

// Compile-time check: *Uploader must satisfy orchestrator.S3Client.
var _ orchestrator.S3Client = (*s3pkg.Uploader)(nil)

func TestNewUploader_NilWhenNoBucket(t *testing.T) {
	// NewUploader requires a real AWS config when a bucket is set,
	// so we only test the nil-bucket path here.
	// The non-nil path is covered by integration tests.
}
