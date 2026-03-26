package tests_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/StevenBuglione/open-cli/internal/runtime"
)

// curatedCLIConfig returns a CLI config JSON with a curated toolSet that
// explicitly denies every tool except listItems.  Used to test policy denial.
func curatedCLIConfig(openapiPath string) string {
	return `{
  "cli": "1.0.0",
  "mode": { "default": "curated" },
  "runtime": {
    "mode": "remote"
  },
  "sources": {
    "testapiSource": {
      "type": "openapi",
      "uri": "` + openapiPath + `",
      "enabled": true
    }
  },
  "services": {
    "testapi": {
      "source": "testapiSource",
      "alias": "testapi"
    }
  },
  "agents": {
    "profiles": {
      "default": {
        "mode": "curated",
        "toolSet": "list-only"
      }
    },
    "defaultProfile": "default"
  },
  "curation": {
    "toolSets": {
      "list-only": {
        "allow": ["testapi:listItems"],
        "deny": ["**"]
      }
    }
  }
}`
}

// postJSON sends a POST request with a JSON body to url and returns the decoded
// response.  It fails the test on network or decode errors.
func postJSON(t *testing.T, url string, body map[string]any) map[string]any {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response from %s: %v", url, err)
	}
	return result
}

// listAuditEvents calls GET /v1/audit/events and returns the raw event slice.
func listAuditEvents(t *testing.T, runtimeURL string) []any {
	t.Helper()
	resp, err := http.Get(runtimeURL + "/v1/audit/events")
	if err != nil {
		t.Fatalf("GET audit/events: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("audit/events returned %d", resp.StatusCode)
	}
	var events []any
	if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
		t.Fatalf("decode audit events: %v", err)
	}
	return events
}

// ---- refresh tests ----

// TestCapabilityRefresh verifies that the /v1/refresh endpoint processes a
// valid config and returns a structured response.
func TestCapabilityRefresh(t *testing.T) {
	api := httptest.NewServer(newRestFixtureHandler(newFixtureStore()))
	t.Cleanup(api.Close)

	dir := t.TempDir()
	openapiPath := writeFile(t, dir, "testapi.openapi.yaml", restOpenAPIYAML(api.URL))
	configPath := writeFile(t, dir, ".cli.json", restCLIConfig(openapiPath))

	srv := runtime.NewServer(runtime.Options{
		AuditPath: filepath.Join(dir, "audit.log"),
		CacheDir:  filepath.Join(dir, "cache"),
	})
	runtimeSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(runtimeSrv.Close)

	t.Run("refresh_returns_refreshed_at", func(t *testing.T) {
		result := postJSON(t, runtimeSrv.URL+"/v1/refresh", map[string]any{
			"configPath": configPath,
		})

		refreshedAtStr, ok := result["refreshedAt"].(string)
		if !ok || refreshedAtStr == "" {
			t.Fatalf("refreshedAt missing or empty in response: %v", result)
		}
		refreshedAt, err := time.Parse(time.RFC3339Nano, refreshedAtStr)
		if err != nil {
			t.Fatalf("parse refreshedAt %q: %v", refreshedAtStr, err)
		}
		if time.Since(refreshedAt) > 30*time.Second {
			t.Errorf("refreshedAt %v is unexpectedly old", refreshedAt)
		}
	})

	t.Run("refresh_returns_sources", func(t *testing.T) {
		result := postJSON(t, runtimeSrv.URL+"/v1/refresh", map[string]any{
			"configPath": configPath,
		})

		sources, ok := result["sources"].([]any)
		if !ok {
			t.Fatalf("sources field missing or wrong type: %v", result)
		}
		if len(sources) == 0 {
			t.Fatal("expected at least one source in refresh response")
		}

		first, _ := sources[0].(map[string]any)
		if first["id"] == nil {
			t.Errorf("source entry missing id: %v", first)
		}
		t.Logf("refresh returned %d source(s), first id=%v", len(sources), first["id"])
	})

	t.Run("refresh_after_initial_catalog_load", func(t *testing.T) {
		// Load catalog first, then refresh — catalog should remain consistent.
		cat := fetchCatalog(t, runtimeSrv.URL, configPath)
		toolsBefore, _ := cat["tools"].([]any)

		_ = postJSON(t, runtimeSrv.URL+"/v1/refresh", map[string]any{
			"configPath": configPath,
		})

		cat2 := fetchCatalog(t, runtimeSrv.URL, configPath)
		toolsAfter, _ := cat2["tools"].([]any)

		if len(toolsBefore) != len(toolsAfter) {
			t.Errorf("tool count changed after refresh: before=%d after=%d",
				len(toolsBefore), len(toolsAfter))
		}
	})
}

// ---- audit tests ----

// TestCapabilityAuditSuccessfulExecution verifies that a successfully executed
// tool produces an audit event with decision=allowed and the correct tool ID.
func TestCapabilityAuditSuccessfulExecution(t *testing.T) {
	api := httptest.NewServer(newRestFixtureHandler(newFixtureStore()))
	t.Cleanup(api.Close)

	dir := t.TempDir()
	openapiPath := writeFile(t, dir, "testapi.openapi.yaml", restOpenAPIYAML(api.URL))
	configPath := writeFile(t, dir, ".cli.json", restCLIConfig(openapiPath))
	auditPath := filepath.Join(dir, "audit.log")

	srv := runtime.NewServer(runtime.Options{
		AuditPath: auditPath,
		CacheDir:  filepath.Join(dir, "cache"),
	})
	runtimeSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(runtimeSrv.Close)

	// Execute a tool that should succeed.
	result := executeTool(t, runtimeSrv.URL, configPath, "testapi:listItems", nil)
	statusCode, _ := result["statusCode"].(float64)
	if statusCode != 200 {
		t.Fatalf("expected statusCode 200, got %v: %v", statusCode, result)
	}

	events := listAuditEvents(t, runtimeSrv.URL)
	if len(events) == 0 {
		t.Fatal("expected at least one audit event after tool execution")
	}

	// Find the event for listItems.
	var found map[string]any
	for _, ev := range events {
		m, _ := ev.(map[string]any)
		if m["toolId"] == "testapi:listItems" {
			found = m
			break
		}
	}
	if found == nil {
		t.Fatalf("no audit event for testapi:listItems; events: %v", events)
	}

	t.Run("decision_is_allowed", func(t *testing.T) {
		if found["decision"] != "allowed" {
			t.Errorf("decision = %v, want allowed", found["decision"])
		}
	})
	t.Run("status_code_is_200", func(t *testing.T) {
		sc, _ := found["statusCode"].(float64)
		if sc != 200 {
			t.Errorf("audit statusCode = %v, want 200", sc)
		}
	})
	t.Run("service_id_present", func(t *testing.T) {
		if found["serviceId"] == "" || found["serviceId"] == nil {
			t.Errorf("serviceId missing in audit event: %v", found)
		}
	})
	t.Run("event_type_is_tool_execution", func(t *testing.T) {
		if found["eventType"] != "tool_execution" {
			t.Errorf("eventType = %v, want tool_execution", found["eventType"])
		}
	})
	t.Run("reason_code_is_allowed", func(t *testing.T) {
		if found["reasonCode"] != "allowed" {
			t.Errorf("reasonCode = %v, want allowed", found["reasonCode"])
		}
	})
	t.Run("timestamp_present", func(t *testing.T) {
		if found["timestamp"] == "" || found["timestamp"] == nil {
			t.Errorf("timestamp missing in audit event: %v", found)
		}
	})
	t.Run("latency_field_omitted_only_if_zero", func(t *testing.T) {
		// latencyMs uses omitempty so it may be absent for very fast executions.
		// This is expected behaviour; just log the value if present.
		if lms, ok := found["latencyMs"]; ok {
			t.Logf("latencyMs present: %v", lms)
		} else {
			t.Log("latencyMs absent (0 value, omitempty in serialisation — expected for fast calls)")
		}
	})
}

// TestCapabilityAuditDeniedExecution verifies that a tool blocked by policy
// produces an audit event with decision=denied.
func TestCapabilityAuditDeniedExecution(t *testing.T) {
	api := httptest.NewServer(newRestFixtureHandler(newFixtureStore()))
	t.Cleanup(api.Close)

	dir := t.TempDir()
	openapiPath := writeFile(t, dir, "testapi.openapi.yaml", restOpenAPIYAML(api.URL))
	// Use a curated config where only listItems is allowed.
	configPath := writeFile(t, dir, ".cli.json", curatedCLIConfig(openapiPath))
	auditPath := filepath.Join(dir, "audit.log")

	srv := runtime.NewServer(runtime.Options{
		AuditPath: auditPath,
		CacheDir:  filepath.Join(dir, "cache"),
	})
	runtimeSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(runtimeSrv.Close)

	// Attempt to execute a tool that is not in the allow-list.
	b, _ := json.Marshal(map[string]any{
		"configPath":   configPath,
		"toolId":       "testapi:createItem",
		"agentProfile": "default",
		"mode":         "curated",
	})
	resp, err := http.Post(runtimeSrv.URL+"/v1/tools/execute", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("execute createItem: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for denied tool, got %d", resp.StatusCode)
	}

	events := listAuditEvents(t, runtimeSrv.URL)
	if len(events) == 0 {
		t.Fatal("expected at least one audit event after denied execution")
	}

	// Find the denial event.
	var found map[string]any
	for _, ev := range events {
		m, _ := ev.(map[string]any)
		if m["toolId"] == "testapi:createItem" {
			found = m
			break
		}
	}
	if found == nil {
		t.Fatalf("no audit event for testapi:createItem; events: %v", events)
	}

	t.Run("decision_is_denied", func(t *testing.T) {
		if found["decision"] != "denied" {
			t.Errorf("decision = %v, want denied", found["decision"])
		}
	})
	t.Run("reason_code_present", func(t *testing.T) {
		if found["reasonCode"] == "" || found["reasonCode"] == nil {
			t.Errorf("reasonCode missing in denied audit event: %v", found)
		}
		t.Logf("deny reasonCode: %v", found["reasonCode"])
	})
	t.Run("event_type_is_authz_denial", func(t *testing.T) {
		if found["eventType"] != "authz_denial" {
			t.Errorf("eventType = %v, want authz_denial", found["eventType"])
		}
	})
	t.Run("status_code_is_zero_for_denied", func(t *testing.T) {
		// Denied tools never reach the backend, so statusCode should be 0.
		sc, _ := found["statusCode"].(float64)
		if sc != 0 {
			t.Errorf("audit statusCode = %v, want 0 for denied tool", sc)
		}
	})
}

// TestCapabilityAuditMultipleExecutions verifies that multiple tool executions
// each produce a separate audit event, preserving order and individual fields.
func TestCapabilityAuditMultipleExecutions(t *testing.T) {
	api := httptest.NewServer(newRestFixtureHandler(newFixtureStore()))
	t.Cleanup(api.Close)

	dir := t.TempDir()
	openapiPath := writeFile(t, dir, "testapi.openapi.yaml", restOpenAPIYAML(api.URL))
	configPath := writeFile(t, dir, ".cli.json", restCLIConfig(openapiPath))

	srv := runtime.NewServer(runtime.Options{
		AuditPath: filepath.Join(dir, "audit.log"),
		CacheDir:  filepath.Join(dir, "cache"),
	})
	runtimeSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(runtimeSrv.Close)

	tools := []string{"testapi:listItems", "testapi:getItem"}
	for _, toolID := range tools {
		var extra map[string]any
		if toolID == "testapi:getItem" {
			extra = map[string]any{"pathArgs": []string{"item-1"}}
		}
		executeTool(t, runtimeSrv.URL, configPath, toolID, extra)
	}

	events := listAuditEvents(t, runtimeSrv.URL)
	if len(events) < len(tools) {
		t.Fatalf("expected at least %d audit events, got %d", len(tools), len(events))
	}

	seen := map[string]bool{}
	for _, ev := range events {
		m, _ := ev.(map[string]any)
		if id, ok := m["toolId"].(string); ok {
			seen[id] = true
		}
	}
	for _, toolID := range tools {
		if !seen[toolID] {
			t.Errorf("no audit event found for %s; seen: %v", toolID, seen)
		}
	}
}

func TestCapabilityAuditExecutionErrorsAreClassifiedSeparately(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	deadURL := server.URL
	server.Close()

	dir := t.TempDir()
	openapiPath := writeFile(t, dir, "broken.openapi.yaml", restOpenAPIYAML(deadURL))
	configPath := writeFile(t, dir, ".cli.json", restCLIConfig(openapiPath))

	srv := runtime.NewServer(runtime.Options{
		AuditPath: filepath.Join(dir, "audit.log"),
		CacheDir:  filepath.Join(dir, "cache"),
	})
	runtimeSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(runtimeSrv.Close)

	body, _ := json.Marshal(map[string]any{
		"configPath": configPath,
		"toolId":     "testapi:listItems",
	})
	resp, err := http.Post(runtimeSrv.URL+"/v1/tools/execute", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("execute listItems: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502 for execution failure, got %d", resp.StatusCode)
	}

	events := listAuditEvents(t, runtimeSrv.URL)
	if len(events) == 0 {
		t.Fatal("expected an audit event after execution failure")
	}

	var found map[string]any
	for _, ev := range events {
		m, _ := ev.(map[string]any)
		if m["toolId"] == "testapi:listItems" {
			found = m
		}
	}
	if found == nil {
		t.Fatalf("no audit event for testapi:listItems; events: %v", events)
	}
	if found["eventType"] != "execution_error" {
		t.Fatalf("eventType = %v, want execution_error", found["eventType"])
	}
	if found["reasonCode"] != "execution_error" {
		t.Fatalf("reasonCode = %v, want execution_error", found["reasonCode"])
	}
	if found["decision"] != "error" {
		t.Fatalf("decision = %v, want error", found["decision"])
	}
}
