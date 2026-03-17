package helpers

import (
	"path/filepath"
	"testing"
)

func TestLoadCapabilityMatrix(t *testing.T) {
	t.Parallel()

	matrix, err := LoadCapabilityMatrix(filepath.Join(repoRoot(t), "product-tests", "testdata", "fleet", "capability-matrix.yaml"))
	if err != nil {
		t.Fatalf("load matrix: %v", err)
	}
	if len(matrix.Lanes) == 0 {
		t.Fatal("expected at least one fleet lane")
	}
}

func TestLoadCapabilityMatrixRejectsMissingCapabilityID(t *testing.T) {
	t.Parallel()

	_, err := LoadCapabilityMatrix(filepath.Join(repoRoot(t), "product-tests", "testdata", "fleet", "invalid-capability-matrix.yaml"))
	if err == nil {
		t.Fatal("expected validation error")
	}
}
