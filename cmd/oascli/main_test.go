package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/StevenBuglione/oas-cli-go/pkg/catalog"
	configpkg "github.com/StevenBuglione/oas-cli-go/pkg/config"
	"github.com/StevenBuglione/oas-cli-go/pkg/instance"
)

func TestRootCommandInvokesRuntimeToolsAndSchemas(t *testing.T) {
	tool := catalog.Tool{
		ID:        "tickets:listTickets",
		ServiceID: "tickets",
		Method:    http.MethodGet,
		Path:      "/tickets",
		Group:     "tickets",
		Command:   "list",
		Flags: []catalog.Parameter{
			{Name: "state", OriginalName: "status", Location: "query"},
		},
	}
	view := catalog.EffectiveView{Name: "discover", Mode: "discover", Tools: []catalog.Tool{tool}}
	runtimeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/catalog/effective":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"catalog": map[string]any{
					"services": []map[string]any{{"id": "tickets", "alias": "tickets"}},
					"tools":    []catalog.Tool{tool},
				},
				"view": view,
			})
		case "/v1/tools/execute":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"statusCode": 200,
				"body": map[string]any{
					"items": []map[string]any{{"id": "T-1"}},
				},
			})
		case "/v1/workflows/run":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"workflowId": "nightlySync",
				"steps":      []string{"listTickets"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer runtimeServer.Close()

	var stdout bytes.Buffer
	cmd, err := NewRootCommand(CommandOptions{
		RuntimeURL: runtimeServer.URL,
		ConfigPath: "/tmp/project/.cli.json",
		Stdout:     &stdout,
		Stderr:     &stdout,
	}, []string{"tickets", "tickets", "list", "--state", "open", "--format", "json"})
	if err != nil {
		t.Fatalf("NewRootCommand returned error: %v", err)
	}
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"items"`)) {
		t.Fatalf("expected runtime response in stdout, got %s", stdout.String())
	}

	stdout.Reset()
	cmd, err = NewRootCommand(CommandOptions{
		RuntimeURL: runtimeServer.URL,
		ConfigPath: "/tmp/project/.cli.json",
		Stdout:     &stdout,
		Stderr:     &stdout,
	}, []string{"tool", "schema", "tickets:listTickets"})
	if err != nil {
		t.Fatalf("NewRootCommand returned error: %v", err)
	}
	if err := cmd.Execute(); err != nil {
		t.Fatalf("schema command failed: %v", err)
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"id":"tickets:listTickets"`)) {
		t.Fatalf("expected tool schema output, got %s", stdout.String())
	}
}

func TestRootCommandUsesServiceAlias(t *testing.T) {
	tool := catalog.Tool{
		ID:        "tickets:listTickets",
		ServiceID: "tickets",
		Method:    http.MethodGet,
		Path:      "/tickets",
		Group:     "tickets",
		Command:   "list",
	}
	view := catalog.EffectiveView{Name: "discover", Mode: "discover", Tools: []catalog.Tool{tool}}
	runtimeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/catalog/effective":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"catalog": map[string]any{
					"services": []map[string]any{{"id": "tickets", "alias": "helpdesk"}},
					"tools":    []catalog.Tool{tool},
				},
				"view": view,
			})
		case "/v1/tools/execute":
			_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "body": map[string]any{"ok": true}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer runtimeServer.Close()

	var stdout bytes.Buffer
	cmd, err := NewRootCommand(CommandOptions{
		RuntimeURL: runtimeServer.URL,
		ConfigPath: "/tmp/project/.cli.json",
		Stdout:     &stdout,
		Stderr:     &stdout,
	}, []string{"helpdesk", "tickets", "list", "--format", "json"})
	if err != nil {
		t.Fatalf("NewRootCommand returned error: %v", err)
	}
	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected alias command to execute, got %v", err)
	}
}

func TestRootCommandReadsBodyFromFileAndStdin(t *testing.T) {
	tool := catalog.Tool{
		ID:        "tickets:createTicket",
		ServiceID: "tickets",
		Method:    http.MethodPost,
		Path:      "/tickets",
		Group:     "tickets",
		Command:   "create",
	}
	view := catalog.EffectiveView{Name: "discover", Mode: "discover", Tools: []catalog.Tool{tool}}

	t.Run("file", func(t *testing.T) {
		bodyFile, err := os.CreateTemp(t.TempDir(), "body-*.json")
		if err != nil {
			t.Fatalf("create temp body file: %v", err)
		}
		if _, err := bodyFile.WriteString(`{"title":"file"}`); err != nil {
			t.Fatalf("write body file: %v", err)
		}
		if err := bodyFile.Close(); err != nil {
			t.Fatalf("close body file: %v", err)
		}

		var captured executeRequest
		runtimeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/v1/catalog/effective":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"catalog": map[string]any{
						"services": []map[string]any{{"id": "tickets", "alias": "tickets"}},
						"tools":    []catalog.Tool{tool},
					},
					"view": view,
				})
			case "/v1/tools/execute":
				if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
					t.Fatalf("decode execute request: %v", err)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "body": map[string]any{"ok": true}})
			default:
				http.NotFound(w, r)
			}
		}))
		defer runtimeServer.Close()

		var stdout bytes.Buffer
		cmd, err := NewRootCommand(CommandOptions{
			RuntimeURL: runtimeServer.URL,
			ConfigPath: "/tmp/project/.cli.json",
			Stdout:     &stdout,
			Stderr:     &stdout,
		}, []string{"tickets", "tickets", "create", "--body", "@" + bodyFile.Name()})
		if err != nil {
			t.Fatalf("NewRootCommand returned error: %v", err)
		}
		if err := cmd.Execute(); err != nil {
			t.Fatalf("expected file body command to execute, got %v", err)
		}
		if string(captured.Body) != `{"title":"file"}` {
			t.Fatalf("expected request body from file, got %q", string(captured.Body))
		}
	})

	t.Run("stdin", func(t *testing.T) {
		var captured executeRequest
		runtimeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/v1/catalog/effective":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"catalog": map[string]any{
						"services": []map[string]any{{"id": "tickets", "alias": "tickets"}},
						"tools":    []catalog.Tool{tool},
					},
					"view": view,
				})
			case "/v1/tools/execute":
				if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
					t.Fatalf("decode execute request: %v", err)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "body": map[string]any{"ok": true}})
			default:
				http.NotFound(w, r)
			}
		}))
		defer runtimeServer.Close()

		var stdout bytes.Buffer
		cmd, err := NewRootCommand(CommandOptions{
			RuntimeURL: runtimeServer.URL,
			ConfigPath: "/tmp/project/.cli.json",
			Stdout:     &stdout,
			Stderr:     &stdout,
			Stdin:      bytes.NewBufferString(`{"title":"stdin"}`),
		}, []string{"tickets", "tickets", "create", "--body", "-"})
		if err != nil {
			t.Fatalf("NewRootCommand returned error: %v", err)
		}
		if err := cmd.Execute(); err != nil {
			t.Fatalf("expected stdin body command to execute, got %v", err)
		}
		if string(captured.Body) != `{"title":"stdin"}` {
			t.Fatalf("expected request body from stdin, got %q", string(captured.Body))
		}
	})
}

func TestRootCommandUsesRuntimeRegistryForInstance(t *testing.T) {
	tool := catalog.Tool{
		ID:        "tickets:listTickets",
		ServiceID: "tickets",
		Method:    http.MethodGet,
		Path:      "/tickets",
		Group:     "tickets",
		Command:   "list",
	}
	view := catalog.EffectiveView{Name: "discover", Mode: "discover", Tools: []catalog.Tool{tool}}
	runtimeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/catalog/effective":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"catalog": map[string]any{
					"services": []map[string]any{{"id": "tickets", "alias": "tickets"}},
					"tools":    []catalog.Tool{tool},
				},
				"view": view,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer runtimeServer.Close()

	stateDir := t.TempDir()
	paths, err := instance.Resolve(instance.Options{
		InstanceID: "alpha",
		StateRoot:  stateDir,
		CacheRoot:  filepath.Join(stateDir, "cache"),
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if err := instance.WriteRuntimeInfo(paths.RuntimePath, instance.RuntimeInfo{
		InstanceID: "alpha",
		URL:        runtimeServer.URL,
		AuditPath:  paths.AuditPath,
		CacheDir:   paths.CacheDir,
	}); err != nil {
		t.Fatalf("WriteRuntimeInfo: %v", err)
	}

	var stdout bytes.Buffer
	cmd, err := NewRootCommand(CommandOptions{
		ConfigPath: "/tmp/project/.cli.json",
		InstanceID: "alpha",
		StateDir:   stateDir,
		Stdout:     &stdout,
		Stderr:     &stdout,
	}, []string{"catalog", "list", "--format", "json"})
	if err != nil {
		t.Fatalf("NewRootCommand returned error: %v", err)
	}
	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected command to resolve runtime from instance registry, got %v", err)
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"catalog"`)) {
		t.Fatalf("expected catalog output, got %s", stdout.String())
	}
}

func TestRootCommandExecutesEmbeddedRuntimeWithoutDaemon(t *testing.T) {
	dir := t.TempDir()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{"id": "T-1"}}})
	}))
	defer api.Close()

	openapiPath := filepath.Join(dir, "tickets.openapi.yaml")
	if err := os.WriteFile(openapiPath, []byte(`
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
      responses:
        "200":
          description: OK
`), 0o644); err != nil {
		t.Fatalf("WriteFile openapi: %v", err)
	}
	configPath := filepath.Join(dir, ".cli.json")
	if err := os.WriteFile(configPath, []byte(`{
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
	  }
	}`), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	var stdout bytes.Buffer
	cmd, err := NewRootCommand(CommandOptions{
		ConfigPath: configPath,
		Embedded:   true,
		InstanceID: "embedded-alpha",
		StateDir:   filepath.Join(dir, "state"),
		Stdout:     &stdout,
		Stderr:     &stdout,
	}, []string{"tickets", "tickets", "list-tickets", "--format", "json"})
	if err != nil {
		t.Fatalf("NewRootCommand returned error: %v", err)
	}
	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected embedded runtime command to execute, got %v", err)
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"items"`)) {
		t.Fatalf("expected embedded runtime output, got %s", stdout.String())
	}
}

func TestResolveCommandOptionsFallsBackWhenRuntimeRegistryIsStale(t *testing.T) {
	t.Setenv("OASCLI_RUNTIME_URL", "")
	t.Setenv("OASCLI_EMBEDDED", "")

	deadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	deadURL := deadServer.URL
	deadServer.Close()

	stateDir := t.TempDir()
	paths, err := instance.Resolve(instance.Options{
		InstanceID: "stale",
		StateRoot:  stateDir,
		CacheRoot:  filepath.Join(stateDir, "cache"),
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if err := instance.WriteRuntimeInfo(paths.RuntimePath, instance.RuntimeInfo{
		InstanceID: "stale",
		URL:        deadURL,
		AuditPath:  paths.AuditPath,
		CacheDir:   paths.CacheDir,
	}); err != nil {
		t.Fatalf("WriteRuntimeInfo: %v", err)
	}

	resolved, err := resolveCommandOptions(CommandOptions{
		InstanceID: "stale",
		StateDir:   stateDir,
	})
	if err != nil {
		t.Fatalf("resolveCommandOptions: %v", err)
	}
	if resolved.RuntimeURL != "http://127.0.0.1:8765" {
		t.Fatalf("expected stale runtime registry to fall back to default runtime, got %q", resolved.RuntimeURL)
	}
	if _, err := os.Stat(paths.RuntimePath); !os.IsNotExist(err) {
		t.Fatalf("expected stale runtime registry to be removed, stat err=%v", err)
	}
}

func TestResolveCommandOptionsPromotesAutoRuntimeForLocalMCP(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".cli.json")
	if err := os.WriteFile(configPath, []byte(`{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "runtime": {
	    "mode": "auto",
	    "local": {
	      "sessionScope": "terminal",
	      "heartbeatSeconds": 15,
	      "missedHeartbeatLimit": 3,
	      "shutdown": "when-owner-exits",
	      "share": "exclusive"
	    }
	  },
	  "mcpServers": {
	    "filesystem": {
	      "type": "stdio",
	      "command": "npx"
	    }
	  }
	}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	previousStarter := managedRuntimeStarter
	previousHandshake := localSessionHandshake
	t.Cleanup(func() {
		managedRuntimeStarter = previousStarter
		localSessionHandshake = previousHandshake
	})
	managedRuntimeStarter = func(options CommandOptions) (string, error) {
		return "http://127.0.0.1:18887", nil
	}
	localSessionHandshake = func(options CommandOptions) (CommandOptions, error) { return options, nil }

	resolved, err := resolveCommandOptions(CommandOptions{ConfigPath: configPath})
	if err != nil {
		t.Fatalf("resolveCommandOptions: %v", err)
	}
	if resolved.RuntimeDeployment != "local" {
		t.Fatalf("expected auto runtime with local mcp to promote to local deployment, got %q", resolved.RuntimeDeployment)
	}
	if resolved.RuntimeURL != "http://127.0.0.1:18887" {
		t.Fatalf("expected promoted local deployment to use managed runtime url, got %q", resolved.RuntimeURL)
	}
}

func TestResolveCommandOptionsStartsManagedLocalRuntimeWhenRegistryMissing(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".cli.json")
	if err := os.WriteFile(configPath, []byte(`{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "runtime": {
	    "mode": "local",
	    "local": {
	      "sessionScope": "terminal",
	      "heartbeatSeconds": 15,
	      "missedHeartbeatLimit": 3,
	      "shutdown": "when-owner-exits",
	      "share": "exclusive"
	    }
	  },
	  "mcpServers": {
	    "filesystem": {
	      "type": "stdio",
	      "command": "npx"
	    }
	  }
	}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	previousStarter := managedRuntimeStarter
	previousHandshake := localSessionHandshake
	t.Cleanup(func() {
		managedRuntimeStarter = previousStarter
		localSessionHandshake = previousHandshake
	})

	started := 0
	managedRuntimeStarter = func(options CommandOptions) (string, error) {
		started++
		return "http://127.0.0.1:18888", nil
	}
	localSessionHandshake = func(options CommandOptions) (CommandOptions, error) { return options, nil }

	resolved, err := resolveCommandOptions(CommandOptions{
		ConfigPath:        configPath,
		RuntimeDeployment: "local",
		StateDir:          filepath.Join(dir, "state"),
	})
	if err != nil {
		t.Fatalf("resolveCommandOptions: %v", err)
	}
	if started != 1 {
		t.Fatalf("expected managed runtime starter to run once, got %d", started)
	}
	if resolved.RuntimeURL != "http://127.0.0.1:18888" {
		t.Fatalf("expected managed local runtime url, got %q", resolved.RuntimeURL)
	}
}

func TestResolveCommandOptionsRegistersLocalSessionLease(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".cli.json")
	if err := os.WriteFile(configPath, []byte(`{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "runtime": {
	    "mode": "local",
	    "local": {
	      "sessionScope": "terminal",
	      "heartbeatSeconds": 15,
	      "missedHeartbeatLimit": 3,
	      "shutdown": "when-owner-exits",
	      "share": "exclusive"
	    }
	  },
	  "mcpServers": {
	    "filesystem": {
	      "type": "stdio",
	      "command": "npx"
	    }
	  }
	}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var heartbeatBody map[string]any
	runtimeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/runtime/info":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"lifecycle": map[string]any{
					"capabilities":         []string{"heartbeat", "session-close"},
					"heartbeatSeconds":     15,
					"missedHeartbeatLimit": 3,
					"shutdown":             "when-owner-exits",
				},
			})
		case "/v1/runtime/heartbeat":
			if err := json.NewDecoder(r.Body).Decode(&heartbeatBody); err != nil {
				t.Fatalf("decode heartbeat body: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"renewed":   true,
				"sessionId": heartbeatBody["sessionId"],
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer runtimeServer.Close()

	previousHandshake := localSessionHandshake
	t.Cleanup(func() { localSessionHandshake = previousHandshake })
	localSessionHandshake = performLocalSessionHandshake

	resolved, err := resolveCommandOptions(CommandOptions{
		ConfigPath:        configPath,
		RuntimeDeployment: "local",
		RuntimeURL:        runtimeServer.URL,
		StateDir:          filepath.Join(dir, "state"),
	})
	if err != nil {
		t.Fatalf("resolveCommandOptions: %v", err)
	}
	if !resolved.HeartbeatEnabled {
		t.Fatalf("expected local session handshake to enable heartbeat lifecycle")
	}
	if resolved.SessionID == "" {
		t.Fatalf("expected resolved session id")
	}
	if heartbeatBody["sessionId"] != resolved.SessionID {
		t.Fatalf("expected heartbeat registration for %q, got %#v", resolved.SessionID, heartbeatBody)
	}
	if got := heartbeatBody["configFingerprint"]; got == nil || got == "" {
		t.Fatalf("expected config fingerprint to be sent during heartbeat, got %#v", got)
	}
}

func TestResolveCommandOptionsFailsOnLifecycleFingerprintMismatch(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".cli.json")
	if err := os.WriteFile(configPath, []byte(`{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "runtime": {
	    "mode": "local",
	    "local": {
	      "sessionScope": "terminal",
	      "heartbeatSeconds": 15,
	      "missedHeartbeatLimit": 3,
	      "shutdown": "when-owner-exits",
	      "share": "exclusive"
	    }
	  },
	  "mcpServers": {
	    "filesystem": {
	      "type": "stdio",
	      "command": "npx"
	    }
	  }
	}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	runtimeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/runtime/info":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"lifecycle": map[string]any{
					"capabilities":      []string{"heartbeat"},
					"configFingerprint": "server-fingerprint",
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer runtimeServer.Close()

	previousHandshake := localSessionHandshake
	t.Cleanup(func() { localSessionHandshake = previousHandshake })
	localSessionHandshake = performLocalSessionHandshake

	_, err := resolveCommandOptions(CommandOptions{
		ConfigPath:        configPath,
		RuntimeDeployment: "local",
		RuntimeURL:        runtimeServer.URL,
		StateDir:          filepath.Join(dir, "state"),
	})
	if err == nil || !strings.Contains(err.Error(), "runtime_attach_mismatch") {
		t.Fatalf("expected runtime_attach_mismatch error, got %v", err)
	}
}

func TestResolveCommandOptionsUsesConfiguredRemoteRuntimeURL(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".cli.json")
	if err := os.WriteFile(configPath, []byte(`{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "runtime": {
	    "mode": "remote",
	    "remote": {
	      "url": "https://runtime.example.com",
	      "oauth": {
	        "mode": "providedToken",
	        "audience": "oasclird",
	        "scopes": ["bundle:payments"],
	        "tokenRef": "env:OAS_REMOTE_TOKEN"
	      }
	    }
	  },
	  "sources": {
	    "tickets": {
	      "type": "openapi",
	      "uri": "https://example.com/openapi.json"
	    }
	  },
	  "services": {
	    "tickets": {
	      "source": "tickets"
	    }
	  }
	}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	resolved, err := resolveCommandOptions(CommandOptions{ConfigPath: configPath})
	if err != nil {
		t.Fatalf("resolveCommandOptions: %v", err)
	}
	if resolved.RuntimeDeployment != "remote" {
		t.Fatalf("expected remote runtime deployment, got %q", resolved.RuntimeDeployment)
	}
	if resolved.RuntimeURL != "https://runtime.example.com" {
		t.Fatalf("expected remote runtime url from config, got %q", resolved.RuntimeURL)
	}
}

func TestRootCommandUsesProvidedRemoteRuntimeBearerToken(t *testing.T) {
	t.Setenv("OAS_REMOTE_TOKEN", "token-123")

	dir := t.TempDir()
	configPath := filepath.Join(dir, ".cli.json")
	if err := os.WriteFile(configPath, []byte(`{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "runtime": {
	    "mode": "remote",
	    "remote": {
	      "url": "https://runtime.example.com",
	      "oauth": {
	        "mode": "providedToken",
	        "audience": "oasclird",
	        "scopes": ["bundle:payments"],
	        "tokenRef": "env:OAS_REMOTE_TOKEN"
	      }
	    }
	  },
	  "sources": {
	    "tickets": {
	      "type": "openapi",
	      "uri": "https://example.com/openapi.json"
	    }
	  },
	  "services": {
	    "tickets": {
	      "source": "tickets"
	    }
	  }
	}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	runtimeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer token-123" {
			t.Fatalf("expected runtime bearer token, got %q", got)
		}
		switch r.URL.Path {
		case "/v1/catalog/effective":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"catalog": map[string]any{
					"services": []map[string]any{},
					"tools":    []map[string]any{},
				},
				"view": map[string]any{
					"name":  "discover",
					"mode":  "discover",
					"tools": []map[string]any{},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer runtimeServer.Close()

	var stdout bytes.Buffer
	cmd, err := NewRootCommand(CommandOptions{
		RuntimeURL: runtimeServer.URL,
		ConfigPath: configPath,
		Stdout:     &stdout,
		Stderr:     &stdout,
	}, []string{"catalog", "list", "--format", "json"})
	if err != nil {
		t.Fatalf("NewRootCommand returned error: %v", err)
	}
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
}

func TestRootCommandUsesOAuthClientRemoteRuntimeBearerToken(t *testing.T) {
	t.Setenv("OAS_REMOTE_CLIENT_ID", "runtime-client")
	t.Setenv("OAS_REMOTE_CLIENT_SECRET", "runtime-secret")

	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		if got := r.PostForm.Get("grant_type"); got != "client_credentials" {
			t.Fatalf("expected client_credentials grant, got %q", got)
		}
		if got := r.PostForm.Get("client_id"); got != "runtime-client" {
			t.Fatalf("expected oauth client id, got %q", got)
		}
		if got := r.PostForm.Get("client_secret"); got != "runtime-secret" {
			t.Fatalf("expected oauth client secret, got %q", got)
		}
		if got := r.PostForm.Get("audience"); got != "oasclird" {
			t.Fatalf("expected audience oasclird, got %q", got)
		}
		if got := r.PostForm.Get("scope"); got != "bundle:payments" {
			t.Fatalf("expected scope bundle:payments, got %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "oauth-client-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer authServer.Close()

	runtimeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer oauth-client-token" {
			t.Fatalf("expected runtime bearer token from oauth client flow, got %q", got)
		}
		switch r.URL.Path {
		case "/v1/catalog/effective":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"catalog": map[string]any{
					"services": []map[string]any{},
					"tools":    []map[string]any{},
				},
				"view": map[string]any{
					"name":  "discover",
					"mode":  "discover",
					"tools": []map[string]any{},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer runtimeServer.Close()

	dir := t.TempDir()
	configPath := filepath.Join(dir, ".cli.json")
	if err := os.WriteFile(configPath, []byte(fmt.Sprintf(`{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "runtime": {
	    "mode": "remote",
	    "remote": {
	      "url": %q,
	      "oauth": {
	        "mode": "oauthClient",
	        "audience": "oasclird",
	        "scopes": ["bundle:payments"],
	        "client": {
	          "tokenURL": %q,
	          "clientId": { "type": "env", "value": "OAS_REMOTE_CLIENT_ID" },
	          "clientSecret": { "type": "env", "value": "OAS_REMOTE_CLIENT_SECRET" }
	        }
	      }
	    }
	  },
	  "sources": {
	    "tickets": {
	      "type": "openapi",
	      "uri": "https://example.com/openapi.json"
	    }
	  },
	  "services": {
	    "tickets": {
	      "source": "tickets"
	    }
	  }
	}`, runtimeServer.URL, authServer.URL+"/token")), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var stdout bytes.Buffer
	cmd, err := NewRootCommand(CommandOptions{
		ConfigPath: configPath,
		StateDir:   filepath.Join(dir, "state"),
		Stdout:     &stdout,
		Stderr:     &stdout,
	}, []string{"catalog", "list", "--format", "json"})
	if err != nil {
		t.Fatalf("NewRootCommand returned error: %v", err)
	}
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
}

func TestRootCommandUsesRemoteBrowserLoginBearerToken(t *testing.T) {
	previousAcquirer := runtimeBrowserLoginTokenAcquirer
	t.Cleanup(func() { runtimeBrowserLoginTokenAcquirer = previousAcquirer })
	runtimeBrowserLoginTokenAcquirer = func(request runtimeBrowserLoginRequest) (string, error) {
		if request.Metadata.AuthorizationURL != "https://auth.example.com/authorize" {
			t.Fatalf("expected authorization URL from runtime metadata, got %q", request.Metadata.AuthorizationURL)
		}
		if request.Metadata.TokenURL != "https://auth.example.com/token" {
			t.Fatalf("expected token URL from runtime metadata, got %q", request.Metadata.TokenURL)
		}
		if request.Metadata.ClientID != "browser-client" {
			t.Fatalf("expected browser client id from runtime metadata, got %q", request.Metadata.ClientID)
		}
		if request.Audience != "oasclird" {
			t.Fatalf("expected runtime audience oasclird, got %q", request.Audience)
		}
		if len(request.Scopes) != 1 || request.Scopes[0] != "bundle:payments" {
			t.Fatalf("expected runtime scopes from config, got %#v", request.Scopes)
		}
		if request.CallbackPort != 9123 {
			t.Fatalf("expected callback port 9123, got %d", request.CallbackPort)
		}
		if request.StateDir == "" {
			t.Fatalf("expected state dir for browser login token caching")
		}
		return "browser-login-token", nil
	}

	runtimeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/browser-config":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"authorizationURL": "https://auth.example.com/authorize",
				"tokenURL":         "https://auth.example.com/token",
				"clientId":         "browser-client",
				"audience":         "oasclird",
			})
		case "/v1/catalog/effective":
			if got := r.Header.Get("Authorization"); got != "Bearer browser-login-token" {
				t.Fatalf("expected browser login bearer token, got %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"catalog": map[string]any{
					"services": []map[string]any{},
					"tools":    []map[string]any{},
				},
				"view": map[string]any{
					"name":  "discover",
					"mode":  "discover",
					"tools": []map[string]any{},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer runtimeServer.Close()

	dir := t.TempDir()
	configPath := filepath.Join(dir, ".cli.json")
	if err := os.WriteFile(configPath, []byte(fmt.Sprintf(`{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "runtime": {
	    "mode": "remote",
	    "remote": {
	      "url": %q,
	      "oauth": {
	        "mode": "browserLogin",
	        "audience": "oasclird",
	        "scopes": ["bundle:payments"],
	        "browserLogin": {
	          "callbackPort": 9123
	        }
	      }
	    }
	  },
	  "sources": {
	    "tickets": {
	      "type": "openapi",
	      "uri": "https://example.com/openapi.json"
	    }
	  },
	  "services": {
	    "tickets": {
	      "source": "tickets"
	    }
	  }
	}`, runtimeServer.URL)), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var stdout bytes.Buffer
	cmd, err := NewRootCommand(CommandOptions{
		ConfigPath: configPath,
		StateDir:   filepath.Join(dir, "state"),
		Stdout:     &stdout,
		Stderr:     &stdout,
	}, []string{"catalog", "list", "--format", "json"})
	if err != nil {
		t.Fatalf("NewRootCommand returned error: %v", err)
	}
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
}

func TestResolveCommandOptionsUsesTerminalScopedLocalInstanceID(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".cli.json")
	if err := os.WriteFile(configPath, []byte(`{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "runtime": {
	    "mode": "local",
	    "local": {
	      "sessionScope": "terminal",
	      "heartbeatSeconds": 15,
	      "missedHeartbeatLimit": 3,
	      "share": "exclusive",
	      "shutdown": "when-owner-exits"
	    }
	  },
	  "mcpServers": {
	    "filesystem": {
	      "type": "stdio",
	      "command": "npx"
	    }
	  }
	}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	previousStarter := managedRuntimeStarter
	previousTerminalID := terminalSessionIdentityProvider
	previousHandshake := localSessionHandshake
	t.Cleanup(func() {
		managedRuntimeStarter = previousStarter
		terminalSessionIdentityProvider = previousTerminalID
		localSessionHandshake = previousHandshake
	})
	terminalSessionIdentityProvider = func() string { return "/dev/pts/42" }

	var capturedInstanceID string
	managedRuntimeStarter = func(options CommandOptions) (string, error) {
		capturedInstanceID = options.InstanceID
		return "http://127.0.0.1:18889", nil
	}
	localSessionHandshake = func(options CommandOptions) (CommandOptions, error) { return options, nil }

	resolved, err := resolveCommandOptions(CommandOptions{
		ConfigPath:        configPath,
		RuntimeDeployment: "local",
		StateDir:          filepath.Join(dir, "state"),
	})
	if err != nil {
		t.Fatalf("resolveCommandOptions: %v", err)
	}
	if resolved.RuntimeURL != "http://127.0.0.1:18889" {
		t.Fatalf("expected managed local runtime url, got %q", resolved.RuntimeURL)
	}
	if capturedInstanceID == "" {
		t.Fatalf("expected terminal-scoped instance id to be derived")
	}
	if capturedInstanceID == instance.DeriveID("", configPath) {
		t.Fatalf("expected terminal-scoped instance id to differ from plain config-derived id")
	}
	if !bytes.Contains([]byte(capturedInstanceID), []byte("devpts42")) {
		t.Fatalf("expected terminal-scoped instance id to include terminal identity, got %q", capturedInstanceID)
	}
}

func TestResolveCommandOptionsUsesShareKeyForSharedGroupLocalRuntime(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".cli.json")
	if err := os.WriteFile(configPath, []byte(`{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "runtime": {
	    "mode": "local",
	    "local": {
	      "sessionScope": "shared-group",
	      "heartbeatSeconds": 15,
	      "missedHeartbeatLimit": 3,
	      "share": "group",
	      "shareKey": "team-a",
	      "shutdown": "manual"
	    }
	  },
	  "mcpServers": {
	    "filesystem": {
	      "type": "stdio",
	      "command": "npx"
	    }
	  }
	}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	previousStarter := managedRuntimeStarter
	previousHandshake := localSessionHandshake
	t.Cleanup(func() {
		managedRuntimeStarter = previousStarter
		localSessionHandshake = previousHandshake
	})

	var capturedInstanceID string
	managedRuntimeStarter = func(options CommandOptions) (string, error) {
		capturedInstanceID = options.InstanceID
		return "http://127.0.0.1:18890", nil
	}
	localSessionHandshake = func(options CommandOptions) (CommandOptions, error) { return options, nil }

	resolved, err := resolveCommandOptions(CommandOptions{
		ConfigPath: configPath,
		StateDir:   filepath.Join(dir, "state"),
	})
	if err != nil {
		t.Fatalf("resolveCommandOptions: %v", err)
	}
	if resolved.RuntimeURL != "http://127.0.0.1:18890" {
		t.Fatalf("expected managed local runtime url, got %q", resolved.RuntimeURL)
	}
	if capturedInstanceID == "" {
		t.Fatalf("expected shared-group instance id to be derived")
	}
	if !bytes.Contains([]byte(capturedInstanceID), []byte("team-a")) {
		t.Fatalf("expected shared-group instance id to include share key, got %q", capturedInstanceID)
	}
}

func TestConfigureManagedRuntimeCommandSetsParentDeathSignalOnLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("parent death signal is only configured on linux")
	}

	cmd := exec.Command("true")
	configureManagedRuntimeCommand(cmd)

	if cmd.SysProcAttr == nil {
		t.Fatalf("expected SysProcAttr to be configured")
	}
	if cmd.SysProcAttr.Pdeathsig != syscall.SIGTERM {
		t.Fatalf("expected parent death signal SIGTERM, got %v", cmd.SysProcAttr.Pdeathsig)
	}
}

func TestManagedRuntimeArgsIncludeLifecycleFlags(t *testing.T) {
	args := managedRuntimeArgs(CommandOptions{
		ConfigPath:        "/tmp/project/.cli.json",
		InstanceID:        "runtime-1",
		StateDir:          "/tmp/state",
		ConfigFingerprint: "fp-1",
	}, &configpkg.RuntimeConfig{
		Local: &configpkg.LocalRuntimeConfig{
			SessionScope:         "shared-group",
			HeartbeatSeconds:     15,
			MissedHeartbeatLimit: 3,
			Shutdown:             "manual",
			Share:                "group",
			ShareKey:             "team-a",
		},
	})

	expected := []string{
		"--config", "/tmp/project/.cli.json",
		"--instance-id", "runtime-1",
		"--state-dir", "/tmp/state",
		"--heartbeat-seconds", "15",
		"--missed-heartbeat-limit", "3",
		"--shutdown", "manual",
		"--session-scope", "shared-group",
		"--share", "group",
		"--config-fingerprint", "fp-1",
	}
	for i := 0; i < len(expected); i += 2 {
		found := false
		for j := 0; j < len(args)-1; j++ {
			if args[j] == expected[i] && args[j+1] == expected[i+1] {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected managed runtime args to include %q %q, got %#v", expected[i], expected[i+1], args)
		}
	}
}

func TestRootCommandRuntimeCommandsUseRuntimeEndpoints(t *testing.T) {
	runtimeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/catalog/effective":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"catalog": map[string]any{
					"services": []map[string]any{},
					"tools":    []map[string]any{},
				},
				"view": map[string]any{
					"name":  "discover",
					"mode":  "discover",
					"tools": []map[string]any{},
				},
			})
		case "/v1/runtime/info":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"instanceId": "team-a",
				"url":        "http://127.0.0.1:18765",
			})
		case "/v1/runtime/stop":
			_ = json.NewEncoder(w).Encode(map[string]any{"stopped": true})
		case "/v1/runtime/session-close":
			_ = json.NewEncoder(w).Encode(map[string]any{"closed": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer runtimeServer.Close()

	t.Run("info", func(t *testing.T) {
		var stdout bytes.Buffer
		cmd, err := NewRootCommand(CommandOptions{
			RuntimeURL: runtimeServer.URL,
			ConfigPath: "/tmp/project/.cli.json",
			Stdout:     &stdout,
			Stderr:     &stdout,
		}, []string{"runtime", "info", "--format", "json"})
		if err != nil {
			t.Fatalf("NewRootCommand returned error: %v", err)
		}
		if err := cmd.Execute(); err != nil {
			t.Fatalf("runtime info command failed: %v", err)
		}
		if !bytes.Contains(stdout.Bytes(), []byte(`"instanceId":"team-a"`)) {
			t.Fatalf("expected runtime info output, got %s", stdout.String())
		}
	})

	t.Run("stop", func(t *testing.T) {
		var stdout bytes.Buffer
		cmd, err := NewRootCommand(CommandOptions{
			RuntimeURL: runtimeServer.URL,
			ConfigPath: "/tmp/project/.cli.json",
			Stdout:     &stdout,
			Stderr:     &stdout,
		}, []string{"runtime", "stop", "--format", "json"})
		if err != nil {
			t.Fatalf("NewRootCommand returned error: %v", err)
		}
		if err := cmd.Execute(); err != nil {
			t.Fatalf("runtime stop command failed: %v", err)
		}
		if !bytes.Contains(stdout.Bytes(), []byte(`"stopped":true`)) {
			t.Fatalf("expected runtime stop output, got %s", stdout.String())
		}
	})

	t.Run("session-close", func(t *testing.T) {
		var stdout bytes.Buffer
		cmd, err := NewRootCommand(CommandOptions{
			RuntimeURL: runtimeServer.URL,
			ConfigPath: "/tmp/project/.cli.json",
			Stdout:     &stdout,
			Stderr:     &stdout,
		}, []string{"runtime", "session-close", "--format", "json"})
		if err != nil {
			t.Fatalf("NewRootCommand returned error: %v", err)
		}
		if err := cmd.Execute(); err != nil {
			t.Fatalf("runtime session-close command failed: %v", err)
		}
		if !bytes.Contains(stdout.Bytes(), []byte(`"closed":true`)) {
			t.Fatalf("expected runtime session-close output, got %s", stdout.String())
		}
	})
}

func TestRootCommandSendsHeartbeatForManagedLocalRuntime(t *testing.T) {
	previousHandshake := localSessionHandshake
	t.Cleanup(func() { localSessionHandshake = previousHandshake })
	localSessionHandshake = func(options CommandOptions) (CommandOptions, error) {
		options.SessionID = "sess-1"
		options.HeartbeatEnabled = true
		return options, nil
	}

	heartbeats := []string{}
	runtimeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/catalog/effective":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"catalog": map[string]any{
					"services": []map[string]any{},
					"tools":    []map[string]any{},
				},
				"view": map[string]any{
					"name":  "discover",
					"mode":  "discover",
					"tools": []map[string]any{},
				},
			})
		case "/v1/runtime/heartbeat":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode heartbeat payload: %v", err)
			}
			heartbeats = append(heartbeats, payload["sessionId"].(string))
			_ = json.NewEncoder(w).Encode(map[string]any{"renewed": true, "sessionId": payload["sessionId"]})
		default:
			http.NotFound(w, r)
		}
	}))
	defer runtimeServer.Close()

	var stdout bytes.Buffer
	cmd, err := NewRootCommand(CommandOptions{
		RuntimeURL:        runtimeServer.URL,
		RuntimeDeployment: "local",
		ConfigPath:        "/tmp/project/.cli.json",
		Stdout:            &stdout,
		Stderr:            &stdout,
	}, []string{"catalog", "list", "--format", "json"})
	if err != nil {
		t.Fatalf("NewRootCommand returned error: %v", err)
	}
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(heartbeats) != 2 {
		t.Fatalf("expected heartbeat before and after command execution, got %#v", heartbeats)
	}
	if heartbeats[0] != "sess-1" || heartbeats[1] != "sess-1" {
		t.Fatalf("expected heartbeats for sess-1, got %#v", heartbeats)
	}
}

func TestRootCommandSendsSessionCloseOnLocalRuntimeTeardown(t *testing.T) {
	previousHandshake := localSessionHandshake
	t.Cleanup(func() { localSessionHandshake = previousHandshake })
	localSessionHandshake = func(options CommandOptions) (CommandOptions, error) {
		options.SessionID = "sess-1"
		return options, nil
	}

	var closedSessionID string
	runtimeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/catalog/effective":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"catalog": map[string]any{
					"services": []map[string]any{},
					"tools":    []map[string]any{},
				},
				"view": map[string]any{
					"name":  "discover",
					"mode":  "discover",
					"tools": []map[string]any{},
				},
			})
		case "/v1/runtime/session-close":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode session-close payload: %v", err)
			}
			closedSessionID, _ = payload["sessionId"].(string)
			_ = json.NewEncoder(w).Encode(map[string]any{"closed": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer runtimeServer.Close()

	var stdout bytes.Buffer
	cmd, err := NewRootCommand(CommandOptions{
		RuntimeURL:        runtimeServer.URL,
		RuntimeDeployment: "local",
		ConfigPath:        "/tmp/project/.cli.json",
		Stdout:            &stdout,
		Stderr:            &stdout,
	}, []string{"runtime", "session-close", "--format", "json"})
	if err != nil {
		t.Fatalf("NewRootCommand returned error: %v", err)
	}
	if err := cmd.Execute(); err != nil {
		t.Fatalf("runtime session-close command failed: %v", err)
	}
	if closedSessionID != "sess-1" {
		t.Fatalf("expected runtime session-close to send sess-1, got %q", closedSessionID)
	}
}

func TestResolveCommandOptionsFailsOnRuntimeAttachConflict(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".cli.json")
	if err := os.WriteFile(configPath, []byte(`{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "runtime": {
	    "mode": "local",
	    "local": {
	      "sessionScope": "terminal",
	      "heartbeatSeconds": 15,
	      "missedHeartbeatLimit": 3,
	      "shutdown": "when-owner-exits",
	      "share": "exclusive"
	    }
	  },
	  "mcpServers": {
	    "filesystem": {
	      "type": "stdio",
	      "command": "npx"
	    }
	  }
	}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	previousHandshake := localSessionHandshake
	t.Cleanup(func() { localSessionHandshake = previousHandshake })
	localSessionHandshake = performLocalSessionHandshake

	runtimeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/runtime/info":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"lifecycle": map[string]any{
					"capabilities": []string{"heartbeat"},
				},
			})
		case "/v1/runtime/heartbeat":
			http.Error(w, "runtime_attach_conflict", http.StatusConflict)
		default:
			http.NotFound(w, r)
		}
	}))
	defer runtimeServer.Close()

	_, err := resolveCommandOptions(CommandOptions{
		ConfigPath:        configPath,
		RuntimeDeployment: "local",
		RuntimeURL:        runtimeServer.URL,
	})
	if err == nil || !strings.Contains(err.Error(), "runtime_attach_conflict") {
		t.Fatalf("expected runtime_attach_conflict error, got %v", err)
	}
}

func TestResolveCommandOptionsFailsOnRuntimeAttachMismatch(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".cli.json")
	if err := os.WriteFile(configPath, []byte(`{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "runtime": {
	    "mode": "local",
	    "local": {
	      "sessionScope": "terminal",
	      "heartbeatSeconds": 15,
	      "missedHeartbeatLimit": 3,
	      "shutdown": "when-owner-exits",
	      "share": "exclusive"
	    }
	  },
	  "mcpServers": {
	    "filesystem": {
	      "type": "stdio",
	      "command": "npx"
	    }
	  }
	}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	previousHandshake := localSessionHandshake
	t.Cleanup(func() { localSessionHandshake = previousHandshake })
	localSessionHandshake = performLocalSessionHandshake

	runtimeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/runtime/info":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"lifecycle": map[string]any{
					"capabilities": []string{"heartbeat"},
				},
			})
		case "/v1/runtime/heartbeat":
			http.Error(w, "runtime_attach_mismatch", http.StatusConflict)
		default:
			http.NotFound(w, r)
		}
	}))
	defer runtimeServer.Close()

	_, err := resolveCommandOptions(CommandOptions{
		ConfigPath:        configPath,
		RuntimeDeployment: "local",
		RuntimeURL:        runtimeServer.URL,
	})
	if err == nil || !strings.Contains(err.Error(), "runtime_attach_mismatch") {
		t.Fatalf("expected runtime_attach_mismatch error, got %v", err)
	}
}
