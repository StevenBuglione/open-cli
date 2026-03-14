package auth

import (
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
	"strings"
	"testing"
	"time"

	"github.com/StevenBuglione/oas-cli-go/pkg/catalog"
	"github.com/StevenBuglione/oas-cli-go/pkg/config"
)

func TestResolveOAuthAccessTokenAuthorizationCodeUsesLoopbackAndPKCE(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for callback port: %v", err)
	}
	callbackPort := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", callbackPort)

	if err := os.Setenv("AUTH_CODE_CLIENT_ID", "browser-client"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	t.Cleanup(func() { _ = os.Unsetenv("AUTH_CODE_CLIENT_ID") })

	var authServer *httptest.Server
	var codeChallenge string
	authServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/authorize":
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
			if got := query.Get("scope"); got != "pets.read" {
				t.Fatalf("expected scope pets.read, got %q", got)
			}
			if got := query.Get("code_challenge_method"); got != "S256" {
				t.Fatalf("expected S256 challenge method, got %q", got)
			}
			codeChallenge = query.Get("code_challenge")
			if codeChallenge == "" {
				t.Fatalf("expected PKCE code challenge")
			}
			http.Redirect(w, r, redirectURI+"?code=auth-code-123&state="+url.QueryEscape(query.Get("state")), http.StatusFound)
		case "/token":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse token form: %v", err)
			}
			if got := r.Form.Get("grant_type"); got != "authorization_code" {
				t.Fatalf("expected authorization_code grant, got %q", got)
			}
			if got := r.Form.Get("code"); got != "auth-code-123" {
				t.Fatalf("expected auth code, got %q", got)
			}
			if got := r.Form.Get("redirect_uri"); got != redirectURI {
				t.Fatalf("expected redirect URI in token request, got %q", got)
			}
			verifier := r.Form.Get("code_verifier")
			if verifier == "" {
				t.Fatalf("expected PKCE verifier")
			}
			sum := sha256.Sum256([]byte(verifier))
			challenge := base64.RawURLEncoding.EncodeToString(sum[:])
			if challenge != codeChallenge {
				t.Fatalf("expected verifier to match challenge, got %q want %q", challenge, codeChallenge)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "auth-code-token-123",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer authServer.Close()

	previousOpenBrowser := openBrowser
	openBrowser = func(rawURL string) error {
		resp, err := http.Get(rawURL)
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		return err
	}
	defer func() { openBrowser = previousOpenBrowser }()

	secret := config.Secret{
		Type: "oauth2",
		OAuthConfig: config.OAuthConfig{
			Mode: "authorizationCode",
			ClientID: &config.SecretRef{
				Type:  "env",
				Value: "AUTH_CODE_CLIENT_ID",
			},
			RedirectURI: redirectURI,
		},
	}
	requirement := catalog.AuthRequirement{
		Type:   "oauth2",
		Scopes: []string{"pets.read"},
		OAuthFlows: []catalog.OAuthFlow{{
			Mode:             "authorizationCode",
			AuthorizationURL: authServer.URL + "/authorize",
			TokenURL:         authServer.URL + "/token",
		}},
	}

	token, err := ResolveOAuthAccessToken(context.Background(), authServer.Client(), config.PolicyConfig{}, secret, requirement, "pets.oauth", t.TempDir(), nil)
	if err != nil {
		t.Fatalf("ResolveOAuthAccessToken returned error: %v", err)
	}
	if token != "auth-code-token-123" {
		t.Fatalf("expected auth code token, got %q", token)
	}
}

func TestResolveOAuthAccessTokenAuthorizationCodeIgnoresDuplicateCallbacks(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for callback port: %v", err)
	}
	callbackPort := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", callbackPort)

	if err := os.Setenv("AUTH_CODE_DUP_CLIENT_ID", "browser-client"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	t.Cleanup(func() { _ = os.Unsetenv("AUTH_CODE_DUP_CLIENT_ID") })

	duplicateErr := make(chan error, 1)
	var authServer *httptest.Server
	authServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/authorize":
			query := r.URL.Query()
			http.Redirect(w, r, redirectURI+"?code=auth-code-dup&state="+url.QueryEscape(query.Get("state")), http.StatusFound)
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "auth-code-token-dup",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer authServer.Close()

	previousOpenBrowser := openBrowser
	openBrowser = func(rawURL string) error {
		redirectClient := &http.Client{
			Timeout: 500 * time.Millisecond,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
		resp, err := redirectClient.Get(rawURL)
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		if err != nil {
			return err
		}
		location := resp.Header.Get("Location")
		if location == "" {
			return fmt.Errorf("missing redirect location")
		}
		done := make(chan error, 2)
		fire := func() {
			callbackResp, callbackErr := http.Get(location)
			if callbackResp != nil && callbackResp.Body != nil {
				callbackResp.Body.Close()
			}
			done <- callbackErr
		}
		go fire()
		go fire()
		var duplicate error
		for i := 0; i < 2; i++ {
			result := <-done
			if i == 1 {
				duplicate = result
			}
		}
		duplicateErr <- duplicate
		return nil
	}
	defer func() { openBrowser = previousOpenBrowser }()

	secret := config.Secret{
		Type: "oauth2",
		OAuthConfig: config.OAuthConfig{
			Mode: "authorizationCode",
			ClientID: &config.SecretRef{
				Type:  "env",
				Value: "AUTH_CODE_DUP_CLIENT_ID",
			},
			RedirectURI: redirectURI,
		},
	}
	requirement := catalog.AuthRequirement{
		Type:   "oauth2",
		Scopes: []string{"pets.read"},
		OAuthFlows: []catalog.OAuthFlow{{
			Mode:             "authorizationCode",
			AuthorizationURL: authServer.URL + "/authorize",
			TokenURL:         authServer.URL + "/token",
		}},
	}

	token, err := ResolveOAuthAccessToken(context.Background(), authServer.Client(), config.PolicyConfig{}, secret, requirement, "pets.oauth", t.TempDir(), nil)
	if err != nil {
		t.Fatalf("ResolveOAuthAccessToken returned error: %v", err)
	}
	if token != "auth-code-token-dup" {
		t.Fatalf("expected auth code token, got %q", token)
	}

	select {
	case err := <-duplicateErr:
		if err != nil {
			t.Fatalf("expected duplicate callback to return cleanly, got %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("timed out waiting for duplicate callback")
	}
}

func TestPrepareRedirectListenerRejectsNonLoopbackRedirectURI(t *testing.T) {
	secret := config.Secret{
		Type: "oauth2",
		OAuthConfig: config.OAuthConfig{
			Mode:        "authorizationCode",
			RedirectURI: "http://0.0.0.0:0/callback",
		},
	}

	_, _, listener, err := prepareRedirectListener(secret)
	if listener != nil {
		listener.Close()
	}
	if err == nil {
		t.Fatalf("expected non-loopback redirect URI to be rejected")
	}
	if got := err.Error(); got == "" || !strings.Contains(got, "loopback") {
		t.Fatalf("expected loopback validation error, got %v", err)
	}
}

func TestResolveOAuthAccessTokenCachesPerResolvedClientID(t *testing.T) {
	if err := os.Setenv("CACHE_CLIENT_ONE_ID", "client-one"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	if err := os.Setenv("CACHE_CLIENT_ONE_SECRET", "secret-one"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	if err := os.Setenv("CACHE_CLIENT_TWO_ID", "client-two"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	if err := os.Setenv("CACHE_CLIENT_TWO_SECRET", "secret-two"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Unsetenv("CACHE_CLIENT_ONE_ID")
		_ = os.Unsetenv("CACHE_CLIENT_ONE_SECRET")
		_ = os.Unsetenv("CACHE_CLIENT_TWO_ID")
		_ = os.Unsetenv("CACHE_CLIENT_TWO_SECRET")
	})

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse token form: %v", err)
		}
		clientID := r.Form.Get("client_id")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": clientID + "-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer tokenServer.Close()

	requirement := catalog.AuthRequirement{
		Type:   "oauth2",
		Scopes: []string{"pets.read"},
	}
	stateDir := t.TempDir()

	first := config.Secret{
		Type: "oauth2",
		OAuthConfig: config.OAuthConfig{
			Mode:     "clientCredentials",
			TokenURL: tokenServer.URL,
			ClientID: &config.SecretRef{Type: "env", Value: "CACHE_CLIENT_ONE_ID"},
			ClientSecret: &config.SecretRef{
				Type:  "env",
				Value: "CACHE_CLIENT_ONE_SECRET",
			},
		},
	}
	second := config.Secret{
		Type: "oauth2",
		OAuthConfig: config.OAuthConfig{
			Mode:     "clientCredentials",
			TokenURL: tokenServer.URL,
			ClientID: &config.SecretRef{Type: "env", Value: "CACHE_CLIENT_TWO_ID"},
			ClientSecret: &config.SecretRef{
				Type:  "env",
				Value: "CACHE_CLIENT_TWO_SECRET",
			},
		},
	}

	firstToken, err := ResolveOAuthAccessToken(context.Background(), tokenServer.Client(), config.PolicyConfig{}, first, requirement, "pets.oauth", stateDir, nil)
	if err != nil {
		t.Fatalf("ResolveOAuthAccessToken returned error for first secret: %v", err)
	}
	secondToken, err := ResolveOAuthAccessToken(context.Background(), tokenServer.Client(), config.PolicyConfig{}, second, requirement, "pets.oauth", stateDir, nil)
	if err != nil {
		t.Fatalf("ResolveOAuthAccessToken returned error for second secret: %v", err)
	}

	if firstToken != "client-one-token" {
		t.Fatalf("expected first token from first client, got %q", firstToken)
	}
	if secondToken != "client-two-token" {
		t.Fatalf("expected second token from second client, got %q", secondToken)
	}
}
