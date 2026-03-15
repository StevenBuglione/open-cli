package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	stdruntime "runtime"
	"strings"
	"sync"
	"time"

	"github.com/StevenBuglione/oas-cli-go/pkg/catalog"
	"github.com/StevenBuglione/oas-cli-go/pkg/config"
)

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

type oidcDiscoveryDocument struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
}

type cachedToken struct {
	AccessToken string    `json:"accessToken"`
	ExpiresAt   time.Time `json:"expiresAt,omitempty"`
}

var memoryTokenCache = struct {
	mu    sync.Mutex
	items map[string]cachedToken
}{
	items: map[string]cachedToken{},
}

var openBrowser = defaultOpenBrowser
var loopbackTimeout = 120 * time.Second

func ResolveOAuthAccessToken(ctx context.Context, httpClient *http.Client, policy config.PolicyConfig, secret config.Secret, requirement catalog.AuthRequirement, providerKey, stateDir string, keychainResolver func(string) (string, error)) (string, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if secret.Type != "oauth2" {
		return "", fmt.Errorf("unsupported oauth secret type %q", secret.Type)
	}

	switch secret.Mode {
	case "clientCredentials":
		tokenURL, err := resolveTokenURL(ctx, httpClient, secret, requirement)
		if err != nil {
			return "", err
		}
		clientID, err := resolveSecretRef(policy, secret.ClientID, keychainResolver)
		if err != nil {
			return "", fmt.Errorf("resolve oauth clientId: %w", err)
		}
		cacheKey := tokenCacheKey(providerKey, secret, requirement, tokenURL, "", clientID, "")
		if token, ok, err := loadCachedToken(secret, cacheKey, stateDir); err != nil {
			return "", err
		} else if ok {
			return token, nil
		}
		token, expiresAt, err := resolveClientCredentialsToken(ctx, httpClient, policy, secret, requirement, tokenURL, clientID, keychainResolver)
		if err != nil {
			return "", err
		}
		if err := storeCachedToken(secret, cacheKey, stateDir, cachedToken{AccessToken: token, ExpiresAt: expiresAt}); err != nil {
			return "", err
		}
		return token, nil
	case "authorizationCode":
		authorizationURL, tokenURL, err := resolveAuthorizationCodeEndpoints(ctx, httpClient, secret, requirement)
		if err != nil {
			return "", err
		}
		clientID, err := resolveSecretRef(policy, secret.ClientID, keychainResolver)
		if err != nil {
			return "", fmt.Errorf("resolve oauth clientId: %w", err)
		}
		cacheKey := tokenCacheKey(providerKey, secret, requirement, tokenURL, authorizationURL, clientID, redirectCacheValue(secret))
		if token, ok, err := loadCachedToken(secret, cacheKey, stateDir); err != nil {
			return "", err
		} else if ok {
			return token, nil
		}
		token, expiresAt, err := resolveAuthorizationCodeToken(ctx, httpClient, policy, secret, requirement, authorizationURL, tokenURL, clientID, keychainResolver)
		if err != nil {
			return "", err
		}
		if err := storeCachedToken(secret, cacheKey, stateDir, cachedToken{AccessToken: token, ExpiresAt: expiresAt}); err != nil {
			return "", err
		}
		return token, nil
	default:
		return "", fmt.Errorf("oauth mode %q is not supported yet", secret.Mode)
	}
}

func resolveAuthorizationCodeToken(ctx context.Context, httpClient *http.Client, policy config.PolicyConfig, secret config.Secret, requirement catalog.AuthRequirement, authorizationURL, tokenURL, clientID string, keychainResolver func(string) (string, error)) (string, time.Time, error) {
	redirectURI, parsedRedirect, listener, err := prepareRedirectListener(secret)
	if err != nil {
		return "", time.Time{}, err
	}
	defer listener.Close()
	state, err := randomURLSafeString(24)
	if err != nil {
		return "", time.Time{}, err
	}
	verifier, err := randomURLSafeString(32)
	if err != nil {
		return "", time.Time{}, err
	}
	callbackResult := make(chan struct {
		code string
		err  error
	}, 1)
	var callbackOnce sync.Once
	deliverCallback := func(code string, err error) {
		callbackOnce.Do(func() {
			callbackResult <- struct {
				code string
				err  error
			}{code: code, err: err}
		})
	}

	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if parsedRedirect.Path != "" && r.URL.Path != parsedRedirect.Path {
			http.NotFound(w, r)
			return
		}
		if got := r.URL.Query().Get("state"); got != state {
			deliverCallback("", fmt.Errorf("oauth callback state mismatch"))
			http.Error(w, "state mismatch", http.StatusBadRequest)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			deliverCallback("", fmt.Errorf("oauth callback missing code"))
			http.Error(w, "missing code", http.StatusBadRequest)
			return
		}
		io.WriteString(w, "Authentication complete. You can close this window.")
		deliverCallback(code, nil)
	})}
	go func() {
		_ = server.Serve(listener)
	}()
	defer server.Shutdown(context.Background())

	query := url.Values{}
	query.Set("response_type", "code")
	query.Set("client_id", clientID)
	query.Set("redirect_uri", redirectURI)
	query.Set("state", state)
	query.Set("code_challenge_method", "S256")
	query.Set("code_challenge", pkceChallenge(verifier))
	if scopes := joinScopes(secret.Scopes, requirement.Scopes); scopes != "" {
		query.Set("scope", scopes)
	}
	if secret.Audience != "" {
		query.Set("audience", secret.Audience)
	}

	authURL, err := url.Parse(authorizationURL)
	if err != nil {
		return "", time.Time{}, err
	}
	authURL.RawQuery = query.Encode()
	if err := openBrowser(authURL.String()); err != nil {
		return "", time.Time{}, err
	}

	waitCtx, cancel := context.WithTimeout(ctx, loopbackTimeout)
	defer cancel()

	var code string
	select {
	case result := <-callbackResult:
		if result.err != nil {
			return "", time.Time{}, result.err
		}
		code = result.code
	case <-waitCtx.Done():
		return "", time.Time{}, waitCtx.Err()
	}

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", clientID)
	form.Set("code_verifier", verifier)
	if secret.ClientSecret != nil {
		clientSecret, err := resolveSecretRef(policy, secret.ClientSecret, keychainResolver)
		if err != nil {
			return "", time.Time{}, fmt.Errorf("resolve oauth clientSecret: %w", err)
		}
		form.Set("client_secret", clientSecret)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", time.Time{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", time.Time{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return "", time.Time{}, fmt.Errorf("oauth token request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var token tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return "", time.Time{}, err
	}
	if token.AccessToken == "" {
		return "", time.Time{}, fmt.Errorf("oauth token response missing access_token")
	}
	var expiresAt time.Time
	if token.ExpiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
	}
	return token.AccessToken, expiresAt, nil
}

func resolveClientCredentialsToken(ctx context.Context, httpClient *http.Client, policy config.PolicyConfig, secret config.Secret, requirement catalog.AuthRequirement, tokenURL, clientID string, keychainResolver func(string) (string, error)) (string, time.Time, error) {
	clientSecret, err := resolveSecretRef(policy, secret.ClientSecret, keychainResolver)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("resolve oauth clientSecret: %w", err)
	}

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	if scopes := joinScopes(secret.Scopes, requirement.Scopes); scopes != "" {
		form.Set("scope", scopes)
	}
	if secret.Audience != "" {
		form.Set("audience", secret.Audience)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", time.Time{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", time.Time{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return "", time.Time{}, fmt.Errorf("oauth token request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var token tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return "", time.Time{}, err
	}
	if token.AccessToken == "" {
		return "", time.Time{}, fmt.Errorf("oauth token response missing access_token")
	}
	var expiresAt time.Time
	if token.ExpiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
	}
	return token.AccessToken, expiresAt, nil
}

func resolveTokenURL(ctx context.Context, httpClient *http.Client, secret config.Secret, requirement catalog.AuthRequirement) (string, error) {
	if tokenURL := selectTokenURL(secret, requirement); tokenURL != "" {
		return tokenURL, nil
	}

	discoveryURL := selectDiscoveryURL(secret, requirement)
	if discoveryURL == "" {
		return "", fmt.Errorf("oauth token endpoint is not configured")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("openid discovery failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var discovery oidcDiscoveryDocument
	if err := json.NewDecoder(resp.Body).Decode(&discovery); err != nil {
		return "", err
	}
	if discovery.TokenEndpoint == "" {
		return "", fmt.Errorf("openid discovery missing token_endpoint")
	}
	return discovery.TokenEndpoint, nil
}

func resolveAuthorizationCodeEndpoints(ctx context.Context, httpClient *http.Client, secret config.Secret, requirement catalog.AuthRequirement) (string, string, error) {
	var authorizationURL string
	if secret.AuthorizationURL != "" {
		authorizationURL = secret.AuthorizationURL
	}
	var tokenURL string
	if secret.TokenURL != "" {
		tokenURL = secret.TokenURL
	}
	for _, flow := range requirement.OAuthFlows {
		if flow.Mode != "authorizationCode" {
			continue
		}
		if authorizationURL == "" {
			authorizationURL = flow.AuthorizationURL
		}
		if tokenURL == "" {
			tokenURL = flow.TokenURL
		}
	}
	if authorizationURL != "" && tokenURL != "" {
		return authorizationURL, tokenURL, nil
	}

	discoveryURL := selectDiscoveryURL(secret, requirement)
	if discoveryURL == "" {
		return "", "", fmt.Errorf("oauth authorization endpoints are not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("openid discovery failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var discovery oidcDiscoveryDocument
	if err := json.NewDecoder(resp.Body).Decode(&discovery); err != nil {
		return "", "", err
	}
	if authorizationURL == "" {
		authorizationURL = discovery.AuthorizationEndpoint
	}
	if tokenURL == "" {
		tokenURL = discovery.TokenEndpoint
	}
	if authorizationURL == "" || tokenURL == "" {
		return "", "", fmt.Errorf("openid discovery missing required authorization endpoints")
	}
	return authorizationURL, tokenURL, nil
}

func selectTokenURL(secret config.Secret, requirement catalog.AuthRequirement) string {
	if secret.TokenURL != "" {
		return secret.TokenURL
	}
	for _, flow := range requirement.OAuthFlows {
		if flow.Mode == secret.Mode && flow.TokenURL != "" {
			return flow.TokenURL
		}
	}
	return ""
}

func selectDiscoveryURL(secret config.Secret, requirement catalog.AuthRequirement) string {
	issuer := secret.Issuer
	if issuer == "" {
		issuer = requirement.OpenIDConnectURL
	}
	if issuer == "" {
		return ""
	}
	if strings.HasSuffix(issuer, "/.well-known/openid-configuration") {
		return issuer
	}
	parsed, err := url.Parse(issuer)
	if err != nil {
		return ""
	}
	parsed.Path = path.Join(parsed.Path, "/.well-known/openid-configuration")
	return parsed.String()
}

func joinScopes(values ...[]string) string {
	seen := map[string]struct{}{}
	var combined []string
	for _, list := range values {
		for _, scope := range list {
			if scope == "" {
				continue
			}
			if _, exists := seen[scope]; exists {
				continue
			}
			seen[scope] = struct{}{}
			combined = append(combined, scope)
		}
	}
	return strings.Join(combined, " ")
}

func prepareRedirectListener(secret config.Secret) (string, *url.URL, net.Listener, error) {
	if secret.RedirectURI != "" {
		redirectURI, err := url.Parse(secret.RedirectURI)
		if err != nil {
			return "", nil, nil, err
		}
		if err := validateLoopbackRedirectURL(redirectURI); err != nil {
			return "", nil, nil, err
		}
		listener, err := net.Listen("tcp", redirectURI.Host)
		if err != nil {
			return "", nil, nil, err
		}
		return secret.RedirectURI, redirectURI, listener, nil
	}
	address := "127.0.0.1:0"
	if secret.CallbackPort != nil {
		address = fmt.Sprintf("127.0.0.1:%d", *secret.CallbackPort)
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return "", nil, nil, err
	}
	redirectURI := fmt.Sprintf("http://%s/callback", listener.Addr().String())
	parsedRedirect, err := url.Parse(redirectURI)
	if err != nil {
		listener.Close()
		return "", nil, nil, err
	}
	return redirectURI, parsedRedirect, listener, nil
}

func validateLoopbackRedirectURL(redirectURI *url.URL) error {
	if redirectURI == nil {
		return fmt.Errorf("redirect URI is required")
	}
	if redirectURI.Scheme != "http" {
		return fmt.Errorf("redirect URI must use loopback http")
	}
	host := redirectURI.Hostname()
	if host == "" {
		return fmt.Errorf("redirect URI must target a loopback host")
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("redirect URI must target a loopback host")
	}
	return nil
}

func randomURLSafeString(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func ResolveStaticSecret(policy config.PolicyConfig, secret config.Secret, keychainResolver func(string) (string, error)) (string, error) {
	return resolveSecretRef(policy, &config.SecretRef{
		Type:    secret.Type,
		Value:   secret.Value,
		Command: append([]string(nil), secret.Command...),
	}, keychainResolver)
}

func tokenCacheKey(providerKey string, secret config.Secret, requirement catalog.AuthRequirement, tokenURL, authorizationURL, clientID, redirectURI string) string {
	return strings.Join([]string{
		providerKey,
		secret.Mode,
		tokenURL,
		authorizationURL,
		secret.Issuer,
		requirement.OpenIDConnectURL,
		clientID,
		redirectURI,
		joinScopes(secret.Scopes, requirement.Scopes),
		secret.Audience,
	}, "|")
}

func redirectCacheValue(secret config.Secret) string {
	if secret.RedirectURI != "" {
		return secret.RedirectURI
	}
	if secret.CallbackPort != nil {
		return fmt.Sprintf("loopback:%d", *secret.CallbackPort)
	}
	return "loopback:auto"
}

func loadCachedToken(secret config.Secret, cacheKey, stateDir string) (string, bool, error) {
	token, err := readCachedToken(secret, cacheKey, stateDir)
	if err != nil {
		return "", false, err
	}
	if token.AccessToken == "" {
		return "", false, nil
	}
	if !token.ExpiresAt.IsZero() && time.Now().After(token.ExpiresAt.Add(-5*time.Second)) {
		return "", false, nil
	}
	return token.AccessToken, true, nil
}

func readCachedToken(secret config.Secret, cacheKey, stateDir string) (cachedToken, error) {
	if effectiveTokenStorage(secret) == "memory" {
		memoryTokenCache.mu.Lock()
		defer memoryTokenCache.mu.Unlock()
		return memoryTokenCache.items[cacheKey], nil
	}
	path := tokenCachePath(stateDir, cacheKey)
	if path == "" {
		return cachedToken{}, nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cachedToken{}, nil
	}
	if err != nil {
		return cachedToken{}, err
	}
	var token cachedToken
	if err := json.Unmarshal(data, &token); err != nil {
		return cachedToken{}, err
	}
	return token, nil
}

func storeCachedToken(secret config.Secret, cacheKey, stateDir string, token cachedToken) error {
	if token.AccessToken == "" {
		return nil
	}
	if effectiveTokenStorage(secret) == "memory" {
		memoryTokenCache.mu.Lock()
		defer memoryTokenCache.mu.Unlock()
		memoryTokenCache.items[cacheKey] = token
		return nil
	}
	path := tokenCachePath(stateDir, cacheKey)
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(token)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func effectiveTokenStorage(secret config.Secret) string {
	if secret.TokenStorage == "memory" {
		return "memory"
	}
	return "instance"
}

func tokenCachePath(stateDir, cacheKey string) string {
	if stateDir == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(cacheKey))
	return filepath.Join(stateDir, "oauth", hex.EncodeToString(sum[:])+".json")
}

func resolveSecretRef(policy config.PolicyConfig, secret *config.SecretRef, keychainResolver func(string) (string, error)) (string, error) {
	if secret == nil {
		return "", fmt.Errorf("missing secret reference")
	}
	switch secret.Type {
	case "literal":
		return secret.Value, nil
	case "env":
		return os.Getenv(secret.Value), nil
	case "file":
		data, err := os.ReadFile(secret.Value)
		return string(data), err
	case "osKeychain":
		if keychainResolver == nil {
			keychainResolver = defaultKeychainResolver
		}
		return keychainResolver(secret.Value)
	case "exec":
		if !policy.AllowExecSecrets {
			return "", fmt.Errorf("exec secrets are disabled")
		}
		command := append([]string(nil), secret.Command...)
		if len(command) == 0 {
			if secret.Value == "" {
				return "", fmt.Errorf("exec secret requires command or value")
			}
			command = []string{secret.Value}
		}
		output, err := exec.Command(command[0], command[1:]...).Output()
		return strings.TrimSpace(string(output)), err
	default:
		return "", fmt.Errorf("unsupported secret type %q", secret.Type)
	}
}

func defaultKeychainResolver(reference string) (string, error) {
	service, account, err := splitKeychainReference(reference)
	if err != nil {
		return "", err
	}
	switch stdruntime.GOOS {
	case "darwin":
		output, err := exec.Command("security", "find-generic-password", "-s", service, "-a", account, "-w").Output()
		return strings.TrimSpace(string(output)), err
	case "linux":
		output, err := exec.Command("secret-tool", "lookup", "service", service, "account", account).Output()
		return strings.TrimSpace(string(output)), err
	default:
		return "", fmt.Errorf("osKeychain secrets are unsupported on %s", stdruntime.GOOS)
	}
}

func splitKeychainReference(reference string) (string, string, error) {
	for _, separator := range []string{"/", ":"} {
		if strings.Contains(reference, separator) {
			parts := strings.SplitN(reference, separator, 2)
			if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
				return parts[0], parts[1], nil
			}
		}
	}
	return "", "", fmt.Errorf("osKeychain secret reference must be service/account")
}

func defaultOpenBrowser(rawURL string) error {
	var command []string
	switch stdruntime.GOOS {
	case "darwin":
		command = []string{"open", rawURL}
	case "linux":
		command = []string{"xdg-open", rawURL}
	default:
		return fmt.Errorf("browser launch is unsupported on %s", stdruntime.GOOS)
	}
	return exec.Command(command[0], command[1:]...).Start()
}
