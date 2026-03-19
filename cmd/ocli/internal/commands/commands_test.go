package commands

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	authpkg "github.com/StevenBuglione/open-cli/cmd/ocli/internal/auth"
	cfgpkg "github.com/StevenBuglione/open-cli/cmd/ocli/internal/config"
	runtimepkg "github.com/StevenBuglione/open-cli/cmd/ocli/internal/runtime"
	"github.com/StevenBuglione/open-cli/pkg/catalog"
	"github.com/spf13/cobra"
)

type fakeRuntimeClient struct {
	fetchCatalogFn func(runtimepkg.CatalogFetchOptions) (runtimepkg.CatalogResponse, error)
	executeFn      func(runtimepkg.ExecuteRequest) (runtimepkg.ExecuteResponse, error)
	runWorkflowFn  func(map[string]any) (map[string]any, error)
	runtimeInfoFn  func() (map[string]any, error)
	heartbeatFn    func(string) (map[string]any, error)
	stopFn         func() (map[string]any, error)
	sessionCloseFn func() (map[string]any, error)
}

func (client fakeRuntimeClient) FetchCatalog(options runtimepkg.CatalogFetchOptions) (runtimepkg.CatalogResponse, error) {
	if client.fetchCatalogFn != nil {
		return client.fetchCatalogFn(options)
	}
	return runtimepkg.CatalogResponse{}, nil
}

func (client fakeRuntimeClient) Execute(request runtimepkg.ExecuteRequest) (runtimepkg.ExecuteResponse, error) {
	if client.executeFn != nil {
		return client.executeFn(request)
	}
	return runtimepkg.ExecuteResponse{}, nil
}

func (client fakeRuntimeClient) RunWorkflow(request map[string]any) (map[string]any, error) {
	if client.runWorkflowFn != nil {
		return client.runWorkflowFn(request)
	}
	return map[string]any{}, nil
}

func (client fakeRuntimeClient) RuntimeInfo() (map[string]any, error) {
	if client.runtimeInfoFn != nil {
		return client.runtimeInfoFn()
	}
	return map[string]any{}, nil
}

func (client fakeRuntimeClient) Heartbeat(sessionID string) (map[string]any, error) {
	if client.heartbeatFn != nil {
		return client.heartbeatFn(sessionID)
	}
	return map[string]any{}, nil
}

func (client fakeRuntimeClient) Stop() (map[string]any, error) {
	if client.stopFn != nil {
		return client.stopFn()
	}
	return map[string]any{}, nil
}

func (client fakeRuntimeClient) SessionClose() (map[string]any, error) {
	if client.sessionCloseFn != nil {
		return client.sessionCloseFn()
	}
	return map[string]any{}, nil
}

func testOptions(stdout, stderr *bytes.Buffer) cfgpkg.Options {
	return cfgpkg.Options{
		Stdout: stdout,
		Stderr: stderr,
		Stdin:  strings.NewReader(""),
		Format: "json",
	}
}

func testCatalogResponse() runtimepkg.CatalogResponse {
	tools := []catalog.Tool{
		{
			ID:        "demo:listItems",
			ServiceID: "demo",
			Group:     "items",
			Command:   "list-items",
			Method:    http.MethodGet,
			Summary:   "List items",
			Safety:    catalog.Safety{ReadOnly: true, Idempotent: true},
		},
		{
			ID:        "demo:createItem",
			ServiceID: "demo",
			Group:     "items",
			Command:   "create-item",
			Method:    http.MethodPost,
			Summary:   "Create item",
		},
		{
			ID:        "demo:deleteItem",
			ServiceID: "demo",
			Group:     "admin",
			Command:   "delete-item",
			Method:    http.MethodDelete,
			Summary:   "Delete item",
			Safety:    catalog.Safety{Destructive: true, RequiresApproval: true},
		},
	}
	return runtimepkg.CatalogResponse{
		Catalog: catalog.NormalizedCatalog{
			Services: []catalog.Service{{ID: "demo", Alias: "demo", SourceID: "demoSource", Title: "Demo"}},
			Tools:    tools,
		},
		View: catalog.EffectiveView{Name: "discover", Mode: "discover", Tools: tools},
	}
}

func TestCatalogListFilters(t *testing.T) {
	response := testCatalogResponse()
	var stdout bytes.Buffer
	cmd := NewCatalogCommand(testOptions(&stdout, &stdout), response)
	cmd.SetArgs([]string{"list", "--group", "admin"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	var payload runtimepkg.CatalogResponse
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if len(payload.View.Tools) != 1 || payload.View.Tools[0].ID != "demo:deleteItem" {
		t.Fatalf("expected filtered admin tool, got %#v", payload.View.Tools)
	}
}

func TestSearchCommandFiltersResults(t *testing.T) {
	response := testCatalogResponse()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := NewSearchCommand(testOptions(&stdout, &stderr), &response)
	cmd.SetArgs([]string{"create"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	var payload runtimepkg.CatalogResponse
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if len(payload.View.Tools) != 1 || payload.View.Tools[0].ID != "demo:createItem" {
		t.Fatalf("expected create-item result, got %#v", payload.View.Tools)
	}
}

func TestSearchCommandRuntimeUnavailable(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := NewSearchCommand(testOptions(&stdout, &stderr), nil)
	cmd.SetArgs([]string{"create"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected runtime unavailable error")
	}
}

func TestStatusCommandShowsRuntimeAndConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".cli.json")
	if err := os.WriteFile(configPath, []byte(`{
  "sources": {
    "demoSource": {"type":"openapi","uri":"https://example.com/openapi.json","enabled":true},
    "mcpSource": {"type":"mcp","enabled":true}
  }
}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var stdout bytes.Buffer
	options := testOptions(&stdout, &stdout)
	options.Format = "table"
	options.ConfigPath = configPath
	options.RuntimeDeployment = "local"
	cmd := NewStatusCommand(options, fakeRuntimeClient{
		runtimeInfoFn: func() (map[string]any, error) {
			return map[string]any{"version": "1.2.3"}, nil
		},
	}, false)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	output := stdout.String()
	if !strings.Contains(output, "Runtime:  ") || !strings.Contains(output, "Config:") || !strings.Contains(output, "Sources:") {
		t.Fatalf("unexpected status output: %s", output)
	}
}

func TestConfigCommandsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer os.Chdir(origWD)

	var stdout bytes.Buffer

	addSource := newConfigAddSourceCommand()
	addSource.SetOut(&stdout)
	addSource.SetErr(&stdout)
	addSource.SetArgs([]string{"demo", "--uri", "https://example.com/openapi.json"})
	if err := addSource.Execute(); err != nil {
		t.Fatalf("add-source failed: %v", err)
	}

	addSecret := newConfigAddSecretCommand()
	addSecret.SetOut(&stdout)
	addSecret.SetErr(&stdout)
	addSecret.SetArgs([]string{"demo.oauth", "--type", "env", "--env-value", "DEMO_TOKEN"})
	if err := addSecret.Execute(); err != nil {
		t.Fatalf("add-secret failed: %v", err)
	}

	options := testOptions(&stdout, &stdout)
	options.ConfigPath = ".cli.json"
	show := NewConfigCommand(options)
	show.SetArgs([]string{"show"})
	if err := show.Execute(); err != nil {
		t.Fatalf("config show failed: %v", err)
	}

	remove := newConfigRemoveSourceCommand()
	remove.SetOut(&stdout)
	remove.SetErr(&stdout)
	remove.SetArgs([]string{"demo"})
	if err := remove.Execute(); err != nil {
		t.Fatalf("remove-source failed: %v", err)
	}
}

func TestAuthStatusShowsConfiguredSecret(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".cli.json")
	if err := os.WriteFile(configPath, []byte(`{
  "sources": {
    "demoSource": {"type":"openapi","enabled":true}
  },
  "services": {
    "demo": {"source":"demoSource","alias":"demo"}
  },
  "secrets": {
    "demo.oauth": {"type":"oauth2"}
  }
}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var stdout bytes.Buffer
	options := testOptions(&stdout, &stdout)
	options.Format = "table"
	options.ConfigPath = configPath
	cmd := NewAuthCommand(options, fakeRuntimeClient{}, false)
	cmd.SetArgs([]string{"status"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("auth status failed: %v", err)
	}
	if !strings.Contains(stdout.String(), "demo") || !strings.Contains(stdout.String(), "demo.oauth") {
		t.Fatalf("unexpected auth status output: %s", stdout.String())
	}
}

func TestAuthLoginUsesBrowserMetadata(t *testing.T) {
	originalAcquirer := authpkg.BrowserLoginTokenAcquirer
	authpkg.BrowserLoginTokenAcquirer = func(request authpkg.BrowserLoginRequest) (string, error) {
		return "token", nil
	}
	defer func() {
		authpkg.BrowserLoginTokenAcquirer = originalAcquirer
	}()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/browser-login":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"authorizationURL": "https://issuer.example.com/auth",
				"tokenURL":         "https://issuer.example.com/token",
				"clientId":         "client-id",
				"audience":         "api://demo",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	var stdout bytes.Buffer
	options := testOptions(&stdout, &stdout)
	options.RuntimeURL = server.URL
	cmd := NewAuthCommand(options, fakeRuntimeClient{
		runtimeInfoFn: func() (map[string]any, error) {
			return map[string]any{
				"auth": map[string]any{
					"browserLoginEndpoint": "/browser-login",
					"audience":             "api://demo",
					"scopes":               []any{"openid"},
				},
			}, nil
		},
	}, false)
	cmd.SetArgs([]string{"login"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("auth login failed: %v", err)
	}
	if !strings.Contains(stdout.String(), "Login successful") {
		t.Fatalf("unexpected auth login output: %s", stdout.String())
	}
}

func TestDynamicCommandDryRun(t *testing.T) {
	tool := catalog.Tool{
		ID:        "demo:getItem",
		ServiceID: "demo",
		Group:     "items",
		Command:   "get-item",
		Method:    http.MethodGet,
		Path:      "/items/{id}",
		PathParams: []catalog.Parameter{
			{Name: "id", OriginalName: "id", Required: true},
		},
		Flags: []catalog.Parameter{
			{Name: "tag", OriginalName: "tag", Location: "query"},
		},
		Servers: []string{"https://api.example.com"},
	}
	root := &cobra.Command{Use: "ocli"}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	options := testOptions(&stdout, &stderr)
	options.Stdout = &stdout
	options.Stderr = &stderr
	root.SetIn(strings.NewReader(""))
	root.SetOut(options.Stdout)
	root.SetErr(options.Stderr)
	AddDynamicToolCommands(root, options, fakeRuntimeClient{
		executeFn: func(request runtimepkg.ExecuteRequest) (runtimepkg.ExecuteResponse, error) {
			return runtimepkg.ExecuteResponse{StatusCode: 200, Body: json.RawMessage(`{"ok":true}`)}, nil
		},
	}, []catalog.Service{{ID: "demo", Alias: "demo"}}, []catalog.Tool{tool})

	root.SetArgs([]string{"demo", "items", "get-item", "42", "--tag", "blue", "--dry-run"})
	if err := root.Execute(); err != nil {
		t.Fatalf("dry-run execute failed: %v", err)
	}
	if !strings.Contains(stdout.String(), "GET https://api.example.com/items/42?tag=blue") {
		t.Fatalf("unexpected dry-run output: %s", stdout.String())
	}
}

func TestPromptForMissingArgs(t *testing.T) {
	result, err := PromptForMissingArgs(strings.NewReader("42\n"), &bytes.Buffer{}, []catalog.Parameter{{Name: "id"}}, nil)
	if err != nil {
		t.Fatalf("PromptForMissingArgs returned error: %v", err)
	}
	if len(result) != 1 || result[0] != "42" {
		t.Fatalf("expected prompted args [42], got %#v", result)
	}
}

func TestInitCommandSupportsMCP(t *testing.T) {
	dir := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer os.Chdir(origWD)

	var stdout bytes.Buffer
	cmd := NewInitCommand()
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	cmd.SetArgs([]string{"--type", "mcp", "--transport", "stdio", "--command", "npx", "filesystem"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init mcp failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".cli.json"))
	if err != nil {
		t.Fatalf("read created config: %v", err)
	}
	if !bytes.Contains(data, []byte(`"type": "mcp"`)) {
		t.Fatalf("expected mcp source in config: %s", string(data))
	}
}
