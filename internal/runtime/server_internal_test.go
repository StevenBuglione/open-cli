package runtime

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/StevenBuglione/open-cli/pkg/config"
)

func TestAuthenticateRequestRequiresBearerTokenForCanonicalIntrospectionValidationProfile(t *testing.T) {
	server := NewServer(Options{})
	request := httptest.NewRequest("GET", "/v1/catalog/effective", nil)
	cfg := config.Config{
		Runtime: &config.RuntimeConfig{
			Server: &config.RuntimeServerConfig{
				Auth: &config.RuntimeServerAuthConfig{
					ValidationProfile: "oauth2_introspection",
				},
			},
		},
	}

	result, err := server.authenticateRequest(context.Background(), request, cfg)
	if err == nil {
		t.Fatalf("expected missing bearer token to be rejected, got result %#v", result)
	}
	authErr, ok := err.(*runtimeAuthError)
	if !ok {
		t.Fatalf("expected runtimeAuthError, got %T", err)
	}
	if authErr.StatusCode != 401 {
		t.Fatalf("expected 401 for canonical introspection profile without token, got %d", authErr.StatusCode)
	}
	if authErr.Code != "authn_failed" {
		t.Fatalf("expected authn_failed code, got %q", authErr.Code)
	}
}

func TestRuntimeServerAuthEnabledRequiresCanonicalOIDCJWKSValidationProfile(t *testing.T) {
	if !runtimeServerAuthEnabled(&config.RuntimeServerAuthConfig{ValidationProfile: "oidc_jwks"}) {
		t.Fatalf("expected canonical oidc_jwks validationProfile to enable runtime auth")
	}
	if runtimeServerAuthEnabled(&config.RuntimeServerAuthConfig{Mode: "oidcJWKS"}) {
		t.Fatalf("expected unsupported oidcJWKS mode alias not to enable runtime auth")
	}
}
