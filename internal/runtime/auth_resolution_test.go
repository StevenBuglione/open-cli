package runtime

import (
	"context"
	"os"
	"testing"

	"github.com/StevenBuglione/open-cli/pkg/catalog"
	"github.com/StevenBuglione/open-cli/pkg/config"
)

func TestResolveAuthPlanPrefersNonInteractiveAlternative(t *testing.T) {
	if err := os.Setenv("AUTH_ALT_API_KEY", "static-secret"); err != nil {
		t.Fatalf("setenv: %v", err)
	}
	t.Cleanup(func() { _ = os.Unsetenv("AUTH_ALT_API_KEY") })

	server := NewServer(Options{StateDir: t.TempDir()})
	cfg := config.Config{
		Secrets: map[string]config.Secret{
			"pets.petstore_oauth": {
				Type: "oauth2",
				OAuthConfig: config.OAuthConfig{
					Mode:             "authorizationCode",
					AuthorizationURL: "https://auth.example.com/authorize",
					TokenURL:         "https://auth.example.com/token",
					ClientID:         &config.SecretRef{Type: "literal", Value: "browser-client"},
				},
			},
			"pets.api_key": {Type: "env", Value: "AUTH_ALT_API_KEY"},
		},
	}
	tool := catalog.Tool{
		ServiceID: "pets",
		AuthAlternatives: []catalog.AuthAlternative{
			{
				Requirements: []catalog.AuthRequirement{{
					Name:   "petstore_oauth",
					Type:   "oauth2",
					Scopes: []string{"pets.read"},
					OAuthFlows: []catalog.OAuthFlow{{
						Mode:             "authorizationCode",
						AuthorizationURL: "https://auth.example.com/authorize",
						TokenURL:         "https://auth.example.com/token",
					}},
				}},
			},
			{
				Requirements: []catalog.AuthRequirement{{
					Name:      "api_key",
					Type:      "apiKey",
					In:        "query",
					ParamName: "api_key",
				}},
			},
		},
	}

	plan, err := server.resolveAuthPlan(context.Background(), cfg, tool)
	if err != nil {
		t.Fatalf("resolveAuthPlan: %v", err)
	}
	if got := plan.Query["api_key"]; got != "static-secret" {
		t.Fatalf("expected api_key query auth plan, got %#v", plan)
	}
	if _, ok := plan.Headers["Authorization"]; ok {
		t.Fatalf("did not expect Authorization header when api key alternative is selected, got %#v", plan)
	}
}
