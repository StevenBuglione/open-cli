package helpers

import "testing"

func TestCampaignRubricIncludesFleetMetadata(t *testing.T) {
	t.Parallel()

	rec := NewFindingsRecorder("remote-runtime-auth")
	rec.SetLaneMetadata("product-validation", "remote-runtime", "ci-containerized", "oauthClient")
	rec.CheckBool("runtime-info", "runtime info succeeded", true, "")

	rub := rec.Rubric()
	if rub.Workstream != "product-validation" {
		t.Fatalf("expected workstream product-validation, got %q", rub.Workstream)
	}
	if rub.Capability != "remote-runtime" {
		t.Fatalf("expected capability remote-runtime, got %q", rub.Capability)
	}
	if rub.EnvironmentClass != "ci-containerized" {
		t.Fatalf("expected environment class ci-containerized, got %q", rub.EnvironmentClass)
	}
	if rub.AuthPattern != "oauthClient" {
		t.Fatalf("expected auth pattern oauthClient, got %q", rub.AuthPattern)
	}
}
