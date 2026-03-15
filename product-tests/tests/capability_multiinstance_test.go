package tests_test

// capability_multiinstance_test.go verifies that two or more oasclird
// runtime instances can operate concurrently while keeping all mutable
// filesystem state (audit log, state dir, cache dir) strictly per-instance.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/StevenBuglione/oas-cli-go/internal/runtime"
	"github.com/StevenBuglione/oas-cli-go/product-tests/tests/helpers"
)

// minimalOpenAPIYAML builds the smallest valid OpenAPI document that exposes a
// single GET /ping endpoint backed by the given server URL.
func minimalOpenAPIYAML(serverURL string) string {
	return `openapi: 3.1.0
info:
  title: Ping API
  version: "1.0.0"
servers:
  - url: ` + serverURL + `
paths:
  /ping:
    get:
      operationId: ping
      tags: [health]
      responses:
        "200":
          description: OK
  /data/{id}:
    parameters:
      - name: id
        in: path
        required: true
        schema: { type: string }
    get:
      operationId: getData
      tags: [data]
      responses:
        "200":
          description: OK
`
}

// minimalCLIConfig returns a .cli.json that sources the given OpenAPI file.
func minimalCLIConfig(openapiPath string) string {
	return `{
  "cli": "1.0.0",
  "mode": { "default": "discover" },
  "sources": {
    "src": {
      "type": "openapi",
      "uri": "` + openapiPath + `",
      "enabled": true
    }
  },
  "services": {
    "svc": {
      "source": "src",
      "alias": "svc"
    }
  }
}`
}

// newPingHandler returns a minimal HTTP handler that answers /ping and /data/{id}.
func newPingHandler(instanceLabel string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ping", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"instance":%q,"ok":true}`, instanceLabel)
	})
	mux.HandleFunc("/data/", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Path[len("/data/"):]
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"instance":%q,"id":%q}`, instanceLabel, id)
	})
	return mux
}

// setupInstance builds a fully isolated test setup: a backing API server, an
// OpenAPI spec, a .cli.json, and a runtime.Instance — all in their own dirs.
func setupInstance(t *testing.T, label string) (inst *helpers.Instance, configPath string) {
	t.Helper()

	api := httptest.NewServer(newPingHandler(label))
	t.Cleanup(api.Close)

	dir := t.TempDir()
	openapiPath := writeFile(t, dir, label+".openapi.yaml", minimalOpenAPIYAML(api.URL))
	configPath = writeFile(t, dir, label+".cli.json", minimalCLIConfig(openapiPath))

	inst = helpers.NewIsolatedInstance(t)
	return inst, configPath
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestMultiInstance_AuditLogIsolation proves that audit events written by
// instance A never appear in instance B's audit log, and vice-versa.
func TestMultiInstance_AuditLogIsolation(t *testing.T) {
	instA, cfgA := setupInstance(t, "alpha")
	instB, cfgB := setupInstance(t, "beta")

	// Execute one call on A and two calls on B.
	instA.ExecuteTool(t, cfgA, "svc:ping", nil)
	instB.ExecuteTool(t, cfgB, "svc:ping", nil)
	instB.ExecuteTool(t, cfgB, "svc:ping", nil)

	eventsA := instA.AuditEvents(t)
	eventsB := instB.AuditEvents(t)

	if len(eventsA) != 1 {
		t.Errorf("instance A: expected 1 audit event, got %d", len(eventsA))
	}
	if len(eventsB) != 2 {
		t.Errorf("instance B: expected 2 audit events, got %d", len(eventsB))
	}

	// Confirm A's audit path is a completely different file from B's.
	if instA.AuditPath == instB.AuditPath {
		t.Error("instances share the same AuditPath — isolation violated")
	}
}

// TestMultiInstance_StateDirIsolation proves that the state directories for two
// instances are distinct and do not share any filesystem prefix.
func TestMultiInstance_StateDirIsolation(t *testing.T) {
	instA, _ := setupInstance(t, "alpha")
	instB, _ := setupInstance(t, "beta")

	if instA.StateDir == instB.StateDir {
		t.Error("instances share the same StateDir — isolation violated")
	}
	if instA.CacheDir == instB.CacheDir {
		t.Error("instances share the same CacheDir — isolation violated")
	}

	// Verify each state dir (or its parent) exists under its own unique root.
	rootA := filepath.Dir(instA.StateDir)
	rootB := filepath.Dir(instB.StateDir)
	if rootA == rootB {
		t.Errorf("instances share the same root dir %q — isolation violated", rootA)
	}
}

// TestMultiInstance_NoAuditCrossContamination creates separate audit logs, writes
// to each independently, and confirms writes do not appear in the other file.
func TestMultiInstance_NoAuditCrossContamination(t *testing.T) {
	instA, cfgA := setupInstance(t, "alpha")
	instB, cfgB := setupInstance(t, "beta")

	// Record baseline counts (both should be empty).
	if c := instA.AuditEventCount(t); c != 0 {
		t.Fatalf("instance A baseline: expected 0 events, got %d", c)
	}
	if c := instB.AuditEventCount(t); c != 0 {
		t.Fatalf("instance B baseline: expected 0 events, got %d", c)
	}

	// Trigger three calls on A only.
	for range 3 {
		instA.ExecuteTool(t, cfgA, "svc:ping", nil)
	}

	// B must still have zero events.
	if c := instB.AuditEventCount(t); c != 0 {
		t.Errorf("after 3 calls on A: instance B still has %d events (expected 0)", c)
	}
	if c := instA.AuditEventCount(t); c != 3 {
		t.Errorf("instance A: expected 3 events, got %d", c)
	}

	// Trigger two calls on B — A must still have exactly 3.
	for range 2 {
		instB.ExecuteTool(t, cfgB, "svc:ping", nil)
	}
	if c := instA.AuditEventCount(t); c != 3 {
		t.Errorf("after 2 calls on B: instance A event count changed to %d (expected 3)", c)
	}
	if c := instB.AuditEventCount(t); c != 2 {
		t.Errorf("instance B: expected 2 events, got %d", c)
	}
}

// TestMultiInstance_ConcurrentRequests proves that multiple instances can
// handle requests simultaneously without interfering with each other.
func TestMultiInstance_ConcurrentRequests(t *testing.T) {
	const workers = 3
	const callsPerWorker = 5

	type setup struct {
		inst       *helpers.Instance
		configPath string
	}

	setups := make([]setup, workers)
	for i := range workers {
		inst, cfg := setupInstance(t, fmt.Sprintf("worker-%d", i))
		setups[i] = setup{inst: inst, configPath: cfg}
	}

	var wg sync.WaitGroup
	errors := make(chan string, workers*callsPerWorker)

	for i := range workers {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			s := setups[idx]
			for range callsPerWorker {
				result := s.inst.ExecuteTool(t, s.configPath, "svc:ping", nil)
				if sc, ok := result["statusCode"].(float64); !ok || int(sc) != 200 {
					errors <- fmt.Sprintf("worker %d: expected statusCode 200, got %v", idx, result)
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for msg := range errors {
		t.Error(msg)
	}

	// Each instance should have exactly callsPerWorker audit events.
	for i, s := range setups {
		if c := s.inst.AuditEventCount(t); c != callsPerWorker {
			t.Errorf("worker %d: expected %d audit events, got %d", i, callsPerWorker, c)
		}
	}
}

// TestMultiInstance_FilesystemStateDoesNotLeak verifies that a file written
// into instance A's state directory does not appear under instance B's state
// directory, and that the directories themselves exist independently.
func TestMultiInstance_FilesystemStateDoesNotLeak(t *testing.T) {
	instA, _ := setupInstance(t, "alpha")
	instB, _ := setupInstance(t, "beta")

	// Manually create a sentinel file inside A's state dir.
	if err := os.MkdirAll(instA.StateDir, 0o755); err != nil {
		t.Fatalf("mkdir A state dir: %v", err)
	}
	sentinel := filepath.Join(instA.StateDir, "sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("instance-a"), 0o644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	// The sentinel must NOT be reachable from B's state directory.
	shadowPath := filepath.Join(instB.StateDir, "sentinel.txt")
	if _, err := os.Stat(shadowPath); err == nil {
		t.Errorf("sentinel file is visible in instance B's state dir (%s) — leak detected", shadowPath)
	}

	// B's state dir must be on a completely different path.
	if instB.StateDir == instA.StateDir {
		t.Error("instances share the same StateDir path")
	}
}

func TestCapabilityLocalLifecycleExclusiveConflict(t *testing.T) {
	inst := helpers.NewLifecycleInstance(t, runtime.Options{
		HeartbeatSeconds:     15,
		MissedHeartbeatLimit: 3,
		ShutdownMode:         "when-owner-exits",
		SessionScope:         "terminal",
		ShareMode:            "exclusive",
		ConfigFingerprint:    "fp-1",
	})

	status, _, body := inst.Heartbeat(t, "sess-1", "fp-1")
	if status != http.StatusOK {
		t.Fatalf("expected first heartbeat 200, got %d (%s)", status, body)
	}
	status, _, body = inst.Heartbeat(t, "sess-2", "fp-1")
	if status != http.StatusConflict {
		t.Fatalf("expected conflicting heartbeat 409, got %d (%s)", status, body)
	}
	if body != "runtime_attach_conflict" {
		t.Fatalf("expected runtime_attach_conflict body, got %q", body)
	}
}

func TestCapabilityLocalLifecycleSharedGroupAllowsMultipleSessions(t *testing.T) {
	inst := helpers.NewLifecycleInstance(t, runtime.Options{
		HeartbeatSeconds:     15,
		MissedHeartbeatLimit: 3,
		ShutdownMode:         "manual",
		SessionScope:         "shared-group",
		ShareMode:            "group",
		ConfigFingerprint:    "fp-1",
	})

	status, payload, body := inst.Heartbeat(t, "sess-1", "fp-1")
	if status != http.StatusOK {
		t.Fatalf("expected first heartbeat 200, got %d (%s)", status, body)
	}
	if payload["activeSessions"] != float64(1) {
		t.Fatalf("expected one active session after first heartbeat, got %#v", payload)
	}
	status, payload, body = inst.Heartbeat(t, "sess-2", "fp-1")
	if status != http.StatusOK {
		t.Fatalf("expected second heartbeat 200, got %d (%s)", status, body)
	}
	if payload["activeSessions"] != float64(2) {
		t.Fatalf("expected two active sessions after second heartbeat, got %#v", payload)
	}
}

func TestCapabilityLocalLifecycleFingerprintMismatch(t *testing.T) {
	inst := helpers.NewLifecycleInstance(t, runtime.Options{
		HeartbeatSeconds:     15,
		MissedHeartbeatLimit: 3,
		ShutdownMode:         "when-owner-exits",
		SessionScope:         "terminal",
		ShareMode:            "exclusive",
		ConfigFingerprint:    "fp-1",
	})

	status, _, body := inst.Heartbeat(t, "sess-1", "fp-2")
	if status != http.StatusConflict {
		t.Fatalf("expected mismatched heartbeat 409, got %d (%s)", status, body)
	}
	if body != "runtime_attach_mismatch" {
		t.Fatalf("expected runtime_attach_mismatch body, got %q", body)
	}
}

func TestCapabilityLocalLifecycleManualRetentionAfterExpiry(t *testing.T) {
	inst := helpers.NewLifecycleInstance(t, runtime.Options{
		HeartbeatSeconds:     1,
		MissedHeartbeatLimit: 1,
		ShutdownMode:         "manual",
		SessionScope:         "shared-group",
		ShareMode:            "group",
		ConfigFingerprint:    "fp-1",
	})

	status, _, body := inst.Heartbeat(t, "sess-1", "fp-1")
	if status != http.StatusOK {
		t.Fatalf("expected heartbeat 200, got %d (%s)", status, body)
	}
	select {
	case <-inst.ShutdownSignal:
		t.Fatalf("expected manual runtime retention after lease expiry")
	case <-time.After(1500 * time.Millisecond):
	}
}
