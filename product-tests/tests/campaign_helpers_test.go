package tests_test

// campaign_helpers_test.go contains test fixtures and helpers shared
// across campaign test files.  Keeping them here makes the dependency
// explicit: capability tests that also happen to use these helpers pull
// them from this single, campaign-owned source rather than being silently
// coupled across unrelated files.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// ── REST fixture store ────────────────────────────────────────────────────────

type fixtureItem struct {
	ID   string   `json:"id"`
	Name string   `json:"name"`
	Tags []string `json:"tags,omitempty"`
}

type fixtureStore struct {
	mu     sync.Mutex
	items  map[string]*fixtureItem
	nextID int
}

func newFixtureStore() *fixtureStore {
	s := &fixtureStore{items: make(map[string]*fixtureItem), nextID: 4}
	for i := 1; i <= 3; i++ {
		id := fmt.Sprintf("item-%d", i)
		s.items[id] = &fixtureItem{ID: id, Name: fmt.Sprintf("Item %d", i)}
	}
	return s
}

func newRestFixtureHandler(store *fixtureStore) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/items", func(w http.ResponseWriter, r *http.Request) {
		store.mu.Lock()
		defer store.mu.Unlock()
		switch r.Method {
		case http.MethodGet:
			items := make([]*fixtureItem, 0, len(store.items))
			for _, it := range store.items {
				items = append(items, it)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": items, "total": len(items), "page": 1, "pageSize": 20, "totalPages": 1,
			})
		case http.MethodPost:
			var inp struct {
				Name string   `json:"name"`
				Tags []string `json:"tags"`
			}
			if err := json.NewDecoder(r.Body).Decode(&inp); err != nil || inp.Name == "" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			id := fmt.Sprintf("item-%d", store.nextID)
			store.nextID++
			it := &fixtureItem{ID: id, Name: inp.Name, Tags: inp.Tags}
			store.items[id] = it
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(it)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/items/", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Path[len("/items/"):]
		store.mu.Lock()
		defer store.mu.Unlock()
		switch r.Method {
		case http.MethodGet:
			it, ok := store.items[id]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(it)
		case http.MethodPut:
			var inp struct {
				Name string   `json:"name"`
				Tags []string `json:"tags"`
			}
			_ = json.NewDecoder(r.Body).Decode(&inp)
			it, ok := store.items[id]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			it.Name = inp.Name
			it.Tags = inp.Tags
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(it)
		case http.MethodDelete:
			_, ok := store.items[id]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			delete(store.items, id)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/errors/unauthorized", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"code": "unauthorized", "message": "authentication required"})
	})
	mux.HandleFunc("/errors/forbidden", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"code": "forbidden", "message": "access denied"})
	})
	mux.HandleFunc("/errors/rate-limited", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]string{"code": "too_many_requests", "message": "rate limit exceeded"})
	})
	mux.HandleFunc("/errors/internal", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"code": "internal_error", "message": "unexpected server error"})
	})

	var opMu sync.Mutex
	ops := map[string]map[string]any{}
	opNextID := 1
	mux.HandleFunc("/operations", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		opMu.Lock()
		id := fmt.Sprintf("op-%d", opNextID)
		opNextID++
		op := map[string]any{"id": id, "status": "running", "progress": 50}
		ops[id] = op
		opMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(op)
	})
	mux.HandleFunc("/operations/", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Path[len("/operations/"):]
		opMu.Lock()
		op, ok := ops[id]
		opMu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(op)
	})

	return mux
}

// ── File and HTTP test utilities ──────────────────────────────────────────────

// writeFile creates a file at dir/name with the given content and returns its path.
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	content = normalizeRuntimeFixture(name, content)
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func normalizeRuntimeFixture(name, content string) string {
	if !strings.HasSuffix(name, ".cli.json") {
		return content
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(content), &cfg); err != nil {
		return content
	}
	runtimeCfg, _ := cfg["runtime"].(map[string]any)
	if runtimeCfg == nil {
		runtimeCfg = map[string]any{}
		cfg["runtime"] = runtimeCfg
	}
	if _, ok := runtimeCfg["mode"]; !ok {
		runtimeCfg["mode"] = "local"
	}
	normalized, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return content
	}
	return string(normalized) + "\n"
}

// executeTool posts a /v1/tools/execute request to runtimeURL and returns the
// decoded JSON response body.
func executeTool(t *testing.T, runtimeURL, configPath, toolID string, extra map[string]any) map[string]any {
	t.Helper()
	payload := map[string]any{
		"configPath": configPath,
		"toolId":     toolID,
	}
	for k, v := range extra {
		payload[k] = v
	}
	b, _ := json.Marshal(payload)
	resp, err := http.Post(runtimeURL+"/v1/tools/execute", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("execute %s: %v", toolID, err)
	}
	defer resp.Body.Close()
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response for %s: %v", toolID, err)
	}
	return result
}

// ── OAuth fixture helpers ─────────────────────────────────────────────────────

// oauthOpenAPIYAML returns a minimal OpenAPI spec with OAuth2 clientCredentials
// security pointing at tokenURL, served at serverURL.
func oauthOpenAPIYAML(serverURL, tokenURL string) string {
	return `openapi: 3.1.0
info:
  title: Protected API
  version: "1.0.0"
servers:
  - url: ` + serverURL + `
security:
  - testapi_oauth: [api.read]
components:
  securitySchemes:
    testapi_oauth:
      type: oauth2
      flows:
        clientCredentials:
          tokenUrl: ` + tokenURL + `
          scopes:
            api.read: Read access
            api.write: Write access
paths:
  /items:
    get:
      operationId: listItems
      tags: [items]
      responses:
        "200":
          description: OK
`
}

// oauthCLIConfig returns a CLI config JSON that references openapiPath and
// configures OAuth2 clientCredentials secrets from env vars TEST_CLIENT_ID /
// TEST_CLIENT_SECRET.
func oauthCLIConfig(openapiPath, clientID, clientSecret string) string {
	return `{
  "cli": "1.0.0",
  "mode": { "default": "discover" },
  "runtime": {
    "mode": "remote"
  },
  "sources": {
    "protectedSource": {
      "type": "openapi",
      "uri": "` + openapiPath + `",
      "enabled": true
    }
  },
  "services": {
    "protected": {
      "source": "protectedSource",
      "alias": "protected"
    }
  },
  "secrets": {
    "protected.testapi_oauth": {
      "type": "oauth2",
      "mode": "clientCredentials",
      "clientId": {
        "type": "env",
        "value": "TEST_CLIENT_ID"
      },
      "clientSecret": {
        "type": "env",
        "value": "TEST_CLIENT_SECRET"
      }
    }
  }
}`
}
