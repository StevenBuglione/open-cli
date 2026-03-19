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

type statusJSONReport struct {
	Runtime struct {
		Available bool    `json:"available"`
		Mode      string  `json:"mode"`
		Version   *string `json:"version"`
	} `json:"runtime"`
	Config struct {
		ActivePath *string `json:"activePath"`
	} `json:"config"`
	Sources struct {
		TotalActive int            `json:"totalActive"`
		ByType      map[string]int `json:"byType"`
	} `json:"sources"`
	Auth struct {
		Mode                   string   `json:"mode"`
		Required               *bool    `json:"required"`
		Audience               *string  `json:"audience"`
		Scopes                 []string `json:"scopes"`
		BrowserLoginConfigured *bool    `json:"browserLoginConfigured"`
	} `json:"auth"`
	Approval struct {
		HasApprovalGatedTools *bool  `json:"hasApprovalGatedTools"`
		Status                string `json:"status"`
	} `json:"approval"`
	ScopePaths struct {
		Managed *string `json:"managed"`
		User    *string `json:"user"`
		Project *string `json:"project"`
		Local   *string `json:"local"`
	} `json:"scopePaths"`
}

type authJSONReport struct {
	Posture string `json:"posture"`
	Runtime struct {
		Available bool    `json:"available"`
		Mode      string  `json:"mode"`
		Posture   string  `json:"posture"`
		Version   *string `json:"version"`
	} `json:"runtime"`
	Config struct {
		ActivePath *string `json:"activePath"`
		Posture    string  `json:"posture"`
	} `json:"config"`
	Services []struct {
		Name       string `json:"name"`
		AuthType   string `json:"authType"`
		Configured string `json:"configured"`
	} `json:"services"`
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

func testExplainResponse() runtimepkg.CatalogResponse {
	tool := catalog.Tool{
		ID:          "demo:deleteItem",
		ServiceID:   "demo",
		Group:       "admin",
		Command:     "delete-item",
		Method:      http.MethodDelete,
		Path:        "/items/{id}",
		Summary:     "Delete item",
		Description: "Delete an item",
		Auth: []catalog.AuthRequirement{
			{
				Name:   "oauth2",
				Type:   "oauth2",
				Scheme: "bearer",
				Scopes: []string{"openid", "profile"},
			},
		},
		Safety: catalog.Safety{RequiresApproval: false},
	}
	return runtimepkg.CatalogResponse{
		Catalog: catalog.NormalizedCatalog{
			Services: []catalog.Service{{ID: "demo", Alias: "demo", SourceID: "demoSource", Title: "Demo"}},
			Tools:    []catalog.Tool{tool},
		},
		View: catalog.EffectiveView{Name: "discover", Mode: "discover", Tools: []catalog.Tool{tool}},
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

func TestExplainCommandStructuredIncludesSecuritySummary(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".cli.json")
	if err := os.WriteFile(configPath, []byte(`{
  "cli": "1.0.0",
  "mode": {"default": "discover"},
  "policy": {
    "approvalRequired": ["demo:deleteItem"]
  },
  "sources": {
    "demoSource": {"type": "openapi", "enabled": true}
  },
  "services": {
    "demo": {"source": "demoSource", "alias": "demo"}
  }
}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var stdout bytes.Buffer
	options := testOptions(&stdout, &stdout)
	options.ConfigPath = configPath
	options.RuntimeDeployment = "remote"
	options.RuntimeURL = "https://runtime.example.invalid"
	cmd := NewExplainCommand(options, testExplainResponse())
	cmd.SetArgs([]string{"demo:deleteItem"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	var payload struct {
		ToolID           string                    `json:"toolId"`
		Summary          string                    `json:"summary"`
		Method           string                    `json:"method"`
		Path             string                    `json:"path"`
		Service          string                    `json:"service"`
		Group            string                    `json:"group"`
		Command          string                    `json:"command"`
		Safety           catalog.Safety            `json:"safety"`
		Auth             []catalog.AuthRequirement `json:"auth"`
		ApprovalRequired bool                      `json:"approvalRequired"`
		ApprovalStatus   string                    `json:"approvalStatus"`
		Runtime          struct {
			Mode string `json:"mode"`
		} `json:"runtime"`
		RuntimeAvailable bool `json:"runtimeAvailable"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if payload.ToolID != "demo:deleteItem" || payload.Service != "demo" || payload.Command != "delete-item" {
		t.Fatalf("unexpected explain payload: %#v", payload)
	}
	if len(payload.Auth) != 1 || payload.Auth[0].Name != "oauth2" || payload.Auth[0].Scheme != "bearer" {
		t.Fatalf("expected oauth2 auth requirement, got %#v", payload.Auth)
	}
	if !payload.ApprovalRequired {
		t.Fatal("expected approvalRequired=true")
	}
	if payload.ApprovalStatus != "required" {
		t.Fatalf("expected approvalStatus required, got %q", payload.ApprovalStatus)
	}
	if payload.Runtime.Mode != "remote" {
		t.Fatalf("expected runtime mode remote, got %q", payload.Runtime.Mode)
	}
	if !payload.RuntimeAvailable {
		t.Fatal("expected runtimeAvailable=true")
	}
}

func TestExplainCommandTerminalShowsSecuritySummary(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".cli.json")
	if err := os.WriteFile(configPath, []byte(`{
  "cli": "1.0.0",
  "mode": {"default": "discover"},
  "policy": {
    "approvalRequired": ["demo:deleteItem"]
  },
  "sources": {
    "demoSource": {"type": "openapi", "enabled": true}
  },
  "services": {
    "demo": {"source": "demoSource", "alias": "demo"}
  }
}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var stdout bytes.Buffer
	options := testOptions(&stdout, &stdout)
	options.Format = "table"
	options.ConfigPath = configPath
	options.RuntimeDeployment = "remote"
	options.RuntimeURL = "https://runtime.example.invalid"
	cmd := NewExplainCommand(options, testExplainResponse())
	cmd.SetArgs([]string{"demo:deleteItem"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	output := stdout.String()
	for _, want := range []string{
		"Auth:",
		"Approval:",
		"Runtime:",
		"Runtime available:",
		"required",
		"remote",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected %q in explain output, got: %s", want, output)
		}
	}
}

func TestExplainCommandDegradesWhenContextMissing(t *testing.T) {
	var stdout bytes.Buffer
	options := testOptions(&stdout, &stdout)
	options.Format = "json"
	cmd := NewExplainCommand(options, testExplainResponse())
	cmd.SetArgs([]string{"demo:deleteItem"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	var payload struct {
		ApprovalRequired bool   `json:"approvalRequired"`
		ApprovalStatus   string `json:"approvalStatus"`
		Runtime          struct {
			Mode string `json:"mode"`
		} `json:"runtime"`
		RuntimeAvailable bool `json:"runtimeAvailable"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if payload.ApprovalStatus != "unknown" {
		t.Fatalf("expected approvalStatus unknown, got %q", payload.ApprovalStatus)
	}
	if payload.ApprovalRequired {
		t.Fatal("expected approvalRequired=false when context is missing")
	}
	if payload.Runtime.Mode != "unknown" {
		t.Fatalf("expected runtime mode unknown, got %q", payload.Runtime.Mode)
	}
	if payload.RuntimeAvailable {
		t.Fatal("expected runtimeAvailable=false when runtime context is missing")
	}
}

func TestExplainCommandUsesLayeredApprovalPolicy(t *testing.T) {
	root := t.TempDir()
	homeDir := filepath.Join(root, "home")
	projectDir := filepath.Join(root, "project")
	userDir := filepath.Join(homeDir, ".config", "oas-cli")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatalf("mkdir user config: %v", err)
	}
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(homeDir, ".config"))
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(projectDir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origWD)
	})

	projectPath := filepath.Join(projectDir, ".cli.json")
	if err := os.WriteFile(projectPath, []byte(`{
  "cli": "1.0.0",
  "sources": {
    "demoSource": {"type": "openapi", "enabled": true}
  },
  "services": {
    "demo": {"source": "demoSource", "alias": "demo"}
  }
}`), 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(userDir, ".cli.json"), []byte(`{
  "cli": "1.0.0",
  "policy": {
    "approvalRequired": ["demo:delete*"]
  },
  "sources": {}
}`), 0o644); err != nil {
		t.Fatalf("write user config: %v", err)
	}

	var stdout bytes.Buffer
	options := testOptions(&stdout, &stdout)
	options.ConfigPath = projectPath
	options.RuntimeDeployment = "remote"
	options.RuntimeURL = "https://runtime.example.invalid"
	cmd := NewExplainCommand(options, testExplainResponse())
	cmd.SetArgs([]string{"demo:deleteItem"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	var payload struct {
		ApprovalRequired bool   `json:"approvalRequired"`
		ApprovalStatus   string `json:"approvalStatus"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if !payload.ApprovalRequired {
		t.Fatal("expected layered approval policy to require approval")
	}
	if payload.ApprovalStatus != "required" {
		t.Fatalf("expected approvalStatus required, got %q", payload.ApprovalStatus)
	}
}

func TestExplainCommandPreservesAuthAlternatives(t *testing.T) {
	response := testExplainResponse()
	response.Catalog.Tools[0].Auth = nil
	response.Catalog.Tools[0].AuthAlternatives = []catalog.AuthAlternative{
		{
			Requirements: []catalog.AuthRequirement{{
				Name:   "oauth2",
				Type:   "oauth2",
				Scheme: "bearer",
				Scopes: []string{"items:write"},
			}},
		},
		{
			Requirements: []catalog.AuthRequirement{{
				Name:      "apiKey",
				Type:      "apiKey",
				In:        "header",
				ParamName: "X-API-Key",
			}},
		},
	}
	response.View.Tools[0] = response.Catalog.Tools[0]

	var stdout bytes.Buffer
	options := testOptions(&stdout, &stdout)
	options.RuntimeDeployment = "remote"
	options.RuntimeURL = "https://runtime.example.invalid"
	cmd := NewExplainCommand(options, response)
	cmd.SetArgs([]string{"demo:deleteItem"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	var payload struct {
		Auth             []catalog.AuthRequirement `json:"auth"`
		AuthAlternatives []catalog.AuthAlternative `json:"authAlternatives"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if len(payload.Auth) != 0 {
		t.Fatalf("expected flattened auth to stay empty when alternatives exist, got %#v", payload.Auth)
	}
	if len(payload.AuthAlternatives) != 2 {
		t.Fatalf("expected two auth alternatives, got %#v", payload.AuthAlternatives)
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

func TestStatusCommandStructuredIncludesSecuritySummary(t *testing.T) {
	root := t.TempDir()
	homeDir := filepath.Join(root, "home")
	projectDir := filepath.Join(root, "project")
	userDir := filepath.Join(homeDir, ".config", "oas-cli")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatalf("mkdir user config: %v", err)
	}
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(homeDir, ".config"))

	projectPath := filepath.Join(projectDir, ".cli.json")
	if err := os.WriteFile(projectPath, []byte(`{
  "cli": "1.0.0",
  "mode": {"default": "discover"},
  "runtime": {
    "mode": "remote",
    "remote": {
      "url": "https://runtime.example.invalid",
      "oauth": {
        "mode": "browserLogin",
        "audience": "api://demo",
        "scopes": ["openid", "profile"]
      }
    }
  },
  "sources": {
    "openapi": {"type": "openapi", "uri": "https://example.com/openapi.json", "enabled": true},
    "mcp": {"type": "mcp", "enabled": true}
  },
  "services": {
    "openapi": {"source": "openapi"},
    "mcp": {"source": "mcp"}
  }
}`), 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".cli.local.json"), []byte(`{"sources":{}}`), 0o644); err != nil {
		t.Fatalf("write local config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(userDir, ".cli.json"), []byte(`{"sources":{}}`), 0o644); err != nil {
		t.Fatalf("write user config: %v", err)
	}
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(projectDir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origWD)
	})

	var stdout bytes.Buffer
	options := testOptions(&stdout, &stdout)
	options.Format = "json"
	options.ConfigPath = projectPath
	options.RuntimeDeployment = "remote"
	options.RuntimeURL = "https://runtime.example.invalid"
	cmd := NewStatusCommand(options, fakeRuntimeClient{
		runtimeInfoFn: func() (map[string]any, error) {
			return map[string]any{
				"version": "1.2.3",
				"auth": map[string]any{
					"required": true,
					"audience": "api://demo",
					"scopes":   []any{"openid", "profile"},
					"browserLogin": map[string]any{
						"configured": true,
					},
				},
			}, nil
		},
		fetchCatalogFn: func(runtimepkg.CatalogFetchOptions) (runtimepkg.CatalogResponse, error) {
			return testCatalogResponse(), nil
		},
	}, false)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	var got statusJSONReport
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if !got.Runtime.Available {
		t.Fatal("expected runtime to be available")
	}
	if got.Runtime.Mode != "remote" {
		t.Fatalf("expected remote runtime mode, got %q", got.Runtime.Mode)
	}
	if got.Runtime.Version == nil || *got.Runtime.Version != "1.2.3" {
		t.Fatalf("expected runtime version 1.2.3, got %#v", got.Runtime.Version)
	}
	if got.Config.ActivePath == nil || *got.Config.ActivePath != projectPath {
		t.Fatalf("expected active path %q, got %#v", projectPath, got.Config.ActivePath)
	}
	if got.Sources.TotalActive != 2 || got.Sources.ByType["openapi"] != 1 || got.Sources.ByType["mcp"] != 1 {
		t.Fatalf("unexpected sources summary: %#v", got.Sources)
	}
	if got.Auth.Mode != "browserLogin" {
		t.Fatalf("expected browserLogin auth mode, got %q", got.Auth.Mode)
	}
	if got.Auth.Required == nil || !*got.Auth.Required {
		t.Fatalf("expected auth.required=true, got %#v", got.Auth.Required)
	}
	if got.Auth.Audience == nil || *got.Auth.Audience != "api://demo" {
		t.Fatalf("expected auth audience api://demo, got %#v", got.Auth.Audience)
	}
	if len(got.Auth.Scopes) != 2 || got.Auth.Scopes[0] != "openid" || got.Auth.Scopes[1] != "profile" {
		t.Fatalf("unexpected auth scopes: %#v", got.Auth.Scopes)
	}
	if got.Auth.BrowserLoginConfigured == nil || !*got.Auth.BrowserLoginConfigured {
		t.Fatalf("expected browserLoginConfigured=true, got %#v", got.Auth.BrowserLoginConfigured)
	}
	if got.Approval.HasApprovalGatedTools == nil || !*got.Approval.HasApprovalGatedTools {
		t.Fatalf("expected approval-gated tools, got %#v", got.Approval.HasApprovalGatedTools)
	}
	if got.Approval.Status != "required" {
		t.Fatalf("expected approval status required, got %q", got.Approval.Status)
	}
	if got.ScopePaths.User == nil || *got.ScopePaths.User != filepath.Join(userDir, ".cli.json") {
		t.Fatalf("expected user scope path, got %#v", got.ScopePaths.User)
	}
	if got.ScopePaths.Project == nil || *got.ScopePaths.Project != projectPath {
		t.Fatalf("expected project scope path, got %#v", got.ScopePaths.Project)
	}
	if got.ScopePaths.Local == nil || *got.ScopePaths.Local != filepath.Join(projectDir, ".cli.local.json") {
		t.Fatalf("expected local scope path, got %#v", got.ScopePaths.Local)
	}
}

func TestStatusCommandStructuredDegradesPartialAuthMetadata(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(projectDir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origWD)
	})

	projectPath := filepath.Join(projectDir, ".cli.json")
	if err := os.WriteFile(projectPath, []byte(`{
  "cli": "1.0.0",
  "mode": {"default": "discover"},
  "sources": {
    "openapi": {"type": "openapi", "uri": "https://example.com/openapi.json", "enabled": true}
  },
  "services": {
    "openapi": {"source": "openapi"}
  }
}`), 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".cli.local.json"), []byte(`{"sources":{}}`), 0o644); err != nil {
		t.Fatalf("write local config: %v", err)
	}

	var stdout bytes.Buffer
	options := testOptions(&stdout, &stdout)
	options.Format = "json"
	options.ConfigPath = projectPath
	options.RuntimeDeployment = "remote"
	options.RuntimeURL = "https://runtime.example.invalid"
	cmd := NewStatusCommand(options, fakeRuntimeClient{
		runtimeInfoFn: func() (map[string]any, error) {
			return map[string]any{
				"auth": map[string]any{
					"required": true,
				},
			}, nil
		},
		fetchCatalogFn: func(runtimepkg.CatalogFetchOptions) (runtimepkg.CatalogResponse, error) {
			return runtimepkg.CatalogResponse{
				Catalog: catalog.NormalizedCatalog{
					Services: []catalog.Service{{ID: "openapi", Alias: "openapi", SourceID: "openapi", Title: "OpenAPI"}},
					Tools: []catalog.Tool{
						{
							ID:        "openapi:listItems",
							ServiceID: "openapi",
							Group:     "items",
							Command:   "list-items",
							Method:    http.MethodGet,
							Summary:   "List items",
							Safety:    catalog.Safety{ReadOnly: true, Idempotent: true},
						},
					},
				},
				View: catalog.EffectiveView{
					Name: "discover",
					Mode: "discover",
					Tools: []catalog.Tool{{
						ID:        "openapi:listItems",
						ServiceID: "openapi",
						Group:     "items",
						Command:   "list-items",
						Method:    http.MethodGet,
						Summary:   "List items",
						Safety:    catalog.Safety{ReadOnly: true, Idempotent: true},
					}},
				},
			}, nil
		},
	}, false)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	var got statusJSONReport
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if got.Auth.Mode != "unknown" {
		t.Fatalf("expected unknown auth mode, got %q", got.Auth.Mode)
	}
	if got.Auth.Required == nil || !*got.Auth.Required {
		t.Fatalf("expected auth.required=true, got %#v", got.Auth.Required)
	}
	if got.Auth.Audience != nil {
		t.Fatalf("expected auth audience to be null, got %#v", got.Auth.Audience)
	}
	if len(got.Auth.Scopes) != 0 {
		t.Fatalf("expected empty auth scopes, got %#v", got.Auth.Scopes)
	}
	if got.Auth.BrowserLoginConfigured != nil {
		t.Fatalf("expected browserLoginConfigured to be null, got %#v", got.Auth.BrowserLoginConfigured)
	}
	if got.Approval.HasApprovalGatedTools == nil || *got.Approval.HasApprovalGatedTools {
		t.Fatalf("expected no approval-gated tools, got %#v", got.Approval.HasApprovalGatedTools)
	}
	if got.Approval.Status != "not_required" {
		t.Fatalf("expected approval status not_required, got %q", got.Approval.Status)
	}
}

func TestStatusCommandRuntimeUnavailableStillReportsConfig(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(projectDir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origWD)
	})

	projectPath := filepath.Join(projectDir, ".cli.json")
	if err := os.WriteFile(projectPath, []byte(`{
  "cli": "1.0.0",
  "mode": {"default": "discover"},
  "sources": {
    "openapi": {"type": "openapi", "uri": "https://example.com/openapi.json", "enabled": true}
  }
}`), 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	var stdout bytes.Buffer
	options := testOptions(&stdout, &stdout)
	options.Format = "json"
	options.ConfigPath = projectPath
	cmd := NewStatusCommand(options, fakeRuntimeClient{}, true)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	var got statusJSONReport
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if got.Runtime.Available {
		t.Fatal("expected runtime to be unavailable")
	}
	if got.Runtime.Mode != "unknown" {
		t.Fatalf("expected unknown runtime mode, got %q", got.Runtime.Mode)
	}
	if got.Config.ActivePath == nil || *got.Config.ActivePath != projectPath {
		t.Fatalf("expected active path %q, got %#v", projectPath, got.Config.ActivePath)
	}
	if got.Sources.TotalActive != 1 || got.Sources.ByType["openapi"] != 1 {
		t.Fatalf("unexpected sources summary: %#v", got.Sources)
	}
	if got.Approval.HasApprovalGatedTools != nil {
		t.Fatalf("expected approval summary to be null without catalog context, got %#v", got.Approval.HasApprovalGatedTools)
	}
	if got.Approval.Status != "unknown" {
		t.Fatalf("expected approval status unknown, got %q", got.Approval.Status)
	}
}

func TestStatusCommandTerminalIncludesCompactSummaries(t *testing.T) {
	projectDir := t.TempDir()
	projectPath := filepath.Join(projectDir, ".cli.json")
	if err := os.WriteFile(projectPath, []byte(`{
  "cli": "1.0.0",
  "mode": {"default": "discover"},
  "sources": {
    "openapi": {"type": "openapi", "uri": "https://example.com/openapi.json", "enabled": true}
  }
}`), 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	var stdout bytes.Buffer
	options := testOptions(&stdout, &stdout)
	options.Format = "table"
	options.ConfigPath = projectPath
	cmd := NewStatusCommand(options, fakeRuntimeClient{
		runtimeInfoFn: func() (map[string]any, error) {
			return map[string]any{
				"version": "1.2.3",
				"auth": map[string]any{
					"required": true,
				},
			}, nil
		},
		fetchCatalogFn: func(runtimepkg.CatalogFetchOptions) (runtimepkg.CatalogResponse, error) {
			return runtimepkg.CatalogResponse{}, nil
		},
	}, false)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	output := stdout.String()
	for _, want := range []string{"Runtime:", "Config:", "Sources:", "Auth:", "Scope paths:"} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected %q in status output, got: %s", want, output)
		}
	}
}

func TestStatusCommandTerminalDoesNotRenderScopePrefixesAsScopes(t *testing.T) {
	projectDir := t.TempDir()
	projectPath := filepath.Join(projectDir, ".cli.json")
	if err := os.WriteFile(projectPath, []byte(`{
  "cli": "1.0.0",
  "sources": {
    "demoSource": {"type":"openapi","enabled":true}
  }
}`), 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	var stdout bytes.Buffer
	options := testOptions(&stdout, &stdout)
	options.Format = "table"
	options.ConfigPath = projectPath
	options.Embedded = true
	cmd := NewStatusCommand(options, fakeRuntimeClient{
		runtimeInfoFn: func() (map[string]any, error) {
			return map[string]any{
				"auth": map[string]any{
					"required":      false,
					"scopePrefixes": []any{"bundle:", "profile:", "tool:"},
					"browserLogin": map[string]any{
						"configured": false,
					},
				},
			}, nil
		},
		fetchCatalogFn: func(runtimepkg.CatalogFetchOptions) (runtimepkg.CatalogResponse, error) {
			return runtimepkg.CatalogResponse{}, nil
		},
	}, false)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	output := stdout.String()
	if strings.Contains(output, "bundle:") || strings.Contains(output, "profile:") || strings.Contains(output, "tool:") {
		t.Fatalf("expected scope prefixes to stay out of terminal auth scopes, got: %s", output)
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

func TestAuthStatusPrefersOAuthSpecificSecretDeterministically(t *testing.T) {
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
    "demo": {"type":"env"},
    "demo.oauth": {"type":"oauth2"}
  }
}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var stdout bytes.Buffer
	options := testOptions(&stdout, &stdout)
	options.Format = "json"
	options.ConfigPath = configPath
	cmd := NewAuthCommand(options, fakeRuntimeClient{}, true)
	cmd.SetArgs([]string{"status"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("auth status failed: %v", err)
	}

	var got authJSONReport
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal auth status: %v", err)
	}
	if len(got.Services) != 1 {
		t.Fatalf("expected one service, got %#v", got.Services)
	}
	if got.Services[0].AuthType != "oauth2" || got.Services[0].Configured != "ok secret: demo.oauth" {
		t.Fatalf("expected oauth-specific secret to win, got %#v", got.Services[0])
	}
}

func TestAuthStatusReportsPostureAcrossRuntimeAndConfigEvidence(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	projectPath := filepath.Join(projectDir, ".cli.json")
	if err := os.WriteFile(projectPath, []byte(`{
  "cli": "1.0.0",
  "mode": {"default": "discover"},
  "runtime": {
    "mode": "remote",
    "remote": {
      "url": "https://runtime.example.invalid",
      "oauth": {
        "mode": "browserLogin",
        "audience": "api://demo",
        "scopes": ["openid"]
      }
    }
  },
  "sources": {
    "demoSource": {"type": "openapi", "enabled": true}
  },
  "services": {
    "demo": {"source": "demoSource", "alias": "demo"}
  },
  "secrets": {
    "demo.oauth": {"type": "oauth2"}
  }
}`), 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	t.Run("runtime session", func(t *testing.T) {
		var stdout bytes.Buffer
		options := testOptions(&stdout, &stdout)
		options.Format = "json"
		options.ConfigPath = projectPath
		options.RuntimeDeployment = "remote"
		options.RuntimeURL = "https://runtime.example.invalid"
		cmd := NewAuthCommand(options, fakeRuntimeClient{
			runtimeInfoFn: func() (map[string]any, error) {
				return map[string]any{
					"auth": map[string]any{
						"required": true,
					},
				}, nil
			},
		}, false)
		cmd.SetArgs([]string{"status"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("auth status failed: %v", err)
		}

		var got authJSONReport
		if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
			t.Fatalf("unmarshal output: %v", err)
		}
		if got.Posture != "runtimeSession" {
			t.Fatalf("expected runtimeSession posture, got %q", got.Posture)
		}
		if !got.Runtime.Available {
			t.Fatal("expected runtime to be available")
		}
		if got.Runtime.Posture != "runtimeSession" {
			t.Fatalf("expected runtime posture runtimeSession, got %q", got.Runtime.Posture)
		}
		if got.Config.Posture != "configOnly" {
			t.Fatalf("expected config posture configOnly, got %q", got.Config.Posture)
		}
		if got.Config.ActivePath == nil || *got.Config.ActivePath != projectPath {
			t.Fatalf("expected active path %q, got %#v", projectPath, got.Config.ActivePath)
		}
		if len(got.Services) != 1 || got.Services[0].Name != "demo" {
			t.Fatalf("expected demo service status, got %#v", got.Services)
		}
	})

	t.Run("config only", func(t *testing.T) {
		var stdout bytes.Buffer
		options := testOptions(&stdout, &stdout)
		options.Format = "json"
		options.ConfigPath = projectPath
		cmd := NewAuthCommand(options, fakeRuntimeClient{}, true)
		cmd.SetArgs([]string{"status"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("auth status failed: %v", err)
		}

		var got authJSONReport
		if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
			t.Fatalf("unmarshal output: %v", err)
		}
		if got.Posture != "configOnly" {
			t.Fatalf("expected configOnly posture, got %q", got.Posture)
		}
		if got.Runtime.Available {
			t.Fatal("expected runtime to be unavailable")
		}
		if got.Config.Posture != "configOnly" {
			t.Fatalf("expected configOnly config posture, got %q", got.Config.Posture)
		}
		if len(got.Services) != 1 || got.Services[0].Name != "demo" {
			t.Fatalf("expected demo service status, got %#v", got.Services)
		}
	})

	t.Run("unknown", func(t *testing.T) {
		var stdout bytes.Buffer
		options := testOptions(&stdout, &stdout)
		options.Format = "json"
		options.ConfigPath = filepath.Join(root, "missing.json")
		cmd := NewAuthCommand(options, fakeRuntimeClient{}, true)
		cmd.SetArgs([]string{"status"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("auth status failed: %v", err)
		}

		var got authJSONReport
		if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
			t.Fatalf("unmarshal output: %v", err)
		}
		if got.Posture != "unknown" {
			t.Fatalf("expected unknown posture, got %q", got.Posture)
		}
		if got.Config.Posture != "unknown" {
			t.Fatalf("expected unknown config posture, got %q", got.Config.Posture)
		}
		if len(got.Services) != 0 {
			t.Fatalf("expected no services, got %#v", got.Services)
		}
	})
}

func TestAuthStatusDoesNotClaimConfigOnlyWithoutAuthEvidence(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".cli.json")
	if err := os.WriteFile(configPath, []byte(`{
  "cli": "1.0.0",
  "sources": {
    "demoSource": {"type":"openapi","enabled":true}
  },
  "services": {
    "demo": {"source":"demoSource","alias":"demo"}
  }
}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var stdout bytes.Buffer
	options := testOptions(&stdout, &stdout)
	options.Format = "json"
	options.ConfigPath = configPath
	options.Embedded = true
	cmd := NewAuthCommand(options, fakeRuntimeClient{
		runtimeInfoFn: func() (map[string]any, error) {
			return map[string]any{
				"auth": map[string]any{
					"required": false,
				},
			}, nil
		},
	}, false)
	cmd.SetArgs([]string{"status"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("auth status failed: %v", err)
	}

	var got authJSONReport
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal auth status: %v", err)
	}
	if got.Config.Posture != "unknown" {
		t.Fatalf("expected config posture unknown without auth evidence, got %q", got.Config.Posture)
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
