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

func TestCampaignRubricPreservesInlineArtifacts(t *testing.T) {
	t.Parallel()

	rec := NewFindingsRecorder("remote-runtime-auth")
	if err := rec.AddJSONArtifact("browser-config.json", map[string]any{
		"clientId": "oascli-browser",
		"audience": "oasclird",
	}); err != nil {
		t.Fatalf("AddJSONArtifact: %v", err)
	}

	rub := rec.Rubric()
	if len(rub.ArtifactPaths) != 1 || rub.ArtifactPaths[0] != "browser-config.json" {
		t.Fatalf("expected browser-config.json artifact path, got %#v", rub.ArtifactPaths)
	}
	if len(rub.Artifacts) != 1 {
		t.Fatalf("expected one inline artifact, got %#v", rub.Artifacts)
	}
	if rub.Artifacts[0].Path != "browser-config.json" {
		t.Fatalf("expected inline artifact path browser-config.json, got %#v", rub.Artifacts[0].Path)
	}
}
