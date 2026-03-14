package tests_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"

	"github.com/StevenBuglione/oas-cli-go/internal/runtime"
)

// ---- catalog snapshot types for deterministic golden comparison ----

// catalogSnapshot contains only stable, non-volatile catalog fields.
// Timestamps, fingerprints, server URLs, and fetch provenance are excluded.
type catalogSnapshot struct {
	CatalogVersion string            `json:"catalogVersion"`
	Sources        []sourceSnapshot  `json:"sources"`
	Services       []serviceSnapshot `json:"services"`
	Tools          []toolSnapshot    `json:"tools"`
}

type sourceSnapshot struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

type serviceSnapshot struct {
	ID       string `json:"id"`
	Alias    string `json:"alias"`
	SourceID string `json:"sourceId"`
}

type toolSnapshot struct {
	ID        string `json:"id"`
	ServiceID string `json:"serviceId"`
	Method    string `json:"method"`
	Path      string `json:"path"`
	Group     string `json:"group"`
	Command   string `json:"command"`
}

// snapshotFromCatalog extracts a stable snapshot from a raw catalog JSON map.
func snapshotFromCatalog(raw map[string]any) catalogSnapshot {
	snap := catalogSnapshot{}

	if v, ok := raw["catalogVersion"].(string); ok {
		snap.CatalogVersion = v
	}

	if sources, ok := raw["sources"].([]any); ok {
		for _, s := range sources {
			m, _ := s.(map[string]any)
			snap.Sources = append(snap.Sources, sourceSnapshot{
				ID:   stringField(m, "id"),
				Type: stringField(m, "type"),
			})
		}
		sort.Slice(snap.Sources, func(i, j int) bool { return snap.Sources[i].ID < snap.Sources[j].ID })
	}

	if services, ok := raw["services"].([]any); ok {
		for _, s := range services {
			m, _ := s.(map[string]any)
			snap.Services = append(snap.Services, serviceSnapshot{
				ID:       stringField(m, "id"),
				Alias:    stringField(m, "alias"),
				SourceID: stringField(m, "sourceId"),
			})
		}
		sort.Slice(snap.Services, func(i, j int) bool { return snap.Services[i].ID < snap.Services[j].ID })
	}

	if tools, ok := raw["tools"].([]any); ok {
		for _, t := range tools {
			m, _ := t.(map[string]any)
			snap.Tools = append(snap.Tools, toolSnapshot{
				ID:        stringField(m, "id"),
				ServiceID: stringField(m, "serviceId"),
				Method:    stringField(m, "method"),
				Path:      stringField(m, "path"),
				Group:     stringField(m, "group"),
				Command:   stringField(m, "command"),
			})
		}
		sort.Slice(snap.Tools, func(i, j int) bool { return snap.Tools[i].ID < snap.Tools[j].ID })
	}

	return snap
}

func stringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, _ := m[key].(string)
	return v
}

// compareOrUpdateGolden compares snap to the golden file at path.
// If UPDATE_GOLDEN=1 is set (or the file does not yet exist), the golden file
// is written from snap and the test passes.
func compareOrUpdateGolden(t *testing.T, goldenPath string, snap catalogSnapshot) {
	t.Helper()

	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}

	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("mkdir golden dir: %v", err)
		}
		if err := os.WriteFile(goldenPath, append(data, '\n'), 0o644); err != nil {
			t.Fatalf("write golden file: %v", err)
		}
		t.Logf("golden file updated: %s", goldenPath)
		return
	}

	goldenBytes, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden file %s: %v\n(set UPDATE_GOLDEN=1 to create it)", goldenPath, err)
	}

	var want catalogSnapshot
	if err := json.Unmarshal(goldenBytes, &want); err != nil {
		t.Fatalf("unmarshal golden file: %v", err)
	}

	// Re-marshal golden snapshot to normalise formatting before comparing.
	wantData, _ := json.MarshalIndent(want, "", "  ")
	if string(data) != string(wantData) {
		t.Errorf("catalog snapshot does not match golden file %s\ngot:\n%s\nwant:\n%s",
			goldenPath, data, wantData)
	}
}

// goldenPath returns the absolute path to a golden fixture file relative to the
// product-tests/testdata/expected directory.
func goldenPath(t *testing.T, name string) string {
	t.Helper()
	// Walk up from this file's directory to find the product-tests root.
	dir, err := filepath.Abs(filepath.Join("..", "testdata", "expected"))
	if err != nil {
		t.Fatalf("resolve golden dir: %v", err)
	}
	return filepath.Join(dir, name)
}

// fetchCatalog calls GET /v1/catalog/effective on the given runtime server and
// returns the parsed JSON response body under the "catalog" key.
func fetchCatalog(t *testing.T, runtimeURL, configPath string) map[string]any {
	t.Helper()
	resp, err := http.Get(runtimeURL + "/v1/catalog/effective?config=" + configPath)
	if err != nil {
		t.Fatalf("GET catalog: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET catalog returned %d", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode catalog response: %v", err)
	}
	cat, ok := body["catalog"].(map[string]any)
	if !ok {
		t.Fatalf("catalog field missing or wrong type in response: %v", body)
	}
	return cat
}

// ---- REST catalog tests ----

// TestCapabilityCatalogREST verifies that the runtime correctly builds a catalog
// from an OpenAPI source and that the catalog structure matches the golden file.
func TestCapabilityCatalogREST(t *testing.T) {
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

	t.Run("catalog_structure", func(t *testing.T) {
		cat := fetchCatalog(t, runtimeSrv.URL, configPath)

		if cat["catalogVersion"] != "1.0.0" {
			t.Errorf("catalogVersion = %v, want 1.0.0", cat["catalogVersion"])
		}
		tools, _ := cat["tools"].([]any)
		if len(tools) == 0 {
			t.Fatal("expected tools in catalog")
		}
		services, _ := cat["services"].([]any)
		if len(services) == 0 {
			t.Fatal("expected services in catalog")
		}
		sources, _ := cat["sources"].([]any)
		if len(sources) == 0 {
			t.Fatal("expected sources in catalog")
		}
		t.Logf("catalog: %d tools, %d services, %d sources", len(tools), len(services), len(sources))
	})

	t.Run("catalog_tool_ids", func(t *testing.T) {
		cat := fetchCatalog(t, runtimeSrv.URL, configPath)
		snap := snapshotFromCatalog(cat)

		expectedIDs := []string{
			"testapi:createItem",
			"testapi:createOperation",
			"testapi:deleteItem",
			"testapi:getItem",
			"testapi:getOperation",
			"testapi:listItems",
			"testapi:triggerForbidden",
			"testapi:triggerInternalError",
			"testapi:triggerRateLimited",
			"testapi:triggerUnauthorized",
			"testapi:updateItem",
		}
		gotIDs := make([]string, len(snap.Tools))
		for i, tool := range snap.Tools {
			gotIDs[i] = tool.ID
		}
		if len(gotIDs) != len(expectedIDs) {
			t.Errorf("tool count: got %d, want %d\ngot IDs: %v\nwant IDs: %v",
				len(gotIDs), len(expectedIDs), gotIDs, expectedIDs)
		}
		for i, id := range expectedIDs {
			if i >= len(gotIDs) {
				break
			}
			if gotIDs[i] != id {
				t.Errorf("tool[%d]: got %q, want %q", i, gotIDs[i], id)
			}
		}
	})

	t.Run("catalog_golden", func(t *testing.T) {
		cat := fetchCatalog(t, runtimeSrv.URL, configPath)
		snap := snapshotFromCatalog(cat)
		compareOrUpdateGolden(t, goldenPath(t, "catalog-rest.json"), snap)
	})
}

// ---- MCP catalog tests ----

// mcpCLIConfig returns a CLI config JSON that uses the filesystem MCP server
// via npx stdio transport.
func mcpCLIConfig(serverURL string) string {
	return `{
  "cli": "1.0.0",
  "mode": { "default": "discover" },
  "mcpServers": {
    "filesystem": {
      "type": "stdio",
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
    }
  }
}`
}

// TestCapabilityCatalogMCP verifies that the runtime correctly builds a catalog
// from an MCP stdio source.  Requires npx and Node.js.
func TestCapabilityCatalogMCP(t *testing.T) {
	if _, err := exec.LookPath("npx"); err != nil {
		t.Skip("npx required for MCP catalog product test: install Node.js")
	}

	dir := t.TempDir()
	configPath := writeFile(t, dir, ".cli.json", mcpCLIConfig(""))

	srv := runtime.NewServer(runtime.Options{
		AuditPath: filepath.Join(dir, "audit.log"),
		CacheDir:  filepath.Join(dir, "cache"),
		StateDir:  filepath.Join(dir, "state"),
	})
	runtimeSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(runtimeSrv.Close)

	t.Run("catalog_structure", func(t *testing.T) {
		cat := fetchCatalog(t, runtimeSrv.URL, configPath)

		tools, _ := cat["tools"].([]any)
		if len(tools) == 0 {
			t.Fatal("expected tools in MCP catalog")
		}
		services, _ := cat["services"].([]any)
		if len(services) == 0 {
			t.Fatal("expected services in MCP catalog")
		}
		t.Logf("MCP catalog: %d tools, %d services", len(tools), len(services))
	})

	t.Run("catalog_contains_list_directory", func(t *testing.T) {
		cat := fetchCatalog(t, runtimeSrv.URL, configPath)
		snap := snapshotFromCatalog(cat)

		var found bool
		for _, tool := range snap.Tools {
			if tool.ID == "filesystem:list_directory" {
				found = true
				break
			}
		}
		if !found {
			ids := make([]string, len(snap.Tools))
			for i, tool := range snap.Tools {
				ids[i] = tool.ID
			}
			t.Errorf("expected filesystem:list_directory in catalog, got: %v", ids)
		}
	})

	t.Run("catalog_service_is_filesystem", func(t *testing.T) {
		cat := fetchCatalog(t, runtimeSrv.URL, configPath)
		snap := snapshotFromCatalog(cat)

		if len(snap.Services) == 0 {
			t.Fatal("expected at least one service")
		}
		if snap.Services[0].ID != "filesystem" {
			t.Errorf("service ID = %q, want %q", snap.Services[0].ID, "filesystem")
		}
	})

	t.Run("catalog_golden", func(t *testing.T) {
		cat := fetchCatalog(t, runtimeSrv.URL, configPath)
		snap := snapshotFromCatalog(cat)
		compareOrUpdateGolden(t, goldenPath(t, "catalog-mcp.json"), snap)
	})
}
