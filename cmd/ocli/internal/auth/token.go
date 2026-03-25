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
	RuntimeURL   string
	StateDir     string
	ConfigPath   string
	AgentProfile string
	ActorID      string
}

const (
	tokenExchangeGrantType = "urn:ietf:params:oauth:grant-type:token-exchange"
	accessTokenType        = "urn:ietf:params:oauth:token-type:access_token"
)

type resolvedRuntimeToken struct {
	token             runtimepkg.SessionToken
	refresh           func(context.Context) (runtimepkg.SessionToken, error)
	reacquireForChild func(context.Context) (runtimepkg.SessionToken, error)
	tokenExchangeURL  string
	effectiveAudience string
	effectiveScopes   []string
}

// ResolveToken determines the runtime bearer token according to the configured
// OAuth mode and returns the raw token string together with a refreshable
// TokenSession.
func ResolveToken(opts TokenResolveOptions, oauth configpkg.RemoteOAuthConfig) (string, *runtimepkg.TokenSession, error) {
	var runtimeHandshake embeddedruntime.HandshakeInfo
	if opts.RuntimeURL != "" && (oauth.Mode == "oauthClient" || oauth.Mode == "browserLogin") {
		info, err := FetchHandshake(opts.RuntimeURL, opts.ConfigPath)
		if err != nil {
			return "", nil, err
		}
		runtimeHandshake = info
	}
	resolved, err := resolvePrimaryToken(context.Background(), opts, oauth, runtimeHandshake)
	if err != nil {
		return "", nil, err
	}
	if oauth.Delegation == nil || !oauth.Delegation.Enabled {
		return resolved.token.AccessToken, runtimepkg.NewTokenSession(resolved.token, resolved.refresh), nil
	}
	delegated, refresh, err := resolveDelegatedRuntimeToken(context.Background(), opts, resolved)
	if err != nil {
		return "", nil, err
	}
	return delegated.AccessToken, runtimepkg.NewTokenSession(delegated, refresh), nil
}

func resolvePrimaryToken(ctx context.Context, opts TokenResolveOptions, oauth configpkg.RemoteOAuthConfig, runtimeHandshake embeddedruntime.HandshakeInfo) (resolvedRuntimeToken, error) {
	switch oauth.Mode {
	case "", "providedToken":
		acquire := func(context.Context) (runtimepkg.SessionToken, error) {
			return resolveProvidedToken(oauth), nil
		}
		token, err := acquire(ctx)
		if err != nil {
			return resolvedRuntimeToken{}, err
		}
		return resolvedRuntimeToken{
			token:             token,
			reacquireForChild: acquire,
			tokenExchangeURL:  delegationTokenExchangeURL(oauth, ""),
			effectiveAudience: oauth.Audience,
			effectiveScopes:   delegationScopes(oauth),
		}, nil
	case "oauthClient":
		if oauth.Client == nil {
			return resolvedRuntimeToken{}, fmt.Errorf("runtime.remote.oauth.client is required for oauthClient mode")
		}
		effectiveOAuth := oauth
		if effectiveOAuth.Audience == "" && runtimeHandshake.Auth != nil {
			effectiveOAuth.Audience = runtimeHandshake.Auth.Audience
		}
		acquire := func(ctx context.Context) (runtimepkg.SessionToken, error) {
			return ResolveOAuthClientToken(ctx, effectiveOAuth)
		}
		token, err := acquire(ctx)
		if err != nil {
			return resolvedRuntimeToken{}, err
		}
		return resolvedRuntimeToken{
			token:             token,
			refresh:           acquire,
			reacquireForChild: acquire,
			tokenExchangeURL:  delegationTokenExchangeURL(oauth, oauth.Client.TokenURL),
			effectiveAudience: effectiveOAuth.Audience,
			effectiveScopes:   delegationScopes(oauth),
		}, nil
	case "browserLogin":
		if opts.RuntimeURL == "" {
			return resolvedRuntimeToken{}, fmt.Errorf("runtime URL is required for browserLogin mode")
		}
		browserConfigEndpoint := "/v1/auth/browser-config"
		if runtimeHandshake.Auth != nil && runtimeHandshake.Auth.BrowserLogin != nil {
			if !runtimeHandshake.Auth.BrowserLogin.Configured {
				return resolvedRuntimeToken{}, fmt.Errorf("runtime browser login is not configured")
			}
			if endpoint := strings.TrimSpace(runtimeHandshake.Auth.BrowserLogin.ConfigEndpoint); endpoint != "" {
				browserConfigEndpoint = endpoint
			}
		}
		metadata, err := FetchBrowserLoginMetadata(opts.RuntimeURL, browserConfigEndpoint, opts.ConfigPath)
		if err != nil {
			return resolvedRuntimeToken{}, err
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
			return resolvedRuntimeToken{}, fmt.Errorf("runtime browser login metadata missing audience")
		}
		if oauth.BrowserLogin != nil {
			request.CallbackPort = oauth.BrowserLogin.CallbackPort
		}
		token, err := BrowserLoginTokenAcquirer(request)
		if err != nil {
			return resolvedRuntimeToken{}, err
		}
		return resolvedRuntimeToken{
			token:             runtimepkg.SessionToken{AccessToken: token},
			tokenExchangeURL:  delegationTokenExchangeURL(oauth, metadata.TokenURL),
			effectiveAudience: request.Audience,
			effectiveScopes:   delegationScopes(oauth),
		}, nil
	default:
		return resolvedRuntimeToken{}, fmt.Errorf("runtime.remote.oauth.mode %q is not supported yet", oauth.Mode)
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

type DelegatedTokenRequest struct {
	TokenExchangeURL string
	ParentToken      string
	Audience         string
	Scopes           []string
	ActorID          string
	AgentProfile     string
}

func ResolveDelegatedToken(ctx context.Context, request DelegatedTokenRequest) (runtimepkg.SessionToken, error) {
	if strings.TrimSpace(request.TokenExchangeURL) == "" {
		return runtimepkg.SessionToken{}, fmt.Errorf("runtime delegated token exchange URL is required")
	}
	if strings.TrimSpace(request.ParentToken) == "" {
		return runtimepkg.SessionToken{}, fmt.Errorf("runtime delegated token exchange requires a parent token")
	}
	form := url.Values{}
	form.Set("grant_type", tokenExchangeGrantType)
	form.Set("subject_token", request.ParentToken)
	form.Set("subject_token_type", accessTokenType)
	form.Set("requested_token_type", accessTokenType)
	if request.Audience != "" {
		form.Set("audience", request.Audience)
	}
	if len(request.Scopes) > 0 {
		form.Set("scope", strings.Join(request.Scopes, " "))
	}
	if actorID := strings.TrimSpace(request.ActorID); actorID != "" {
		form.Set("actor_id", actorID)
	}
	if agentProfile := strings.TrimSpace(request.AgentProfile); agentProfile != "" {
		form.Set("agent_profile", agentProfile)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, request.TokenExchangeURL, strings.NewReader(form.Encode()))
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
		return runtimepkg.SessionToken{}, fmt.Errorf("runtime delegated token request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var token struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return runtimepkg.SessionToken{}, err
	}
	if token.AccessToken == "" {
		return runtimepkg.SessionToken{}, fmt.Errorf("runtime delegated token response missing access_token")
	}
	expiresAt := time.Time{}
	if token.ExpiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
	}
	return runtimepkg.SessionToken{AccessToken: token.AccessToken, ExpiresAt: expiresAt}, nil
}

func resolveDelegatedRuntimeToken(ctx context.Context, opts TokenResolveOptions, resolved resolvedRuntimeToken) (runtimepkg.SessionToken, func(context.Context) (runtimepkg.SessionToken, error), error) {
	if strings.TrimSpace(resolved.tokenExchangeURL) == "" {
		return runtimepkg.SessionToken{}, nil, fmt.Errorf("runtime.remote.oauth.delegation.tokenExchangeURL is required when delegation is enabled")
	}
	acquire := func(ctx context.Context, parentToken string) (runtimepkg.SessionToken, error) {
		return ResolveDelegatedToken(ctx, DelegatedTokenRequest{
			TokenExchangeURL: resolved.tokenExchangeURL,
			ParentToken:      parentToken,
			Audience:         resolved.effectiveAudience,
			Scopes:           resolved.effectiveScopes,
			ActorID:          opts.ActorID,
			AgentProfile:     opts.AgentProfile,
		})
	}
	token, err := acquire(ctx, resolved.token.AccessToken)
	if err != nil {
		return runtimepkg.SessionToken{}, nil, err
	}
	if resolved.reacquireForChild == nil {
		return token, nil, nil
	}
	refresh := func(ctx context.Context) (runtimepkg.SessionToken, error) {
		parent, err := resolved.reacquireForChild(ctx)
		if err != nil {
			return runtimepkg.SessionToken{}, err
		}
		return acquire(ctx, parent.AccessToken)
	}
	return token, refresh, nil
}

func resolveProvidedToken(oauth configpkg.RemoteOAuthConfig) runtimepkg.SessionToken {
	token := ""
	if strings.HasPrefix(oauth.TokenRef, "env:") {
		token = os.Getenv(strings.TrimPrefix(oauth.TokenRef, "env:"))
	}
	return runtimepkg.SessionToken{AccessToken: token}
}

func delegationTokenExchangeURL(oauth configpkg.RemoteOAuthConfig, fallback string) string {
	if oauth.Delegation != nil && strings.TrimSpace(oauth.Delegation.TokenExchangeURL) != "" {
		return oauth.Delegation.TokenExchangeURL
	}
	return fallback
}

func delegationScopes(oauth configpkg.RemoteOAuthConfig) []string {
	if oauth.Delegation != nil && len(oauth.Delegation.Scopes) > 0 {
		return append([]string(nil), oauth.Delegation.Scopes...)
	}
	return append([]string(nil), oauth.Scopes...)
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
