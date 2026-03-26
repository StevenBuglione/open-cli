package runtime_test

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/StevenBuglione/open-cli/internal/runtime"
)

type oidcJWKSTestIssuer struct {
	issuer     string
	jwksURL    string
	keyID      string
	privateKey *rsa.PrivateKey
	server     *httptest.Server
}

func newOIDCJWKSTestIssuer(t *testing.T) *oidcJWKSTestIssuer {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}

	fixture := &oidcJWKSTestIssuer{
		keyID:      "test-key",
		privateKey: privateKey,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/keys", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]any{{
				"kty": "RSA",
				"use": "sig",
				"alg": "RS256",
				"kid": fixture.keyID,
				"n":   base64.RawURLEncoding.EncodeToString(fixture.privateKey.PublicKey.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(fixture.privateKey.PublicKey.E)).Bytes()),
			}},
		}); err != nil {
			t.Fatalf("encode jwks: %v", err)
		}
	})

	fixture.server = httptest.NewServer(mux)
	fixture.issuer = fixture.server.URL
	fixture.jwksURL = fixture.server.URL + "/keys"
	t.Cleanup(func() { fixture.server.Close() })

	return fixture
}

func (fixture *oidcJWKSTestIssuer) signToken(t *testing.T, claims map[string]any) string {
	t.Helper()

	payload := map[string]any{}
	for key, value := range claims {
		payload[key] = value
	}
	if _, ok := payload["iss"]; !ok {
		payload["iss"] = fixture.issuer
	}

	header := map[string]any{
		"alg": "RS256",
		"kid": fixture.keyID,
		"typ": "JWT",
	}

	encodedHeader := mustJWTPart(t, header)
	encodedPayload := mustJWTPart(t, payload)
	signingInput := encodedHeader + "." + encodedPayload
	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, fixture.privateKey, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func mustJWTPart(t *testing.T, value any) string {
	t.Helper()

	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal jwt part: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(encoded)
}

func writeOIDCJWKSRuntimeConfig(t *testing.T, dir string, issuer *oidcJWKSTestIssuer, ticketsURL, usersURL string) string {
	t.Helper()

	writeRuntimeFile(t, dir, "tickets.openapi.yaml", fmt.Sprintf(`
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

	writeRuntimeFile(t, dir, "users.openapi.yaml", fmt.Sprintf(`
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

	return writeRuntimeFile(t, dir, ".cli.json", fmt.Sprintf(`{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "runtime": {
	    "server": {
	      "auth": {
	        "validationProfile": "oidc_jwks",
	        "issuer": %q,
	        "jwksURL": %q,
	        "audience": "open-cli-toolbox"
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
	}`, issuer.issuer, issuer.jwksURL))
}

func readTrimmedBody(t *testing.T, resp *http.Response) string {
	t.Helper()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	return strings.TrimSpace(string(body))
}

func TestServerAcceptsValidOIDCJWKSToken(t *testing.T) {
	dir := t.TempDir()
	issuer := newOIDCJWKSTestIssuer(t)
	configPath := writeOIDCJWKSRuntimeConfig(t, dir, issuer, "https://tickets.example.com", "https://users.example.com")

	token := issuer.signToken(t, map[string]any{
		"sub":   "agent-123",
		"aud":   "open-cli-toolbox",
		"scope": "bundle:tickets",
		"exp":   time.Now().Add(time.Hour).Unix(),
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
		t.Fatalf("expected 200 effective catalog, got %d with body %q", resp.StatusCode, readTrimmedBody(t, resp))
	}

	var effective map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&effective); err != nil {
		t.Fatalf("decode effective catalog: %v", err)
	}
	catalogData := effective["catalog"].(map[string]any)
	tools := catalogData["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("expected one oidc_jwks scoped tool, got %#v", tools)
	}
	tool := tools[0].(map[string]any)
	if got := tool["id"]; got != "tickets:listTickets" {
		t.Fatalf("expected tickets:listTickets in oidc_jwks scoped catalog, got %#v", got)
	}
}

func TestServerAcceptsMultipleOIDCJWKSBundleScopes(t *testing.T) {
	dir := t.TempDir()
	issuer := newOIDCJWKSTestIssuer(t)
	configPath := writeOIDCJWKSRuntimeConfig(t, dir, issuer, "https://tickets.example.com", "https://users.example.com")

	token := issuer.signToken(t, map[string]any{
		"sub":   "agent-123",
		"aud":   "open-cli-toolbox",
		"scope": "bundle:tickets bundle:users",
		"exp":   time.Now().Add(time.Hour).Unix(),
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
		t.Fatalf("expected 200 effective catalog, got %d with body %q", resp.StatusCode, readTrimmedBody(t, resp))
	}

	var effective map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&effective); err != nil {
		t.Fatalf("decode effective catalog: %v", err)
	}
	catalogData := effective["catalog"].(map[string]any)
	tools := catalogData["tools"].([]any)
	if len(tools) != 2 {
		t.Fatalf("expected two oidc_jwks scoped tools for multiple bundle scopes, got %#v", tools)
	}

	var toolIDs []string
	for _, item := range tools {
		toolIDs = append(toolIDs, item.(map[string]any)["id"].(string))
	}
	sort.Strings(toolIDs)
	if !reflect.DeepEqual(toolIDs, []string{"tickets:listTickets", "users:listUsers"}) {
		t.Fatalf("expected tickets and users tools for multiple bundle scopes, got %#v", toolIDs)
	}
}

func TestServerRejectsWrongAudienceOIDCJWKSToken(t *testing.T) {
	dir := t.TempDir()
	issuer := newOIDCJWKSTestIssuer(t)
	configPath := writeOIDCJWKSRuntimeConfig(t, dir, issuer, "https://tickets.example.com", "https://users.example.com")

	token := issuer.signToken(t, map[string]any{
		"sub":   "agent-123",
		"aud":   "wrong-audience",
		"scope": "bundle:tickets",
		"exp":   time.Now().Add(time.Hour).Unix(),
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
		t.Fatalf("expected 401 effective catalog for wrong oidc_jwks audience, got %d", resp.StatusCode)
	}
	if got := readTrimmedBody(t, resp); got != "authn_failed" {
		t.Fatalf("expected authn_failed body for wrong oidc_jwks audience, got %q", got)
	}
}

func TestServerRejectsOIDCJWKSConfigWithoutAudience(t *testing.T) {
	dir := t.TempDir()
	issuer := newOIDCJWKSTestIssuer(t)
	writeRuntimeFile(t, dir, "tickets.openapi.yaml", `
openapi: 3.1.0
info:
  title: Tickets API
  version: "1.0.0"
paths:
  /tickets:
    get:
      operationId: listTickets
      responses:
        "200":
          description: OK
`)
	configPath := writeRuntimeFile(t, dir, ".cli.json", fmt.Sprintf(`{
	  "cli": "1.0.0",
	  "mode": { "default": "discover" },
	  "runtime": {
	    "server": {
	      "auth": {
	        "validationProfile": "oidc_jwks",
	        "issuer": %q,
	        "jwksURL": %q
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
	}`, issuer.issuer, issuer.jwksURL))

	server := runtime.NewServer(runtime.Options{AuditPath: filepath.Join(dir, "audit.log")})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	resp, err := http.Get(httpServer.URL + "/v1/catalog/effective?config=" + configPath)
	if err != nil {
		t.Fatalf("get effective catalog: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 effective catalog for oidc_jwks config without audience, got %d", resp.StatusCode)
	}
	body := readTrimmedBody(t, resp)
	if !strings.Contains(body, "runtime.server.auth.audience") {
		t.Fatalf("expected missing audience validation error, got %q", body)
	}
}

func TestServerRejectsOIDCJWKSTokenWithoutExpiration(t *testing.T) {
	dir := t.TempDir()
	issuer := newOIDCJWKSTestIssuer(t)
	configPath := writeOIDCJWKSRuntimeConfig(t, dir, issuer, "https://tickets.example.com", "https://users.example.com")

	token := issuer.signToken(t, map[string]any{
		"sub":   "agent-123",
		"aud":   "open-cli-toolbox",
		"scope": "bundle:tickets",
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
		t.Fatalf("expected 401 effective catalog for oidc_jwks token without exp, got %d", resp.StatusCode)
	}
	if got := readTrimmedBody(t, resp); got != "authn_failed" {
		t.Fatalf("expected authn_failed body for oidc_jwks token without exp, got %q", got)
	}
}
