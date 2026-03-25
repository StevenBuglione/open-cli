package auth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/StevenBuglione/open-cli/cmd/ocli/internal/auth"
	configpkg "github.com/StevenBuglione/open-cli/pkg/config"
)

func TestValidateBrowserLoginMetadataMissingFields(t *testing.T) {
	tests := []struct {
		name     string
		metadata auth.BrowserLoginMetadata
		wantErr  string
	}{
		{
			"missing authorizationURL",
			auth.BrowserLoginMetadata{TokenURL: "http://t", ClientID: "c"},
			"authorizationURL",
		},
		{
			"missing tokenURL",
			auth.BrowserLoginMetadata{AuthorizationURL: "http://a", ClientID: "c"},
			"tokenURL",
		},
		{
			"missing clientId",
			auth.BrowserLoginMetadata{AuthorizationURL: "http://a", TokenURL: "http://t"},
			"clientId",
		},
		{
			"all fields present",
			auth.BrowserLoginMetadata{AuthorizationURL: "http://a", TokenURL: "http://t", ClientID: "c"},
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := auth.ValidateBrowserLoginMetadata(tt.metadata)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !containsStr(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
			}
		})
	}
}

func TestResolveEndpointURL(t *testing.T) {
	tests := []struct {
		name     string
		base     string
		endpoint string
		want     string
		wantErr  bool
	}{
		{"empty base", "", "/path", "", true},
		{"relative endpoint", "http://host", "/v1/auth", "http://host/v1/auth", false},
		{"absolute endpoint", "http://host", "http://other/path", "http://other/path", false},
		{"no leading slash", "http://host", "v1/auth", "http://host/v1/auth", false},
		{"trailing slash on base", "http://host/", "/v1/auth", "http://host/v1/auth", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := auth.ResolveEndpointURL(tt.base, tt.endpoint)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFetchBrowserLoginMetadataSuccess(t *testing.T) {
	t.Helper()
	meta := auth.BrowserLoginMetadata{
		AuthorizationURL: "http://auth/authorize",
		TokenURL:         "http://auth/token",
		ClientID:         "client-1",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(meta)
	}))
	defer srv.Close()

	got, err := auth.FetchBrowserLoginMetadata(srv.URL, "/config", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ClientID != meta.ClientID {
		t.Errorf("clientId mismatch: got %q, want %q", got.ClientID, meta.ClientID)
	}
}

func TestFetchBrowserLoginMetadataServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not configured", http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := auth.FetchBrowserLoginMetadata(srv.URL, "/config", "")
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}

func TestResolveOAuthSecretNilReturnsError(t *testing.T) {
	_, err := auth.ResolveOAuthSecret(nil)
	if err == nil {
		t.Fatal("expected error for nil secret ref")
	}
}

func TestResolveOAuthSecretLiteral(t *testing.T) {
	ref := &configpkg.SecretRef{Type: "literal", Value: "my-secret"}
	got, err := auth.ResolveOAuthSecret(ref)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "my-secret" {
		t.Errorf("got %q, want %q", got, "my-secret")
	}
}

func TestResolveTokenProvidedTokenFromEnv(t *testing.T) {
	t.Setenv("TEST_RUNTIME_TOKEN", "tok-abc")
	opts := auth.TokenResolveOptions{}
	oauth := configpkg.RemoteOAuthConfig{Mode: "providedToken", TokenRef: "env:TEST_RUNTIME_TOKEN"}
	token, session, err := auth.ResolveToken(opts, oauth)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "tok-abc" {
		t.Errorf("got %q, want %q", token, "tok-abc")
	}
	if session == nil {
		t.Fatal("expected non-nil session")
	}
}

func TestResolveTokenUnsupportedModeReturnsError(t *testing.T) {
	opts := auth.TokenResolveOptions{}
	oauth := configpkg.RemoteOAuthConfig{Mode: "unsupported"}
	_, _, err := auth.ResolveToken(opts, oauth)
	if err == nil {
		t.Fatal("expected error for unsupported mode")
	}
}

func TestResolveOAuthClientTokenSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok-xyz","expires_in":3600}`))
	}))
	defer srv.Close()

	oauth := configpkg.RemoteOAuthConfig{
		Mode: "oauthClient",
		Client: &configpkg.RemoteOAuthClientConfig{
			TokenURL:     srv.URL + "/token",
			ClientID:     &configpkg.SecretRef{Type: "literal", Value: "cid"},
			ClientSecret: &configpkg.SecretRef{Type: "literal", Value: "csec"},
		},
	}
	token, err := auth.ResolveOAuthClientToken(t.Context(), oauth)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token.AccessToken != "tok-xyz" {
		t.Errorf("got %q, want %q", token.AccessToken, "tok-xyz")
	}
}

func TestResolveOAuthClientTokenServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	oauth := configpkg.RemoteOAuthConfig{
		Mode: "oauthClient",
		Client: &configpkg.RemoteOAuthClientConfig{
			TokenURL:     srv.URL + "/token",
			ClientID:     &configpkg.SecretRef{Type: "literal", Value: "cid"},
			ClientSecret: &configpkg.SecretRef{Type: "literal", Value: "csec"},
		},
	}
	_, err := auth.ResolveOAuthClientToken(t.Context(), oauth)
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
}

func TestResolveDelegatedTokenSuccess(t *testing.T) {
	var captured url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		captured = r.Form
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"child-token","expires_in":3600}`))
	}))
	defer srv.Close()

	token, err := auth.ResolveDelegatedToken(t.Context(), auth.DelegatedTokenRequest{
		TokenExchangeURL: srv.URL,
		ParentToken:      "parent-token",
		Audience:         "oclird",
		Scopes:           []string{"bundle:tickets", "tool:tickets:listTickets"},
		ActorID:          "subagent:triage-01",
		AgentProfile:     "triage",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token.AccessToken != "child-token" {
		t.Fatalf("expected child-token, got %q", token.AccessToken)
	}
	if got := captured.Get("grant_type"); got != "urn:ietf:params:oauth:grant-type:token-exchange" {
		t.Fatalf("expected token-exchange grant, got %q", got)
	}
	if got := captured.Get("subject_token"); got != "parent-token" {
		t.Fatalf("expected parent token subject_token, got %q", got)
	}
	if got := captured.Get("actor_id"); got != "subagent:triage-01" {
		t.Fatalf("expected actor_id to round-trip, got %q", got)
	}
	if got := captured.Get("agent_profile"); got != "triage" {
		t.Fatalf("expected agent_profile to round-trip, got %q", got)
	}
	if got := captured.Get("scope"); got != "bundle:tickets tool:tickets:listTickets" {
		t.Fatalf("expected delegated scopes, got %q", got)
	}
}

func TestResolveTokenDelegatesProvidedTokenAndRefreshes(t *testing.T) {
	t.Setenv("TEST_RUNTIME_PARENT_TOKEN", "parent-token-1")

	requests := 0
	var captured []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		requests++
		captured = append(captured, r.Form)
		expiresIn := 1
		token := "child-token-1"
		if requests > 1 {
			expiresIn = 3600
			token = "child-token-2"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": token,
			"expires_in":   expiresIn,
		})
	}))
	defer srv.Close()

	token, session, err := auth.ResolveToken(auth.TokenResolveOptions{
		AgentProfile: "triage",
		ActorID:      "subagent:triage-01",
	}, configpkg.RemoteOAuthConfig{
		Mode:     "providedToken",
		TokenRef: "env:TEST_RUNTIME_PARENT_TOKEN",
		Audience: "oclird",
		Scopes:   []string{"bundle:tickets"},
		Delegation: &configpkg.RemoteOAuthDelegation{
			Enabled:          true,
			TokenExchangeURL: srv.URL,
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "child-token-1" {
		t.Fatalf("expected delegated child token, got %q", token)
	}
	if session == nil {
		t.Fatal("expected non-nil session")
	}

	t.Setenv("TEST_RUNTIME_PARENT_TOKEN", "parent-token-2")
	time.Sleep(1100 * time.Millisecond)

	refreshed, err := session.TokenForPreflight(t.Context(), 0)
	if err != nil {
		t.Fatalf("TokenForPreflight: %v", err)
	}
	if refreshed != "child-token-2" {
		t.Fatalf("expected refreshed delegated child token, got %q", refreshed)
	}
	if requests != 2 {
		t.Fatalf("expected two delegated exchange requests, got %d", requests)
	}
	if got := captured[0].Get("subject_token"); got != "parent-token-1" {
		t.Fatalf("expected initial parent token, got %q", got)
	}
	if got := captured[1].Get("subject_token"); got != "parent-token-2" {
		t.Fatalf("expected refreshed parent token, got %q", got)
	}
	if got := captured[0].Get("actor_id"); got != "subagent:triage-01" {
		t.Fatalf("expected actor_id on delegated request, got %q", got)
	}
	if got := captured[0].Get("agent_profile"); got != "triage" {
		t.Fatalf("expected agent_profile on delegated request, got %q", got)
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
