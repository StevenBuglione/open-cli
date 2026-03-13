package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/StevenBuglione/oas-cli-go/pkg/catalog"
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
