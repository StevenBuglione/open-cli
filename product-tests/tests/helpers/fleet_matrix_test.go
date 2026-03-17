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

func TestLoadLiveProofMatrix(t *testing.T) {
	t.Parallel()

	matrix, err := LoadLiveProofMatrix(filepath.Join(repoRoot(t), "product-tests", "testdata", "fleet", "live-proof-matrix.yaml"))
	if err != nil {
		t.Fatalf("load live proof matrix: %v", err)
	}
	if len(matrix.Lanes) != 2 {
		t.Fatalf("expected 2 live proof lanes, got %d", len(matrix.Lanes))
	}
	if matrix.Lanes[0].ID != "entra-browser-federation" {
		t.Fatalf("expected first live proof lane to be entra-browser-federation, got %q", matrix.Lanes[0].ID)
	}
}

func TestLoadWebsiteReviewRubric(t *testing.T) {
	t.Parallel()

	rubric, err := LoadWebsiteReviewRubric(filepath.Join(repoRoot(t), "product-tests", "testdata", "fleet", "website-review-rubric.yaml"))
	if err != nil {
		t.Fatalf("load website review rubric: %v", err)
	}
	for _, sectionID := range []string{"onboarding", "depth", "enterprise"} {
		if !rubric.HasSection(sectionID) {
			t.Fatalf("expected rubric section %q", sectionID)
		}
	}
}
