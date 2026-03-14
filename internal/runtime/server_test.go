package runtime_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/StevenBuglione/oas-cli-go/internal/runtime"
	"github.com/StevenBuglione/oas-cli-go/pkg/obs"
)

func writeRuntimeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func TestServerEnforcesCuratedViewExecutesAllowedToolsAndAuditsAttempts(t *testing.T) {
	dir := t.TempDir()
	var deleteCalls int

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{"id": "T-1"}}})
		case http.MethodDelete:
			deleteCalls++
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer api.Close()

	writeRuntimeFile(t, dir, "tickets.openapi.yaml", `
openapi: 3.1.0
info:
  title: Tickets API
  version: "1.0.0"
servers:
  - url: `+api.URL+`
paths:
  /tickets:
    get:
      operationId: listTickets
      tags: [tickets]
      parameters:
        - name: status
          in: query
          schema: { type: string }
      responses:
        "200":
          description: OK
  /tickets/{id}:
    delete:
      operationId: deleteTicket
      tags: [tickets]
      parameters:
        - name: id
          in: path
          required: true
          schema: { type: string }
      responses:
        "204":
          description: Deleted
`)
	configPath := writeRuntimeFile(t, dir, ".cli.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "sources": {
	    "ticketsSource": {
	      "type": "openapi",
	      "uri": "./tickets.openapi.yaml",
	      "enabled": true
	    }
	  },
	  "services": {
	    "tickets": {
	      "source": "ticketsSource",
	      "alias": "tickets"
	    }
	  },
	  "curation": {
	    "toolSets": {
	      "sandbox": {
	        "allow": ["tickets:listTickets"],
	        "deny": ["**"]
	      }
	    }
	  },
	  "agents": {
	    "profiles": {
	      "sandbox": {
	        "mode": "curated",
	        "toolSet": "sandbox"
	      }
	    },
	    "defaultProfile": "sandbox"
	  }
	}`)

	server := runtime.NewServer(runtime.Options{AuditPath: filepath.Join(dir, "audit.log")})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	resp, err := http.Get(httpServer.URL + "/v1/catalog/effective?config=" + configPath + "&agentProfile=sandbox")
	if err != nil {
		t.Fatalf("get effective catalog: %v", err)
	}
	defer resp.Body.Close()
	var effective map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&effective); err != nil {
		t.Fatalf("decode effective catalog: %v", err)
	}
	view := effective["view"].(map[string]any)
	tools := view["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("expected one curated tool, got %#v", tools)
	}

	denyBody := bytes.NewBufferString(`{
	  "configPath": "` + configPath + `",
	  "agentProfile": "sandbox",
	  "toolId": "tickets:deleteTicket",
	  "pathArgs": ["T-1"]
	}`)
	denyResp, err := http.Post(httpServer.URL+"/v1/tools/execute", "application/json", denyBody)
	if err != nil {
		t.Fatalf("deny execute request: %v", err)
	}
	if denyResp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for denied tool, got %d", denyResp.StatusCode)
	}
	if deleteCalls != 0 {
		t.Fatalf("expected denied tool not to hit upstream, got %d delete calls", deleteCalls)
	}

	allowBody := bytes.NewBufferString(`{
	  "configPath": "` + configPath + `",
	  "agentProfile": "sandbox",
	  "toolId": "tickets:listTickets",
	  "flags": { "status": "open" }
	}`)
	allowResp, err := http.Post(httpServer.URL+"/v1/tools/execute", "application/json", allowBody)
	if err != nil {
		t.Fatalf("allow execute request: %v", err)
	}
	defer allowResp.Body.Close()
	if allowResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for allowed tool, got %d", allowResp.StatusCode)
	}

	auditResp, err := http.Get(httpServer.URL + "/v1/audit/events")
	if err != nil {
		t.Fatalf("get audit events: %v", err)
	}
	defer auditResp.Body.Close()
	var events []map[string]any
	if err := json.NewDecoder(auditResp.Body).Decode(&events); err != nil {
		t.Fatalf("decode audit events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 audit events, got %#v", events)
	}
}

func TestServerResolvesBearerAuthFromSecretReferences(t *testing.T) {
	dir := t.TempDir()
	if err := os.Setenv("TICKETS_TOKEN", "token-abc"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	t.Cleanup(func() { _ = os.Unsetenv("TICKETS_TOKEN") })

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer token-abc" {
			t.Fatalf("expected bearer auth header, got %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer api.Close()

	writeRuntimeFile(t, dir, "tickets.openapi.yaml", `
openapi: 3.1.0
info:
  title: Tickets API
  version: "1.0.0"
servers:
  - url: `+api.URL+`
security:
  - bearerAuth: []
components:
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
paths:
  /tickets:
    get:
      operationId: listTickets
      tags: [tickets]
      responses:
        "200":
          description: OK
`)
	configPath := writeRuntimeFile(t, dir, ".cli.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "sources": {
	    "ticketsSource": {
	      "type": "openapi",
	      "uri": "./tickets.openapi.yaml",
	      "enabled": true
	    }
	  },
	  "services": {
	    "tickets": {
	      "source": "ticketsSource",
	      "alias": "tickets"
	    }
	  },
	  "secrets": {
	    "bearerAuth": {
	      "type": "env",
	      "value": "TICKETS_TOKEN"
	    }
	  }
	}`)

	server := runtime.NewServer(runtime.Options{AuditPath: filepath.Join(dir, "audit.log")})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	allowBody := bytes.NewBufferString(`{
	  "configPath": "` + configPath + `",
	  "toolId": "tickets:listTickets"
	}`)
	allowResp, err := http.Post(httpServer.URL+"/v1/tools/execute", "application/json", allowBody)
	if err != nil {
		t.Fatalf("allow execute request: %v", err)
	}
	defer allowResp.Body.Close()
	if allowResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for allowed tool, got %d", allowResp.StatusCode)
	}
}

func TestServerResolvesExecSecretReferencesWhenAllowed(t *testing.T) {
	dir := t.TempDir()

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer token-from-exec" {
			t.Fatalf("expected bearer auth header from exec secret, got %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer api.Close()

	writeRuntimeFile(t, dir, "tickets.openapi.yaml", `
openapi: 3.1.0
info:
  title: Tickets API
  version: "1.0.0"
servers:
  - url: `+api.URL+`
security:
  - bearerAuth: []
components:
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
paths:
  /tickets:
    get:
      operationId: listTickets
      tags: [tickets]
      responses:
        "200":
          description: OK
`)
	configPath := writeRuntimeFile(t, dir, ".cli.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "sources": {
	    "ticketsSource": {
	      "type": "openapi",
	      "uri": "./tickets.openapi.yaml",
	      "enabled": true
	    }
	  },
	  "services": {
	    "tickets": {
	      "source": "ticketsSource",
	      "alias": "tickets"
	    }
	  },
	  "policy": {
	    "allowExecSecrets": true
	  },
	  "secrets": {
	    "bearerAuth": {
	      "type": "exec",
	      "command": ["sh", "-lc", "printf token-from-exec"]
	    }
	  }
	}`)

	server := runtime.NewServer(runtime.Options{AuditPath: filepath.Join(dir, "audit.log")})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	allowBody := bytes.NewBufferString(`{
	  "configPath": "` + configPath + `",
	  "toolId": "tickets:listTickets"
	}`)
	allowResp, err := http.Post(httpServer.URL+"/v1/tools/execute", "application/json", allowBody)
	if err != nil {
		t.Fatalf("allow execute request: %v", err)
	}
	defer allowResp.Body.Close()
	if allowResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for allowed tool, got %d", allowResp.StatusCode)
	}
}

func TestServerResolvesOSKeychainSecretReferences(t *testing.T) {
	dir := t.TempDir()

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer token-from-keychain" {
			t.Fatalf("expected bearer auth header from keychain secret, got %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer api.Close()

	writeRuntimeFile(t, dir, "tickets.openapi.yaml", `
openapi: 3.1.0
info:
  title: Tickets API
  version: "1.0.0"
servers:
  - url: `+api.URL+`
security:
  - bearerAuth: []
components:
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
paths:
  /tickets:
    get:
      operationId: listTickets
      tags: [tickets]
      responses:
        "200":
          description: OK
`)
	configPath := writeRuntimeFile(t, dir, ".cli.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "sources": {
	    "ticketsSource": {
	      "type": "openapi",
	      "uri": "./tickets.openapi.yaml",
	      "enabled": true
	    }
	  },
	  "services": {
	    "tickets": {
	      "source": "ticketsSource",
	      "alias": "tickets"
	    }
	  },
	  "secrets": {
	    "bearerAuth": {
	      "type": "osKeychain",
	      "value": "tickets/token"
	    }
	  }
	}`)

	server := runtime.NewServer(runtime.Options{
		AuditPath: filepath.Join(dir, "audit.log"),
		KeychainResolver: func(reference string) (string, error) {
			if reference != "tickets/token" {
				t.Fatalf("expected keychain lookup for tickets/token, got %q", reference)
			}
			return "token-from-keychain", nil
		},
	})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	body := bytes.NewBufferString(`{
	  "configPath": "` + configPath + `",
	  "toolId": "tickets:listTickets"
	}`)
	resp, err := http.Post(httpServer.URL+"/v1/tools/execute", "application/json", body)
	if err != nil {
		t.Fatalf("execute request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for keychain-backed secret, got %d", resp.StatusCode)
	}
}

func TestServerRefreshEndpointRevalidatesCachedSources(t *testing.T) {
	dir := t.TempDir()
	observer := obs.NewRecorder()
	var sawConditional bool

	var api *httptest.Server
	api = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if match := r.Header.Get("If-None-Match"); match != "" {
			sawConditional = true
			if match != `"tickets-v1"` {
				t.Fatalf("expected If-None-Match \"tickets-v1\", got %q", match)
			}
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"tickets-v1"`)
		w.Header().Set("Cache-Control", "max-age=0")
		_, _ = w.Write([]byte(`{
		  "openapi": "3.1.0",
		  "info": { "title": "Tickets API", "version": "1.0.0" },
		  "servers": [{ "url": "` + api.URL + `" }],
		  "paths": {
		    "/tickets": {
		      "get": {
		        "operationId": "listTickets",
		        "tags": ["tickets"],
		        "responses": { "200": { "description": "OK" } }
		      }
		    }
		  }
		}`))
	}))
	defer api.Close()

	configPath := writeRuntimeFile(t, dir, ".cli.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "sources": {
	    "ticketsSource": {
	      "type": "openapi",
	      "uri": "`+api.URL+`/tickets.openapi.json",
	      "enabled": true
	    }
	  },
	  "services": {
	    "tickets": {
	      "source": "ticketsSource",
	      "alias": "tickets"
	    }
	  }
	}`)

	server := runtime.NewServer(runtime.Options{
		AuditPath:  filepath.Join(dir, "audit.log"),
		CacheDir:   filepath.Join(dir, ".cache", "http"),
		HTTPClient: api.Client(),
		Observer:   observer,
	})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	if _, err := http.Get(httpServer.URL + "/v1/catalog/effective?config=" + configPath); err != nil {
		t.Fatalf("prime catalog cache: %v", err)
	}

	body := bytes.NewBufferString(`{"configPath":"` + configPath + `"}`)
	resp, err := http.Post(httpServer.URL+"/v1/refresh", "application/json", body)
	if err != nil {
		t.Fatalf("refresh request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 refresh response, got %d", resp.StatusCode)
	}

	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode refresh response: %v", err)
	}
	sources := payload["sources"].([]any)
	first := sources[0].(map[string]any)
	if first["cacheOutcome"] != "revalidated_hit" {
		t.Fatalf("expected revalidated source outcome, got %#v", first)
	}
	if !sawConditional {
		t.Fatalf("expected conditional refresh request")
	}
	if len(observer.Events()) == 0 {
		t.Fatalf("expected observer events during refresh")
	}
}

func TestServerRefreshEndpointReportsStaleFallback(t *testing.T) {
	dir := t.TempDir()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"tickets-v1"`)
		w.Header().Set("Cache-Control", "max-age=0")
		_, _ = w.Write([]byte(`{
		  "openapi": "3.1.0",
		  "info": { "title": "Tickets API", "version": "1.0.0" },
		  "servers": [{ "url": "https://api.example.com" }],
		  "paths": {
		    "/tickets": {
		      "get": {
		        "operationId": "listTickets",
		        "tags": ["tickets"],
		        "responses": { "200": { "description": "OK" } }
		      }
		    }
		  }
		}`))
	}))

	configPath := writeRuntimeFile(t, dir, ".cli.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "sources": {
	    "ticketsSource": {
	      "type": "openapi",
	      "uri": "`+api.URL+`/tickets.openapi.json",
	      "enabled": true
	    }
	  },
	  "services": {
	    "tickets": {
	      "source": "ticketsSource",
	      "alias": "tickets"
	    }
	  }
	}`)

	server := runtime.NewServer(runtime.Options{
		AuditPath:  filepath.Join(dir, "audit.log"),
		CacheDir:   filepath.Join(dir, ".cache", "http"),
		HTTPClient: api.Client(),
		Observer:   obs.NewRecorder(),
	})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	if _, err := http.Get(httpServer.URL + "/v1/catalog/effective?config=" + configPath); err != nil {
		t.Fatalf("prime catalog cache: %v", err)
	}
	api.Close()

	body := bytes.NewBufferString(`{"configPath":"` + configPath + `"}`)
	resp, err := http.Post(httpServer.URL+"/v1/refresh", "application/json", body)
	if err != nil {
		t.Fatalf("refresh request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 refresh response, got %d", resp.StatusCode)
	}

	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode refresh response: %v", err)
	}
	sources := payload["sources"].([]any)
	first := sources[0].(map[string]any)
	if first["cacheOutcome"] != "stale_hit" || first["stale"] != true {
		t.Fatalf("expected stale fallback source outcome, got %#v", first)
	}
}
