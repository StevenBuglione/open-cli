package service

import (
	"context"
	"database/sql"
	"testing"

	"github.com/StevenBuglione/open-cli/internal/admin/domain"
	"github.com/StevenBuglione/open-cli/internal/admin/store"
	_ "modernc.org/sqlite"
)

// NewTestService creates a service with an in-memory store for testing
func NewTestService(t *testing.T) *Service {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	st := store.New(db)
	if err := st.InitSchema(context.Background()); err != nil {
		t.Fatalf("failed to init schema: %v", err)
	}
	return New(st)
}

func TestServiceCreateBundle(t *testing.T) {
	svc := NewTestService(t)
	bundleID, err := svc.CreateBundle(context.Background(), domain.CreateBundleInput{
		Name:        "Engineering Access",
		Description: "Access to engineering APIs",
	})
	if err != nil {
		t.Fatal(err)
	}
	if bundleID == "" {
		t.Fatal("expected bundle id")
	}

	bundle, err := svc.GetBundle(context.Background(), bundleID)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Name != "Engineering Access" {
		t.Fatalf("unexpected bundle name: %q", bundle.Name)
	}
}

func TestServiceGetBundle(t *testing.T) {
	svc := NewTestService(t)
	bundleID, err := svc.CreateBundle(context.Background(), domain.CreateBundleInput{
		Name:        "Engineering",
		Description: "Engineering APIs",
	})
	if err != nil {
		t.Fatal(err)
	}

	bundle, err := svc.GetBundle(context.Background(), bundleID)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.ID != bundleID || bundle.Name != "Engineering" {
		t.Fatalf("unexpected bundle: %+v", bundle)
	}
}

func TestServiceListBundles(t *testing.T) {
	svc := NewTestService(t)

	_, err := svc.CreateBundle(context.Background(), domain.CreateBundleInput{
		Name:        "Engineering",
		Description: "Eng APIs",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.CreateBundle(context.Background(), domain.CreateBundleInput{
		Name:        "Sales",
		Description: "Sales APIs",
	})
	if err != nil {
		t.Fatal(err)
	}

	bundles, err := svc.ListBundles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(bundles) != 2 {
		t.Fatalf("expected 2 bundles, got %d", len(bundles))
	}
}

func TestServiceUpdateBundle(t *testing.T) {
	svc := NewTestService(t)
	bundleID, err := svc.CreateBundle(context.Background(), domain.CreateBundleInput{
		Name:        "Original",
		Description: "Original desc",
	})
	if err != nil {
		t.Fatal(err)
	}

	err = svc.UpdateBundle(context.Background(), bundleID, domain.UpdateBundleInput{
		Name:        "Updated",
		Description: "Updated desc",
	})
	if err != nil {
		t.Fatal(err)
	}

	bundle, err := svc.GetBundle(context.Background(), bundleID)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Name != "Updated" {
		t.Fatalf("bundle not updated: %q", bundle.Name)
	}
}

func TestServiceDeleteBundle(t *testing.T) {
	svc := NewTestService(t)
	bundleID, err := svc.CreateBundle(context.Background(), domain.CreateBundleInput{
		Name:        "Engineering",
		Description: "Eng APIs",
	})
	if err != nil {
		t.Fatal(err)
	}

	err = svc.DeleteBundle(context.Background(), bundleID)
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.GetBundle(context.Background(), bundleID)
	if err == nil {
		t.Fatal("expected bundle to be deleted")
	}
}

func TestServiceCreateBundleAssignment(t *testing.T) {
	svc := NewTestService(t)

	// Create a bundle first
	bundleID, err := svc.CreateBundle(context.Background(), domain.CreateBundleInput{
		Name:        "Engineering",
		Description: "Eng APIs",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create assignment
	assignmentID, err := svc.CreateBundleAssignment(context.Background(), domain.CreateBundleAssignmentInput{
		BundleID:      bundleID,
		PrincipalType: "user",
		PrincipalID:   "user_123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if assignmentID == "" {
		t.Fatal("expected assignment id")
	}

	// List assignments
	assignments, err := svc.ListBundleAssignments(context.Background(), bundleID)
	if err != nil {
		t.Fatal(err)
	}
	if len(assignments) != 1 {
		t.Fatalf("expected 1 assignment, got %d", len(assignments))
	}
	if assignments[0].PrincipalID != "user_123" {
		t.Fatalf("unexpected principal id: %q", assignments[0].PrincipalID)
	}
}

func TestServiceListBundleAssignments(t *testing.T) {
	svc := NewTestService(t)

	bundleID, err := svc.CreateBundle(context.Background(), domain.CreateBundleInput{
		Name:        "Engineering",
		Description: "Eng APIs",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create multiple assignments
	_, err = svc.CreateBundleAssignment(context.Background(), domain.CreateBundleAssignmentInput{
		BundleID:      bundleID,
		PrincipalType: "user",
		PrincipalID:   "user_123",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.CreateBundleAssignment(context.Background(), domain.CreateBundleAssignmentInput{
		BundleID:      bundleID,
		PrincipalType: "group",
		PrincipalID:   "group_456",
	})
	if err != nil {
		t.Fatal(err)
	}

	assignments, err := svc.ListBundleAssignments(context.Background(), bundleID)
	if err != nil {
		t.Fatal(err)
	}
	if len(assignments) != 2 {
		t.Fatalf("expected 2 assignments, got %d", len(assignments))
	}
}

func TestServiceDeleteBundleAssignment(t *testing.T) {
	svc := NewTestService(t)

	bundleID, err := svc.CreateBundle(context.Background(), domain.CreateBundleInput{
		Name:        "Engineering",
		Description: "Eng APIs",
	})
	if err != nil {
		t.Fatal(err)
	}

	assignmentID, err := svc.CreateBundleAssignment(context.Background(), domain.CreateBundleAssignmentInput{
		BundleID:      bundleID,
		PrincipalType: "user",
		PrincipalID:   "user_123",
	})
	if err != nil {
		t.Fatal(err)
	}

	err = svc.DeleteBundleAssignment(context.Background(), assignmentID)
	if err != nil {
		t.Fatal(err)
	}

	assignments, err := svc.ListBundleAssignments(context.Background(), bundleID)
	if err != nil {
		t.Fatal(err)
	}
	if len(assignments) != 0 {
		t.Fatalf("expected 0 assignments after deletion, got %d", len(assignments))
	}
}

func TestServiceValidatesBundleAssignmentPrincipalType(t *testing.T) {
	svc := NewTestService(t)

	bundleID, err := svc.CreateBundle(context.Background(), domain.CreateBundleInput{
		Name:        "Engineering",
		Description: "Eng APIs",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Try to create assignment with invalid principal type
	_, err = svc.CreateBundleAssignment(context.Background(), domain.CreateBundleAssignmentInput{
		BundleID:      bundleID,
		PrincipalType: "invalid",
		PrincipalID:   "user_123",
	})
	if err == nil {
		t.Fatal("expected validation error for invalid principal type")
	}
}

func TestServiceCreateSource(t *testing.T) {
	svc := NewTestService(t)
	source, err := svc.CreateSource(context.Background(), domain.CreateSourceInput{
		Kind:        "openapi",
		DisplayName: "GitHub API",
	})
	if err != nil {
		t.Fatal(err)
	}
	if source.ID == "" {
		t.Fatal("expected source id")
	}
	if source.Kind != "openapi" {
		t.Errorf("expected kind %q, got %q", "openapi", source.Kind)
	}
	if source.Status != "draft" {
		t.Errorf("expected status %q, got %q", "draft", source.Status)
	}
}

func TestServiceGetSource(t *testing.T) {
	svc := NewTestService(t)
	created, err := svc.CreateSource(context.Background(), domain.CreateSourceInput{
		Kind:        "openapi",
		DisplayName: "GitHub API",
	})
	if err != nil {
		t.Fatal(err)
	}

	retrieved, err := svc.GetSource(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if retrieved.ID != created.ID {
		t.Errorf("expected ID %q, got %q", created.ID, retrieved.ID)
	}
}

func TestServiceListSources(t *testing.T) {
	svc := NewTestService(t)
	_, err := svc.CreateSource(context.Background(), domain.CreateSourceInput{
		Kind:        "openapi",
		DisplayName: "GitHub API",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.CreateSource(context.Background(), domain.CreateSourceInput{
		Kind:        "mcp",
		DisplayName: "Slack MCP",
	})
	if err != nil {
		t.Fatal(err)
	}

	sources, err := svc.ListSources(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(sources))
	}
}

func TestServiceValidateSource(t *testing.T) {
	svc := NewTestService(t)
	created, err := svc.CreateSource(context.Background(), domain.CreateSourceInput{
		Kind:        "openapi",
		DisplayName: "GitHub API",
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := svc.ValidateSource(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.SourceID != created.ID {
		t.Errorf("expected source ID %q, got %q", created.ID, result.SourceID)
	}
	if result.Valid == false {
		t.Error("expected validation to succeed")
	}
	if len(result.Services) == 0 {
		t.Error("expected at least one service candidate")
	}
}

func TestServicePublishBundle(t *testing.T) {
	svc := NewTestService(t)
	ctx := context.Background()

	// Create a bundle
	bundleID, err := svc.CreateBundle(ctx, domain.CreateBundleInput{
		Name:        "Production Bundle",
		Description: "Bundle for production",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Publish the bundle
	revisionID, err := svc.PublishBundle(ctx, bundleID, "admin@example.com")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if revisionID == "" {
		t.Fatal("expected revision ID, got empty string")
	}

	// Get the revision
	revision, err := svc.GetRevision(ctx, revisionID)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if revision.Status != "published" {
		t.Errorf("expected status 'published', got %q", revision.Status)
	}

	// Should be the active revision
	activeRev, err := svc.GetActiveRevision(ctx, bundleID)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if activeRev.ID != revisionID {
		t.Errorf("expected active revision ID %q, got %q", revisionID, activeRev.ID)
	}
}
