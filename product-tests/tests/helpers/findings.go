// Package helpers provides utilities for product-test scenarios.
// This file implements structured rubric and freeform findings capture
// for campaign runs.
package helpers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// RubricCriterion describes a single pass/fail checkpoint within a campaign.
type RubricCriterion struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Expected    string `json:"expected"`
	Actual      string `json:"actual,omitempty"`
	Pass        bool   `json:"pass"`
	Note        string `json:"note,omitempty"`
}

// KnownGapEntry documents a feature gap that is expected to not yet pass.
type KnownGapEntry struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Reason      string `json:"reason"`
	StillFails  bool   `json:"stillFails"`
}

// CampaignRubric is the structured output emitted after every campaign run.
// It satisfies the agent-rubric.schema.json schema.
type CampaignRubric struct {
	Schema           string            `json:"$schema,omitempty"`
	Campaign         string            `json:"campaign"`
	Workstream       string            `json:"workstream,omitempty"`
	Capability       string            `json:"capability,omitempty"`
	EnvironmentClass string            `json:"environmentClass,omitempty"`
	AuthPattern      string            `json:"authPattern,omitempty"`
	ArtifactPaths    []string          `json:"artifactPaths,omitempty"`
	RunAt            string            `json:"runAt"`
	Pass             bool              `json:"pass"`
	Criteria         []RubricCriterion `json:"criteria"`
	KnownGaps        []KnownGapEntry   `json:"knownGaps,omitempty"`
	Findings         []string          `json:"findings"`
}

// FindingsRecorder accumulates rubric criteria and freeform findings
// during a campaign run and emits a final CampaignRubric.
type FindingsRecorder struct {
	campaign         string
	workstream       string
	capability       string
	environmentClass string
	authPattern      string
	artifactPaths    []string
	runAt            string
	criteria         []RubricCriterion
	knownGaps        []KnownGapEntry
	findings         []string
}

// NewFindingsRecorder creates a recorder for the named campaign.
func NewFindingsRecorder(campaign string) *FindingsRecorder {
	return &FindingsRecorder{
		campaign:  campaign,
		runAt:     time.Now().UTC().Format(time.RFC3339),
		criteria:  []RubricCriterion{},
		knownGaps: []KnownGapEntry{},
		findings:  []string{},
	}
}

// SetLaneMetadata records the fleet lane metadata for this campaign run.
func (r *FindingsRecorder) SetLaneMetadata(workstream, capability, environmentClass, authPattern string) {
	r.workstream = workstream
	r.capability = capability
	r.environmentClass = environmentClass
	r.authPattern = authPattern
}

// AddArtifactPath records an artifact path associated with this campaign run.
func (r *FindingsRecorder) AddArtifactPath(path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	r.artifactPaths = append(r.artifactPaths, path)
}

// Check records a rubric criterion. If pass is false, an automatic finding
// is appended to the freeform findings list.
func (r *FindingsRecorder) Check(id, description, expected, actual string, pass bool, note string) {
	r.criteria = append(r.criteria, RubricCriterion{
		ID:          id,
		Description: description,
		Expected:    expected,
		Actual:      actual,
		Pass:        pass,
		Note:        note,
	})
	if !pass {
		msg := fmt.Sprintf("FAIL [%s]: %s — expected %q, got %q", id, description, expected, actual)
		if note != "" {
			msg += " (" + note + ")"
		}
		r.findings = append(r.findings, msg)
	}
}

// CheckBool is a convenience wrapper for boolean pass/fail checks.
func (r *FindingsRecorder) CheckBool(id, description string, pass bool, note string) {
	expected := "true"
	actual := "false"
	if pass {
		actual = "true"
	}
	r.Check(id, description, expected, actual, pass, note)
}

// Note appends a freeform finding without affecting pass/fail status.
func (r *FindingsRecorder) Note(msg string) {
	r.findings = append(r.findings, "NOTE: "+msg)
}

// RecordKnownGap documents a known-gap scenario. fn is called to determine
// whether the gap still fails. If fn returns false (gap is fixed), a "gap fixed"
// finding is recorded. The test is never failed due to a known gap.
func (r *FindingsRecorder) RecordKnownGap(id, description, reason string, fn func() bool) {
	stillFails := fn()
	r.knownGaps = append(r.knownGaps, KnownGapEntry{
		ID:          id,
		Description: description,
		Reason:      reason,
		StillFails:  stillFails,
	})
	if !stillFails {
		r.findings = append(r.findings, fmt.Sprintf("GAP FIXED [%s]: %s — this known gap now passes", id, description))
	} else {
		r.findings = append(r.findings, fmt.Sprintf("KNOWN GAP [%s]: %s — %s", id, description, reason))
	}
}

// Rubric assembles and returns the completed CampaignRubric.
func (r *FindingsRecorder) Rubric() *CampaignRubric {
	pass := true
	for _, c := range r.criteria {
		if !c.Pass {
			pass = false
			break
		}
	}
	return &CampaignRubric{
		Campaign:         r.campaign,
		Workstream:       r.workstream,
		Capability:       r.capability,
		EnvironmentClass: r.environmentClass,
		AuthPattern:      r.authPattern,
		ArtifactPaths:    append([]string(nil), r.artifactPaths...),
		RunAt:            r.runAt,
		Pass:             pass,
		Criteria:         r.criteria,
		KnownGaps:        r.knownGaps,
		Findings:         r.findings,
	}
}

// Emit serialises the rubric to JSON. If dir is non-empty the file is written
// to dir and the path is returned. If dir is empty, the JSON string is returned
// without writing any file.
func (r *FindingsRecorder) Emit(dir string) (string, error) {
	rub := r.Rubric()
	data, err := json.MarshalIndent(rub, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal rubric: %w", err)
	}
	if dir == "" {
		return string(data), nil
	}
	// Build a filesystem-safe filename.
	safe := strings.NewReplacer(":", "-", " ", "_").Replace(r.runAt)
	fname := fmt.Sprintf("%s-%s.rubric.json", r.campaign, safe)
	path := filepath.Join(dir, fname)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write rubric to %s: %w", path, err)
	}
	return path, nil
}

// MustEmitToTest logs the rubric JSON via t.Log and calls t.Error for each
// failed criterion. Known gaps are logged but never fail the test.
func (r *FindingsRecorder) MustEmitToTest(t *testing.T) {
	t.Helper()
	data, err := r.Emit("")
	if err != nil {
		t.Fatalf("emit rubric: %v", err)
	}
	t.Logf("=== Campaign Rubric [%s] ===\n%s", r.campaign, data)

	rub := r.Rubric()
	for _, c := range rub.Criteria {
		if !c.Pass {
			if c.Note != "" {
				t.Errorf("rubric [%s] FAILED: %s (note: %s)", c.ID, c.Description, c.Note)
			} else {
				t.Errorf("rubric [%s] FAILED: %s — expected %q, got %q", c.ID, c.Description, c.Expected, c.Actual)
			}
		}
	}
}
