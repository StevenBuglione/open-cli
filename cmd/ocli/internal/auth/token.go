package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	runtimepkg "github.com/StevenBuglione/open-cli/cmd/ocli/internal/runtime"
	embeddedruntime "github.com/StevenBuglione/open-cli/internal/runtime"
	oauthruntime "github.com/StevenBuglione/open-cli/pkg/auth"
	configpkg "github.com/StevenBuglione/open-cli/pkg/config"
)

// TokenResolveOptions holds the subset of command options needed by
// ResolveToken.
type TokenResolveOptions struct {
	RuntimeURL string
	StateDir   string
}

// ResolveToken determines the runtime bearer token according to the configured
// OAuth mode and returns the raw token string together with a refreshable
// TokenSession.
func ResolveToken(opts TokenResolveOptions, oauth configpkg.RemoteOAuthConfig) (string, *runtimepkg.TokenSession, error) {
	var runtimeHandshake embeddedruntime.HandshakeInfo
	if opts.RuntimeURL != "" && (oauth.Mode == "oauthClient" || oauth.Mode == "browserLogin") {
		info, err := FetchHandshake(opts.RuntimeURL)
		if err != nil {
			return "", nil, err
		}
		runtimeHandshake = info
	}
	switch oauth.Mode {
	case "", "providedToken":
		token := ""
		if strings.HasPrefix(oauth.TokenRef, "env:") {
			token = os.Getenv(strings.TrimPrefix(oauth.TokenRef, "env:"))
		}
		return token, runtimepkg.NewTokenSession(runtimepkg.SessionToken{AccessToken: token}, nil), nil
	case "oauthClient":
		if oauth.Client == nil {
			return "", nil, fmt.Errorf("runtime.remote.oauth.client is required for oauthClient mode")
		}
		effectiveOAuth := oauth
		if effectiveOAuth.Audience == "" && runtimeHandshake.Auth != nil {
			effectiveOAuth.Audience = runtimeHandshake.Auth.Audience
		}
		acquire := func(ctx context.Context) (runtimepkg.SessionToken, error) {
			return ResolveOAuthClientToken(ctx, effectiveOAuth)
		}
		token, err := acquire(context.Background())
		if err != nil {
			return "", nil, err
		}
		return token.AccessToken, runtimepkg.NewTokenSession(token, acquire), nil
	case "browserLogin":
		if opts.RuntimeURL == "" {
			return "", nil, fmt.Errorf("runtime URL is required for browserLogin mode")
		}
		browserConfigEndpoint := "/v1/auth/browser-config"
		if runtimeHandshake.Auth != nil && runtimeHandshake.Auth.BrowserLogin != nil {
			if !runtimeHandshake.Auth.BrowserLogin.Configured {
				return "", nil, fmt.Errorf("runtime browser login is not configured")
			}
			if endpoint := strings.TrimSpace(runtimeHandshake.Auth.BrowserLogin.ConfigEndpoint); endpoint != "" {
				browserConfigEndpoint = endpoint
			}
		}
		metadata, err := FetchBrowserLoginMetadata(opts.RuntimeURL, browserConfigEndpoint)
		if err != nil {
			return "", nil, err
		}
		request := BrowserLoginRequest{
			Metadata: metadata,
			Scopes:   append([]string(nil), oauth.Scopes...),
			Audience: metadata.Audience,
			StateDir: opts.StateDir,
		}
		if request.Audience == "" && runtimeHandshake.Auth != nil {
			request.Audience = runtimeHandshake.Auth.Audience
		}
		if oauth.Audience != "" {
			request.Audience = oauth.Audience
		}
		if strings.TrimSpace(request.Audience) == "" {
			return "", nil, fmt.Errorf("runtime browser login metadata missing audience")
		}
		if oauth.BrowserLogin != nil {
			request.CallbackPort = oauth.BrowserLogin.CallbackPort
		}
		token, err := BrowserLoginTokenAcquirer(request)
		if err != nil {
			return "", nil, err
		}
		return token, runtimepkg.NewTokenSession(runtimepkg.SessionToken{AccessToken: token}, nil), nil
	default:
		return "", nil, fmt.Errorf("runtime.remote.oauth.mode %q is not supported yet", oauth.Mode)
	}
}

// ResolveOAuthClientToken performs a client-credentials OAuth token exchange.
func ResolveOAuthClientToken(ctx context.Context, oauth configpkg.RemoteOAuthConfig) (runtimepkg.SessionToken, error) {
	if oauth.Client == nil {
		return runtimepkg.SessionToken{}, fmt.Errorf("runtime.remote.oauth.client is required for oauthClient mode")
	}
	clientID, err := ResolveOAuthSecret(oauth.Client.ClientID)
	if err != nil {
		return runtimepkg.SessionToken{}, fmt.Errorf("resolve runtime oauth clientId: %w", err)
	}
	clientSecret, err := ResolveOAuthSecret(oauth.Client.ClientSecret)
	if err != nil {
		return runtimepkg.SessionToken{}, fmt.Errorf("resolve runtime oauth clientSecret: %w", err)
	}
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	if len(oauth.Scopes) > 0 {
		form.Set("scope", strings.Join(oauth.Scopes, " "))
	}
	if oauth.Audience != "" {
		form.Set("audience", oauth.Audience)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, oauth.Client.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return runtimepkg.SessionToken{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return runtimepkg.SessionToken{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return runtimepkg.SessionToken{}, fmt.Errorf("runtime oauth token request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var token struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return runtimepkg.SessionToken{}, err
	}
	if token.AccessToken == "" {
		return runtimepkg.SessionToken{}, fmt.Errorf("runtime oauth token response missing access_token")
	}
	expiresAt := time.Time{}
	if token.ExpiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
	}
	return runtimepkg.SessionToken{AccessToken: token.AccessToken, ExpiresAt: expiresAt}, nil
}

// ResolveOAuthSecret resolves a secret reference (literal value, env var, or
// command) using the standard secret resolution logic.
func ResolveOAuthSecret(secret *configpkg.SecretRef) (string, error) {
	if secret == nil {
		return "", fmt.Errorf("missing secret reference")
	}
	return oauthruntime.ResolveStaticSecret(configpkg.PolicyConfig{}, configpkg.Secret{
		Type:    secret.Type,
		Value:   secret.Value,
		Command: append([]string(nil), secret.Command...),
	}, nil)
}
