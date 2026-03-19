package commands

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	authpkg "github.com/StevenBuglione/open-cli/cmd/ocli/internal/auth"
	cfgpkg "github.com/StevenBuglione/open-cli/cmd/ocli/internal/config"
	runtimepkg "github.com/StevenBuglione/open-cli/cmd/ocli/internal/runtime"
	"github.com/StevenBuglione/open-cli/pkg/catalog"
	"github.com/getkin/kin-openapi/openapi3"
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

func runInitCommand(t *testing.T, source string) (string, map[string]any) {
	t.Helper()

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	var stdout bytes.Buffer
	cmd := NewInitCommand()
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	cmd.SetArgs([]string{"--global", source})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(homeDir, ".config", "oas-cli", ".cli.json"))
	if err != nil {
		t.Fatalf("read created config: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	return stdout.String(), cfg
}

func TestBuildInitSubprocessEnvStripsInheritedProxyVars(t *testing.T) {
	baseEnv := []string{
		"PATH=/usr/bin",
		"HOME=/parent/home",
		"HTTP_PROXY=http://127.0.0.1:1",
		"http_proxy=http://127.0.0.1:1",
		"HTTPS_PROXY=http://127.0.0.1:2",
		"NO_PROXY=*",
	}

	got := buildInitSubprocessEnv(baseEnv, map[string]string{
		"http_proxy": "http://proxy.test",
		"HTTP_PROXY": "",
		"NO_PROXY":   "",
		"HOME":       "/tmp/home",
	})

	if containsEnvPrefix(got, "HTTP_PROXY=") {
		t.Fatalf("expected HTTP_PROXY to be stripped, got %v", got)
	}
	if containsEnvPrefix(got, "HTTPS_PROXY=") {
		t.Fatalf("expected HTTPS_PROXY to be stripped, got %v", got)
	}
	if containsEnvPrefix(got, "NO_PROXY=") {
		t.Fatalf("expected NO_PROXY to be stripped, got %v", got)
	}
	if !containsEnvExact(got, "http_proxy=http://proxy.test") {
		t.Fatalf("expected explicit http_proxy override, got %v", got)
	}
	if !containsEnvExact(got, "HOME=/tmp/home") {
		t.Fatalf("expected HOME override, got %v", got)
	}
	if countEnvKey(got, "HOME") != 1 {
		t.Fatalf("expected HOME to appear once, got %v", got)
	}
}

func runInitCommandSubprocess(t *testing.T, source string, env map[string]string) (string, string, error) {
	t.Helper()

	homeDir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=TestInitCommandHelperProcess")
	childEnv := map[string]string{
		"OCLI_HELPER_INIT":   "1",
		"OCLI_HELPER_SOURCE": source,
		"HOME":               homeDir,
	}
	for key, value := range env {
		childEnv[key] = value
	}
	cmd.Env = buildInitSubprocessEnv(os.Environ(), childEnv)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), homeDir, err
}

func buildInitSubprocessEnv(baseEnv []string, overrides map[string]string) []string {
	filtered := make([]string, 0, len(baseEnv)+len(overrides))
	for _, entry := range baseEnv {
		if isProxyEnvEntry(entry) {
			continue
		}
		key, _, found := strings.Cut(entry, "=")
		if found {
			if _, overridden := overrides[key]; overridden {
				continue
			}
		}
		filtered = append(filtered, entry)
	}
	for key, value := range overrides {
		if isProxyEnvKey(key) && value == "" {
			continue
		}
		filtered = append(filtered, key+"="+value)
	}
	return filtered
}

func isProxyEnvEntry(entry string) bool {
	key, _, found := strings.Cut(entry, "=")
	if !found {
		return false
	}
	return isProxyEnvKey(key)
}

func isProxyEnvKey(key string) bool {
	switch key {
	case "HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy", "NO_PROXY", "no_proxy":
		return true
	default:
		return false
	}
}

func containsEnvPrefix(env []string, prefix string) bool {
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return true
		}
	}
	return false
}

func containsEnvExact(env []string, want string) bool {
	for _, entry := range env {
		if entry == want {
			return true
		}
	}
	return false
}

func countEnvKey(env []string, key string) int {
	count := 0
	for _, entry := range env {
		entryKey, _, found := strings.Cut(entry, "=")
		if found && entryKey == key {
			count++
		}
	}
	return count
}

func TestInitCommandHelperProcess(t *testing.T) {
	if os.Getenv("OCLI_HELPER_INIT") != "1" {
		return
	}

	var stdout bytes.Buffer
	cmd := NewInitCommand()
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	cmd.SetArgs([]string{"--global", os.Getenv("OCLI_HELPER_SOURCE")})
	err := cmd.Execute()
	_, _ = os.Stdout.Write(stdout.Bytes())
	if err != nil {
		_, _ = os.Stderr.WriteString(err.Error())
		os.Exit(1)
	}
	os.Exit(0)
}

func assertInitConfigName(t *testing.T, cfg map[string]any, want string) {
	t.Helper()

	services, ok := cfg["services"].(map[string]any)
	if !ok {
		t.Fatalf("expected services map, got %#v", cfg["services"])
	}
	serviceEntry, ok := services[want].(map[string]any)
	if !ok {
		t.Fatalf("expected service %q in config, got %#v", want, services)
	}

	sources, ok := cfg["sources"].(map[string]any)
	if !ok {
		t.Fatalf("expected sources map, got %#v", cfg["sources"])
	}
	sourceKey := want + "Source"
	if _, ok := sources[sourceKey]; !ok {
		t.Fatalf("expected source %q in config, got %#v", want+"Source", sources)
	}
	if got := serviceEntry["source"]; got != sourceKey {
		t.Fatalf("expected service %q to point at source %q, got %#v", want, sourceKey, got)
	}
	if got := serviceEntry["alias"]; got != want {
		t.Fatalf("expected service %q alias %q, got %#v", want, want, got)
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

func TestInitCommandGlobalCreatesConfigDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	specPath := filepath.Join(dir, "spec.json")
	if err := os.WriteFile(specPath, []byte(`{
  "openapi": "3.0.3",
  "info": {"title": "Demo", "version": "1.0.0"},
  "paths": {}
}`), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	var stdout bytes.Buffer
	cmd := NewInitCommand()
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	cmd.SetArgs([]string{"--global", specPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init --global failed: %v", err)
	}

	configPath := filepath.Join(dir, ".config", "oas-cli", ".cli.json")
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("expected global config at %s: %v", configPath, err)
	}
}

func TestInitCommandDerivesNameFromGenericBasenameAndTitle(t *testing.T) {
	specPath := filepath.Join(t.TempDir(), "openapi-v2.json")
	if err := os.WriteFile(specPath, []byte(`{
  "openapi": "3.0.3",
  "info": {"title": "Billing Service", "version": "1.0.0"},
  "paths": {}
}`), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	stdout, cfg := runInitCommand(t, specPath)
	if !strings.Contains(stdout, "ocli billing-service --help") {
		t.Fatalf("expected billing-service next step, got: %s", stdout)
	}
	assertInitConfigName(t, cfg, "billing-service")
}

func TestDeriveServiceNameFallsBackToServiceWhenFirstHostLabelIsGeneric(t *testing.T) {
	doc := &openapi3.T{
		Info: &openapi3.Info{
			Title: "OpenAPI 3.0",
		},
	}

	got := deriveServiceName("https://api.staging.payments.example.com/openapi.json", doc)
	if got != "service" {
		t.Fatalf("expected service fallback, got %q", got)
	}
}

func TestDeriveServiceNameRejectsVersionOnlyNames(t *testing.T) {
	doc := &openapi3.T{
		Info: &openapi3.Info{
			Title: "OpenAPI 3.0",
		},
	}

	for _, source := range []string{
		"/tmp/v1.json",
		"/tmp/1.json",
		"/tmp/1-0.json",
		"https://v1.example.com/openapi.json",
	} {
		if got := deriveServiceName(source, doc); got != "service" {
			t.Fatalf("expected service fallback for %q, got %q", source, got)
		}
	}
}

func TestDeriveServiceNameFromTitleTrimsBoilerplateUntilStable(t *testing.T) {
	got := deriveServiceNameFromTitle("Swagger Petstore - OpenAPI 3.0")
	if got != "petstore" {
		t.Fatalf("expected petstore, got %q", got)
	}
}

func TestInitCommandFallsBackToServiceForLocalFiles(t *testing.T) {
	specPath := filepath.Join(t.TempDir(), "api-1.yaml")
	if err := os.WriteFile(specPath, []byte(`{
  "openapi": "3.0.3",
  "info": {"title": "OpenAPI 3.0", "version": "1.0.0"},
  "paths": {}
}`), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	stdout, cfg := runInitCommand(t, specPath)
	if !strings.Contains(stdout, "ocli service --help") {
		t.Fatalf("expected service next step, got: %s", stdout)
	}
	assertInitConfigName(t, cfg, "service")
}

func TestInitCommandRemoteURLFallsBackToFirstHostLabel(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "openapi": "3.0.3",
  "info": {"title": "OpenAPI 3.0", "version": "1.0.0"},
  "paths": {}
}`))
	}))
	defer proxy.Close()

	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("http_proxy", "http://127.0.0.1:1")
	t.Setenv("NO_PROXY", "*")
	t.Setenv("no_proxy", "*")

	stdout, homeDir, err := runInitCommandSubprocess(t, "http://www.billing.example.invalid/openapi.json", map[string]string{
		"http_proxy":  proxy.URL,
		"HTTP_PROXY":  "",
		"ALL_PROXY":   "",
		"all_proxy":   "",
		"HTTPS_PROXY": "",
		"https_proxy": "",
		"NO_PROXY":    "",
		"no_proxy":    "",
	})
	if err != nil {
		t.Fatalf("init failed: %v\n%s", err, stdout)
	}

	data, err := os.ReadFile(filepath.Join(homeDir, ".config", "oas-cli", ".cli.json"))
	if err != nil {
		t.Fatalf("read created config: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	if !strings.Contains(stdout, "ocli billing --help") {
		t.Fatalf("expected billing next step, got: %s", stdout)
	}
	assertInitConfigName(t, cfg, "billing")
}

func TestInitCommandRemoteURLHonorsStandardHTTPProxyCGIGuard(t *testing.T) {
	var proxyHits atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"unexpected":true}`))
	}))
	defer proxy.Close()

	stdout, _, err := runInitCommandSubprocess(t, "http://www.billing.example.invalid/openapi.json", map[string]string{
		"HTTP_PROXY":     proxy.URL,
		"http_proxy":     "",
		"ALL_PROXY":      "",
		"all_proxy":      "",
		"HTTPS_PROXY":    "",
		"https_proxy":    "",
		"NO_PROXY":       "",
		"no_proxy":       "",
		"REQUEST_METHOD": "GET",
	})
	if err == nil {
		t.Fatalf("expected init to fail under CGI HTTP_PROXY guard, got output: %s", stdout)
	}
	if proxyHits.Load() != 0 {
		t.Fatalf("expected CGI guard to bypass proxy, got %d proxy requests", proxyHits.Load())
	}
	if !strings.Contains(stdout, "Cannot fetch spec") {
		t.Fatalf("expected fetch failure output, got: %s", stdout)
	}
}

func TestInitCommandAuthHintsPrefixDigitLeadingEnvVarNames(t *testing.T) {
	specPath := filepath.Join(t.TempDir(), "3-billing.yaml")
	if err := os.WriteFile(specPath, []byte(`{
  "openapi": "3.0.3",
  "info": {"title": "OpenAPI 3.0", "version": "1.0.0"},
  "paths": {},
  "components": {
    "securitySchemes": {
      "apiKeyAuth": {"type": "apiKey", "in": "header", "name": "X-API-Key"},
      "bearerAuth": {"type": "http", "scheme": "bearer"}
    }
  }
}`), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	stdout, _ := runInitCommand(t, specPath)
	if !strings.Contains(stdout, "OCLI_3_BILLING_API_KEY") {
		t.Fatalf("expected OCLI_3_BILLING_API_KEY auth hint, got: %s", stdout)
	}
	if !strings.Contains(stdout, "OCLI_3_BILLING_TOKEN") {
		t.Fatalf("expected OCLI_3_BILLING_TOKEN auth hint, got: %s", stdout)
	}
}

func TestInitCommandAuthHintsUseShellSafeEnvVarNames(t *testing.T) {
	specPath := filepath.Join(t.TempDir(), "openapi-v2.json")
	if err := os.WriteFile(specPath, []byte(`{
  "openapi": "3.0.3",
  "info": {"title": "Billing Service", "version": "1.0.0"},
  "paths": {},
  "components": {
    "securitySchemes": {
      "apiKeyAuth": {"type": "apiKey", "in": "header", "name": "X-API-Key"},
      "bearerAuth": {"type": "http", "scheme": "bearer"}
    }
  }
}`), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	stdout, _ := runInitCommand(t, specPath)
	if !strings.Contains(stdout, "BILLING_SERVICE_API_KEY") {
		t.Fatalf("expected BILLING_SERVICE_API_KEY auth hint, got: %s", stdout)
	}
	if !strings.Contains(stdout, "BILLING_SERVICE_TOKEN") {
		t.Fatalf("expected BILLING_SERVICE_TOKEN auth hint, got: %s", stdout)
	}
	if strings.Contains(stdout, "BILLING-SERVICE_API_KEY") || strings.Contains(stdout, "BILLING-SERVICE_TOKEN") {
		t.Fatalf("unexpected hyphenated env var in auth hints: %s", stdout)
	}
}
