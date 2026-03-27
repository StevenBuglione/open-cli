package publish

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/StevenBuglione/open-cli/internal/admin/domain"
	"github.com/StevenBuglione/open-cli/internal/admin/store"
	_ "modernc.org/sqlite"
)

func newTestStore(t *testing.T) *store.Store {
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
	return st
}

func TestCompileBundle_EmptyBundle(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	// Create an empty bundle
	bundleID, err := st.CreateBundle(ctx, domain.CreateBundleInput{
		Name:        "Empty Bundle",
		Description: "A bundle with no sources",
	})
	if err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler(st)
	snapshot, err := compiler.CompileBundle(ctx, bundleID)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if snapshot == nil {
		t.Fatal("expected snapshot, got nil")
	}
	if snapshot.BundleID != bundleID {
		t.Errorf("expected bundle ID %q, got %q", bundleID, snapshot.BundleID)
	}
	if len(snapshot.Sources) != 0 {
		t.Errorf("expected 0 sources, got %d", len(snapshot.Sources))
	}
	if len(snapshot.Services) != 0 {
		t.Errorf("expected 0 services, got %d", len(snapshot.Services))
	}
}

func TestCompileBundle_NonExistentBundle(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	compiler := NewCompiler(st)
	_, err := compiler.CompileBundle(ctx, "bun_nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent bundle")
	}
}

func TestCreateRevision_Success(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	bundleID, err := st.CreateBundle(ctx, domain.CreateBundleInput{
		Name:        "Test Bundle",
		Description: "Test bundle for revision",
	})
	if err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler(st)
	revisionID, err := compiler.CreateRevision(ctx, bundleID, "admin@example.com")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if revisionID == "" {
		t.Fatal("expected revision ID, got empty string")
	}

	// Should start with rev_ prefix
	if len(revisionID) < 4 || revisionID[:4] != "rev_" {
		t.Errorf("expected revision ID to start with 'rev_', got %q", revisionID)
	}
}

func TestGetRevision(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	bundleID, err := st.CreateBundle(ctx, domain.CreateBundleInput{
		Name:        "Test Bundle",
		Description: "Test bundle for revision",
	})
	if err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler(st)
	revisionID, err := compiler.CreateRevision(ctx, bundleID, "admin@example.com")
	if err != nil {
		t.Fatal(err)
	}

	revision, err := compiler.GetRevision(ctx, revisionID)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if revision.ID != revisionID {
		t.Errorf("expected revision ID %q, got %q", revisionID, revision.ID)
	}
	if revision.BundleID != bundleID {
		t.Errorf("expected bundle ID %q, got %q", bundleID, revision.BundleID)
	}
	if revision.CreatedBy != "admin@example.com" {
		t.Errorf("expected created by %q, got %q", "admin@example.com", revision.CreatedBy)
	}
	if revision.Status != "draft" {
		t.Errorf("expected status 'draft', got %q", revision.Status)
	}
}

func TestPublishRevision(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	bundleID, err := st.CreateBundle(ctx, domain.CreateBundleInput{
		Name:        "Test Bundle",
		Description: "Test bundle for publishing",
	})
	if err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler(st)
	revisionID, err := compiler.CreateRevision(ctx, bundleID, "admin@example.com")
	if err != nil {
		t.Fatal(err)
	}

	err = compiler.PublishRevision(ctx, revisionID)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	revision, err := compiler.GetRevision(ctx, revisionID)
	if err != nil {
		t.Fatal(err)
	}

	if revision.Status != "published" {
		t.Errorf("expected status 'published', got %q", revision.Status)
	}
	if revision.PublishedAt == nil {
		t.Error("expected published_at to be set")
	}
}

func TestListRevisions(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	bundleID, err := st.CreateBundle(ctx, domain.CreateBundleInput{
		Name:        "Test Bundle",
		Description: "Test bundle for revisions",
	})
	if err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler(st)

	// Create multiple revisions
	rev1, err := compiler.CreateRevision(ctx, bundleID, "admin@example.com")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond) // Ensure different timestamps

	rev2, err := compiler.CreateRevision(ctx, bundleID, "admin2@example.com")
	if err != nil {
		t.Fatal(err)
	}

	revisions, err := compiler.ListRevisions(ctx, bundleID)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if len(revisions) != 2 {
		t.Fatalf("expected 2 revisions, got %d", len(revisions))
	}

	// Should be in descending order (newest first)
	if revisions[0].ID != rev2 {
		t.Errorf("expected first revision to be %q, got %q", rev2, revisions[0].ID)
	}
	if revisions[1].ID != rev1 {
		t.Errorf("expected second revision to be %q, got %q", rev1, revisions[1].ID)
	}
}

func TestGetActiveRevision(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	bundleID, err := st.CreateBundle(ctx, domain.CreateBundleInput{
		Name:        "Test Bundle",
		Description: "Test bundle for active revision",
	})
	if err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler(st)

	// No active revision initially
	_, err = compiler.GetActiveRevision(ctx, bundleID)
	if err == nil {
		t.Fatal("expected error for non-existent active revision")
	}

	// Create and publish a revision
	revisionID, err := compiler.CreateRevision(ctx, bundleID, "admin@example.com")
	if err != nil {
		t.Fatal(err)
	}

	err = compiler.PublishRevision(ctx, revisionID)
	if err != nil {
		t.Fatal(err)
	}

	// Should now have an active revision
	activeRev, err := compiler.GetActiveRevision(ctx, bundleID)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if activeRev.ID != revisionID {
		t.Errorf("expected active revision ID %q, got %q", revisionID, activeRev.ID)
	}
	if activeRev.Status != "published" {
		t.Errorf("expected status 'published', got %q", activeRev.Status)
	}
}

func TestDiffRevisions(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	bundleID, err := st.CreateBundle(ctx, domain.CreateBundleInput{
		Name:        "Test Bundle",
		Description: "Test bundle for diff",
	})
	if err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler(st)

	rev1, err := compiler.CreateRevision(ctx, bundleID, "admin@example.com")
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(10 * time.Millisecond)

	rev2, err := compiler.CreateRevision(ctx, bundleID, "admin@example.com")
	if err != nil {
		t.Fatal(err)
	}

	diff, err := compiler.DiffRevisions(ctx, rev1, rev2)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if diff == nil {
		t.Fatal("expected diff, got nil")
	}
	if diff.FromRevisionID != rev1 {
		t.Errorf("expected from revision %q, got %q", rev1, diff.FromRevisionID)
	}
	if diff.ToRevisionID != rev2 {
		t.Errorf("expected to revision %q, got %q", rev2, diff.ToRevisionID)
	}
}
