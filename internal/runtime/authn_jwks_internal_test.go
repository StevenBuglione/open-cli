package runtime

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/StevenBuglione/open-cli/pkg/config"
)

type oidcJWKSInternalTestIssuer struct {
	issuer     string
	jwksURL    string
	keyID      string
	privateKey *rsa.PrivateKey
	server     *httptest.Server
}

func newOIDCJWKSInternalTestIssuer(t *testing.T) *oidcJWKSInternalTestIssuer {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}

	fixture := &oidcJWKSInternalTestIssuer{
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

func (fixture *oidcJWKSInternalTestIssuer) signToken(t *testing.T, claims map[string]any) string {
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

	encodedHeader := mustJWKSInternalJWTPart(t, header)
	encodedPayload := mustJWKSInternalJWTPart(t, payload)
	signingInput := encodedHeader + "." + encodedPayload
	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, fixture.privateKey, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func mustJWKSInternalJWTPart(t *testing.T, value any) string {
	t.Helper()

	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal jwt part: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(encoded)
}

func TestAuthenticateRequestRejectsOIDCJWKSTokenWithoutPrincipalIdentity(t *testing.T) {
	issuer := newOIDCJWKSInternalTestIssuer(t)
	server := NewServer(Options{})
	request := httptest.NewRequest(http.MethodGet, "/v1/catalog/effective", nil)
	request.Header.Set("Authorization", "Bearer "+issuer.signToken(t, map[string]any{
		"aud":   "open-cli-toolbox",
		"scope": "bundle:tickets",
		"exp":   time.Now().Add(time.Hour).Unix(),
	}))
	cfg := config.Config{
		Runtime: &config.RuntimeConfig{
			Server: &config.RuntimeServerConfig{
				Auth: &config.RuntimeServerAuthConfig{
					ValidationProfile: "oidc_jwks",
					Issuer:            issuer.issuer,
					JWKSURL:           issuer.jwksURL,
					Audience:          "open-cli-toolbox",
				},
			},
		},
	}

	result, err := server.authenticateRequest(context.Background(), request, cfg)
	if err == nil {
		t.Fatalf("expected oidc_jwks token without sub or client_id to be rejected, got result %#v", result)
	}
	authErr, ok := err.(*runtimeAuthError)
	if !ok {
		t.Fatalf("expected runtimeAuthError, got %T", err)
	}
	if authErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for oidc_jwks token without principal identity, got %d", authErr.StatusCode)
	}
	if authErr.Code != "authn_failed" {
		t.Fatalf("expected authn_failed code, got %q", authErr.Code)
	}
	if authErr.Message != "oidc_jwks token must contain sub or client_id" {
		t.Fatalf("expected missing principal identity error, got %q", authErr.Message)
	}
}

func TestAuthenticateRequestCapturesOIDCJWKSDelegationLineage(t *testing.T) {
	issuer := newOIDCJWKSInternalTestIssuer(t)
	server := NewServer(Options{})
	request := httptest.NewRequest(http.MethodGet, "/v1/catalog/effective", nil)
	request.Header.Set("Authorization", "Bearer "+issuer.signToken(t, map[string]any{
		"sub":           "subagent:triage-01",
		"aud":           "open-cli-toolbox",
		"scope":         "bundle:tickets profile:sandbox",
		"delegated_by":  "github:user-123",
		"delegation_id": "delegation-123",
		"act": map[string]string{
			"sub":       "github:user-123",
			"client_id": "ocli-browser",
			"actor_id":  "lead-agent",
		},
		"exp": time.Now().Add(time.Hour).Unix(),
	}))
	cfg := config.Config{
		Runtime: &config.RuntimeConfig{
			Server: &config.RuntimeServerConfig{
				Auth: &config.RuntimeServerAuthConfig{
					ValidationProfile: "oidc_jwks",
					Issuer:            issuer.issuer,
					JWKSURL:           issuer.jwksURL,
					Audience:          "open-cli-toolbox",
				},
			},
		},
	}

	result, err := server.authenticateRequest(context.Background(), request, cfg)
	if err != nil {
		t.Fatalf("authenticate delegated token: %v", err)
	}
	if result.Principal != "subagent:triage-01" {
		t.Fatalf("expected delegated subject principal, got %#v", result.Principal)
	}
	if !reflect.DeepEqual(result.Scopes, []string{"bundle:tickets", "profile:sandbox"}) {
		t.Fatalf("expected scopes from delegated token, got %#v", result.Scopes)
	}
	if result.Lineage == nil {
		t.Fatal("expected delegation lineage on auth result")
	}
	if result.Lineage.DelegatedBy != "github:user-123" {
		t.Fatalf("expected delegated_by lineage, got %#v", result.Lineage)
	}
	if result.Lineage.DelegationID != "delegation-123" {
		t.Fatalf("expected delegation_id lineage, got %#v", result.Lineage)
	}
	if !reflect.DeepEqual(result.Lineage.Actor, map[string]string{
		"sub":       "github:user-123",
		"client_id": "ocli-browser",
		"actor_id":  "lead-agent",
	}) {
		t.Fatalf("expected act lineage map, got %#v", result.Lineage.Actor)
	}
}
