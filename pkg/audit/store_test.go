package audit_test

import (
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/StevenBuglione/open-cli/pkg/audit"
)

func TestFileStoreListEmptyWhenNoFile(t *testing.T) {
	dir := t.TempDir()
	store := audit.NewFileStore(filepath.Join(dir, "audit.jsonl"))
	events, err := store.List()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected empty, got %d events", len(events))
	}
}

func TestFileStoreAppendAndList(t *testing.T) {
	dir := t.TempDir()
	store := audit.NewFileStore(filepath.Join(dir, "audit.jsonl"))

	events := []audit.Event{
		{
			Timestamp: time.Now(),
			Principal: "subagent:triage-01",
			Lineage: &audit.DelegationLineage{
				DelegatedBy:  "github:user-123",
				DelegationID: "delegation-123",
				Actor: map[string]string{
					"sub":       "github:user-123",
					"client_id": "ocli-browser",
				},
			},
			ToolID:     "svc:op1",
			Decision:   "allow",
			ReasonCode: "allowed",
		},
		{Timestamp: time.Now(), ToolID: "svc:op2", Decision: "deny", ReasonCode: "managed_deny"},
	}
	for _, e := range events {
		if err := store.Append(e); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	got, err := store.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != len(events) {
		t.Fatalf("expected %d events, got %d", len(events), len(got))
	}
	for i, e := range events {
		if got[i].ToolID != e.ToolID || got[i].Decision != e.Decision {
			t.Errorf("event %d mismatch: got %+v, want %+v", i, got[i], e)
		}
		if !reflect.DeepEqual(got[i].Lineage, e.Lineage) {
			t.Errorf("event %d lineage mismatch: got %+v, want %+v", i, got[i].Lineage, e.Lineage)
		}
	}
}

func TestFileStorePathReturnsConfiguredPath(t *testing.T) {
	store := audit.NewFileStore("/tmp/test-audit.jsonl")
	if store.Path() != "/tmp/test-audit.jsonl" {
		t.Fatalf("unexpected path: %s", store.Path())
	}
}

func TestFileStoreNilPathReturnsEmpty(t *testing.T) {
	var store *audit.FileStore
	if store.Path() != "" {
		t.Fatalf("expected empty path for nil store")
	}
}

func TestFileStoreCreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "a", "b", "c", "audit.jsonl")
	store := audit.NewFileStore(nested)
	if err := store.Append(audit.Event{ToolID: "x", Decision: "allow", ReasonCode: "allowed"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if _, err := os.Stat(nested); err != nil {
		t.Fatalf("file not created: %v", err)
	}
}

func TestFileStoreConcurrentAppend(t *testing.T) {
	dir := t.TempDir()
	store := audit.NewFileStore(filepath.Join(dir, "audit.jsonl"))
	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_ = store.Append(audit.Event{ToolID: "svc:op", Decision: "allow", ReasonCode: "allowed"})
		}()
	}
	wg.Wait()
	events, err := store.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(events) != n {
		t.Fatalf("expected %d events, got %d", n, len(events))
	}
}
