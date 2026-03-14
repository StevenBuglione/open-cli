// Package helpers provides utilities for product-test scenarios that
// require fully-isolated oasclird runtime instances.
package helpers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/StevenBuglione/oas-cli-go/internal/runtime"
	"github.com/StevenBuglione/oas-cli-go/pkg/audit"
)

// Instance wraps a runtime.Server backed by an httptest.Server with fully
// isolated filesystem directories for audit logs, state, and HTTP cache.
type Instance struct {
	URL       string
	AuditPath string
	StateDir  string
	CacheDir  string
}

// NewIsolatedInstance creates a runtime.Server with a unique temp dir hierarchy
// so that two instances created within the same test never share any filesystem
// state.  The httptest.Server is closed automatically via t.Cleanup.
func NewIsolatedInstance(t *testing.T) *Instance {
	t.Helper()

	root := t.TempDir()
	auditPath := filepath.Join(root, "audit.log")
	stateDir := filepath.Join(root, "state")
	cacheDir := filepath.Join(root, "cache")

	srv := runtime.NewServer(runtime.Options{
		AuditPath: auditPath,
		StateDir:  stateDir,
		CacheDir:  cacheDir,
	})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	return &Instance{
		URL:       ts.URL,
		AuditPath: auditPath,
		StateDir:  stateDir,
		CacheDir:  cacheDir,
	}
}

// ExecuteTool sends a /v1/tools/execute request to this instance.
func (inst *Instance) ExecuteTool(t *testing.T, configPath, toolID string, extra map[string]any) map[string]any {
	t.Helper()

	payload := map[string]any{
		"configPath": configPath,
		"toolId":     toolID,
	}
	for k, v := range extra {
		payload[k] = v
	}
	body, _ := json.Marshal(payload)
	resp, err := http.Post(inst.URL+"/v1/tools/execute", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("ExecuteTool %s on %s: %v", toolID, inst.URL, err)
	}
	defer resp.Body.Close()

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode execute response for %s: %v", toolID, err)
	}
	return result
}

// AuditEvents reads the audit log directly from the filesystem (bypassing the
// HTTP endpoint) so tests can inspect events without being coupled to the API.
func (inst *Instance) AuditEvents(t *testing.T) []audit.Event {
	t.Helper()
	store := audit.NewFileStore(inst.AuditPath)
	events, err := store.List()
	if err != nil {
		t.Fatalf("read audit events from %s: %v", inst.AuditPath, err)
	}
	return events
}

// AuditEventCount returns the number of audit events recorded by this instance.
func (inst *Instance) AuditEventCount(t *testing.T) int {
	t.Helper()
	return len(inst.AuditEvents(t))
}
