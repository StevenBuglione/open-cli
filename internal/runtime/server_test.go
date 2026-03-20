package runtime_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/StevenBuglione/open-cli/internal/runtime"
	"github.com/StevenBuglione/open-cli/pkg/audit"
	"github.com/StevenBuglione/open-cli/pkg/config"
	"github.com/StevenBuglione/open-cli/pkg/obs"
)

func writeRuntimeFile(t *testing.T, dir, name, content string) string {
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

func postRuntimeJSON(t *testing.T, endpoint string, payload any) (*http.Response, map[string]any) {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	resp, err := http.Post(endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post %s: %v", endpoint, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	var decoded map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode %s response: %v", endpoint, err)
	}
	return resp, decoded
}

func expectSignal(t *testing.T, signal <-chan struct{}, timeout time.Duration, message string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(timeout):
		t.Fatal(message)
	}
}

func expectNoSignal(t *testing.T, signal <-chan struct{}, duration time.Duration, message string) {
	t.Helper()
	select {
	case <-signal:
		t.Fatal(message)
	case <-time.After(duration):
	}
}

func expectJSONStringSlice(t *testing.T, got any, want []string, field string) {
	t.Helper()
	values, ok := got.([]any)
	if !ok {
		t.Fatalf("expected %s to be an array, got %#v", field, got)
	}
	if len(values) != len(want) {
		t.Fatalf("expected %s length %d, got %#v", field, len(want), values)
	}
	for i, expected := range want {
		if values[i] != expected {
			t.Fatalf("expected %s[%d]=%q, got %#v", field, i, expected, values[i])
		}
	}
}

type remoteConfigBindingFixture struct {
	configPath      string
	otherConfigPath string
	serverURL       string
	toolID          string
	workflowID      string
}

type remoteAuthBindingFixture struct {
	configPath      string
	otherConfigPath string
	serverURL       string
}

func newRemoteConfigBindingFixture(t *testing.T) remoteConfigBindingFixture {
	t.Helper()

	dir := t.TempDir()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tickets" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{"id": "T-1"}}})
	}))
	t.Cleanup(api.Close)

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
      responses:
        "200":
          description: OK
`)
	writeRuntimeFile(t, dir, "workflows/tickets.arazzo.yaml", `
arazzo: 1.0.0
info:
  title: Ticket workflows
  version: 1.0.0
workflows:
  - workflowId: triageTicket
    steps:
      - stepId: list
        operationId: listTickets
`)

	configBody := `{
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
	      "alias": "tickets",
	      "workflows": ["./workflows/tickets.arazzo.yaml"]
	    }
	  }
	}`
	configPath := writeRuntimeFile(t, dir, ".cli.json", configBody)
	otherConfigPath := writeRuntimeFile(t, dir, "other.cli.json", configBody)

	server := runtime.NewServer(runtime.Options{
		AuditPath:         filepath.Join(dir, "audit.log"),
		DefaultConfigPath: configPath,
		RuntimeMode:       "remote",
	})
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)

	return remoteConfigBindingFixture{
		configPath:      configPath,
		otherConfigPath: otherConfigPath,
		serverURL:       httpServer.URL,
		toolID:          "tickets:listTickets",
		workflowID:      "triageTicket",
	}
}

func newRemoteAuthBindingFixture(t *testing.T) remoteAuthBindingFixture {
	t.Helper()

	dir := t.TempDir()
	configPath := writeRuntimeFile(t, dir, ".cli.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "runtime": {
	    "server": {
	      "auth": {
	        "validationProfile": "oauth2_introspection",
	        "audience": "oclird",
	        "introspectionURL": "https://auth.example.com/introspect",
	        "authorizationURL": "https://auth.example.com/authorize",
	        "tokenURL": "https://auth.example.com/token",
	        "browserClientId": "browser-client"
	      }
	    }
	  },
	  "sources": {
	    "placeholder": {
	      "type": "openapi",
	      "uri": "https://example.com/openapi.json",
	      "enabled": false
	    }
	  }
	}`)
	otherConfigPath := writeRuntimeFile(t, dir, "other.cli.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "sources": {
	    "placeholder": {
	      "type": "openapi",
	      "uri": "https://example.com/openapi.json",
	      "enabled": false
	    }
	  }
	}`)

	server := runtime.NewServer(runtime.Options{
		AuditPath:         filepath.Join(dir, "audit.log"),
		DefaultConfigPath: configPath,
		RuntimeMode:       "remote",
	})
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)

	return remoteAuthBindingFixture{
		configPath:      configPath,
		otherConfigPath: otherConfigPath,
		serverURL:       httpServer.URL,
	}
}

func TestServerRuntimeInfoIncludesLeaseMetadata(t *testing.T) {
	dir := t.TempDir()
	server := runtime.NewServer(runtime.Options{
		AuditPath:            filepath.Join(dir, "audit.log"),
		StateDir:             filepath.Join(dir, "state"),
		CacheDir:             filepath.Join(dir, "cache"),
		InstanceID:           "runtime-1",
		RuntimeURL:           "http://127.0.0.1:18765",
		HeartbeatSeconds:     15,
		MissedHeartbeatLimit: 3,
		ShutdownMode:         "when-owner-exits",
		SessionScope:         "shared-group",
		ShareMode:            "group",
		ConfigFingerprint:    "fp-1",
	})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	resp, err := http.Get(httpServer.URL + "/v1/runtime/info")
	if err != nil {
		t.Fatalf("get runtime info: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 runtime info, got %d", resp.StatusCode)
	}
	var info map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		t.Fatalf("decode runtime info: %v", err)
	}
	lifecycle, ok := info["lifecycle"].(map[string]any)
	if !ok {
		t.Fatalf("expected lifecycle metadata, got %#v", info["lifecycle"])
	}
	capabilities, ok := lifecycle["capabilities"].([]any)
	if !ok {
		t.Fatalf("expected capabilities array, got %#v", lifecycle["capabilities"])
	}
	if len(capabilities) != 2 || capabilities[0] != "heartbeat" || capabilities[1] != "session-close" {
		t.Fatalf("expected heartbeat/session-close capabilities, got %#v", capabilities)
	}
	if got := lifecycle["heartbeatSeconds"]; got != float64(15) {
		t.Fatalf("expected heartbeatSeconds 15, got %#v", got)
	}
	if got := lifecycle["missedHeartbeatLimit"]; got != float64(3) {
		t.Fatalf("expected missedHeartbeatLimit 3, got %#v", got)
	}
	if got := lifecycle["shutdown"]; got != "when-owner-exits" {
		t.Fatalf("expected shutdown mode when-owner-exits, got %#v", got)
	}
	if got := info["runtimeMode"]; got != "local" {
		t.Fatalf("expected runtimeMode local, got %#v", got)
	}
	if got := lifecycle["sessionScope"]; got != "shared-group" {
		t.Fatalf("expected sessionScope shared-group, got %#v", got)
	}
	if got := lifecycle["shareMode"]; got != "group" {
		t.Fatalf("expected shareMode group, got %#v", got)
	}
	if got := lifecycle["configFingerprint"]; got != "fp-1" {
		t.Fatalf("expected configFingerprint fp-1, got %#v", got)
	}
	if got := lifecycle["shareKeyPresent"]; got != false {
		t.Fatalf("expected shareKeyPresent false, got %#v", got)
	}
}

func TestServerRemoteCatalogBindsRequestsToRuntimeOwnedConfig(t *testing.T) {
	fixture := newRemoteConfigBindingFixture(t)

	testCases := []struct {
		name       string
		configPath string
		wantStatus int
	}{
		{name: "allows absent selector", wantStatus: http.StatusOK},
		{name: "allows matching selector", configPath: fixture.configPath, wantStatus: http.StatusOK},
		{name: "rejects mismatched selector", configPath: fixture.otherConfigPath, wantStatus: http.StatusConflict},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			url := fixture.serverURL + "/v1/catalog/effective"
			if tc.configPath != "" {
				url += "?config=" + tc.configPath
			}
			resp, err := http.Get(url)
			if err != nil {
				t.Fatalf("catalog request: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("expected %d catalog response, got %d with body %q", tc.wantStatus, resp.StatusCode, readTrimmedBody(t, resp))
			}
			if tc.wantStatus != http.StatusOK {
				if got := readTrimmedBody(t, resp); got != "runtime_attach_mismatch" {
					t.Fatalf("expected runtime_attach_mismatch body, got %q", got)
				}
				return
			}

			var payload map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
				t.Fatalf("decode catalog response: %v", err)
			}
			catalogData, ok := payload["catalog"].(map[string]any)
			if !ok {
				t.Fatalf("expected catalog object, got %#v", payload["catalog"])
			}
			tools, ok := catalogData["tools"].([]any)
			if !ok || len(tools) != 1 {
				t.Fatalf("expected one catalog tool, got %#v", catalogData["tools"])
			}
			tool, ok := tools[0].(map[string]any)
			if !ok {
				t.Fatalf("expected tool object, got %#v", tools[0])
			}
			if got := tool["id"]; got != fixture.toolID {
				t.Fatalf("expected tool %q, got %#v", fixture.toolID, got)
			}
		})
	}
}

func TestServerRemoteExecuteBindsRequestsToRuntimeOwnedConfig(t *testing.T) {
	fixture := newRemoteConfigBindingFixture(t)

	testCases := []struct {
		name       string
		configPath string
		wantStatus int
	}{
		{name: "allows absent selector", wantStatus: http.StatusOK},
		{name: "allows matching selector", configPath: fixture.configPath, wantStatus: http.StatusOK},
		{name: "rejects mismatched selector", configPath: fixture.otherConfigPath, wantStatus: http.StatusConflict},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			payload := map[string]any{"toolId": fixture.toolID}
			if tc.configPath != "" {
				payload["configPath"] = tc.configPath
			}
			body, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("marshal execute payload: %v", err)
			}
			resp, err := http.Post(fixture.serverURL+"/v1/tools/execute", "application/json", bytes.NewReader(body))
			if err != nil {
				t.Fatalf("execute request: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("expected %d execute response, got %d with body %q", tc.wantStatus, resp.StatusCode, readTrimmedBody(t, resp))
			}
			if tc.wantStatus != http.StatusOK {
				if got := readTrimmedBody(t, resp); got != "runtime_attach_mismatch" {
					t.Fatalf("expected runtime_attach_mismatch body, got %q", got)
				}
				return
			}

			var payloadResp map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&payloadResp); err != nil {
				t.Fatalf("decode execute response: %v", err)
			}
			if got := payloadResp["statusCode"]; got != float64(http.StatusOK) {
				t.Fatalf("expected statusCode 200, got %#v", got)
			}
		})
	}
}

func TestServerRemoteWorkflowBindsRequestsToRuntimeOwnedConfig(t *testing.T) {
	fixture := newRemoteConfigBindingFixture(t)

	testCases := []struct {
		name       string
		configPath string
		wantStatus int
	}{
		{name: "allows absent selector", wantStatus: http.StatusOK},
		{name: "allows matching selector", configPath: fixture.configPath, wantStatus: http.StatusOK},
		{name: "rejects mismatched selector", configPath: fixture.otherConfigPath, wantStatus: http.StatusConflict},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			payload := map[string]any{"workflowId": fixture.workflowID}
			if tc.configPath != "" {
				payload["configPath"] = tc.configPath
			}
			body, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("marshal workflow payload: %v", err)
			}
			resp, err := http.Post(fixture.serverURL+"/v1/workflows/run", "application/json", bytes.NewReader(body))
			if err != nil {
				t.Fatalf("workflow request: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("expected %d workflow response, got %d with body %q", tc.wantStatus, resp.StatusCode, readTrimmedBody(t, resp))
			}
			if tc.wantStatus != http.StatusOK {
				if got := readTrimmedBody(t, resp); got != "runtime_attach_mismatch" {
					t.Fatalf("expected runtime_attach_mismatch body, got %q", got)
				}
				return
			}

			var payloadResp map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&payloadResp); err != nil {
				t.Fatalf("decode workflow response: %v", err)
			}
			if got := payloadResp["workflowId"]; got != fixture.workflowID {
				t.Fatalf("expected workflow %q, got %#v", fixture.workflowID, got)
			}
		})
	}
}

func TestServerRemoteAncillaryEndpointsBindRequestsToRuntimeOwnedConfig(t *testing.T) {
	fixture := newRemoteConfigBindingFixture(t)

	testCases := []struct {
		name       string
		method     string
		path       string
		configKey  string
		configPath string
		wantStatus int
		assertOK   func(*testing.T, *http.Response)
	}{
		{
			name:       "refresh allows absent selector",
			method:     http.MethodPost,
			path:       "/v1/refresh",
			wantStatus: http.StatusOK,
			assertOK: func(t *testing.T, resp *http.Response) {
				t.Helper()
				var payload map[string]any
				if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
					t.Fatalf("decode refresh response: %v", err)
				}
				if _, ok := payload["refreshedAt"]; !ok {
					t.Fatalf("expected refreshedAt, got %#v", payload)
				}
			},
		},
		{
			name:       "refresh allows matching selector",
			method:     http.MethodPost,
			path:       "/v1/refresh",
			configKey:  "configPath",
			configPath: fixture.configPath,
			wantStatus: http.StatusOK,
			assertOK: func(t *testing.T, resp *http.Response) {
				t.Helper()
				var payload map[string]any
				if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
					t.Fatalf("decode refresh response: %v", err)
				}
				if _, ok := payload["refreshedAt"]; !ok {
					t.Fatalf("expected refreshedAt, got %#v", payload)
				}
			},
		},
		{
			name:       "refresh rejects mismatched selector",
			method:     http.MethodPost,
			path:       "/v1/refresh",
			configKey:  "configPath",
			configPath: fixture.otherConfigPath,
			wantStatus: http.StatusConflict,
		},
		{
			name:       "audit allows absent selector",
			method:     http.MethodGet,
			path:       "/v1/audit/events",
			wantStatus: http.StatusOK,
			assertOK: func(t *testing.T, resp *http.Response) {
				t.Helper()
				var payload []map[string]any
				if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
					t.Fatalf("decode audit response: %v", err)
				}
			},
		},
		{
			name:       "audit allows matching selector",
			method:     http.MethodGet,
			path:       "/v1/audit/events?config=" + fixture.configPath,
			wantStatus: http.StatusOK,
			assertOK: func(t *testing.T, resp *http.Response) {
				t.Helper()
				var payload []map[string]any
				if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
					t.Fatalf("decode audit response: %v", err)
				}
			},
		},
		{
			name:       "audit rejects mismatched selector",
			method:     http.MethodGet,
			path:       "/v1/audit/events?config=" + fixture.otherConfigPath,
			wantStatus: http.StatusConflict,
		},
		{
			name:       "runtime info allows absent selector",
			method:     http.MethodGet,
			path:       "/v1/runtime/info",
			wantStatus: http.StatusOK,
			assertOK: func(t *testing.T, resp *http.Response) {
				t.Helper()
				var payload map[string]any
				if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
					t.Fatalf("decode runtime info response: %v", err)
				}
				if got := payload["runtimeMode"]; got != "remote" {
					t.Fatalf("expected runtimeMode remote, got %#v", got)
				}
			},
		},
		{
			name:       "runtime info allows matching selector",
			method:     http.MethodGet,
			path:       "/v1/runtime/info?config=" + fixture.configPath,
			wantStatus: http.StatusOK,
			assertOK: func(t *testing.T, resp *http.Response) {
				t.Helper()
				var payload map[string]any
				if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
					t.Fatalf("decode runtime info response: %v", err)
				}
				if got := payload["runtimeMode"]; got != "remote" {
					t.Fatalf("expected runtimeMode remote, got %#v", got)
				}
			},
		},
		{
			name:       "runtime info rejects mismatched selector",
			method:     http.MethodGet,
			path:       "/v1/runtime/info?config=" + fixture.otherConfigPath,
			wantStatus: http.StatusConflict,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var body io.Reader
			if tc.method == http.MethodPost {
				payload := map[string]any{}
				if tc.configKey != "" {
					payload[tc.configKey] = tc.configPath
				}
				data, err := json.Marshal(payload)
				if err != nil {
					t.Fatalf("marshal request payload: %v", err)
				}
				body = bytes.NewReader(data)
			}
			req, err := http.NewRequest(tc.method, fixture.serverURL+tc.path, body)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			if tc.method == http.MethodPost {
				req.Header.Set("Content-Type", "application/json")
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("do request: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("expected %d response, got %d with body %q", tc.wantStatus, resp.StatusCode, readTrimmedBody(t, resp))
			}
			if tc.wantStatus != http.StatusOK {
				if got := readTrimmedBody(t, resp); got != "runtime_attach_mismatch" {
					t.Fatalf("expected runtime_attach_mismatch body, got %q", got)
				}
				return
			}
			tc.assertOK(t, resp)
		})
	}
}

func TestServerRemoteBrowserConfigBindsRequestsToRuntimeOwnedConfig(t *testing.T) {
	fixture := newRemoteAuthBindingFixture(t)

	testCases := []struct {
		name       string
		configPath string
		wantStatus int
	}{
		{name: "allows absent selector", wantStatus: http.StatusOK},
		{name: "allows matching selector", configPath: fixture.configPath, wantStatus: http.StatusOK},
		{name: "rejects mismatched selector", configPath: fixture.otherConfigPath, wantStatus: http.StatusConflict},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			url := fixture.serverURL + "/v1/auth/browser-config"
			if tc.configPath != "" {
				url += "?config=" + tc.configPath
			}
			resp, err := http.Get(url)
			if err != nil {
				t.Fatalf("browser config request: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("expected %d browser config response, got %d with body %q", tc.wantStatus, resp.StatusCode, readTrimmedBody(t, resp))
			}
			if tc.wantStatus != http.StatusOK {
				if got := readTrimmedBody(t, resp); got != "runtime_attach_mismatch" {
					t.Fatalf("expected runtime_attach_mismatch body, got %q", got)
				}
				return
			}
			var metadata map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&metadata); err != nil {
				t.Fatalf("decode browser config response: %v", err)
			}
			if got := metadata["clientId"]; got != "browser-client" {
				t.Fatalf("expected default runtime browser config, got %#v", got)
			}
		})
	}
}

func TestServerRemoteLifecycleEndpointsBindRequestsToRuntimeOwnedConfig(t *testing.T) {
	fixture := newRemoteAuthBindingFixture(t)

	testCases := []struct {
		name       string
		method     string
		path       string
		body       string
		configPath string
		wantStatus int
		wantBody   string
	}{
		{
			name:       "heartbeat requires auth on bound config",
			method:     http.MethodPost,
			path:       "/v1/runtime/heartbeat",
			body:       `{"sessionId":"sess-1"}`,
			wantStatus: http.StatusUnauthorized,
			wantBody:   "authn_failed",
		},
		{
			name:       "heartbeat rejects mismatched selector",
			method:     http.MethodPost,
			path:       "/v1/runtime/heartbeat",
			body:       `{"sessionId":"sess-1"}`,
			configPath: fixture.otherConfigPath,
			wantStatus: http.StatusConflict,
			wantBody:   "runtime_attach_mismatch",
		},
		{
			name:       "stop requires auth on bound config",
			method:     http.MethodPost,
			path:       "/v1/runtime/stop",
			wantStatus: http.StatusUnauthorized,
			wantBody:   "authn_failed",
		},
		{
			name:       "stop rejects mismatched selector",
			method:     http.MethodPost,
			path:       "/v1/runtime/stop",
			configPath: fixture.otherConfigPath,
			wantStatus: http.StatusConflict,
			wantBody:   "runtime_attach_mismatch",
		},
		{
			name:       "session-close requires auth on bound config",
			method:     http.MethodPost,
			path:       "/v1/runtime/session-close",
			body:       `{"sessionId":"sess-1"}`,
			wantStatus: http.StatusUnauthorized,
			wantBody:   "authn_failed",
		},
		{
			name:       "session-close rejects mismatched selector",
			method:     http.MethodPost,
			path:       "/v1/runtime/session-close",
			body:       `{"sessionId":"sess-1"}`,
			configPath: fixture.otherConfigPath,
			wantStatus: http.StatusConflict,
			wantBody:   "runtime_attach_mismatch",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			url := fixture.serverURL + tc.path
			if tc.configPath != "" {
				url += "?config=" + tc.configPath
			}
			req, err := http.NewRequest(tc.method, url, strings.NewReader(tc.body))
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("lifecycle request: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("expected %d response, got %d with body %q", tc.wantStatus, resp.StatusCode, readTrimmedBody(t, resp))
			}
			if got := readTrimmedBody(t, resp); got != tc.wantBody {
				t.Fatalf("expected body %q, got %q", tc.wantBody, got)
			}
		})
	}
}

func TestServerHeartbeatRenewsLease(t *testing.T) {
	dir := t.TempDir()
	shutdownCalled := make(chan struct{}, 1)
	server := runtime.NewServer(runtime.Options{
		AuditPath:            filepath.Join(dir, "audit.log"),
		StateDir:             filepath.Join(dir, "state"),
		HeartbeatSeconds:     1,
		MissedHeartbeatLimit: 1,
		ShutdownMode:         "when-owner-exits",
		Shutdown: func() error {
			select {
			case shutdownCalled <- struct{}{}:
			default:
			}
			return nil
		},
	})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	resp, payload := postRuntimeJSON(t, httpServer.URL+"/v1/runtime/heartbeat", map[string]any{"sessionId": "sess-1"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected first heartbeat 200, got %d", resp.StatusCode)
	}
	if renewed, ok := payload["renewed"].(bool); !ok || !renewed {
		t.Fatalf("expected renewed=true, got %#v", payload)
	}
	if got := payload["sessionId"]; got != "sess-1" {
		t.Fatalf("expected sessionId sess-1, got %#v", got)
	}

	time.Sleep(700 * time.Millisecond)
	resp, payload = postRuntimeJSON(t, httpServer.URL+"/v1/runtime/heartbeat", map[string]any{"sessionId": "sess-1"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected second heartbeat 200, got %d", resp.StatusCode)
	}
	if got := payload["sessionId"]; got != "sess-1" {
		t.Fatalf("expected renewed sessionId sess-1, got %#v", got)
	}
	expectNoSignal(t, shutdownCalled, 650*time.Millisecond, "expected renewed lease to remain active past original ttl")
	expectSignal(t, shutdownCalled, 1200*time.Millisecond, "expected lease to expire after renewed ttl elapsed")
}

func TestServerSessionCloseRemovesLease(t *testing.T) {
	dir := t.TempDir()
	shutdownCalled := make(chan struct{}, 1)
	server := runtime.NewServer(runtime.Options{
		AuditPath:            filepath.Join(dir, "audit.log"),
		StateDir:             filepath.Join(dir, "state"),
		HeartbeatSeconds:     5,
		MissedHeartbeatLimit: 1,
		ShutdownMode:         "when-owner-exits",
		SessionScope:         "shared-group",
		ShareMode:            "group",
		Shutdown: func() error {
			select {
			case shutdownCalled <- struct{}{}:
			default:
			}
			return nil
		},
	})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	postRuntimeJSON(t, httpServer.URL+"/v1/runtime/heartbeat", map[string]any{"sessionId": "sess-1"})
	postRuntimeJSON(t, httpServer.URL+"/v1/runtime/heartbeat", map[string]any{"sessionId": "sess-2"})

	resp, payload := postRuntimeJSON(t, httpServer.URL+"/v1/runtime/session-close", map[string]any{"sessionId": "sess-1"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected session-close 200, got %d", resp.StatusCode)
	}
	if closed, ok := payload["closed"].(bool); !ok || !closed {
		t.Fatalf("expected closed=true, got %#v", payload)
	}
	expectNoSignal(t, shutdownCalled, 300*time.Millisecond, "expected surviving session lease to keep runtime alive")

	postRuntimeJSON(t, httpServer.URL+"/v1/runtime/session-close", map[string]any{"sessionId": "sess-2"})
	expectSignal(t, shutdownCalled, time.Second, "expected shutdown after last session lease removed")
}

func TestServerLeaseExpiryTriggersShutdown(t *testing.T) {
	dir := t.TempDir()
	shutdownCalled := make(chan struct{}, 1)
	server := runtime.NewServer(runtime.Options{
		AuditPath:            filepath.Join(dir, "audit.log"),
		StateDir:             filepath.Join(dir, "state"),
		HeartbeatSeconds:     1,
		MissedHeartbeatLimit: 1,
		ShutdownMode:         "when-owner-exits",
		Shutdown: func() error {
			select {
			case shutdownCalled <- struct{}{}:
			default:
			}
			return nil
		},
	})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	postRuntimeJSON(t, httpServer.URL+"/v1/runtime/heartbeat", map[string]any{"sessionId": "sess-1"})
	expectSignal(t, shutdownCalled, 2*time.Second, "expected expired sole lease to trigger shutdown")
}

func TestServerLeaseExpiryRetainsManualRuntime(t *testing.T) {
	dir := t.TempDir()
	shutdownCalled := make(chan struct{}, 1)
	server := runtime.NewServer(runtime.Options{
		AuditPath:            filepath.Join(dir, "audit.log"),
		StateDir:             filepath.Join(dir, "state"),
		HeartbeatSeconds:     1,
		MissedHeartbeatLimit: 1,
		ShutdownMode:         "manual",
		Shutdown: func() error {
			select {
			case shutdownCalled <- struct{}{}:
			default:
			}
			return nil
		},
	})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	postRuntimeJSON(t, httpServer.URL+"/v1/runtime/heartbeat", map[string]any{"sessionId": "sess-1"})
	expectNoSignal(t, shutdownCalled, 1500*time.Millisecond, "expected manual runtime to ignore lease expiry")
}

func TestServerRejectsExclusiveLeaseConflict(t *testing.T) {
	dir := t.TempDir()
	server := runtime.NewServer(runtime.Options{
		AuditPath:            filepath.Join(dir, "audit.log"),
		StateDir:             filepath.Join(dir, "state"),
		HeartbeatSeconds:     15,
		MissedHeartbeatLimit: 3,
		ShutdownMode:         "when-owner-exits",
		SessionScope:         "terminal",
		ShareMode:            "exclusive",
		ConfigFingerprint:    "fp-1",
	})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	resp, _ := postRuntimeJSON(t, httpServer.URL+"/v1/runtime/heartbeat", map[string]any{
		"sessionId":         "sess-1",
		"configFingerprint": "fp-1",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected first heartbeat 200, got %d", resp.StatusCode)
	}

	body, err := json.Marshal(map[string]any{
		"sessionId":         "sess-2",
		"configFingerprint": "fp-1",
	})
	if err != nil {
		t.Fatalf("marshal heartbeat: %v", err)
	}
	conflictResp, err := http.Post(httpServer.URL+"/v1/runtime/heartbeat", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post conflicting heartbeat: %v", err)
	}
	defer conflictResp.Body.Close()
	if conflictResp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 conflicting heartbeat, got %d", conflictResp.StatusCode)
	}
	data, _ := io.ReadAll(conflictResp.Body)
	if strings.TrimSpace(string(data)) != "runtime_attach_conflict" {
		t.Fatalf("expected runtime_attach_conflict response, got %q", strings.TrimSpace(string(data)))
	}
}

func TestServerRejectsLeaseFingerprintMismatch(t *testing.T) {
	dir := t.TempDir()
	server := runtime.NewServer(runtime.Options{
		AuditPath:            filepath.Join(dir, "audit.log"),
		StateDir:             filepath.Join(dir, "state"),
		HeartbeatSeconds:     15,
		MissedHeartbeatLimit: 3,
		ShutdownMode:         "when-owner-exits",
		SessionScope:         "terminal",
		ShareMode:            "exclusive",
		ConfigFingerprint:    "fp-1",
	})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	body, err := json.Marshal(map[string]any{
		"sessionId":         "sess-1",
		"configFingerprint": "fp-2",
	})
	if err != nil {
		t.Fatalf("marshal heartbeat: %v", err)
	}
	resp, err := http.Post(httpServer.URL+"/v1/runtime/heartbeat", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post mismatched heartbeat: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 mismatched heartbeat, got %d", resp.StatusCode)
	}
	data, _ := io.ReadAll(resp.Body)
	if strings.TrimSpace(string(data)) != "runtime_attach_mismatch" {
		t.Fatalf("expected runtime_attach_mismatch response, got %q", strings.TrimSpace(string(data)))
	}
}

func TestServerRecordsSessionLifecycleAuditEvents(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.log")
	server := runtime.NewServer(runtime.Options{
		AuditPath:            auditPath,
		StateDir:             filepath.Join(dir, "state"),
		HeartbeatSeconds:     1,
		MissedHeartbeatLimit: 1,
		ShutdownMode:         "manual",
		SessionScope:         "shared-group",
		ShareMode:            "group",
		ConfigFingerprint:    "fp-1",
	})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	postRuntimeJSON(t, httpServer.URL+"/v1/runtime/heartbeat", map[string]any{
		"sessionId":         "close-me",
		"configFingerprint": "fp-1",
	})
	postRuntimeJSON(t, httpServer.URL+"/v1/runtime/session-close", map[string]any{"sessionId": "close-me"})
	postRuntimeJSON(t, httpServer.URL+"/v1/runtime/heartbeat", map[string]any{
		"sessionId":         "expire-me",
		"configFingerprint": "fp-1",
	})
	time.Sleep(1500 * time.Millisecond)

	events, err := audit.NewFileStore(auditPath).List()
	if err != nil {
		t.Fatalf("list audit events: %v", err)
	}

	var sawClose, sawExpiry bool
	for _, event := range events {
		if event.EventType == "session_close" && event.SessionID == "close-me" {
			sawClose = true
		}
		if event.EventType == "session_expiry" && event.SessionID == "expire-me" {
			sawExpiry = true
		}
	}
	if !sawClose {
		t.Fatalf("expected session_close audit event, got %#v", events)
	}
	if !sawExpiry {
		t.Fatalf("expected session_expiry audit event, got %#v", events)
	}
}

func TestServerRecordsAuthLifecycleAuditEvents(t *testing.T) {
	dir := t.TempDir()
	writeRuntimeFile(t, dir, "tickets.openapi.yaml", `
openapi: 3.1.0
info:
  title: Tickets API
  version: "1.0.0"
servers:
  - url: https://example.com
paths:
  /tickets:
    get:
      operationId: listTickets
      tags: [tickets]
      responses:
        "200":
          description: OK
`)

	introspectionServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse introspection form: %v", err)
		}
		if got := r.Form.Get("token"); got != "scoped-token" {
			t.Fatalf("expected scoped-token, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"active": true,
			"scope":  "bundle:tickets",
			"aud":    "oclird",
			"sub":    "agent-1",
		})
	}))
	defer introspectionServer.Close()

	configPath := writeRuntimeFile(t, dir, ".cli.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "runtime": {
	    "server": {
	      "auth": {
	        "mode": "oauth2Introspection",
	        "audience": "oclird",
	        "introspectionURL": "`+introspectionServer.URL+`"
	      }
	    }
	  },
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
	}`)

	auditPath := filepath.Join(dir, "audit.log")
	server := runtime.NewServer(runtime.Options{
		AuditPath:         auditPath,
		DefaultConfigPath: configPath,
	})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	req, err := http.NewRequest(http.MethodGet, httpServer.URL+"/v1/catalog/effective?config="+configPath, nil)
	if err != nil {
		t.Fatalf("new catalog request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer scoped-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("authorized catalog request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 authorized catalog, got %d", resp.StatusCode)
	}

	unauthorizedResp, err := http.Get(httpServer.URL + "/v1/catalog/effective?config=" + configPath)
	if err != nil {
		t.Fatalf("unauthorized catalog request: %v", err)
	}
	unauthorizedResp.Body.Close()
	if unauthorizedResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 unauthorized catalog, got %d", unauthorizedResp.StatusCode)
	}

	refreshBody := bytes.NewBufferString(`{"configPath":"` + configPath + `"}`)
	refreshReq, err := http.NewRequest(http.MethodPost, httpServer.URL+"/v1/refresh", refreshBody)
	if err != nil {
		t.Fatalf("new refresh request: %v", err)
	}
	refreshReq.Header.Set("Content-Type", "application/json")
	refreshReq.Header.Set("Authorization", "Bearer scoped-token")
	refreshResp, err := http.DefaultClient.Do(refreshReq)
	if err != nil {
		t.Fatalf("authorized refresh request: %v", err)
	}
	refreshResp.Body.Close()
	if refreshResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 authorized refresh, got %d", refreshResp.StatusCode)
	}

	events, err := audit.NewFileStore(auditPath).List()
	if err != nil {
		t.Fatalf("list audit events: %v", err)
	}

	var sawConnect, sawCatalog, sawAuthnFailure, sawRefresh bool
	for _, event := range events {
		switch event.EventType {
		case "authenticated_connect":
			if event.Principal == "agent-1" {
				sawConnect = true
			}
		case "catalog_filtered":
			if event.Principal == "agent-1" {
				sawCatalog = true
			}
		case "authn_failure":
			sawAuthnFailure = true
		case "token_refresh":
			if event.Principal == "agent-1" {
				sawRefresh = true
			}
		}
	}
	if !sawConnect {
		t.Fatalf("expected authenticated_connect audit event, got %#v", events)
	}
	if !sawCatalog {
		t.Fatalf("expected catalog_filtered audit event, got %#v", events)
	}
	if !sawAuthnFailure {
		t.Fatalf("expected authn_failure audit event, got %#v", events)
	}
	if !sawRefresh {
		t.Fatalf("expected token_refresh audit event, got %#v", events)
	}
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

func TestServerFiltersCatalogAndExecutionByRemoteBundleScope(t *testing.T) {
	dir := t.TempDir()

	introspectionServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/introspect" {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		if got := r.PostForm.Get("token"); got != "scoped-token" {
			t.Fatalf("expected token scoped-token, got %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"active": true,
			"scope":  "bundle:tickets",
			"aud":    "oclird",
			"sub":    "agent-123",
		})
	}))
	defer introspectionServer.Close()

	ticketsAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{"id": "T-1"}}})
	}))
	defer ticketsAPI.Close()

	usersAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{"id": "U-1"}}})
	}))
	defer usersAPI.Close()

	writeRuntimeFile(t, dir, "tickets.openapi.yaml", `
openapi: 3.1.0
info:
  title: Tickets API
  version: "1.0.0"
servers:
  - url: `+ticketsAPI.URL+`
paths:
  /tickets:
    get:
      operationId: listTickets
      tags: [tickets]
      responses:
        "200":
          description: OK
`)
	writeRuntimeFile(t, dir, "users.openapi.yaml", `
openapi: 3.1.0
info:
  title: Users API
  version: "1.0.0"
servers:
  - url: `+usersAPI.URL+`
paths:
  /users:
    get:
      operationId: listUsers
      tags: [users]
      responses:
        "200":
          description: OK
`)
	configPath := writeRuntimeFile(t, dir, ".cli.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "runtime": {
	    "server": {
	      "auth": {
	        "mode": "oauth2Introspection",
	        "audience": "oclird",
	        "introspectionURL": "`+introspectionServer.URL+`/introspect"
	      }
	    }
	  },
	  "sources": {
	    "ticketsSource": {
	      "type": "openapi",
	      "uri": "./tickets.openapi.yaml",
	      "enabled": true
	    },
	    "usersSource": {
	      "type": "openapi",
	      "uri": "./users.openapi.yaml",
	      "enabled": true
	    }
	  },
	  "services": {
	    "tickets": {
	      "source": "ticketsSource",
	      "alias": "tickets"
	    },
	    "users": {
	      "source": "usersSource",
	      "alias": "users"
	    }
	  }
	}`)

	server := runtime.NewServer(runtime.Options{AuditPath: filepath.Join(dir, "audit.log")})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	req, err := http.NewRequest(http.MethodGet, httpServer.URL+"/v1/catalog/effective?config="+configPath, nil)
	if err != nil {
		t.Fatalf("NewRequest catalog: %v", err)
	}
	req.Header.Set("Authorization", "Bearer scoped-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get effective catalog: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 effective catalog, got %d", resp.StatusCode)
	}
	var effective map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&effective); err != nil {
		t.Fatalf("decode effective catalog: %v", err)
	}
	catalogData := effective["catalog"].(map[string]any)
	tools := catalogData["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("expected one scoped tool, got %#v", tools)
	}
	tool := tools[0].(map[string]any)
	if got := tool["id"]; got != "tickets:listTickets" {
		t.Fatalf("expected tickets tool in scoped catalog, got %#v", got)
	}

	denyBody := bytes.NewBufferString(`{
	  "configPath": "` + configPath + `",
	  "toolId": "users:listUsers"
	}`)
	denyReq, err := http.NewRequest(http.MethodPost, httpServer.URL+"/v1/tools/execute", denyBody)
	if err != nil {
		t.Fatalf("NewRequest execute deny: %v", err)
	}
	denyReq.Header.Set("Content-Type", "application/json")
	denyReq.Header.Set("Authorization", "Bearer scoped-token")
	denyResp, err := http.DefaultClient.Do(denyReq)
	if err != nil {
		t.Fatalf("deny execute request: %v", err)
	}
	defer denyResp.Body.Close()
	if denyResp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for out-of-scope tool, got %d", denyResp.StatusCode)
	}
}

func TestServerRejectsCatalogWithoutBearerTokenWhenRemoteAuthEnabled(t *testing.T) {
	dir := t.TempDir()
	writeRuntimeFile(t, dir, "tickets.openapi.yaml", `
openapi: 3.1.0
info:
  title: Tickets API
  version: "1.0.0"
servers:
  - url: https://example.com
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
	  "runtime": {
	    "server": {
	      "auth": {
	        "mode": "oauth2Introspection",
	        "audience": "oclird",
	        "introspectionURL": "https://auth.example.com/introspect"
	      }
	    }
	  },
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
	}`)

	server := runtime.NewServer(runtime.Options{AuditPath: filepath.Join(dir, "audit.log")})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	resp, err := http.Get(httpServer.URL + "/v1/catalog/effective?config=" + configPath)
	if err != nil {
		t.Fatalf("get effective catalog: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without bearer token, got %d", resp.StatusCode)
	}
}

func TestServerRejectsCatalogWithoutBearerTokenWhenValidationProfileNormalizesIntrospectionAuth(t *testing.T) {
	dir := t.TempDir()
	writeRuntimeFile(t, dir, "tickets.openapi.yaml", `
openapi: 3.1.0
info:
  title: Tickets API
  version: "1.0.0"
servers:
  - url: https://example.com
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
	  "runtime": {
	    "server": {
	      "auth": {
	        "validationProfile": "oauth2_introspection",
	        "audience": "oclird",
	        "introspectionURL": "https://auth.example.com/introspect"
	      }
	    }
	  },
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
	}`)

	effective, err := config.LoadEffective(config.LoadOptions{ProjectPath: configPath, WorkingDir: dir})
	if err != nil {
		t.Fatalf("LoadEffective returned error: %v", err)
	}
	if effective.Config.Runtime == nil || effective.Config.Runtime.Server == nil || effective.Config.Runtime.Server.Auth == nil {
		t.Fatalf("expected runtime server auth configuration to load")
	}
	auth := *effective.Config.Runtime.Server.Auth
	if auth.Mode != "oauth2Introspection" {
		t.Fatalf("expected canonical validation profile to normalize legacy mode, got %q", auth.Mode)
	}
	if auth.ValidationProfile != "oauth2_introspection" {
		t.Fatalf("expected canonical validation profile to be preserved, got %q", auth.ValidationProfile)
	}

	server := runtime.NewServer(runtime.Options{AuditPath: filepath.Join(dir, "audit.log")})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	resp, err := http.Get(httpServer.URL + "/v1/catalog/effective?config=" + configPath)
	if err != nil {
		t.Fatalf("get effective catalog: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without bearer token after canonical normalization, got %d", resp.StatusCode)
	}
}

func TestServerRejectsExpiredOIDCJWKSToken(t *testing.T) {
	dir := t.TempDir()
	issuer := newOIDCJWKSTestIssuer(t)
	configPath := writeOIDCJWKSRuntimeConfig(t, dir, issuer, "https://tickets.example.com", "https://users.example.com")

	token := issuer.signToken(t, map[string]any{
		"sub":   "agent-123",
		"aud":   "oclird",
		"scope": "bundle:tickets",
		"exp":   time.Now().Add(-time.Hour).Unix(),
	})

	server := runtime.NewServer(runtime.Options{AuditPath: filepath.Join(dir, "audit.log")})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	req, err := http.NewRequest(http.MethodGet, httpServer.URL+"/v1/catalog/effective?config="+configPath, nil)
	if err != nil {
		t.Fatalf("new catalog request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get effective catalog: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 effective catalog for expired oidc_jwks token, got %d", resp.StatusCode)
	}
	if got := readTrimmedBody(t, resp); got != "authn_failed" {
		t.Fatalf("expected authn_failed body for expired oidc_jwks token, got %q", got)
	}
}

func TestServerReturnsEmptyAuthorizationEnvelopeWhenOIDCJWKSTokenMissingScopes(t *testing.T) {
	dir := t.TempDir()
	issuer := newOIDCJWKSTestIssuer(t)

	var ticketsCalls int
	ticketsAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ticketsCalls++
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{"id": "T-1"}}})
	}))
	defer ticketsAPI.Close()

	usersAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{"id": "U-1"}}})
	}))
	defer usersAPI.Close()

	configPath := writeOIDCJWKSRuntimeConfig(t, dir, issuer, ticketsAPI.URL, usersAPI.URL)
	token := issuer.signToken(t, map[string]any{
		"sub": "agent-123",
		"aud": "oclird",
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	server := runtime.NewServer(runtime.Options{AuditPath: filepath.Join(dir, "audit.log")})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	req, err := http.NewRequest(http.MethodGet, httpServer.URL+"/v1/catalog/effective?config="+configPath, nil)
	if err != nil {
		t.Fatalf("new catalog request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get effective catalog: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 effective catalog for scope-less oidc_jwks token, got %d", resp.StatusCode)
	}

	var effective map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&effective); err != nil {
		t.Fatalf("decode effective catalog: %v", err)
	}
	catalogData := effective["catalog"].(map[string]any)
	tools := catalogData["tools"].([]any)
	if len(tools) != 0 {
		t.Fatalf("expected empty oidc_jwks authorization envelope without runtime scopes, got %#v", tools)
	}

	execBody := bytes.NewBufferString(`{
	  "configPath": "` + configPath + `",
	  "toolId": "tickets:listTickets"
	}`)
	execReq, err := http.NewRequest(http.MethodPost, httpServer.URL+"/v1/tools/execute", execBody)
	if err != nil {
		t.Fatalf("new execute request: %v", err)
	}
	execReq.Header.Set("Content-Type", "application/json")
	execReq.Header.Set("Authorization", "Bearer "+token)

	execResp, err := http.DefaultClient.Do(execReq)
	if err != nil {
		t.Fatalf("execute tool request: %v", err)
	}
	defer execResp.Body.Close()
	if execResp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 authz_denied for scope-less oidc_jwks token, got %d", execResp.StatusCode)
	}
	if got := readTrimmedBody(t, execResp); got != "authz_denied" {
		t.Fatalf("expected authz_denied body for scope-less oidc_jwks token, got %q", got)
	}
	if ticketsCalls != 0 {
		t.Fatalf("expected denied oidc_jwks execution not to hit upstream, got %d calls", ticketsCalls)
	}
}

func TestServerBrowserConfigIncludesBrokeredAuthMetadata(t *testing.T) {
	dir := t.TempDir()
	configPath := writeRuntimeFile(t, dir, ".cli.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "runtime": {
	    "server": {
	      "auth": {
	        "validationProfile": "oauth2_introspection",
	        "audience": "oclird",
	        "introspectionURL": "https://auth.example.com/introspect",
	        "authorizationURL": "https://auth.example.com/authorize",
	        "tokenURL": "https://auth.example.com/token",
	        "browserClientId": "browser-client"
	      }
	    }
	  },
	  "sources": {
	    "ticketsSource": {
	      "type": "openapi",
	      "uri": "https://example.com/openapi.yaml",
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

	server := runtime.NewServer(runtime.Options{AuditPath: filepath.Join(dir, "audit.log"), DefaultConfigPath: configPath})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	resp, err := http.Get(httpServer.URL + "/v1/auth/browser-config")
	if err != nil {
		t.Fatalf("get browser config: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 browser config response, got %d", resp.StatusCode)
	}
	var metadata map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&metadata); err != nil {
		t.Fatalf("decode browser config: %v", err)
	}
	if got := metadata["authorizationURL"]; got != "https://auth.example.com/authorize" {
		t.Fatalf("expected authorization URL in metadata, got %#v", got)
	}
	if got := metadata["tokenURL"]; got != "https://auth.example.com/token" {
		t.Fatalf("expected token URL in metadata, got %#v", got)
	}
	if got := metadata["clientId"]; got != "browser-client" {
		t.Fatalf("expected browser client id in metadata, got %#v", got)
	}
	if got := metadata["audience"]; got != "oclird" {
		t.Fatalf("expected audience in metadata, got %#v", got)
	}
	if got := metadata["required"]; got != true {
		t.Fatalf("expected required=true in metadata, got %#v", got)
	}
	expectJSONStringSlice(t, metadata["tokenValidationProfiles"], []string{"oauth2_introspection"}, "tokenValidationProfiles")
	expectJSONStringSlice(t, metadata["scopePrefixes"], []string{"bundle:", "profile:", "tool:"}, "scopePrefixes")
	envelope, ok := metadata["authorizationEnvelope"].(map[string]any)
	if !ok {
		t.Fatalf("expected authorizationEnvelope metadata, got %#v", metadata["authorizationEnvelope"])
	}
	if got := envelope["version"]; got != "1.0" {
		t.Fatalf("expected authorization envelope version 1.0, got %#v", got)
	}
	expectJSONStringSlice(t, envelope["scopePrefixes"], []string{"bundle:", "profile:", "tool:"}, "authorizationEnvelope.scopePrefixes")
}

func TestServerFiltersCatalogByProfileAndExplicitToolScopes(t *testing.T) {
	dir := t.TempDir()

	introspectionServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"active": true,
			"scope":  "profile:reader tool:tickets:getTicket",
			"aud":    "oclird",
			"sub":    "agent-456",
		})
	}))
	defer introspectionServer.Close()

	writeRuntimeFile(t, dir, "tickets.openapi.yaml", `
openapi: 3.1.0
info:
  title: Tickets API
  version: "1.0.0"
servers:
  - url: https://example.com
paths:
  /tickets:
    get:
      operationId: listTickets
      tags: [tickets]
      responses:
        "200":
          description: OK
  /tickets/{id}:
    get:
      operationId: getTicket
      tags: [tickets]
      parameters:
        - name: id
          in: path
          required: true
          schema: { type: string }
      responses:
        "200":
          description: OK
`)
	configPath := writeRuntimeFile(t, dir, ".cli.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "runtime": {
	    "server": {
	      "auth": {
	        "mode": "oauth2Introspection",
	        "audience": "oclird",
	        "introspectionURL": "`+introspectionServer.URL+`"
	      }
	    }
	  },
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
	      "reader": {
	        "allow": ["tickets:listTickets", "tickets:getTicket"]
	      }
	    }
	  },
	  "agents": {
	    "profiles": {
	      "reader": {
	        "mode": "curated",
	        "toolSet": "reader"
	      }
	    }
	  }
	}`)

	server := runtime.NewServer(runtime.Options{AuditPath: filepath.Join(dir, "audit.log")})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	req, err := http.NewRequest(http.MethodGet, httpServer.URL+"/v1/catalog/effective?config="+configPath, nil)
	if err != nil {
		t.Fatalf("NewRequest catalog: %v", err)
	}
	req.Header.Set("Authorization", "Bearer scoped-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get effective catalog: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 effective catalog, got %d", resp.StatusCode)
	}
	var effective map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&effective); err != nil {
		t.Fatalf("decode effective catalog: %v", err)
	}
	catalogData := effective["catalog"].(map[string]any)
	tools := catalogData["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("expected one intersected tool, got %#v", tools)
	}
	tool := tools[0].(map[string]any)
	if got := tool["id"]; got != "tickets:getTicket" {
		t.Fatalf("expected tickets:getTicket in scoped catalog, got %#v", got)
	}
}

func TestServerAppliesPolicyDenyAfterRemoteScopes(t *testing.T) {
	dir := t.TempDir()

	introspectionServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"active": true,
			"scope":  "bundle:tickets",
			"aud":    "oclird",
		})
	}))
	defer introspectionServer.Close()

	writeRuntimeFile(t, dir, "tickets.openapi.yaml", `
openapi: 3.1.0
info:
  title: Tickets API
  version: "1.0.0"
servers:
  - url: https://example.com
paths:
  /tickets:
    get:
      operationId: listTickets
      tags: [tickets]
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
	  "runtime": {
	    "server": {
	      "auth": {
	        "mode": "oauth2Introspection",
	        "audience": "oclird",
	        "introspectionURL": "`+introspectionServer.URL+`"
	      }
	    }
	  },
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
	    "deny": ["tickets:deleteTicket"]
	  }
	}`)

	server := runtime.NewServer(runtime.Options{AuditPath: filepath.Join(dir, "audit.log")})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	req, err := http.NewRequest(http.MethodGet, httpServer.URL+"/v1/catalog/effective?config="+configPath, nil)
	if err != nil {
		t.Fatalf("NewRequest catalog: %v", err)
	}
	req.Header.Set("Authorization", "Bearer scoped-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get effective catalog: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 effective catalog, got %d", resp.StatusCode)
	}
	var effective map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&effective); err != nil {
		t.Fatalf("decode effective catalog: %v", err)
	}
	catalogData := effective["catalog"].(map[string]any)
	tools := catalogData["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("expected deny-filtered catalog to contain one tool, got %#v", tools)
	}
	tool := tools[0].(map[string]any)
	if got := tool["id"]; got != "tickets:listTickets" {
		t.Fatalf("expected deny-filtered tool tickets:listTickets, got %#v", got)
	}
}

func TestServerRejectsRefreshWithoutBearerTokenWhenRemoteAuthEnabled(t *testing.T) {
	dir := t.TempDir()
	writeRuntimeFile(t, dir, "tickets.openapi.yaml", `
openapi: 3.1.0
info:
  title: Tickets API
  version: "1.0.0"
servers:
  - url: https://example.com
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
	  "runtime": {
	    "server": {
	      "auth": {
	        "mode": "oauth2Introspection",
	        "audience": "oclird",
	        "introspectionURL": "https://auth.example.com/introspect"
	      }
	    }
	  },
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
	}`)

	server := runtime.NewServer(runtime.Options{AuditPath: filepath.Join(dir, "audit.log")})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	requestBody := bytes.NewBufferString(`{"configPath":"` + configPath + `"}`)
	resp, err := http.Post(httpServer.URL+"/v1/refresh", "application/json", requestBody)
	if err != nil {
		t.Fatalf("refresh request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 refresh response without bearer token, got %d", resp.StatusCode)
	}
}

func TestServerRejectsAuditEventsWithoutBearerTokenWhenRemoteAuthEnabled(t *testing.T) {
	dir := t.TempDir()
	configPath := writeRuntimeFile(t, dir, ".cli.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "runtime": {
	    "server": {
	      "auth": {
	        "mode": "oauth2Introspection",
	        "audience": "oclird",
	        "introspectionURL": "https://auth.example.com/introspect"
	      }
	    }
	  },
	  "sources": {
	    "ticketsSource": {
	      "type": "openapi",
	      "uri": "https://example.com/openapi.yaml",
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
		AuditPath:         filepath.Join(dir, "audit.log"),
		DefaultConfigPath: configPath,
	})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	resp, err := http.Get(httpServer.URL + "/v1/audit/events")
	if err != nil {
		t.Fatalf("audit events request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 audit events response without bearer token, got %d", resp.StatusCode)
	}
}

func TestServerRuntimeInfoIncludesBrokeredAuthMetadata(t *testing.T) {
	dir := t.TempDir()
	introspectionServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"active": true,
			"scope":  "profile:reader tool:tickets:getTicket",
			"aud":    "oclird",
			"sub":    "agent-456",
		})
	}))
	defer introspectionServer.Close()

	configPath := writeRuntimeFile(t, dir, ".cli.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "runtime": {
	    "server": {
	      "auth": {
	        "validationProfile": "oauth2_introspection",
	        "audience": "oclird",
	        "introspectionURL": "`+introspectionServer.URL+`",
	        "authorizationURL": "https://auth.example.com/authorize",
	        "tokenURL": "https://auth.example.com/token",
	        "browserClientId": "browser-client"
	      }
	    }
	  },
	  "sources": {
	    "ticketsSource": {
	      "type": "openapi",
	      "uri": "https://example.com/openapi.yaml",
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
		AuditPath:         filepath.Join(dir, "audit.log"),
		DefaultConfigPath: configPath,
		InstanceID:        "team-a",
		RuntimeURL:        "http://127.0.0.1:18765",
	})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	req, err := http.NewRequest(http.MethodGet, httpServer.URL+"/v1/runtime/info", nil)
	if err != nil {
		t.Fatalf("new runtime info request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer brokered-token")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("runtime info request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 runtime info response, got %d with body %q", resp.StatusCode, readTrimmedBody(t, resp))
	}
	var info map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		t.Fatalf("decode runtime info: %v", err)
	}
	if got := info["instanceId"]; got != "team-a" {
		t.Fatalf("expected instance id team-a, got %#v", got)
	}
	if got := info["url"]; got != "http://127.0.0.1:18765" {
		t.Fatalf("expected runtime url in info response, got %#v", got)
	}
	auth, ok := info["auth"].(map[string]any)
	if !ok {
		t.Fatalf("expected auth metadata block, got %#v", info["auth"])
	}
	if got := auth["required"]; got != true {
		t.Fatalf("expected auth.required=true, got %#v", got)
	}
	if got := auth["audience"]; got != "oclird" {
		t.Fatalf("expected auth audience oclird, got %#v", got)
	}
	expectJSONStringSlice(t, auth["tokenValidationProfiles"], []string{"oauth2_introspection"}, "auth.tokenValidationProfiles")
	expectJSONStringSlice(t, auth["scopePrefixes"], []string{"bundle:", "profile:", "tool:"}, "auth.scopePrefixes")
	if got := auth["principal"]; got != "agent-456" {
		t.Fatalf("expected resolved auth principal agent-456, got %#v", got)
	}
	browserLogin, ok := auth["browserLogin"].(map[string]any)
	if !ok {
		t.Fatalf("expected browserLogin metadata, got %#v", auth["browserLogin"])
	}
	if got := browserLogin["configured"]; got != true {
		t.Fatalf("expected browserLogin.configured=true, got %#v", got)
	}
	if got := browserLogin["configEndpoint"]; got != "/v1/auth/browser-config" {
		t.Fatalf("expected browserLogin.configEndpoint=/v1/auth/browser-config, got %#v", got)
	}
	envelope, ok := auth["authorizationEnvelope"].(map[string]any)
	if !ok {
		t.Fatalf("expected auth.authorizationEnvelope metadata, got %#v", auth["authorizationEnvelope"])
	}
	if got := envelope["version"]; got != "1.0" {
		t.Fatalf("expected auth authorization envelope version 1.0, got %#v", got)
	}
	expectJSONStringSlice(t, envelope["scopePrefixes"], []string{"bundle:", "profile:", "tool:"}, "auth.authorizationEnvelope.scopePrefixes")
}

func TestServerStopEndpointInvokesShutdownHook(t *testing.T) {
	stopped := make(chan struct{})
	server := runtime.NewServer(runtime.Options{
		AuditPath: filepath.Join(t.TempDir(), "audit.log"),
		Shutdown: func() error {
			close(stopped)
			return nil
		},
	})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	resp, err := http.Post(httpServer.URL+"/v1/runtime/stop", "application/json", nil)
	if err != nil {
		t.Fatalf("runtime stop request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 runtime stop response, got %d", resp.StatusCode)
	}
	select {
	case <-stopped:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("expected shutdown hook to be invoked")
	}
}

func TestServerSessionCloseClearsOAuthCache(t *testing.T) {
	dir := t.TempDir()
	oauthDir := filepath.Join(dir, "oauth")
	if err := os.MkdirAll(oauthDir, 0o755); err != nil {
		t.Fatalf("mkdir oauth dir: %v", err)
	}
	tokenPath := filepath.Join(oauthDir, "cached.json")
	if err := os.WriteFile(tokenPath, []byte(`{"accessToken":"cached"}`), 0o600); err != nil {
		t.Fatalf("write cached token: %v", err)
	}

	server := runtime.NewServer(runtime.Options{
		AuditPath: filepath.Join(dir, "audit.log"),
		StateDir:  dir,
	})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	resp, err := http.Post(httpServer.URL+"/v1/runtime/session-close", "application/json", nil)
	if err != nil {
		t.Fatalf("session close request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 session close response, got %d", resp.StatusCode)
	}
	if _, err := os.Stat(tokenPath); !os.IsNotExist(err) {
		t.Fatalf("expected oauth cache to be cleared, stat err=%v", err)
	}
}

func TestServerReturnsRuntimeInfoHandshakeWithoutBearerTokenWhenRemoteAuthEnabled(t *testing.T) {
	dir := t.TempDir()
	configPath := writeRuntimeFile(t, dir, ".cli.json", `{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "runtime": {
	    "server": {
	      "auth": {
	        "validationProfile": "oauth2_introspection",
	        "audience": "oclird",
	        "introspectionURL": "https://auth.example.com/introspect"
	      }
	    }
	  },
	  "sources": {
	    "ticketsSource": {
	      "type": "openapi",
	      "uri": "https://example.com/openapi.yaml",
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
		AuditPath:         filepath.Join(dir, "audit.log"),
		DefaultConfigPath: configPath,
	})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	resp, err := http.Get(httpServer.URL + "/v1/runtime/info")
	if err != nil {
		t.Fatalf("runtime info request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 runtime info response without bearer token, got %d", resp.StatusCode)
	}
	var info map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		t.Fatalf("decode runtime info: %v", err)
	}
	auth, ok := info["auth"].(map[string]any)
	if !ok {
		t.Fatalf("expected auth metadata block, got %#v", info["auth"])
	}
	if got := auth["required"]; got != true {
		t.Fatalf("expected auth.required=true, got %#v", got)
	}
	expectJSONStringSlice(t, auth["tokenValidationProfiles"], []string{"oauth2_introspection"}, "auth.tokenValidationProfiles")
	if _, ok := auth["principal"]; ok {
		t.Fatalf("expected principal to be omitted without bearer token, got %#v", auth["principal"])
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

// TestServerLeaseExpiryShutdownEmitsAuditOutcome verifies that when a session
// lease expires and triggers server shutdown, a "lease_expiry_shutdown" audit
// event is written.  This covers review finding #1: missing audit outcome for
// the lease-expiry shutdown decision.
func TestServerLeaseExpiryShutdownEmitsAuditOutcome(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.log")
	shutdownCalled := make(chan struct{}, 1)
	server := runtime.NewServer(runtime.Options{
		AuditPath:            auditPath,
		StateDir:             filepath.Join(dir, "state"),
		HeartbeatSeconds:     1,
		MissedHeartbeatLimit: 1,
		ShutdownMode:         "when-owner-exits",
		Shutdown: func() error {
			select {
			case shutdownCalled <- struct{}{}:
			default:
			}
			return nil
		},
	})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	postRuntimeJSON(t, httpServer.URL+"/v1/runtime/heartbeat", map[string]any{"sessionId": "audit-sess"})
	expectSignal(t, shutdownCalled, 2*time.Second, "expected expired lease to trigger shutdown")

	events, err := audit.NewFileStore(auditPath).List()
	if err != nil {
		t.Fatalf("list audit events: %v", err)
	}

	var sawShutdownAudit bool
	for _, event := range events {
		if event.EventType == "lease_expiry_shutdown" {
			sawShutdownAudit = true
		}
	}
	if !sawShutdownAudit {
		t.Fatalf("expected lease_expiry_shutdown audit event, events were: %#v", events)
	}
}

// TestServerInflightTrackedForNonLeaseRequests verifies that the server-level
// in-flight counter increments while a non-lease request is being served and
// returns to zero after the response is sent.  This covers review finding #2:
// the previous test used a lease endpoint (excluded from tracking) so the
// counter was always zero.
func TestServerInflightTrackedForNonLeaseRequests(t *testing.T) {
	dir := t.TempDir()

	// Slow backend: blocks until signalled so we can observe inflight > 0.
	proceed := make(chan struct{})
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-proceed
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer backend.Close()

	writeRuntimeFile(t, dir, "slow.openapi.yaml", `
openapi: 3.1.0
info:
  title: Slow API
  version: "1.0.0"
servers:
  - url: `+backend.URL+`
paths:
  /ping:
    get:
      operationId: ping
      responses:
        "200":
          description: OK
`)
	configPath := writeRuntimeFile(t, dir, ".cli.json", `{
  "cli": "1.0.0",
  "mode": { "default": "discover" },
  "sources": {
    "slowSource": {
      "type": "openapi",
      "uri": "./slow.openapi.yaml",
      "enabled": true
    }
  },
  "services": {
    "slow": { "source": "slowSource", "alias": "slow" }
  }
}`)

	server := runtime.NewServer(runtime.Options{
		AuditPath:   filepath.Join(dir, "audit.log"),
		GracePeriod: 50 * time.Millisecond,
	})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	// Fire a tool execute request that blocks inside the slow backend.
	reqDone := make(chan struct{})
	go func() {
		defer close(reqDone)
		body := bytes.NewBufferString(`{"configPath":"` + configPath + `","toolId":"slow:ping"}`)
		resp, err := http.Post(httpServer.URL+"/v1/tools/execute", "application/json", body)
		if err == nil {
			_ = resp.Body.Close()
		}
	}()

	// Poll until inflight > 0 (allow up to 500 ms).
	var countDuring int64
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		countDuring = server.InflightCount()
		if countDuring > 0 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	close(proceed)
	<-reqDone

	// Allow brief settling time for the defer in inflightMiddleware to run.
	time.Sleep(10 * time.Millisecond)
	countAfter := server.InflightCount()

	if countDuring == 0 {
		t.Fatal("expected inflight > 0 while tool execute request was in-flight")
	}
	if countAfter != 0 {
		t.Fatalf("expected inflight=0 after request completed, got %d", countAfter)
	}
}
