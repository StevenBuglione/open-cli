package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	authpkg "github.com/StevenBuglione/open-cli/cmd/ocli/internal/auth"
	runtimepkg "github.com/StevenBuglione/open-cli/cmd/ocli/internal/runtime"
	"github.com/StevenBuglione/open-cli/pkg/catalog"
	configpkg "github.com/StevenBuglione/open-cli/pkg/config"
	"github.com/StevenBuglione/open-cli/pkg/instance"
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
	}, []string{"tickets", "list", "--state", "open", "--format", "json"})
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
		}, []string{"tickets", "create", "--body", "@" + bodyFile.Name()})
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
		}, []string{"tickets", "create", "--body", "-"})
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
	}, []string{"tickets", "list-tickets", "--format", "json"})
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
	t.Setenv("OCLI_RUNTIME_URL", "")
	t.Setenv("OCLI_EMBEDDED", "")

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

func TestResolveCommandOptionsUsesOCLIEnvironmentVariables(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("OCLI_INSTANCE_ID", "ocli-instance")
	t.Setenv("OCLI_STATE_DIR", stateDir)
	t.Setenv("OCLI_RUNTIME_URL", "http://127.0.0.1:18765")

	resolved, err := resolveCommandOptions(CommandOptions{})
	if err != nil {
		t.Fatalf("resolveCommandOptions: %v", err)
	}
	if resolved.InstanceID != "ocli-instance" {
		t.Fatalf("expected instance id from OCLI_INSTANCE_ID, got %q", resolved.InstanceID)
	}
	if resolved.StateDir != stateDir {
		t.Fatalf("expected state dir from OCLI_STATE_DIR, got %q", resolved.StateDir)
	}
	if resolved.RuntimeURL != "http://127.0.0.1:18765" {
		t.Fatalf("expected runtime url from OCLI_RUNTIME_URL, got %q", resolved.RuntimeURL)
	}
}

func TestResolveCommandOptionsUsesOCLIEmbeddedEnvironmentVariable(t *testing.T) {
	t.Setenv("OCLI_EMBEDDED", "1")

	resolved, err := resolveCommandOptions(CommandOptions{})
	if err != nil {
		t.Fatalf("resolveCommandOptions: %v", err)
	}
	if !resolved.Embedded {
		t.Fatalf("expected embedded mode when OCLI_EMBEDDED=1")
	}
	if resolved.RuntimeDeployment != "embedded" {
		t.Fatalf("expected embedded runtime deployment, got %q", resolved.RuntimeDeployment)
	}
}

func TestResolveCommandOptionsCleansManagedMCPProcessesWhenLocalRuntimeRegistryIsStale(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("managed process cleanup test uses POSIX sleep")
	}

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

	deadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	deadURL := deadServer.URL
	deadServer.Close()

	stateDir := filepath.Join(dir, "state")
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

	sleep := exec.Command("sleep", "30")
	if err := sleep.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	waitDone := make(chan error, 1)
	go func() {
		waitDone <- sleep.Wait()
	}()
	t.Cleanup(func() {
		select {
		case <-waitDone:
			return
		default:
		}
		if sleep.Process != nil {
			_ = sleep.Process.Signal(syscall.SIGKILL)
		}
		select {
		case <-waitDone:
		case <-time.After(2 * time.Second):
		}
	})

	registryPath := filepath.Join(paths.StateDir, "managed-mcp-pids.json")
	if err := os.MkdirAll(filepath.Dir(registryPath), 0o755); err != nil {
		t.Fatalf("mkdir managed registry dir: %v", err)
	}
	if err := os.WriteFile(registryPath, []byte(fmt.Sprintf(`{"pids":[%d]}`, sleep.Process.Pid)), 0o644); err != nil {
		t.Fatalf("write managed registry: %v", err)
	}

	previousStarter := managedRuntimeStarter
	previousHandshake := localSessionHandshake
	t.Cleanup(func() {
		managedRuntimeStarter = previousStarter
		localSessionHandshake = previousHandshake
	})
	managedRuntimeStarter = func(options CommandOptions) (string, error) {
		return "http://127.0.0.1:18888", nil
	}
	localSessionHandshake = func(options CommandOptions) (CommandOptions, error) { return options, nil }

	resolved, err := resolveCommandOptions(CommandOptions{
		ConfigPath:        configPath,
		RuntimeDeployment: "local",
		InstanceID:        "stale",
		StateDir:          stateDir,
	})
	if err != nil {
		t.Fatalf("resolveCommandOptions: %v", err)
	}
	if resolved.RuntimeURL != "http://127.0.0.1:18888" {
		t.Fatalf("expected managed local runtime url after stale cleanup, got %q", resolved.RuntimeURL)
	}

	select {
	case err := <-waitDone:
		if err != nil && !strings.Contains(err.Error(), "signal: terminated") && !strings.Contains(err.Error(), "signal: killed") {
			t.Fatalf("expected stale managed process to be terminated, got wait err %v", err)
		}
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("expected stale managed process to be terminated before local runtime restart")
	}
}

func TestResolveCommandOptionsFailsWhenManagedCleanupFailsForStaleLocalRuntime(t *testing.T) {
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

	deadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	deadURL := deadServer.URL
	deadServer.Close()

	stateDir := filepath.Join(dir, "state")
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

	previousCleanup := cleanupManagedProcesses
	previousStarter := managedRuntimeStarter
	t.Cleanup(func() {
		cleanupManagedProcesses = previousCleanup
		managedRuntimeStarter = previousStarter
	})
	cleanupManagedProcesses = func(_ context.Context, _ string) error {
		return fmt.Errorf("cleanup failed")
	}
	started := 0
	managedRuntimeStarter = func(options CommandOptions) (string, error) {
		started++
		return "http://127.0.0.1:18888", nil
	}

	_, err = resolveCommandOptions(CommandOptions{
		ConfigPath:        configPath,
		RuntimeDeployment: "local",
		InstanceID:        "stale",
		StateDir:          stateDir,
	})
	if err == nil || !strings.Contains(err.Error(), "cleanup failed") {
		t.Fatalf("expected stale managed cleanup failure, got %v", err)
	}
	if started != 0 {
		t.Fatalf("expected local runtime restart to fail closed, starter ran %d times", started)
	}
}

func TestResolveCommandOptionsTreatsDeadRuntimePIDAsStaleEvenWhenURLIsReachable(t *testing.T) {
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

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	stateDir := filepath.Join(dir, "state")
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
		URL:        "http://" + listener.Addr().String(),
		PID:        999999,
		AuditPath:  paths.AuditPath,
		CacheDir:   paths.CacheDir,
	}); err != nil {
		t.Fatalf("WriteRuntimeInfo: %v", err)
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
		return "http://127.0.0.1:18889", nil
	}
	localSessionHandshake = func(options CommandOptions) (CommandOptions, error) { return options, nil }

	resolved, err := resolveCommandOptions(CommandOptions{
		ConfigPath:        configPath,
		RuntimeDeployment: "local",
		InstanceID:        "stale",
		StateDir:          stateDir,
	})
	if err != nil {
		t.Fatalf("resolveCommandOptions: %v", err)
	}
	if started != 1 {
		t.Fatalf("expected stale runtime with dead pid to restart once, got %d", started)
	}
	if resolved.RuntimeURL != "http://127.0.0.1:18889" {
		t.Fatalf("expected replacement managed runtime url, got %q", resolved.RuntimeURL)
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
	        "audience": "oclird",
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
	        "audience": "oclird",
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
		if got := r.PostForm.Get("audience"); got != "oclird" {
			t.Fatalf("expected audience oclird, got %q", got)
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
		switch r.URL.Path {
		case "/v1/runtime/info":
			if got := r.Header.Get("Authorization"); got != "" {
				t.Fatalf("expected unauthenticated runtime info discovery, got %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"contractVersion": "1.1",
				"capabilities":    []string{"catalog", "brokered-auth"},
				"auth": map[string]any{
					"required":                true,
					"audience":                "oclird",
					"scopePrefixes":           []string{"bundle:", "profile:", "tool:"},
					"tokenValidationProfiles": []string{"oidc_jwks"},
				},
			})
		case "/v1/catalog/effective":
			if got := r.Header.Get("Authorization"); got != "Bearer oauth-client-token" {
				t.Fatalf("expected runtime bearer token from oauth client flow, got %q", got)
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
	        "mode": "oauthClient",
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

func TestHTTPRuntimeClientRefreshesExpiredOAuthClientTokenOnce(t *testing.T) {
	t.Setenv("OAS_REMOTE_CLIENT_ID", "runtime-client")
	t.Setenv("OAS_REMOTE_CLIENT_SECRET", "runtime-secret")

	tokenFetches := 0
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		tokenFetches++
		expiresIn := 3600
		if tokenFetches == 1 {
			expiresIn = 1
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": fmt.Sprintf("oauth-client-token-%d", tokenFetches),
			"token_type":   "Bearer",
			"expires_in":   expiresIn,
		})
	}))
	defer authServer.Close()

	var seenAuth []string
	runtimeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = append(seenAuth, r.Header.Get("Authorization"))
		switch r.URL.Path {
		case "/v1/runtime/info":
			if got := r.Header.Get("Authorization"); got != "" {
				t.Fatalf("expected unauthenticated runtime info discovery, got %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"contractVersion": "1.1",
				"capabilities":    []string{"catalog", "brokered-auth"},
				"auth": map[string]any{
					"required":                true,
					"audience":                "oclird",
					"scopePrefixes":           []string{"bundle:", "profile:", "tool:"},
					"tokenValidationProfiles": []string{"oidc_jwks"},
				},
			})
		case "/v1/catalog/effective":
			if got := r.Header.Get("Authorization"); got == "Bearer oauth-client-token-1" && len(seenAuth) > 1 {
				http.Error(w, "authn_failed", http.StatusUnauthorized)
				return
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
	        "mode": "oauthClient",
	        "audience": "oclird",
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

	resolved, err := resolveCommandOptions(CommandOptions{
		ConfigPath: configPath,
		StateDir:   filepath.Join(dir, "state"),
	})
	if err != nil {
		t.Fatalf("resolveCommandOptions: %v", err)
	}
	client, err := newRuntimeClient(resolved)
	if err != nil {
		t.Fatalf("newRuntimeClient: %v", err)
	}

	if _, err := client.FetchCatalog(runtimepkg.CatalogFetchOptions{ConfigPath: resolved.ConfigPath, Mode: resolved.Mode, AgentProfile: resolved.AgentProfile, RuntimeToken: resolved.RuntimeToken}); err != nil {
		t.Fatalf("initial FetchCatalog: %v", err)
	}

	time.Sleep(1100 * time.Millisecond)

	if _, err := client.FetchCatalog(runtimepkg.CatalogFetchOptions{ConfigPath: resolved.ConfigPath, Mode: resolved.Mode, AgentProfile: resolved.AgentProfile, RuntimeToken: resolved.RuntimeToken}); err != nil {
		t.Fatalf("expected expired remote runtime token to refresh, got %v", err)
	}
	if tokenFetches != 2 {
		t.Fatalf("expected exactly 2 token fetches after one refresh, got %d", tokenFetches)
	}
	if got := seenAuth[len(seenAuth)-1]; got != "Bearer oauth-client-token-2" {
		t.Fatalf("expected refreshed bearer token on second catalog fetch, got %q", got)
	}
}

func TestHTTPRuntimeClientRefreshesAfterAuthnFailedOnNextRequest(t *testing.T) {
	t.Setenv("OAS_REMOTE_CLIENT_ID", "runtime-client")
	t.Setenv("OAS_REMOTE_CLIENT_SECRET", "runtime-secret")

	tokenFetches := 0
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		tokenFetches++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": fmt.Sprintf("oauth-client-token-%d", tokenFetches),
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer authServer.Close()

	var seenAuth []string
	executeCalls := 0
	runtimeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = append(seenAuth, r.Header.Get("Authorization"))
		switch r.URL.Path {
		case "/v1/runtime/info":
			if got := r.Header.Get("Authorization"); got != "" {
				t.Fatalf("expected unauthenticated runtime info discovery, got %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"contractVersion": "1.1",
				"capabilities":    []string{"catalog", "brokered-auth"},
				"auth": map[string]any{
					"required":                true,
					"audience":                "oclird",
					"scopePrefixes":           []string{"bundle:", "profile:", "tool:"},
					"tokenValidationProfiles": []string{"oidc_jwks"},
				},
			})
		case "/v1/tools/execute":
			executeCalls++
			if executeCalls == 1 {
				http.Error(w, "authn_failed", http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "body": map[string]any{"ok": true}})
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
	        "audience": "oclird",
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

	resolved, err := resolveCommandOptions(CommandOptions{
		ConfigPath: configPath,
		StateDir:   filepath.Join(dir, "state"),
	})
	if err != nil {
		t.Fatalf("resolveCommandOptions: %v", err)
	}
	client, err := newRuntimeClient(resolved)
	if err != nil {
		t.Fatalf("newRuntimeClient: %v", err)
	}

	_, err = client.Execute(executeRequest{ToolID: "tickets:listTickets"})
	if err == nil || !strings.Contains(err.Error(), "authn_failed") {
		t.Fatalf("expected first execute to fail with authn_failed, got %v", err)
	}

	if _, err := client.FetchCatalog(runtimepkg.CatalogFetchOptions{ConfigPath: resolved.ConfigPath, Mode: resolved.Mode, AgentProfile: resolved.AgentProfile, RuntimeToken: resolved.RuntimeToken}); err != nil {
		t.Fatalf("expected next request to refresh after authn_failed, got %v", err)
	}
	if tokenFetches != 2 {
		t.Fatalf("expected exactly 2 token fetches after next-request refresh, got %d", tokenFetches)
	}
	if got := seenAuth[len(seenAuth)-1]; got != "Bearer oauth-client-token-2" {
		t.Fatalf("expected refreshed bearer token after authn_failed, got %q", got)
	}
}

func TestRootCommandUsesRemoteBrowserLoginBearerToken(t *testing.T) {
	previousAcquirer := authpkg.BrowserLoginTokenAcquirer
	t.Cleanup(func() { authpkg.BrowserLoginTokenAcquirer = previousAcquirer })
	authpkg.BrowserLoginTokenAcquirer = func(request runtimeBrowserLoginRequest) (string, error) {
		if request.Metadata.AuthorizationURL != "https://auth.example.com/authorize" {
			t.Fatalf("expected authorization URL from runtime metadata, got %q", request.Metadata.AuthorizationURL)
		}
		if request.Metadata.TokenURL != "https://auth.example.com/token" {
			t.Fatalf("expected token URL from runtime metadata, got %q", request.Metadata.TokenURL)
		}
		if request.Metadata.ClientID != "browser-client" {
			t.Fatalf("expected browser client id from runtime metadata, got %q", request.Metadata.ClientID)
		}
		if request.Audience != "oclird" {
			t.Fatalf("expected runtime audience oclird, got %q", request.Audience)
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
		case "/v1/runtime/info":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"contractVersion": "1.1",
				"capabilities":    []string{"catalog", "brokered-auth", "authorization-envelope"},
				"auth": map[string]any{
					"required":                true,
					"audience":                "oclird",
					"scopePrefixes":           []string{"bundle:", "profile:", "tool:"},
					"tokenValidationProfiles": []string{"oidc_jwks"},
					"browserLogin": map[string]any{
						"configured":     true,
						"configEndpoint": "/v1/runtime/browser-auth",
					},
				},
			})
		case "/v1/runtime/browser-auth":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"authorizationURL": "https://auth.example.com/authorize",
				"tokenURL":         "https://auth.example.com/token",
				"clientId":         "browser-client",
				"audience":         "oclird",
			})
		case "/v1/auth/browser-config":
			t.Fatalf("expected browser login metadata endpoint from runtime info, not default path")
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

func TestRootCommandFailsClosedOnInvalidRuntimeBrowserLoginMetadata(t *testing.T) {
	previousAcquirer := authpkg.BrowserLoginTokenAcquirer
	t.Cleanup(func() { authpkg.BrowserLoginTokenAcquirer = previousAcquirer })
	authpkg.BrowserLoginTokenAcquirer = func(request runtimeBrowserLoginRequest) (string, error) {
		t.Fatalf("expected invalid runtime metadata to fail before browser login token acquisition")
		return "", nil
	}

	runtimeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/runtime/info":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"contractVersion": "1.1",
				"capabilities":    []string{"catalog", "brokered-auth", "authorization-envelope"},
				"auth": map[string]any{
					"required":                true,
					"audience":                "oclird",
					"scopePrefixes":           []string{"bundle:", "profile:", "tool:"},
					"tokenValidationProfiles": []string{"oidc_jwks"},
					"browserLogin": map[string]any{
						"configured":     true,
						"configEndpoint": "/v1/runtime/browser-auth",
					},
				},
			})
		case "/v1/runtime/browser-auth":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"authorizationURL": "https://auth.example.com/authorize",
				"tokenURL":         "https://auth.example.com/token",
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
	_, err := NewRootCommand(CommandOptions{
		ConfigPath: configPath,
		StateDir:   filepath.Join(dir, "state"),
		Stdout:     &stdout,
		Stderr:     &stdout,
	}, []string{"catalog", "list", "--format", "json"})
	if err == nil || !strings.Contains(err.Error(), "runtime browser login metadata missing clientId") {
		t.Fatalf("expected invalid runtime browser login metadata error, got %v", err)
	}
}

func TestRootCommandCompletesRemoteBrowserLoginAuthorizationCodeFlow(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("browser login integration test requires open/xdg-open support")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for callback port: %v", err)
	}
	callbackPort := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", callbackPort)

	dir := t.TempDir()
	openerName := "xdg-open"
	if runtime.GOOS == "darwin" {
		openerName = "open"
	}
	openerPath := filepath.Join(dir, openerName)
	if err := os.WriteFile(openerPath, []byte("#!/bin/sh\ncurl -fsSL \"$1\" >/dev/null\n"), 0o755); err != nil {
		t.Fatalf("write fake browser opener: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	previousAcquirer := authpkg.BrowserLoginTokenAcquirer
	t.Cleanup(func() { authpkg.BrowserLoginTokenAcquirer = previousAcquirer })
	authpkg.BrowserLoginTokenAcquirer = authpkg.AcquireBrowserLoginToken

	var authServer *httptest.Server
	var codeChallenge string
	var authorizeCalls, tokenCalls int
	authServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/authorize":
			authorizeCalls++
			query := r.URL.Query()
			if got := query.Get("response_type"); got != "code" {
				t.Fatalf("expected response_type=code, got %q", got)
			}
			if got := query.Get("client_id"); got != "browser-client" {
				t.Fatalf("expected client_id browser-client, got %q", got)
			}
			if got := query.Get("redirect_uri"); got != redirectURI {
				t.Fatalf("expected redirect_uri %q, got %q", redirectURI, got)
			}
			if got := query.Get("scope"); got != "bundle:payments" {
				t.Fatalf("expected scope bundle:payments, got %q", got)
			}
			if got := query.Get("audience"); got != "oclird" {
				t.Fatalf("expected audience oclird, got %q", got)
			}
			if got := query.Get("code_challenge_method"); got != "S256" {
				t.Fatalf("expected code_challenge_method S256, got %q", got)
			}
			codeChallenge = query.Get("code_challenge")
			if codeChallenge == "" {
				t.Fatalf("expected PKCE code challenge")
			}
			http.Redirect(w, r, redirectURI+"?code=browser-auth-code&state="+url.QueryEscape(query.Get("state")), http.StatusFound)
		case "/token":
			tokenCalls++
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse token form: %v", err)
			}
			if got := r.Form.Get("grant_type"); got != "authorization_code" {
				t.Fatalf("expected authorization_code grant, got %q", got)
			}
			if got := r.Form.Get("code"); got != "browser-auth-code" {
				t.Fatalf("expected browser-auth-code, got %q", got)
			}
			if got := r.Form.Get("redirect_uri"); got != redirectURI {
				t.Fatalf("expected redirect URI %q, got %q", redirectURI, got)
			}
			verifier := r.Form.Get("code_verifier")
			if verifier == "" {
				t.Fatalf("expected PKCE verifier")
			}
			sum := sha256.Sum256([]byte(verifier))
			if challenge := base64.RawURLEncoding.EncodeToString(sum[:]); challenge != codeChallenge {
				t.Fatalf("expected verifier to match challenge, got %q want %q", challenge, codeChallenge)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "browser-login-token-live",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer authServer.Close()

	runtimeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/runtime/info":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"contractVersion": "1.1",
				"capabilities":    []string{"catalog", "brokered-auth", "authorization-envelope"},
				"auth": map[string]any{
					"required":                true,
					"audience":                "oclird",
					"scopePrefixes":           []string{"bundle:", "profile:", "tool:"},
					"tokenValidationProfiles": []string{"oidc_jwks"},
					"browserLogin": map[string]any{
						"configured":     true,
						"configEndpoint": "/v1/auth/browser-config",
					},
				},
			})
		case "/v1/auth/browser-config":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"authorizationURL": authServer.URL + "/authorize",
				"tokenURL":         authServer.URL + "/token",
				"clientId":         "browser-client",
				"audience":         "oclird",
			})
		case "/v1/catalog/effective":
			if got := r.Header.Get("Authorization"); got != "Bearer browser-login-token-live" {
				t.Fatalf("expected real browser login bearer token, got %q", got)
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
	        "audience": "oclird",
	        "scopes": ["bundle:payments"],
	        "browserLogin": {
	          "callbackPort": %d
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
	}`, runtimeServer.URL, callbackPort)), 0o644); err != nil {
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
	if authorizeCalls != 1 {
		t.Fatalf("expected one authorization request, got %d", authorizeCalls)
	}
	if tokenCalls != 1 {
		t.Fatalf("expected one token request, got %d", tokenCalls)
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

func TestDetectTerminalSessionIdentityUsesOCLIEnvironmentVariable(t *testing.T) {
	t.Setenv("OCLI_TERMINAL_SESSION_ID", "ocli-terminal")

	if got := detectTerminalSessionIdentity(); got != "ocli-terminal" {
		t.Fatalf("expected OCLI terminal session id, got %q", got)
	}
}

func TestDetectAgentSessionIdentityUsesOCLIEnvironmentVariable(t *testing.T) {
	t.Setenv("OCLI_AGENT_SESSION_ID", "ocli-agent")
	t.Setenv("COPILOT_SESSION_ID", "copilot-agent")

	if got := detectAgentSessionIdentity(); got != "ocli-agent" {
		t.Fatalf("expected OCLI agent session id, got %q", got)
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
		"--share-key-present", "true",
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

func TestResolveCommandOptionsFailsOnContractMismatch(t *testing.T) {
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
				"contractVersion": "2.0",
				"capabilities":    []string{"catalog", "execute", "refresh", "audit"},
				"lifecycle": map[string]any{
					"capabilities": []string{"heartbeat"},
				},
			})
		case "/v1/runtime/heartbeat":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"renewed":   true,
				"sessionId": "sess-1",
			})
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
	if err == nil || !strings.Contains(err.Error(), "contract_mismatch") {
		t.Fatalf("expected contract_mismatch error, got %v", err)
	}
}
