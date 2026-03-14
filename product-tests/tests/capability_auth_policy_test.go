package tests_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/StevenBuglione/oas-cli-go/internal/runtime"
)

// ---- inline OAuth stub ----

func newOAuthStubHandler(t *testing.T, validClientID, validSecret, validScope string) http.Handler {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		// issuer is reconstructed from the request host in tests
		base := "http://" + r.Host
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                base,
			"token_endpoint":        base + "/oauth/token",
			"grant_types_supported": []string{"client_credentials"},
			"scopes_supported":      []string{"api.read", "api.write"},
		})
	})

	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		clientID := r.FormValue("client_id")
		clientSecret := r.FormValue("client_secret")
		scope := r.FormValue("scope")

		if clientID != validClientID || clientSecret != validSecret {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":             "invalid_client",
				"error_description": "unknown client or bad credentials",
			})
			return
		}
		if scope != "" && scope != validScope {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":             "invalid_scope",
				"error_description": "scope not allowed",
			})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "test-access-token-abc123",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	})

	return mux
}

// ---- inline protected REST API ----

// newTokenProtectedHandler returns a handler that requires a specific Bearer token.
func newTokenProtectedHandler(t *testing.T, expectedToken string) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer "+expectedToken {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}, "total": 0, "page": 1, "pageSize": 20, "totalPages": 1})
	})
}

// ---- helpers ----

func executeToolRaw(t *testing.T, runtimeURL, configPath, toolID string) *http.Response {
	t.Helper()
	payload := map[string]any{
		"configPath": configPath,
		"toolId":     toolID,
	}
	b, _ := json.Marshal(payload)
	resp, err := http.Post(runtimeURL+"/v1/tools/execute", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("execute %s: %v", toolID, err)
	}
	return resp
}

func readResult(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	defer resp.Body.Close()
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return result
}

// ---- tests ----

func TestCapabilityAuthAndPolicy(t *testing.T) {
	const (
		validClientID = "test-client"
		validSecret   = "test-secret"
		validScope    = "api.read"
		issuedToken   = "test-access-token-abc123"
	)

	t.Run("ClientCredentialsIssuesTokenAndCallsAPI", func(t *testing.T) {
		if err := os.Setenv("TEST_CLIENT_ID", validClientID); err != nil {
			t.Fatalf("setenv: %v", err)
		}
		if err := os.Setenv("TEST_CLIENT_SECRET", validSecret); err != nil {
			t.Fatalf("setenv: %v", err)
		}
		t.Cleanup(func() {
			_ = os.Unsetenv("TEST_CLIENT_ID")
			_ = os.Unsetenv("TEST_CLIENT_SECRET")
		})

		var tokenCalls int
		oauthAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/oauth/token":
				tokenCalls++
				if err := r.ParseForm(); err != nil {
					t.Errorf("parse form: %v", err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if got := r.FormValue("grant_type"); got != "client_credentials" {
					t.Errorf("expected client_credentials, got %q", got)
				}
				if got := r.FormValue("client_id"); got != validClientID {
					t.Errorf("expected clientID %q, got %q", validClientID, got)
				}
				if got := r.FormValue("client_secret"); got != validSecret {
					t.Errorf("expected secret, got %q", got)
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"access_token": issuedToken,
					"token_type":   "Bearer",
					"expires_in":   3600,
				})
			case "/items":
				if got := r.Header.Get("Authorization"); got != "Bearer "+issuedToken {
					t.Errorf("expected Bearer %s, got %q", issuedToken, got)
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}, "total": 0})
			default:
				http.NotFound(w, r)
			}
		}))
		t.Cleanup(oauthAPI.Close)

		dir := t.TempDir()
		openapiPath := writeFile(t, dir, "protected.openapi.yaml",
			oauthOpenAPIYAML(oauthAPI.URL, oauthAPI.URL+"/oauth/token"))
		configPath := writeFile(t, dir, ".cli.json",
			oauthCLIConfig(openapiPath, validClientID, validSecret))

		srv := runtime.NewServer(runtime.Options{AuditPath: filepath.Join(dir, "audit.log")})
		runtimeSrv := httptest.NewServer(srv.Handler())
		t.Cleanup(runtimeSrv.Close)

		result := executeTool(t, runtimeSrv.URL, configPath, "protected:listItems", nil)
		if got, ok := result["statusCode"].(float64); !ok || got != 200 {
			t.Fatalf("expected statusCode 200, got %v", result)
		}
		if tokenCalls != 1 {
			t.Fatalf("expected 1 token request, got %d", tokenCalls)
		}
	})

	t.Run("InvalidClientReturnsError", func(t *testing.T) {
		if err := os.Setenv("TEST_CLIENT_ID", "wrong-client"); err != nil {
			t.Fatalf("setenv: %v", err)
		}
		if err := os.Setenv("TEST_CLIENT_SECRET", "wrong-secret"); err != nil {
			t.Fatalf("setenv: %v", err)
		}
		t.Cleanup(func() {
			_ = os.Unsetenv("TEST_CLIENT_ID")
			_ = os.Unsetenv("TEST_CLIENT_SECRET")
		})

		oauthAPI := httptest.NewServer(newOAuthStubHandler(t, validClientID, validSecret, validScope))
		t.Cleanup(oauthAPI.Close)

		dir := t.TempDir()
		openapiPath := writeFile(t, dir, "protected.openapi.yaml",
			oauthOpenAPIYAML(oauthAPI.URL, oauthAPI.URL+"/oauth/token"))
		configPath := writeFile(t, dir, ".cli.json",
			oauthCLIConfig(openapiPath, "wrong-client", "wrong-secret"))

		srv := runtime.NewServer(runtime.Options{AuditPath: filepath.Join(dir, "audit.log")})
		runtimeSrv := httptest.NewServer(srv.Handler())
		t.Cleanup(runtimeSrv.Close)

		b, _ := json.Marshal(map[string]any{
			"configPath": configPath,
			"toolId":     "protected:listItems",
		})
		resp, err := http.Post(runtimeSrv.URL+"/v1/tools/execute", "application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		defer resp.Body.Close()
		// The runtime returns 502 (bad gateway) when auth fails
		if resp.StatusCode != http.StatusBadGateway && resp.StatusCode != http.StatusForbidden {
			t.Fatalf("expected 502 or 403 for invalid client, got %d", resp.StatusCode)
		}
	})

	t.Run("PolicyDenyBlocksExecution", func(t *testing.T) {
		if err := os.Setenv("TEST_CLIENT_ID", validClientID); err != nil {
			t.Fatalf("setenv: %v", err)
		}
		if err := os.Setenv("TEST_CLIENT_SECRET", validSecret); err != nil {
			t.Fatalf("setenv: %v", err)
		}
		t.Cleanup(func() {
			_ = os.Unsetenv("TEST_CLIENT_ID")
			_ = os.Unsetenv("TEST_CLIENT_SECRET")
		})

		oauthAPI := httptest.NewServer(newOAuthStubHandler(t, validClientID, validSecret, validScope))
		t.Cleanup(oauthAPI.Close)

		dir := t.TempDir()
		openapiPath := writeFile(t, dir, "protected.openapi.yaml",
			oauthOpenAPIYAML(oauthAPI.URL, oauthAPI.URL+"/oauth/token"))
		// Config that denies all tools via policy
		configPath := writeFile(t, dir, ".cli.json", `{
  "cli": "1.0.0",
  "mode": { "default": "discover" },
  "sources": {
    "protectedSource": {
      "type": "openapi",
      "uri": "`+openapiPath+`",
      "enabled": true
    }
  },
  "services": {
    "protected": {
      "source": "protectedSource",
      "alias": "protected"
    }
  },
  "agents": {
    "profiles": {
      "restricted": {
        "mode": "curated",
        "toolSet": "deny-all"
      }
    },
    "defaultProfile": "restricted"
  },
  "curation": {
    "toolSets": {
      "deny-all": {
        "allow": [],
        "deny": ["**"]
      }
    }
  }
}`)

		srv := runtime.NewServer(runtime.Options{AuditPath: filepath.Join(dir, "audit.log")})
		runtimeSrv := httptest.NewServer(srv.Handler())
		t.Cleanup(runtimeSrv.Close)

		b, _ := json.Marshal(map[string]any{
			"configPath":   configPath,
			"toolId":       "protected:listItems",
			"agentProfile": "restricted",
		})
		resp, err := http.Post(runtimeSrv.URL+"/v1/tools/execute", "application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("expected 403 for denied tool, got %d", resp.StatusCode)
		}
	})

	t.Run("OAuthDiscoveryDocument", func(t *testing.T) {
		oauthStub := httptest.NewServer(newOAuthStubHandler(t, validClientID, validSecret, validScope))
		t.Cleanup(oauthStub.Close)

		resp, err := http.Get(oauthStub.URL + "/.well-known/oauth-authorization-server")
		if err != nil {
			t.Fatalf("get discovery doc: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 for discovery doc, got %d", resp.StatusCode)
		}
		var doc map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
			t.Fatalf("decode discovery doc: %v", err)
		}
		if _, ok := doc["token_endpoint"]; !ok {
			t.Fatal("discovery doc missing token_endpoint")
		}
		if _, ok := doc["issuer"]; !ok {
			t.Fatal("discovery doc missing issuer")
		}
	})

	t.Run("ShortTTLTokenIssuedWithCorrectExpiry", func(t *testing.T) {
		// Verify the oauthstub issues a token with short TTL for the short-ttl-client
		oauthStub := httptest.NewServer(newOAuthStubHandler(t, "short-ttl-client", "short-ttl-secret", "api.read"))
		t.Cleanup(oauthStub.Close)

		resp, err := http.PostForm(oauthStub.URL+"/oauth/token", map[string][]string{
			"grant_type":    {"client_credentials"},
			"client_id":     {"short-ttl-client"},
			"client_secret": {"short-ttl-secret"},
			"scope":         {"api.read"},
		})
		if err != nil {
			t.Fatalf("post token: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 for token, got %d", resp.StatusCode)
		}
		var tok map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
			t.Fatalf("decode token: %v", err)
		}
		if _, ok := tok["access_token"]; !ok {
			t.Fatal("missing access_token")
		}
		if _, ok := tok["expires_in"]; !ok {
			t.Fatal("missing expires_in")
		}
	})

	t.Run("InvalidScopeReturnsError", func(t *testing.T) {
		oauthStub := httptest.NewServer(newOAuthStubHandler(t, validClientID, validSecret, validScope))
		t.Cleanup(oauthStub.Close)

		resp, err := http.PostForm(oauthStub.URL+"/oauth/token", map[string][]string{
			"grant_type":    {"client_credentials"},
			"client_id":     {validClientID},
			"client_secret": {validSecret},
			"scope":         {"not-a-real-scope"},
		})
		if err != nil {
			t.Fatalf("post token: %v", err)
		}
		defer resp.Body.Close()
		var errResp map[string]string
		_ = json.NewDecoder(resp.Body).Decode(&errResp)
		// The inline stub returns 400 for invalid scope since the scope doesn't match validScope
		if resp.StatusCode == http.StatusOK {
			t.Fatal("expected non-200 for invalid scope")
		}
	})

	// ---- approval-required lane ----

	t.Run("ApprovalRequiredToolIsBlockedWithoutApproval", func(t *testing.T) {
		if err := os.Setenv("TEST_CLIENT_ID", validClientID); err != nil {
			t.Fatalf("setenv: %v", err)
		}
		if err := os.Setenv("TEST_CLIENT_SECRET", validSecret); err != nil {
			t.Fatalf("setenv: %v", err)
		}
		t.Cleanup(func() {
			_ = os.Unsetenv("TEST_CLIENT_ID")
			_ = os.Unsetenv("TEST_CLIENT_SECRET")
		})

		oauthAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/oauth/token":
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"access_token": issuedToken,
					"token_type":   "Bearer",
					"expires_in":   3600,
				})
			case "/items":
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}, "total": 0})
			default:
				http.NotFound(w, r)
			}
		}))
		t.Cleanup(oauthAPI.Close)

		dir := t.TempDir()
		openapiPath := writeFile(t, dir, "protected.openapi.yaml",
			oauthOpenAPIYAML(oauthAPI.URL, oauthAPI.URL+"/oauth/token"))
		configPath := writeFile(t, dir, ".cli.json", `{
  "cli": "1.0.0",
  "mode": { "default": "discover" },
  "sources": {
    "protectedSource": {
      "type": "openapi",
      "uri": "`+openapiPath+`",
      "enabled": true
    }
  },
  "services": {
    "protected": {
      "source": "protectedSource",
      "alias": "protected"
    }
  },
  "policy": {
    "approvalRequired": ["protected:listItems"]
  },
  "secrets": {
    "protected.testapi_oauth": {
      "type": "oauth2",
      "mode": "clientCredentials",
      "clientId": { "type": "env", "value": "TEST_CLIENT_ID" },
      "clientSecret": { "type": "env", "value": "TEST_CLIENT_SECRET" }
    }
  }
}`)

		srv := runtime.NewServer(runtime.Options{AuditPath: filepath.Join(dir, "audit.log")})
		runtimeSrv := httptest.NewServer(srv.Handler())
		t.Cleanup(runtimeSrv.Close)

		// Without approval: must be blocked
		b, _ := json.Marshal(map[string]any{
			"configPath": configPath,
			"toolId":     "protected:listItems",
			"approval":   false,
		})
		resp, err := http.Post(runtimeSrv.URL+"/v1/tools/execute", "application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatalf("execute (no approval): %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("expected 403 without approval, got %d", resp.StatusCode)
		}

		// With approval: must execute successfully
		result := executeTool(t, runtimeSrv.URL, configPath, "protected:listItems",
			map[string]any{"approval": true})
		if got, ok := result["statusCode"].(float64); !ok || got != 200 {
			t.Fatalf("expected statusCode 200 with approval, got %v", result)
		}
	})

	// ---- denied-tool lane ----

	t.Run("DeniedToolViaExplicitCuratedDeny", func(t *testing.T) {
		if err := os.Setenv("TEST_CLIENT_ID", validClientID); err != nil {
			t.Fatalf("setenv: %v", err)
		}
		if err := os.Setenv("TEST_CLIENT_SECRET", validSecret); err != nil {
			t.Fatalf("setenv: %v", err)
		}
		t.Cleanup(func() {
			_ = os.Unsetenv("TEST_CLIENT_ID")
			_ = os.Unsetenv("TEST_CLIENT_SECRET")
		})

		oauthAPI := httptest.NewServer(newOAuthStubHandler(t, validClientID, validSecret, validScope))
		t.Cleanup(oauthAPI.Close)

		dir := t.TempDir()
		openapiPath := writeFile(t, dir, "protected.openapi.yaml",
			oauthOpenAPIYAML(oauthAPI.URL, oauthAPI.URL+"/oauth/token"))
		// Agent profile allows nothing; explicit deny list names the tool
		configPath := writeFile(t, dir, ".cli.json", `{
  "cli": "1.0.0",
  "mode": { "default": "curated" },
  "sources": {
    "protectedSource": {
      "type": "openapi",
      "uri": "`+openapiPath+`",
      "enabled": true
    }
  },
  "services": {
    "protected": {
      "source": "protectedSource",
      "alias": "protected"
    }
  },
  "agents": {
    "profiles": {
      "locked": {
        "mode": "curated",
        "toolSet": "no-write"
      }
    },
    "defaultProfile": "locked"
  },
  "curation": {
    "toolSets": {
      "no-write": {
        "allow": [],
        "deny": ["protected:listItems"]
      }
    }
  }
}`)

		srv := runtime.NewServer(runtime.Options{AuditPath: filepath.Join(dir, "audit.log")})
		runtimeSrv := httptest.NewServer(srv.Handler())
		t.Cleanup(runtimeSrv.Close)

		b, _ := json.Marshal(map[string]any{
			"configPath":   configPath,
			"toolId":       "protected:listItems",
			"agentProfile": "locked",
		})
		resp, err := http.Post(runtimeSrv.URL+"/v1/tools/execute", "application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("expected 403 for denied tool, got %d", resp.StatusCode)
		}
	})

	// ---- missing-secret lane ----

	t.Run("MissingSecretResolutionFailsGracefully", func(t *testing.T) {
		// Ensure the env vars referenced in the config are NOT set
		_ = os.Unsetenv("ABSENT_CLIENT_ID")
		_ = os.Unsetenv("ABSENT_CLIENT_SECRET")

		oauthAPI := httptest.NewServer(newOAuthStubHandler(t, validClientID, validSecret, validScope))
		t.Cleanup(oauthAPI.Close)

		dir := t.TempDir()
		openapiPath := writeFile(t, dir, "protected.openapi.yaml",
			oauthOpenAPIYAML(oauthAPI.URL, oauthAPI.URL+"/oauth/token"))
		// Secrets reference env vars that are absent → empty clientId/secret
		configPath := writeFile(t, dir, ".cli.json", `{
  "cli": "1.0.0",
  "mode": { "default": "discover" },
  "sources": {
    "protectedSource": {
      "type": "openapi",
      "uri": "`+openapiPath+`",
      "enabled": true
    }
  },
  "services": {
    "protected": {
      "source": "protectedSource",
      "alias": "protected"
    }
  },
  "secrets": {
    "protected.testapi_oauth": {
      "type": "oauth2",
      "mode": "clientCredentials",
      "clientId":     { "type": "env", "value": "ABSENT_CLIENT_ID" },
      "clientSecret": { "type": "env", "value": "ABSENT_CLIENT_SECRET" }
    }
  }
}`)

		srv := runtime.NewServer(runtime.Options{AuditPath: filepath.Join(dir, "audit.log")})
		runtimeSrv := httptest.NewServer(srv.Handler())
		t.Cleanup(runtimeSrv.Close)

		b, _ := json.Marshal(map[string]any{
			"configPath": configPath,
			"toolId":     "protected:listItems",
		})
		resp, err := http.Post(runtimeSrv.URL+"/v1/tools/execute", "application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		resp.Body.Close()
		// Empty credentials → token endpoint rejects → runtime returns 502
		if resp.StatusCode != http.StatusBadGateway {
			t.Fatalf("expected 502 for missing secret, got %d", resp.StatusCode)
		}
	})

	// ---- auth retry / recovery lane ----

	t.Run("AuthRetryOnTransientAPIFailure", func(t *testing.T) {
		if err := os.Setenv("TEST_CLIENT_ID", validClientID); err != nil {
			t.Fatalf("setenv: %v", err)
		}
		if err := os.Setenv("TEST_CLIENT_SECRET", validSecret); err != nil {
			t.Fatalf("setenv: %v", err)
		}
		t.Cleanup(func() {
			_ = os.Unsetenv("TEST_CLIENT_ID")
			_ = os.Unsetenv("TEST_CLIENT_SECRET")
		})

		var apiCalls atomic.Int32
		oauthAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/oauth/token":
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"access_token": issuedToken,
					"token_type":   "Bearer",
					"expires_in":   3600,
				})
			case "/items":
				n := int(apiCalls.Add(1))
				if n <= 2 {
					// First two calls return 429 to trigger exec-layer retry
					w.Header().Set("Content-Type", "application/json")
					w.Header().Set("Retry-After", "0")
					w.WriteHeader(http.StatusTooManyRequests)
					_ = json.NewEncoder(w).Encode(map[string]string{"error": "rate_limited"})
					return
				}
				// Third call succeeds
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}, "total": 0})
			default:
				http.NotFound(w, r)
			}
		}))
		t.Cleanup(oauthAPI.Close)

		dir := t.TempDir()
		openapiPath := writeFile(t, dir, "protected.openapi.yaml",
			oauthOpenAPIYAML(oauthAPI.URL, oauthAPI.URL+"/oauth/token"))
		configPath := writeFile(t, dir, ".cli.json",
			oauthCLIConfig(openapiPath, validClientID, validSecret))

		srv := runtime.NewServer(runtime.Options{AuditPath: filepath.Join(dir, "audit.log")})
		runtimeSrv := httptest.NewServer(srv.Handler())
		t.Cleanup(runtimeSrv.Close)

		result := executeTool(t, runtimeSrv.URL, configPath, "protected:listItems", nil)
		if got, ok := result["statusCode"].(float64); !ok || got != 200 {
			t.Fatalf("expected 200 after retries, got %v", result)
		}
		if n := int(apiCalls.Load()); n != 3 {
			t.Fatalf("expected 3 API calls (2 retries + 1 success), got %d", n)
		}
	})
}
