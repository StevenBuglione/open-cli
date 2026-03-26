package store

import (
	"context"
	"database/sql"
	"testing"

	"github.com/StevenBuglione/open-cli/internal/admin/domain"
	_ "modernc.org/sqlite"
)

// NewTestStore creates an in-memory SQLite store for testing
func NewTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	store := New(db)
	if err := store.InitSchema(context.Background()); err != nil {
		t.Fatalf("failed to init schema: %v", err)
	}
	return store
}

func TestStoreCreatesSource(t *testing.T) {
	store := NewTestStore(t)
	sourceID, err := store.CreateSource(context.Background(), domain.CreateSourceInput{
		Kind:        "openapi",
		DisplayName: "GitHub",
	})
	if err != nil {
		t.Fatal(err)
	}
	if sourceID == "" {
		t.Fatal("expected source id")
	}

	var (
		id          string
		kind        string
		displayName string
		status      string
	)
	err = store.db.QueryRowContext(
		context.Background(),
		`SELECT id, kind, display_name, status FROM admin_sources WHERE id = $1`,
		sourceID,
	).Scan(&id, &kind, &displayName, &status)
	if err != nil {
		t.Fatalf("expected stored source row: %v", err)
	}
	if id != sourceID || kind != "openapi" || displayName != "GitHub" || status != "draft" {
		t.Fatalf("unexpected stored source row: id=%q kind=%q displayName=%q status=%q", id, kind, displayName, status)
	}
}

func TestStoreCreateSourceReturnsEmptyIDOnError(t *testing.T) {
	store := NewTestStore(t)
	if err := store.db.Close(); err != nil {
		t.Fatalf("close test db: %v", err)
	}

	sourceID, err := store.CreateSource(context.Background(), domain.CreateSourceInput{
		Kind:        "openapi",
		DisplayName: "GitHub",
	})
	if err == nil {
		t.Fatal("expected create source error")
	}
	if sourceID != "" {
		t.Fatalf("expected empty source id on error, got %q", sourceID)
	}
}

func TestStoreGetSource(t *testing.T) {
	store := NewTestStore(t)
	sourceID, err := store.CreateSource(context.Background(), domain.CreateSourceInput{
		Kind:        "openapi",
		DisplayName: "GitHub API",
	})
	if err != nil {
		t.Fatal(err)
	}

	source, err := store.GetSource(context.Background(), sourceID)
	if err != nil {
		t.Fatalf("expected to get source: %v", err)
	}
	if source.ID != sourceID {
		t.Errorf("expected ID %q, got %q", sourceID, source.ID)
	}
	if source.Kind != "openapi" {
		t.Errorf("expected kind %q, got %q", "openapi", source.Kind)
	}
	if source.DisplayName != "GitHub API" {
		t.Errorf("expected display name %q, got %q", "GitHub API", source.DisplayName)
	}
	if source.Status != "draft" {
		t.Errorf("expected status %q, got %q", "draft", source.Status)
	}
}

func TestStoreGetSourceNotFound(t *testing.T) {
	store := NewTestStore(t)
	_, err := store.GetSource(context.Background(), "src_nonexistent")
	if err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestStoreListSources(t *testing.T) {
	store := NewTestStore(t)
	_, err := store.CreateSource(context.Background(), domain.CreateSourceInput{
		Kind:        "openapi",
		DisplayName: "GitHub API",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.CreateSource(context.Background(), domain.CreateSourceInput{
		Kind:        "mcp",
		DisplayName: "Slack MCP",
	})
	if err != nil {
		t.Fatal(err)
	}

	sources, err := store.ListSources(context.Background())
	if err != nil {
		t.Fatalf("expected to list sources: %v", err)
	}
	if len(sources) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(sources))
	}
}

func TestStoreUpdateSourceStatus(t *testing.T) {
	store := NewTestStore(t)
	sourceID, err := store.CreateSource(context.Background(), domain.CreateSourceInput{
		Kind:        "openapi",
		DisplayName: "GitHub API",
	})
	if err != nil {
		t.Fatal(err)
	}

	err = store.UpdateSourceStatus(context.Background(), sourceID, "validated")
	if err != nil {
		t.Fatalf("expected to update source status: %v", err)
	}

	source, err := store.GetSource(context.Background(), sourceID)
	if err != nil {
		t.Fatal(err)
	}
	if source.Status != "validated" {
		t.Errorf("expected status %q, got %q", "validated", source.Status)
	}
}

func TestStoreCreateBundle(t *testing.T) {
	store := NewTestStore(t)
	bundleID, err := store.CreateBundle(context.Background(), domain.CreateBundleInput{
		Name:        "Engineering Access",
		Description: "Access to engineering APIs",
	})
	if err != nil {
		t.Fatal(err)
	}
	if bundleID == "" {
		t.Fatal("expected bundle id")
	}

	var (
		id          string
		name        string
		description string
	)
	err = store.db.QueryRowContext(
		context.Background(),
		`SELECT id, name, description FROM admin_bundles WHERE id = $1`,
		bundleID,
	).Scan(&id, &name, &description)
	if err != nil {
		t.Fatalf("expected stored bundle row: %v", err)
	}
	if id != bundleID || name != "Engineering Access" || description != "Access to engineering APIs" {
		t.Fatalf("unexpected stored bundle: id=%q name=%q description=%q", id, name, description)
	}
}

func TestStoreGetBundle(t *testing.T) {
	store := NewTestStore(t)
	bundleID, err := store.CreateBundle(context.Background(), domain.CreateBundleInput{
		Name:        "Engineering Access",
		Description: "Access to engineering APIs",
	})
	if err != nil {
		t.Fatal(err)
	}

	bundle, err := store.GetBundle(context.Background(), bundleID)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.ID != bundleID || bundle.Name != "Engineering Access" || bundle.Description != "Access to engineering APIs" {
		t.Fatalf("unexpected bundle: %+v", bundle)
	}
}

func TestStoreGetBundleNotFound(t *testing.T) {
	store := NewTestStore(t)
	_, err := store.GetBundle(context.Background(), "bun_nonexistent")
	if err == nil {
		t.Fatal("expected not found error")
	}
	if err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestStoreListBundles(t *testing.T) {
	store := NewTestStore(t)
	
	// Create multiple bundles
	_, err := store.CreateBundle(context.Background(), domain.CreateBundleInput{
		Name:        "Engineering",
		Description: "Engineering APIs",
	})
	if err != nil {
		t.Fatal(err)
	}
	
	_, err = store.CreateBundle(context.Background(), domain.CreateBundleInput{
		Name:        "Sales",
		Description: "Sales APIs",
	})
	if err != nil {
		t.Fatal(err)
	}

	bundles, err := store.ListBundles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(bundles) != 2 {
		t.Fatalf("expected 2 bundles, got %d", len(bundles))
	}
}

func TestStoreUpdateBundle(t *testing.T) {
	store := NewTestStore(t)
	bundleID, err := store.CreateBundle(context.Background(), domain.CreateBundleInput{
		Name:        "Original Name",
		Description: "Original Description",
	})
	if err != nil {
		t.Fatal(err)
	}

	err = store.UpdateBundle(context.Background(), bundleID, domain.UpdateBundleInput{
		Name:        "Updated Name",
		Description: "Updated Description",
	})
	if err != nil {
		t.Fatal(err)
	}

	bundle, err := store.GetBundle(context.Background(), bundleID)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Name != "Updated Name" || bundle.Description != "Updated Description" {
		t.Fatalf("bundle not updated: %+v", bundle)
	}
}

func TestStoreDeleteBundle(t *testing.T) {
	store := NewTestStore(t)
	bundleID, err := store.CreateBundle(context.Background(), domain.CreateBundleInput{
		Name:        "Engineering",
		Description: "Engineering APIs",
	})
	if err != nil {
		t.Fatal(err)
	}

	err = store.DeleteBundle(context.Background(), bundleID)
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.GetBundle(context.Background(), bundleID)
	if err != sql.ErrNoRows {
		t.Fatalf("expected bundle to be deleted, got err: %v", err)
	}
}

func TestStoreCreateBundleAssignment(t *testing.T) {
	store := NewTestStore(t)
	
	// Create a bundle first
	bundleID, err := store.CreateBundle(context.Background(), domain.CreateBundleInput{
		Name:        "Engineering",
		Description: "Engineering APIs",
	})
	if err != nil {
		t.Fatal(err)
	}

	assignmentID, err := store.CreateBundleAssignment(context.Background(), domain.CreateBundleAssignmentInput{
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

	var (
		id                  string
		storedBundleID      string
		principalType       string
		principalID         string
	)
	err = store.db.QueryRowContext(
		context.Background(),
		`SELECT id, bundle_id, principal_type, principal_id FROM admin_bundle_assignments WHERE id = $1`,
		assignmentID,
	).Scan(&id, &storedBundleID, &principalType, &principalID)
	if err != nil {
		t.Fatalf("expected stored assignment row: %v", err)
	}
	if id != assignmentID || storedBundleID != bundleID || principalType != "user" || principalID != "user_123" {
		t.Fatalf("unexpected assignment: id=%q bundleID=%q principalType=%q principalID=%q", 
			id, storedBundleID, principalType, principalID)
	}
}

func TestStoreListBundleAssignments(t *testing.T) {
	store := NewTestStore(t)
	
	// Create a bundle
	bundleID, err := store.CreateBundle(context.Background(), domain.CreateBundleInput{
		Name:        "Engineering",
		Description: "Engineering APIs",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create multiple assignments
	_, err = store.CreateBundleAssignment(context.Background(), domain.CreateBundleAssignmentInput{
		BundleID:      bundleID,
		PrincipalType: "user",
		PrincipalID:   "user_123",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.CreateBundleAssignment(context.Background(), domain.CreateBundleAssignmentInput{
		BundleID:      bundleID,
		PrincipalType: "group",
		PrincipalID:   "group_456",
	})
	if err != nil {
		t.Fatal(err)
	}

	assignments, err := store.ListBundleAssignments(context.Background(), bundleID)
	if err != nil {
		t.Fatal(err)
	}
	if len(assignments) != 2 {
		t.Fatalf("expected 2 assignments, got %d", len(assignments))
	}
}

func TestStoreDeleteBundleAssignment(t *testing.T) {
	store := NewTestStore(t)
	
	// Create a bundle and assignment
	bundleID, err := store.CreateBundle(context.Background(), domain.CreateBundleInput{
		Name:        "Engineering",
		Description: "Engineering APIs",
	})
	if err != nil {
		t.Fatal(err)
	}

	assignmentID, err := store.CreateBundleAssignment(context.Background(), domain.CreateBundleAssignmentInput{
		BundleID:      bundleID,
		PrincipalType: "user",
		PrincipalID:   "user_123",
	})
	if err != nil {
		t.Fatal(err)
	}

	err = store.DeleteBundleAssignment(context.Background(), assignmentID)
	if err != nil {
		t.Fatal(err)
	}

	// Verify it's deleted
	assignments, err := store.ListBundleAssignments(context.Background(), bundleID)
	if err != nil {
		t.Fatal(err)
	}
	if len(assignments) != 0 {
		t.Fatalf("expected 0 assignments after deletion, got %d", len(assignments))
	}
}

func TestStoreBundleAssignmentUniqueness(t *testing.T) {
	store := NewTestStore(t)
	
	// Create a bundle
	bundleID, err := store.CreateBundle(context.Background(), domain.CreateBundleInput{
		Name:        "Engineering",
		Description: "Engineering APIs",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create first assignment
	_, err = store.CreateBundleAssignment(context.Background(), domain.CreateBundleAssignmentInput{
		BundleID:      bundleID,
		PrincipalType: "user",
		PrincipalID:   "user_123",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Try to create duplicate assignment
	_, err = store.CreateBundleAssignment(context.Background(), domain.CreateBundleAssignmentInput{
		BundleID:      bundleID,
		PrincipalType: "user",
		PrincipalID:   "user_123",
	})
	if err == nil {
		t.Fatal("expected uniqueness constraint error")
	}
}

