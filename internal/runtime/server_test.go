package runtime_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
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

func TestServerExecutesOAuth2ClientCredentialsTools(t *testing.T) {
	dir := t.TempDir()
	if err := os.Setenv("PETSTORE_CLIENT_ID", "client-123"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	if err := os.Setenv("PETSTORE_CLIENT_SECRET", "secret-xyz"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Unsetenv("PETSTORE_CLIENT_ID")
		_ = os.Unsetenv("PETSTORE_CLIENT_SECRET")
	})

	var tokenCalls int
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			tokenCalls++
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse token form: %v", err)
			}
			if got := r.Form.Get("grant_type"); got != "client_credentials" {
				t.Fatalf("expected client_credentials grant, got %q", got)
			}
			if got := r.Form.Get("client_id"); got != "client-123" {
				t.Fatalf("expected client id, got %q", got)
			}
			if got := r.Form.Get("client_secret"); got != "secret-xyz" {
				t.Fatalf("expected client secret, got %q", got)
			}
			if got := r.Form.Get("scope"); got != "pets.read" {
				t.Fatalf("expected scope pets.read, got %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "oauth-token-123",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
		case "/pets":
			if got := r.Header.Get("Authorization"); got != "Bearer oauth-token-123" {
				t.Fatalf("expected oauth bearer auth header, got %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()

	writeRuntimeFile(t, dir, "pets.openapi.yaml", `
openapi: 3.1.0
info:
  title: Pets API
  version: "1.0.0"
servers:
  - url: `+api.URL+`
security:
  - petstore_oauth: [pets.read]
components:
  securitySchemes:
    petstore_oauth:
      type: oauth2
      flows:
        clientCredentials:
          tokenUrl: `+api.URL+`/oauth/token
          scopes:
            pets.read: Read pets
paths:
  /pets:
    get:
      operationId: listPets
      tags: [pets]
      responses:
        "200":
          description: OK
`)
	configPath := writeRuntimeFile(t, dir, ".cli.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "sources": {
	    "petsSource": {
	      "type": "openapi",
	      "uri": "./pets.openapi.yaml",
	      "enabled": true
	    }
	  },
	  "services": {
	    "pets": {
	      "source": "petsSource",
	      "alias": "pets"
	    }
	  },
	  "secrets": {
	    "pets.petstore_oauth": {
	      "type": "oauth2",
	      "mode": "clientCredentials",
	      "clientId": {
	        "type": "env",
	        "value": "PETSTORE_CLIENT_ID"
	      },
	      "clientSecret": {
	        "type": "env",
	        "value": "PETSTORE_CLIENT_SECRET"
	      }
	    }
	  }
	}`)

	server := runtime.NewServer(runtime.Options{AuditPath: filepath.Join(dir, "audit.log")})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	body := bytes.NewBufferString(`{
	  "configPath": "` + configPath + `",
	  "toolId": "pets:listPets"
	}`)
	resp, err := http.Post(httpServer.URL+"/v1/tools/execute", "application/json", body)
	if err != nil {
		t.Fatalf("execute request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for oauth tool execution, got %d", resp.StatusCode)
	}
	if tokenCalls != 1 {
		t.Fatalf("expected one token request, got %d", tokenCalls)
	}
}

func TestServerExecutesOpenIDConnectClientCredentialsTools(t *testing.T) {
	dir := t.TempDir()
	if err := os.Setenv("OIDC_CLIENT_ID", "oidc-client"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	if err := os.Setenv("OIDC_CLIENT_SECRET", "oidc-secret"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Unsetenv("OIDC_CLIENT_ID")
		_ = os.Unsetenv("OIDC_CLIENT_SECRET")
	})

	var discoveryCalls int
	var tokenCalls int
	var api *httptest.Server
	api = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			discoveryCalls++
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"token_endpoint": api.URL + "/oauth/token",
			})
		case "/oauth/token":
			tokenCalls++
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse token form: %v", err)
			}
			if got := r.Form.Get("grant_type"); got != "client_credentials" {
				t.Fatalf("expected client_credentials grant, got %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "oidc-token-123",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
		case "/profile":
			if got := r.Header.Get("Authorization"); got != "Bearer oidc-token-123" {
				t.Fatalf("expected oidc bearer auth header, got %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()

	writeRuntimeFile(t, dir, "profile.openapi.yaml", `
openapi: 3.1.0
info:
  title: Profile API
  version: "1.0.0"
servers:
  - url: `+api.URL+`
security:
  - oidcAuth: [profile.read]
components:
  securitySchemes:
    oidcAuth:
      type: openIdConnect
      openIdConnectUrl: `+api.URL+`/.well-known/openid-configuration
paths:
  /profile:
    get:
      operationId: getProfile
      tags: [profile]
      responses:
        "200":
          description: OK
`)
	configPath := writeRuntimeFile(t, dir, ".cli.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "sources": {
	    "profileSource": {
	      "type": "openapi",
	      "uri": "./profile.openapi.yaml",
	      "enabled": true
	    }
	  },
	  "services": {
	    "profile": {
	      "source": "profileSource",
	      "alias": "profile"
	    }
	  },
	  "secrets": {
	    "profile.oidcAuth": {
	      "type": "oauth2",
	      "mode": "clientCredentials",
	      "clientId": {
	        "type": "env",
	        "value": "OIDC_CLIENT_ID"
	      },
	      "clientSecret": {
	        "type": "env",
	        "value": "OIDC_CLIENT_SECRET"
	      }
	    }
	  }
	}`)

	server := runtime.NewServer(runtime.Options{AuditPath: filepath.Join(dir, "audit.log")})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	body := bytes.NewBufferString(`{
	  "configPath": "` + configPath + `",
	  "toolId": "profile:getProfile"
	}`)
	resp, err := http.Post(httpServer.URL+"/v1/tools/execute", "application/json", body)
	if err != nil {
		t.Fatalf("execute request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for oidc tool execution, got %d", resp.StatusCode)
	}
	if discoveryCalls != 1 {
		t.Fatalf("expected one discovery request, got %d", discoveryCalls)
	}
	if tokenCalls != 1 {
		t.Fatalf("expected one token request, got %d", tokenCalls)
	}
}

func TestServerCachesOAuth2ClientCredentialsTokensPerInstance(t *testing.T) {
	dir := t.TempDir()
	if err := os.Setenv("CACHE_CLIENT_ID", "cache-client"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	if err := os.Setenv("CACHE_CLIENT_SECRET", "cache-secret"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Unsetenv("CACHE_CLIENT_ID")
		_ = os.Unsetenv("CACHE_CLIENT_SECRET")
	})

	var tokenCalls int
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			tokenCalls++
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "cached-token-123",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
		case "/pets":
			if got := r.Header.Get("Authorization"); got != "Bearer cached-token-123" {
				t.Fatalf("expected cached bearer auth header, got %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()

	writeRuntimeFile(t, dir, "pets.openapi.yaml", `
openapi: 3.1.0
info:
  title: Pets API
  version: "1.0.0"
servers:
  - url: `+api.URL+`
security:
  - petstore_oauth: [pets.read]
components:
  securitySchemes:
    petstore_oauth:
      type: oauth2
      flows:
        clientCredentials:
          tokenUrl: `+api.URL+`/oauth/token
          scopes:
            pets.read: Read pets
paths:
  /pets:
    get:
      operationId: listPets
      tags: [pets]
      responses:
        "200":
          description: OK
`)
	configPath := writeRuntimeFile(t, dir, ".cli.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "sources": {
	    "petsSource": {
	      "type": "openapi",
	      "uri": "./pets.openapi.yaml",
	      "enabled": true
	    }
	  },
	  "services": {
	    "pets": {
	      "source": "petsSource",
	      "alias": "pets"
	    }
	  },
	  "secrets": {
	    "pets.petstore_oauth": {
	      "type": "oauth2",
	      "mode": "clientCredentials",
	      "clientId": {
	        "type": "env",
	        "value": "CACHE_CLIENT_ID"
	      },
	      "clientSecret": {
	        "type": "env",
	        "value": "CACHE_CLIENT_SECRET"
	      }
	    }
	  }
	}`)

	server := runtime.NewServer(runtime.Options{AuditPath: filepath.Join(dir, "audit.log")})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	for i := 0; i < 2; i++ {
		body := bytes.NewBufferString(`{
		  "configPath": "` + configPath + `",
		  "toolId": "pets:listPets"
		}`)
		resp, err := http.Post(httpServer.URL+"/v1/tools/execute", "application/json", body)
		if err != nil {
			t.Fatalf("execute request: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 for oauth tool execution, got %d", resp.StatusCode)
		}
	}

	if tokenCalls != 1 {
		t.Fatalf("expected cached token to avoid reauth, got %d token calls", tokenCalls)
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

func TestServerExecutesMCPTools(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is required for MCP runtime integration test")
	}

	dir := t.TempDir()
	serverPath := writeRuntimeFile(t, dir, "fake_mcp_server.py", `
import json
import sys

TOOLS = [
    {
        "name": "ping",
        "description": "Ping the MCP server",
        "inputSchema": {
            "type": "object",
            "properties": {}
        }
    }
]

def read_message():
    line = sys.stdin.readline()
    if not line:
        return None
    line = line.strip()
    if not line:
        return None
    return json.loads(line)

def write_message(message):
    sys.stdout.write(json.dumps(message) + "\n")
    sys.stdout.flush()

while True:
    message = read_message()
    if message is None:
        break
    method = message.get("method")
    if method == "initialize":
        write_message({
            "jsonrpc": "2.0",
            "id": message["id"],
            "result": {
                "protocolVersion": "2025-03-26",
                "capabilities": {"tools": {}},
                "serverInfo": {"name": "fake-mcp", "version": "1.0.0"}
            }
        })
    elif method == "notifications/initialized":
        continue
    elif method == "tools/list":
        write_message({
            "jsonrpc": "2.0",
            "id": message["id"],
            "result": {"tools": TOOLS}
        })
    elif method == "tools/call":
        write_message({
            "jsonrpc": "2.0",
            "id": message["id"],
            "result": {
                "structuredContent": {"ok": True, "name": message["params"]["name"]},
                "content": [{"type": "text", "text": "pong"}],
                "isError": False
            }
        })
    else:
        write_message({
            "jsonrpc": "2.0",
            "id": message.get("id"),
            "error": {"code": -32601, "message": f"unsupported method: {method}"}
        })
`)

	configPath := writeRuntimeFile(t, dir, ".cli.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "sources": {
	    "docs": {
	      "type": "mcp",
	      "enabled": true,
	      "transport": {
	        "type": "stdio",
	        "command": "python3",
	        "args": ["`+serverPath+`"]
	      }
	    }
	  },
	  "services": {
	    "docs": {
	      "source": "docs",
	      "alias": "docs"
	    }
	  }
	}`)

	server := runtime.NewServer(runtime.Options{AuditPath: filepath.Join(dir, "audit.log"), Observer: obs.NewNop()})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	requestBody := bytes.NewBufferString(`{
	  "configPath": "` + configPath + `",
	  "toolId": "docs:ping"
	}`)
	resp, err := http.Post(httpServer.URL+"/v1/tools/execute", "application/json", requestBody)
	if err != nil {
		t.Fatalf("execute request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for MCP tool execution, got %d", resp.StatusCode)
	}

	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode execute response: %v", err)
	}
	body, ok := payload["body"].(map[string]any)
	if !ok {
		t.Fatalf("expected JSON body payload, got %#v", payload)
	}
	if isError, exists := body["isError"]; exists && isError != false {
		t.Fatalf("expected non-error MCP result, got %#v", body)
	}
}

func TestServerExecutesStreamableHTTPMCPTools(t *testing.T) {
	dir := t.TempDir()

	var (
		mu              sync.Mutex
		initializeCalls int
		callHeaders     []string
	)

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mcp" {
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodDelete {
			if got := r.Header.Get("Mcp-Session-Id"); got != "session-1" {
				t.Fatalf("expected session header on delete, got %q", got)
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}

		var message map[string]any
		if err := json.NewDecoder(r.Body).Decode(&message); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		method, _ := message["method"].(string)
		switch method {
		case "initialize":
			mu.Lock()
			initializeCalls++
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Mcp-Session-Id", "session-1")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      message["id"],
				"result": map[string]any{
					"protocolVersion": "2025-03-26",
					"capabilities":    map[string]any{"tools": map[string]any{}},
					"serverInfo":      map[string]any{"name": "remote-mcp", "version": "1.0.0"},
				},
			})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			if got := r.Header.Get("Mcp-Session-Id"); got != "session-1" {
				t.Fatalf("expected session header on tools/list, got %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      message["id"],
				"result": map[string]any{
					"tools": []map[string]any{{
						"name":        "ping",
						"description": "Ping the MCP server",
						"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
					}},
				},
			})
		case "tools/call":
			if got := r.Header.Get("Mcp-Session-Id"); got != "session-1" {
				t.Fatalf("expected session header on tools/call, got %q", got)
			}
			mu.Lock()
			callHeaders = append(callHeaders, r.Header.Get("Mcp-Session-Id"))
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      message["id"],
				"result": map[string]any{
					"structuredContent": map[string]any{"ok": true, "name": "ping"},
					"content":           []map[string]any{{"type": "text", "text": "pong"}},
					"isError":           false,
				},
			})
		default:
			t.Fatalf("unexpected MCP method %q", method)
		}
	}))
	defer api.Close()

	configPath := writeRuntimeFile(t, dir, ".cli.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "sources": {
	    "docs": {
	      "type": "mcp",
	      "enabled": true,
	      "transport": {
	        "type": "streamable-http",
	        "url": "`+api.URL+`/mcp"
	      }
	    }
	  },
	  "services": {
	    "docs": {
	      "source": "docs",
	      "alias": "docs"
	    }
	  }
	}`)

	server := runtime.NewServer(runtime.Options{AuditPath: filepath.Join(dir, "audit.log"), Observer: obs.NewNop()})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	requestBody := bytes.NewBufferString(`{
	  "configPath": "` + configPath + `",
	  "toolId": "docs:ping"
	}`)
	resp, err := http.Post(httpServer.URL+"/v1/tools/execute", "application/json", requestBody)
	if err != nil {
		t.Fatalf("execute request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for MCP tool execution, got %d", resp.StatusCode)
	}

	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode execute response: %v", err)
	}
	body, ok := payload["body"].(map[string]any)
	if !ok {
		t.Fatalf("expected JSON body payload, got %#v", payload)
	}
	if isError, exists := body["isError"]; exists && isError != false {
		t.Fatalf("expected non-error MCP result, got %#v", body)
	}

	mu.Lock()
	defer mu.Unlock()
	if initializeCalls < 2 {
		t.Fatalf("expected separate initialize calls for catalog and execution, got %d", initializeCalls)
	}
	if len(callHeaders) == 0 {
		t.Fatalf("expected at least one tools/call request")
	}
}

func TestServerCachesMCPTransportOAuthTokensPerInstance(t *testing.T) {
	dir := t.TempDir()
	if err := os.Setenv("MCP_TRANSPORT_CLIENT_ID", "transport-client"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	if err := os.Setenv("MCP_TRANSPORT_CLIENT_SECRET", "transport-secret"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Unsetenv("MCP_TRANSPORT_CLIENT_ID")
		_ = os.Unsetenv("MCP_TRANSPORT_CLIENT_SECRET")
	})

	var tokenCalls int
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			tokenCalls++
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "transport-token-123",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
		case "/mcp":
			if r.Method == http.MethodDelete {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			if got := r.Header.Get("Authorization"); got != "Bearer transport-token-123" {
				t.Fatalf("expected transport oauth header, got %q", got)
			}
			var message map[string]any
			if err := json.NewDecoder(r.Body).Decode(&message); err != nil {
				t.Fatalf("decode request body: %v", err)
			}
			method, _ := message["method"].(string)
			switch method {
			case "initialize":
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Mcp-Session-Id", "session-1")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"jsonrpc": "2.0",
					"id":      message["id"],
					"result": map[string]any{
						"protocolVersion": "2025-03-26",
						"capabilities":    map[string]any{"tools": map[string]any{}},
						"serverInfo":      map[string]any{"name": "transport-mcp", "version": "1.0.0"},
					},
				})
			case "notifications/initialized":
				w.WriteHeader(http.StatusAccepted)
			case "tools/list":
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"jsonrpc": "2.0",
					"id":      message["id"],
					"result": map[string]any{
						"tools": []map[string]any{{
							"name":        "ping",
							"description": "Ping the MCP server",
							"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
						}},
					},
				})
			case "tools/call":
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"jsonrpc": "2.0",
					"id":      message["id"],
					"result": map[string]any{
						"structuredContent": map[string]any{"ok": true},
						"content":           []map[string]any{{"type": "text", "text": "pong"}},
						"isError":           false,
					},
				})
			default:
				t.Fatalf("unexpected MCP method %q", method)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()

	configPath := writeRuntimeFile(t, dir, ".cli.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "sources": {
	    "docs": {
	      "type": "mcp",
	      "enabled": true,
	      "transport": {
	        "type": "streamable-http",
	        "url": "`+api.URL+`/mcp"
	      },
	      "oauth": {
	        "mode": "clientCredentials",
	        "tokenURL": "`+api.URL+`/oauth/token",
	        "clientId": { "type": "env", "value": "MCP_TRANSPORT_CLIENT_ID" },
	        "clientSecret": { "type": "env", "value": "MCP_TRANSPORT_CLIENT_SECRET" }
	      }
	    }
	  },
	  "services": {
	    "docs": {
	      "source": "docs",
	      "alias": "docs"
	    }
	  }
	}`)

	server := runtime.NewServer(runtime.Options{AuditPath: filepath.Join(dir, "audit.log"), Observer: obs.NewNop()})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	for i := 0; i < 2; i++ {
		requestBody := bytes.NewBufferString(`{
		  "configPath": "` + configPath + `",
		  "toolId": "docs:ping"
		}`)
		resp, err := http.Post(httpServer.URL+"/v1/tools/execute", "application/json", requestBody)
		if err != nil {
			t.Fatalf("execute request: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 for MCP tool execution, got %d", resp.StatusCode)
		}
	}

	if tokenCalls != 1 {
		t.Fatalf("expected cached transport oauth token, got %d token calls", tokenCalls)
	}
}

func TestServerUsesExplicitStateDirForTransportOAuthCache(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "instance-state")
	auditDir := filepath.Join(dir, "external-audit")
	if err := os.MkdirAll(auditDir, 0o755); err != nil {
		t.Fatalf("mkdir audit dir: %v", err)
	}
	if err := os.Setenv("MCP_STATE_CLIENT_ID", "state-client"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	if err := os.Setenv("MCP_STATE_CLIENT_SECRET", "state-secret"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Unsetenv("MCP_STATE_CLIENT_ID")
		_ = os.Unsetenv("MCP_STATE_CLIENT_SECRET")
	})

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "state-token-123",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
		case "/mcp":
			if r.Method == http.MethodDelete {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			var message map[string]any
			if err := json.NewDecoder(r.Body).Decode(&message); err != nil {
				t.Fatalf("decode request body: %v", err)
			}
			switch message["method"] {
			case "initialize":
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Mcp-Session-Id", "state-session")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"jsonrpc": "2.0",
					"id":      message["id"],
					"result": map[string]any{
						"protocolVersion": "2025-03-26",
						"capabilities":    map[string]any{"tools": map[string]any{}},
						"serverInfo":      map[string]any{"name": "state-mcp", "version": "1.0.0"},
					},
				})
			case "notifications/initialized":
				w.WriteHeader(http.StatusAccepted)
			case "tools/list":
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"jsonrpc": "2.0",
					"id":      message["id"],
					"result": map[string]any{
						"tools": []map[string]any{{
							"name":        "ping",
							"description": "Ping",
							"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
						}},
					},
				})
			case "tools/call":
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"jsonrpc": "2.0",
					"id":      message["id"],
					"result": map[string]any{
						"structuredContent": map[string]any{"ok": true},
						"content":           []map[string]any{{"type": "text", "text": "pong"}},
					},
				})
			default:
				t.Fatalf("unexpected MCP method %q", message["method"])
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()

	configPath := writeRuntimeFile(t, dir, ".cli.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "sources": {
	    "docs": {
	      "type": "mcp",
	      "enabled": true,
	      "transport": {
	        "type": "streamable-http",
	        "url": "`+api.URL+`/mcp"
	      },
	      "oauth": {
	        "mode": "clientCredentials",
	        "tokenURL": "`+api.URL+`/oauth/token",
	        "clientId": { "type": "env", "value": "MCP_STATE_CLIENT_ID" },
	        "clientSecret": { "type": "env", "value": "MCP_STATE_CLIENT_SECRET" }
	      }
	    }
	  },
	  "services": {
	    "docs": { "source": "docs" }
	  }
	}`)

	server := runtime.NewServer(runtime.Options{
		AuditPath: filepath.Join(auditDir, "audit.log"),
		StateDir:  stateDir,
		Observer:  obs.NewNop(),
	})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	requestBody := bytes.NewBufferString(`{
	  "configPath": "` + configPath + `",
	  "toolId": "docs:ping"
	}`)
	resp, err := http.Post(httpServer.URL+"/v1/tools/execute", "application/json", requestBody)
	if err != nil {
		t.Fatalf("execute request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for MCP tool execution, got %d", resp.StatusCode)
	}

	stateCache, err := filepath.Glob(filepath.Join(stateDir, "oauth", "*.json"))
	if err != nil {
		t.Fatalf("glob state cache: %v", err)
	}
	if len(stateCache) == 0 {
		t.Fatalf("expected oauth cache file under explicit state dir")
	}

	auditCache, err := filepath.Glob(filepath.Join(auditDir, "oauth", "*.json"))
	if err != nil {
		t.Fatalf("glob audit cache: %v", err)
	}
	if len(auditCache) != 0 {
		t.Fatalf("expected no oauth cache files under audit directory, got %#v", auditCache)
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
