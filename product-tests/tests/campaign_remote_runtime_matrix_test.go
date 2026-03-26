package tests_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/StevenBuglione/open-cli/internal/runtime"
	helpers "github.com/StevenBuglione/open-cli/product-tests/tests/helpers"
)

func TestCampaignRemoteRuntimeMatrix(t *testing.T) {
	broker := helpers.NewRuntimeAuthBroker(t)

	ticketsAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{map[string]any{"id": "T-1"}}, "total": 1})
	}))
	t.Cleanup(ticketsAPI.Close)

	usersAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{map[string]any{"id": "U-1"}}, "total": 1})
	}))
	t.Cleanup(usersAPI.Close)

	dir := t.TempDir()
	configPath := helpers.WriteRuntimeAuthBrokerConfig(t, dir, broker, ticketsAPI.URL, usersAPI.URL)
	server := runtime.NewServer(runtime.Options{
		AuditPath:         filepath.Join(dir, "audit.log"),
		DefaultConfigPath: configPath,
	})
	runtimeServer := httptest.NewServer(server.Handler())
	t.Cleanup(runtimeServer.Close)

	fr := helpers.NewFindingsRecorder("remote-runtime-matrix")
	fr.SetLaneMetadata("product-validation", "remote-runtime", "ci-containerized", "oauthClient")
	defer fr.MustEmitToTest(t)

	token := broker.AcquireClientCredentialsToken(t, "microsoft", "open-cli-toolbox", []string{
		"bundle:tickets",
		"tool:tickets:listTickets",
	})

	t.Run("authorized-catalog", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodGet, runtimeServer.URL+"/v1/catalog/effective?config="+url.QueryEscape(configPath), nil)
		if err != nil {
			t.Fatalf("new catalog request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("catalog request: %v", err)
		}
		defer resp.Body.Close()

		fr.Check("catalog-http-200", "authorized catalog request succeeds", "200", fmt.Sprintf("%d", resp.StatusCode), resp.StatusCode == http.StatusOK, "")

		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode catalog response: %v", err)
		}
		catalog, _ := body["catalog"].(map[string]any)
		tools, _ := catalog["tools"].([]any)
		fr.Check("catalog-authorized-tools", "catalog is filtered to one authorized tool", "1", fmt.Sprintf("%d", len(tools)), len(tools) == 1, "")
		if len(tools) == 1 {
			tool, _ := tools[0].(map[string]any)
			fr.Check("catalog-tool-id", "catalog exposes the expected authorized tool", "tickets:listTickets", fmt.Sprintf("%v", tool["id"]), tool["id"] == "tickets:listTickets", "")
		}
	})

	t.Run("authorized-execution", func(t *testing.T) {
		payload, _ := json.Marshal(map[string]any{"configPath": configPath, "toolId": "tickets:listTickets"})
		req, err := http.NewRequest(http.MethodPost, runtimeServer.URL+"/v1/tools/execute", strings.NewReader(string(payload)))
		if err != nil {
			t.Fatalf("new execute request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("execute authorized tool: %v", err)
		}
		defer resp.Body.Close()

		fr.Check("execute-authorized-tool", "authorized runtime tool executes successfully", "200", fmt.Sprintf("%d", resp.StatusCode), resp.StatusCode == http.StatusOK, "")
		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode authorized execute response: %v", err)
		}
		resultBody, _ := body["body"].(map[string]any)
		items, _ := resultBody["items"].([]any)
		fr.Check("execute-authorized-body-items", "authorized runtime execution returns the expected payload", "1", fmt.Sprintf("%d", len(items)), len(items) == 1, "")
	})

	t.Run("denied-execution", func(t *testing.T) {
		payload, _ := json.Marshal(map[string]any{"configPath": configPath, "toolId": "users:listUsers"})
		req, err := http.NewRequest(http.MethodPost, runtimeServer.URL+"/v1/tools/execute", strings.NewReader(string(payload)))
		if err != nil {
			t.Fatalf("new denied execute request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("execute denied tool: %v", err)
		}
		defer resp.Body.Close()

		fr.Check("execute-denied-tool", "unauthorized tool execution is rejected", "403", fmt.Sprintf("%d", resp.StatusCode), resp.StatusCode == http.StatusForbidden, "")
	})

	t.Run("browser-metadata", func(t *testing.T) {
		resp, err := http.Get(runtimeServer.URL + "/v1/auth/browser-config")
		if err != nil {
			t.Fatalf("get browser config: %v", err)
		}
		defer resp.Body.Close()

		fr.Check("browser-config-http-200", "browser login metadata is exposed", "200", fmt.Sprintf("%d", resp.StatusCode), resp.StatusCode == http.StatusOK, "")
		var browserConfig struct {
			AuthorizationURL string `json:"authorizationURL"`
			TokenURL         string `json:"tokenURL"`
			ClientID         string `json:"clientId"`
			Audience         string `json:"audience"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&browserConfig); err != nil {
			t.Fatalf("decode browser config: %v", err)
		}
		if err := fr.AddJSONArtifact("browser-config.json", map[string]any{
			"authorizationURL": browserConfig.AuthorizationURL,
			"tokenURL":         browserConfig.TokenURL,
			"clientId":         browserConfig.ClientID,
			"audience":         browserConfig.Audience,
		}); err != nil {
			t.Fatalf("record browser-config artifact: %v", err)
		}
		fr.Check("browser-config-client-id", "browser metadata exposes the configured client ID", broker.BrowserClientID, browserConfig.ClientID, browserConfig.ClientID == broker.BrowserClientID, "")
		fr.Check("browser-config-audience", "browser metadata exposes the runtime audience", "open-cli-toolbox", browserConfig.Audience, browserConfig.Audience == "open-cli-toolbox", "")
		fr.CheckBool("browser-config-authorization-url", "browser metadata exposes an authorization URL", browserConfig.AuthorizationURL != "", browserConfig.AuthorizationURL)
		fr.CheckBool("browser-config-token-url", "browser metadata exposes a token URL", browserConfig.TokenURL != "", browserConfig.TokenURL)
	})
}

func executeRuntimeRequest(t *testing.T, method, url string, token string, payload map[string]any) (*http.Response, string) {
	t.Helper()

	var bodyReader io.Reader
	if payload != nil {
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal request payload: %v", err)
		}
		bodyReader = strings.NewReader(string(body))
	}
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		t.Fatalf("new request %s %s: %v", method, url, err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request %s %s: %v", method, url, err)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		resp.Body.Close()
		t.Fatalf("read response body: %v", err)
	}
	resp.Body.Close()
	resp.Body = io.NopCloser(strings.NewReader(string(data)))
	return resp, strings.TrimSpace(string(data))
}
