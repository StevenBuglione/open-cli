package tests_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/StevenBuglione/open-cli/internal/runtime"
	brokerhelpers "github.com/StevenBuglione/open-cli/product-tests/tests/helpers"
	"github.com/golang-jwt/jwt/v5"
)

func TestCapabilityRuntimeAuthBroker(t *testing.T) {
	broker := brokerhelpers.NewRuntimeAuthBroker(t)

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
	configPath := brokerhelpers.WriteRuntimeAuthBrokerConfig(t, dir, broker, ticketsAPI.URL, usersAPI.URL)
	server := runtime.NewServer(runtime.Options{
		AuditPath:         filepath.Join(dir, "audit.log"),
		DefaultConfigPath: configPath,
	})
	runtimeServer := httptest.NewServer(server.Handler())
	t.Cleanup(runtimeServer.Close)

	for _, upstream := range []string{"microsoft", "google", "github"} {
		t.Run("client_credentials_"+upstream, func(t *testing.T) {
			token := broker.AcquireClientCredentialsToken(t, upstream, "open-cli-toolbox", []string{
				"bundle:tickets",
				"tool:tickets:listTickets",
			})

			catalogReq, err := http.NewRequest(http.MethodGet, runtimeServer.URL+"/v1/catalog/effective?config="+url.QueryEscape(configPath), nil)
			if err != nil {
				t.Fatalf("new catalog request: %v", err)
			}
			catalogReq.Header.Set("Authorization", "Bearer "+token)

			catalogResp, err := http.DefaultClient.Do(catalogReq)
			if err != nil {
				t.Fatalf("get effective catalog: %v", err)
			}
			defer catalogResp.Body.Close()
			if catalogResp.StatusCode != http.StatusOK {
				t.Fatalf("expected 200 effective catalog, got %d", catalogResp.StatusCode)
			}
			var catalogBody map[string]any
			if err := json.NewDecoder(catalogResp.Body).Decode(&catalogBody); err != nil {
				t.Fatalf("decode effective catalog: %v", err)
			}
			catalogPayload, _ := catalogBody["catalog"].(map[string]any)
			tools, _ := catalogPayload["tools"].([]any)
			if len(tools) != 1 {
				t.Fatalf("expected exactly one authorized tool for %s, got %#v", upstream, tools)
			}
			tool, _ := tools[0].(map[string]any)
			if got := tool["id"]; got != "tickets:listTickets" {
				t.Fatalf("expected tickets:listTickets for %s, got %#v", upstream, got)
			}

			executeReqBody := map[string]any{"configPath": configPath, "toolId": "tickets:listTickets"}
			body, _ := json.Marshal(executeReqBody)
			executeReq, err := http.NewRequest(http.MethodPost, runtimeServer.URL+"/v1/tools/execute", strings.NewReader(string(body)))
			if err != nil {
				t.Fatalf("new execute request: %v", err)
			}
			executeReq.Header.Set("Content-Type", "application/json")
			executeReq.Header.Set("Authorization", "Bearer "+token)
			executeResp, err := http.DefaultClient.Do(executeReq)
			if err != nil {
				t.Fatalf("execute tickets:listTickets: %v", err)
			}
			defer executeResp.Body.Close()
			if executeResp.StatusCode != http.StatusOK {
				t.Fatalf("expected 200 execute response for %s, got %d", upstream, executeResp.StatusCode)
			}

			body, _ = json.Marshal(map[string]any{"configPath": configPath, "toolId": "users:listUsers"})
			deniedReq, err := http.NewRequest(http.MethodPost, runtimeServer.URL+"/v1/tools/execute", strings.NewReader(string(body)))
			if err != nil {
				t.Fatalf("new denied execute request: %v", err)
			}
			deniedReq.Header.Set("Content-Type", "application/json")
			deniedReq.Header.Set("Authorization", "Bearer "+token)
			deniedResp, err := http.DefaultClient.Do(deniedReq)
			if err != nil {
				t.Fatalf("execute users:listUsers: %v", err)
			}
			defer deniedResp.Body.Close()
			if deniedResp.StatusCode != http.StatusForbidden {
				t.Fatalf("expected 403 for denied tool %s, got %d", upstream, deniedResp.StatusCode)
			}
		})
	}

	t.Run("browser_login_metadata_and_authorization_code", func(t *testing.T) {
		browserConfigResp, err := http.Get(runtimeServer.URL + "/v1/auth/browser-config")
		if err != nil {
			t.Fatalf("get browser config: %v", err)
		}
		defer browserConfigResp.Body.Close()
		if browserConfigResp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 browser config, got %d", browserConfigResp.StatusCode)
		}
		var browserConfig struct {
			AuthorizationURL string `json:"authorizationURL"`
			TokenURL         string `json:"tokenURL"`
			ClientID         string `json:"clientId"`
			Audience         string `json:"audience"`
		}
		if err := json.NewDecoder(browserConfigResp.Body).Decode(&browserConfig); err != nil {
			t.Fatalf("decode browser config: %v", err)
		}

		callbackListener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen callback: %v", err)
		}
		callbackPort := callbackListener.Addr().(*net.TCPAddr).Port
		_ = callbackListener.Close()
		redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", callbackPort)

		state := "broker-state"
		verifier := "broker-verifier"
		sum := sha256.Sum256([]byte(verifier))
		challenge := base64.RawURLEncoding.EncodeToString(sum[:])
		authorizeURL := fmt.Sprintf("%s?response_type=code&client_id=%s&redirect_uri=%s&scope=%s&audience=%s&state=%s&code_challenge_method=S256&code_challenge=%s&upstream=github",
			browserConfig.AuthorizationURL,
			url.QueryEscape(browserConfig.ClientID),
			url.QueryEscape(redirectURI),
			url.QueryEscape("bundle:tickets tool:tickets:listTickets"),
			url.QueryEscape(browserConfig.Audience),
			url.QueryEscape(state),
			url.QueryEscape(challenge),
		)
		redirectClient := &http.Client{
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
		authResp, err := redirectClient.Get(authorizeURL)
		if err != nil {
			t.Fatalf("authorize request: %v", err)
		}
		defer authResp.Body.Close()
		location := authResp.Header.Get("Location")
		if location == "" {
			t.Fatalf("expected authorize redirect location")
		}
		redirectURL, err := url.Parse(location)
		if err != nil {
			t.Fatalf("parse redirect location: %v", err)
		}
		code := redirectURL.Query().Get("code")
		if code == "" {
			t.Fatalf("expected authorization code in redirect URL")
		}

		token := broker.ExchangeAuthorizationCode(t, code, browserConfig.ClientID, redirectURI, verifier)

		catalogReq, err := http.NewRequest(http.MethodGet, runtimeServer.URL+"/v1/catalog/effective?config="+url.QueryEscape(configPath), nil)
		if err != nil {
			t.Fatalf("new browser catalog request: %v", err)
		}
		catalogReq.Header.Set("Authorization", "Bearer "+token)
		catalogResp, err := http.DefaultClient.Do(catalogReq)
		if err != nil {
			t.Fatalf("browser catalog request: %v", err)
		}
		defer catalogResp.Body.Close()
		if catalogResp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 browser-login catalog response, got %d", catalogResp.StatusCode)
		}
	})

	t.Run("delegated_token_exchange", func(t *testing.T) {
		parentToken := broker.AcquireClientCredentialsToken(t, "github", "open-cli-toolbox", []string{
			"bundle:tickets",
			"tool:tickets:listTickets",
		})
		childToken := broker.ExchangeDelegatedToken(t, parentToken, "open-cli-toolbox", []string{
			"tool:tickets:listTickets",
		}, "subagent:triage-01")

		catalogReq, err := http.NewRequest(http.MethodGet, runtimeServer.URL+"/v1/catalog/effective?config="+url.QueryEscape(configPath), nil)
		if err != nil {
			t.Fatalf("new delegated catalog request: %v", err)
		}
		catalogReq.Header.Set("Authorization", "Bearer "+childToken)

		catalogResp, err := http.DefaultClient.Do(catalogReq)
		if err != nil {
			t.Fatalf("delegated catalog request: %v", err)
		}
		defer catalogResp.Body.Close()
		if catalogResp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 delegated catalog response, got %d", catalogResp.StatusCode)
		}
		var catalogBody map[string]any
		if err := json.NewDecoder(catalogResp.Body).Decode(&catalogBody); err != nil {
			t.Fatalf("decode delegated catalog: %v", err)
		}
		catalogPayload, _ := catalogBody["catalog"].(map[string]any)
		tools, _ := catalogPayload["tools"].([]any)
		if len(tools) != 1 {
			t.Fatalf("expected one delegated tool, got %#v", tools)
		}
		tool, _ := tools[0].(map[string]any)
		if got := tool["id"]; got != "tickets:listTickets" {
			t.Fatalf("expected delegated access to tickets:listTickets, got %#v", got)
		}

		executeBody, _ := json.Marshal(map[string]any{"configPath": configPath, "toolId": "tickets:listTickets"})
		executeReq, err := http.NewRequest(http.MethodPost, runtimeServer.URL+"/v1/tools/execute", strings.NewReader(string(executeBody)))
		if err != nil {
			t.Fatalf("new delegated execute request: %v", err)
		}
		executeReq.Header.Set("Content-Type", "application/json")
		executeReq.Header.Set("Authorization", "Bearer "+childToken)
		executeResp, err := http.DefaultClient.Do(executeReq)
		if err != nil {
			t.Fatalf("execute delegated tickets:listTickets: %v", err)
		}
		defer executeResp.Body.Close()
		if executeResp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 delegated execute response, got %d", executeResp.StatusCode)
		}

		deniedBody, _ := json.Marshal(map[string]any{"configPath": configPath, "toolId": "users:listUsers"})
		deniedReq, err := http.NewRequest(http.MethodPost, runtimeServer.URL+"/v1/tools/execute", strings.NewReader(string(deniedBody)))
		if err != nil {
			t.Fatalf("new delegated denied execute request: %v", err)
		}
		deniedReq.Header.Set("Content-Type", "application/json")
		deniedReq.Header.Set("Authorization", "Bearer "+childToken)
		deniedResp, err := http.DefaultClient.Do(deniedReq)
		if err != nil {
			t.Fatalf("execute delegated users:listUsers: %v", err)
		}
		defer deniedResp.Body.Close()
		if deniedResp.StatusCode != http.StatusForbidden {
			t.Fatalf("expected 403 for delegated denied tool, got %d", deniedResp.StatusCode)
		}

		parser := jwt.NewParser()
		unverifiedClaims := jwt.MapClaims{}
		if _, _, err := parser.ParseUnverified(childToken, unverifiedClaims); err != nil {
			t.Fatalf("parse delegated token claims: %v", err)
		}
		if got := unverifiedClaims["delegated_by"]; got != "github:service-account" {
			t.Fatalf("expected delegated_by github:service-account, got %#v", got)
		}
		if got := unverifiedClaims["delegation_id"]; got == "" {
			t.Fatalf("expected delegation_id claim, got %#v", got)
		}
		act, _ := unverifiedClaims["act"].(map[string]any)
		if got := act["actor_id"]; got != "subagent:triage-01" {
			t.Fatalf("expected act.actor_id subagent:triage-01, got %#v", act)
		}
		exp, ok := unverifiedClaims["exp"].(float64)
		if !ok {
			t.Fatalf("expected numeric exp claim, got %#v", unverifiedClaims["exp"])
		}
		expiresAt := time.Unix(int64(exp), 0)
		if ttl := time.Until(expiresAt); ttl <= 0 || ttl > 5*time.Minute+15*time.Second {
			t.Fatalf("expected delegated token ttl <= %s and >0, got %s", 5*time.Minute+15*time.Second, ttl)
		}
	})

	t.Run("delegated_token_exchange_rejects_scope_escalation", func(t *testing.T) {
		parentToken := broker.AcquireClientCredentialsToken(t, "github", "open-cli-toolbox", []string{
			"bundle:tickets",
			"tool:tickets:listTickets",
		})

		form := url.Values{}
		form.Set("grant_type", "urn:ietf:params:oauth:grant-type:token-exchange")
		form.Set("subject_token", parentToken)
		form.Set("subject_token_type", "urn:ietf:params:oauth:token-type:access_token")
		form.Set("requested_token_type", "urn:ietf:params:oauth:token-type:access_token")
		form.Set("audience", "open-cli-toolbox")
		form.Set("scope", "tool:users:listUsers")

		resp, err := http.PostForm(broker.TokenURL, form)
		if err != nil {
			t.Fatalf("post delegated exchange: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400 delegated scope escalation response, got %d", resp.StatusCode)
		}
		var payload map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			t.Fatalf("decode delegated scope escalation response: %v", err)
		}
		if payload["error"] != "invalid_scope" {
			t.Fatalf("expected invalid_scope for delegated escalation, got %#v", payload)
		}
	})
}
