package helpers

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type RuntimeAuthBroker struct {
	URL              string
	Issuer           string
	JWKSURL          string
	AuthorizationURL string
	TokenURL         string
	BrowserClientID  string
	ClientID         string
	ClientSecret     string

	keyID      string
	privateKey *rsa.PrivateKey
	server     *httptest.Server

	mu    sync.Mutex
	codes map[string]authorizationCodeGrant
}

type authorizationCodeGrant struct {
	Upstream      string
	Audience      string
	ClientID      string
	RedirectURI   string
	CodeChallenge string
	Scopes        []string
}

func NewRuntimeAuthBroker(t *testing.T) *RuntimeAuthBroker {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate broker rsa key: %v", err)
	}

	broker := &RuntimeAuthBroker{
		keyID:           "broker-test-key",
		privateKey:      privateKey,
		BrowserClientID: "oascli-browser",
		ClientID:        "runtime-client",
		ClientSecret:    "runtime-secret",
		codes:           map[string]authorizationCodeGrant{},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", broker.handleDiscovery)
	mux.HandleFunc("/jwks.json", broker.handleJWKS)
	mux.HandleFunc("/authorize", broker.handleAuthorize)
	mux.HandleFunc("/token", broker.handleToken)

	broker.server = httptest.NewServer(mux)
	broker.URL = broker.server.URL
	broker.Issuer = broker.server.URL
	broker.JWKSURL = broker.server.URL + "/jwks.json"
	broker.AuthorizationURL = broker.server.URL + "/authorize"
	broker.TokenURL = broker.server.URL + "/token"

	t.Cleanup(func() { broker.server.Close() })
	return broker
}

func (broker *RuntimeAuthBroker) handleDiscovery(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"issuer":                 broker.Issuer,
		"authorization_endpoint": broker.AuthorizationURL,
		"token_endpoint":         broker.TokenURL,
		"jwks_uri":               broker.JWKSURL,
	})
}

func (broker *RuntimeAuthBroker) handleJWKS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"keys": []map[string]any{{
			"kty": "RSA",
			"use": "sig",
			"alg": "RS256",
			"kid": broker.keyID,
			"n":   base64.RawURLEncoding.EncodeToString(broker.privateKey.PublicKey.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(broker.privateKey.PublicKey.E)).Bytes()),
		}},
	})
}

func (broker *RuntimeAuthBroker) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	query := r.URL.Query()
	redirectURI := query.Get("redirect_uri")
	state := query.Get("state")
	clientID := query.Get("client_id")
	if redirectURI == "" || state == "" || clientID == "" {
		http.Error(w, "missing authorize parameters", http.StatusBadRequest)
		return
	}

	code := fmt.Sprintf("code-%d", time.Now().UnixNano())
	broker.mu.Lock()
	broker.codes[code] = authorizationCodeGrant{
		Upstream:      normalizeBrokerUpstream(query.Get("upstream")),
		Audience:      query.Get("audience"),
		ClientID:      clientID,
		RedirectURI:   redirectURI,
		CodeChallenge: query.Get("code_challenge"),
		Scopes:        strings.Fields(query.Get("scope")),
	}
	broker.mu.Unlock()

	location := redirectURI + "?code=" + url.QueryEscape(code) + "&state=" + url.QueryEscape(state)
	http.Redirect(w, r, location, http.StatusFound)
}

func (broker *RuntimeAuthBroker) handleToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	switch r.Form.Get("grant_type") {
	case "client_credentials":
		if r.Form.Get("client_id") != broker.ClientID || r.Form.Get("client_secret") != broker.ClientSecret {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_client"})
			return
		}
		token, err := broker.signRuntimeToken(normalizeBrokerUpstream(r.Form.Get("upstream")), runtimeTokenClaims{
			Audience: r.Form.Get("audience"),
			Scopes:   strings.Fields(r.Form.Get("scope")),
			Subject:  normalizeBrokerUpstream(r.Form.Get("upstream")) + ":service-account",
			ClientID: r.Form.Get("client_id"),
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeRuntimeTokenResponse(w, token)
	case "authorization_code":
		code := r.Form.Get("code")
		broker.mu.Lock()
		grant, ok := broker.codes[code]
		if ok {
			delete(broker.codes, code)
		}
		broker.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
			return
		}
		if r.Form.Get("client_id") != grant.ClientID || r.Form.Get("redirect_uri") != grant.RedirectURI {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_request"})
			return
		}
		if grant.CodeChallenge != "" {
			verifier := r.Form.Get("code_verifier")
			sum := sha256.Sum256([]byte(verifier))
			if challenge := base64.RawURLEncoding.EncodeToString(sum[:]); challenge != grant.CodeChallenge {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
				return
			}
		}
		token, err := broker.signRuntimeToken(grant.Upstream, runtimeTokenClaims{
			Audience: grant.Audience,
			Scopes:   grant.Scopes,
			Subject:  grant.Upstream + ":user",
			ClientID: grant.ClientID,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeRuntimeTokenResponse(w, token)
	default:
		http.Error(w, "unsupported grant_type", http.StatusBadRequest)
	}
}

type runtimeTokenClaims struct {
	Audience string
	Scopes   []string
	Subject  string
	ClientID string
}

func (broker *RuntimeAuthBroker) signRuntimeToken(upstream string, claims runtimeTokenClaims) (string, error) {
	audience := claims.Audience
	if audience == "" {
		audience = "oasclird"
	}
	scope := strings.Join(claims.Scopes, " ")
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss":               broker.Issuer,
		"aud":               audience,
		"sub":               claims.Subject,
		"client_id":         claims.ClientID,
		"scope":             scope,
		"exp":               time.Now().Add(time.Hour).Unix(),
		"upstream_provider": normalizeBrokerUpstream(upstream),
	})
	token.Header["kid"] = broker.keyID
	return token.SignedString(broker.privateKey)
}

func writeRuntimeTokenResponse(w http.ResponseWriter, token string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token": token,
		"token_type":   "Bearer",
		"expires_in":   3600,
	})
}

func normalizeBrokerUpstream(upstream string) string {
	switch strings.ToLower(strings.TrimSpace(upstream)) {
	case "google":
		return "google"
	case "github":
		return "github"
	default:
		return "microsoft"
	}
}

func (broker *RuntimeAuthBroker) AcquireClientCredentialsToken(t *testing.T, upstream, audience string, scopes []string) string {
	t.Helper()

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", broker.ClientID)
	form.Set("client_secret", broker.ClientSecret)
	form.Set("audience", audience)
	form.Set("scope", strings.Join(scopes, " "))
	form.Set("upstream", upstream)

	resp, err := http.PostForm(broker.TokenURL, form)
	if err != nil {
		t.Fatalf("acquire broker token: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 token response, got %d", resp.StatusCode)
	}
	var payload struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode broker token response: %v", err)
	}
	if payload.AccessToken == "" {
		t.Fatalf("expected access token from broker")
	}
	return payload.AccessToken
}

func (broker *RuntimeAuthBroker) ExchangeAuthorizationCode(t *testing.T, code, clientID, redirectURI, verifier string) string {
	t.Helper()

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("client_id", clientID)
	form.Set("redirect_uri", redirectURI)
	form.Set("code_verifier", verifier)

	resp, err := http.PostForm(broker.TokenURL, form)
	if err != nil {
		t.Fatalf("exchange broker auth code: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 token response, got %d", resp.StatusCode)
	}
	var payload struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode auth code token response: %v", err)
	}
	if payload.AccessToken == "" {
		t.Fatalf("expected access token from broker auth code exchange")
	}
	return payload.AccessToken
}

func WriteRuntimeAuthBrokerConfig(t *testing.T, dir string, broker *RuntimeAuthBroker, ticketsURL, usersURL string) string {
	t.Helper()

	writeFile(t, dir, "tickets.openapi.yaml", fmt.Sprintf(`
openapi: 3.1.0
info:
  title: Tickets API
  version: "1.0.0"
servers:
  - url: %s
paths:
  /tickets:
    get:
      operationId: listTickets
      tags: [tickets]
      responses:
        "200":
          description: OK
`, ticketsURL))

	writeFile(t, dir, "users.openapi.yaml", fmt.Sprintf(`
openapi: 3.1.0
info:
  title: Users API
  version: "1.0.0"
servers:
  - url: %s
paths:
  /users:
    get:
      operationId: listUsers
      tags: [users]
      responses:
        "200":
          description: OK
`, usersURL))

	return writeFile(t, dir, ".cli.json", fmt.Sprintf(`{
  "cli": "1.0.0",
  "mode": { "default": "discover" },
  "runtime": {
    "server": {
      "auth": {
        "validationProfile": "oidc_jwks",
        "issuer": %q,
        "jwksURL": %q,
        "audience": "oasclird",
        "authorizationURL": %q,
        "tokenURL": %q,
        "browserClientId": %q
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
}`, broker.Issuer, broker.JWKSURL, broker.AuthorizationURL, broker.TokenURL, broker.BrowserClientID))
}

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}
