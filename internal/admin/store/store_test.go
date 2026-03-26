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

func TestStoreCreatesSourceBundleAndAssignment(t *testing.T) {
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
}
