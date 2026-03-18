package client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
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

	"github.com/StevenBuglione/open-cli/pkg/config"
)

type transportTokenResponse struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	TokenType        string `json:"token_type"`
	ExpiresIn        int    `json:"expires_in"`
	RefreshExpiresIn int    `json:"refresh_expires_in"`
}

type transportDiscoveryDocument struct {
	TokenEndpoint string `json:"token_endpoint"`
}

type cachedTransportToken struct {
	AccessToken      string    `json:"accessToken"`
	RefreshToken     string    `json:"refreshToken,omitempty"`
	ExpiresAt        time.Time `json:"expiresAt,omitempty"`
	RefreshExpiresAt time.Time `json:"refreshExpiresAt,omitempty"`
	RefreshUsed      bool      `json:"refreshUsed,omitempty"`
	RefreshFailed    bool      `json:"refreshFailed,omitempty"`
}

var memoryTransportTokenCache = struct {
	mu    sync.Mutex
	items map[string]cachedTransportToken
}{
	items: map[string]cachedTransportToken{},
}

var transportTokenKeyLocks = struct {
	mu    sync.Mutex
	items map[string]*sync.Mutex
}{
	items: map[string]*sync.Mutex{},
}

func resolveTransportHeaders(ctx context.Context, source config.Source, secrets map[string]config.Secret, policy config.PolicyConfig, stateDir string, httpClient *http.Client) (map[string]string, *cachedTransportToken, error) {
	headers := map[string]string{}
	if source.Transport != nil {
		for key, value := range source.Transport.Headers {
			headers[key] = value
		}
		for headerName, secretKey := range source.Transport.HeaderSecrets {
			secret, ok := secrets[secretKey]
			if !ok {
				return nil, nil, fmt.Errorf("transport header secret %q not found", secretKey)
			}
			value, err := resolveTransportSecret(policy, secret, nil)
			if err != nil {
				return nil, nil, err
			}
			headers[headerName] = strings.TrimSpace(value)
		}
	}
	if source.OAuth != nil {
		token, err := resolveTransportOAuthToken(ctx, policy, *source.OAuth, source.Transport.URL, stateDir, httpClient, nil)
		if err != nil {
			return nil, nil, err
		}
		headers["Authorization"] = "Bearer " + token.AccessToken
		return headers, &token, nil
	}
	return headers, nil, nil
}

func resolveTransportOAuthToken(ctx context.Context, policy config.PolicyConfig, oauth config.OAuthConfig, transportURL, stateDir string, httpClient *http.Client, keychainResolver func(string) (string, error)) (cachedTransportToken, error) {
	if oauth.Mode != "clientCredentials" {
		return cachedTransportToken{}, fmt.Errorf("transport oauth mode %q is not supported yet", oauth.Mode)
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	tokenURLHint := unresolvedTransportTokenURL(oauth)
	cacheKey := transportTokenCacheKey(oauth, transportURL, tokenURLHint, unresolvedTransportClientID(oauth))
	unlock := lockTransportTokenKey(cacheKey)
	cached, err := readCachedTransportToken(oauth, cacheKey, stateDir)
	unlock()
	if err != nil {
		return cachedTransportToken{}, err
	}
	if token, ok := usableCachedTransportToken(cached); ok {
		cached.AccessToken = token
		return cached, nil
	}
	clientID, err := resolveTransportSecretRef(policy, oauth.ClientID, keychainResolver)
	if err != nil {
		return cachedTransportToken{}, fmt.Errorf("resolve transport oauth clientId: %w", err)
	}
	clientSecret, err := resolveTransportSecretRef(policy, oauth.ClientSecret, keychainResolver)
	if err != nil {
		return cachedTransportToken{}, fmt.Errorf("resolve transport oauth clientSecret: %w", err)
	}
	tokenURL, err := resolveTransportTokenURL(ctx, oauth, oauth.TokenURL, httpClient)
	if err != nil {
		return cachedTransportToken{}, err
	}
	cacheKey = transportTokenCacheKey(oauth, transportURL, tokenURL, clientID)
	unlock = lockTransportTokenKey(cacheKey)
	defer unlock()
	cached, err = readCachedTransportToken(oauth, cacheKey, stateDir)
	if err != nil {
		return cachedTransportToken{}, err
	}
	if token, ok := usableCachedTransportToken(cached); ok {
		cached.AccessToken = token
		return cached, nil
	}
	if token, ok, err := refreshCachedTransportOAuthToken(ctx, policy, oauth, tokenURL, clientID, cacheKey, stateDir, cached, httpClient, keychainResolver); err != nil {
		return cachedTransportToken{}, err
	} else if ok {
		refreshed, err := readCachedTransportToken(oauth, cacheKey, stateDir)
		if err != nil {
			return cachedTransportToken{}, err
		}
		refreshed.AccessToken = token
		return refreshed, nil
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

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return cachedTransportToken{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return cachedTransportToken{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return cachedTransportToken{}, fmt.Errorf("transport oauth token request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var token transportTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return cachedTransportToken{}, err
	}
	if token.AccessToken == "" {
		return cachedTransportToken{}, fmt.Errorf("transport oauth token response missing access_token")
	}
	resolved := cachedTransportTokenFromResponse(token, "", false)
	if err := storeCachedTransportToken(oauth, cacheKey, stateDir, resolved); err != nil {
		return cachedTransportToken{}, err
	}
	return resolved, nil
}

func transportTokenCacheKey(oauth config.OAuthConfig, transportURL, tokenURL, clientID string) string {
	return strings.Join([]string{
		transportURL,
		oauth.Mode,
		tokenURL,
		oauth.Issuer,
		clientID,
		strings.Join(oauth.Scopes, " "),
		oauth.Audience,
	}, "|")
}

func usableCachedTransportToken(token cachedTransportToken) (string, bool) {
	if token.AccessToken == "" {
		return "", false
	}
	if transportTokenExpiresSoon(token.ExpiresAt) {
		return "", false
	}
	return token.AccessToken, true
}

func transportTokenExpiresSoon(expiresAt time.Time) bool {
	return !expiresAt.IsZero() && time.Now().After(expiresAt.Add(-5*time.Second))
}

func cachedTransportTokenFromResponse(token transportTokenResponse, previousRefreshToken string, refreshUsed bool) cachedTransportToken {
	refreshToken := token.RefreshToken
	if refreshToken == "" {
		refreshToken = previousRefreshToken
	}
	return cachedTransportToken{
		AccessToken:      token.AccessToken,
		RefreshToken:     refreshToken,
		ExpiresAt:        transportExpiryFromSeconds(token.ExpiresIn),
		RefreshExpiresAt: transportExpiryFromSeconds(token.RefreshExpiresIn),
		RefreshUsed:      refreshUsed,
	}
}

func transportExpiryFromSeconds(seconds int) time.Time {
	if seconds <= 0 {
		return time.Time{}
	}
	return time.Now().Add(time.Duration(seconds) * time.Second)
}

func refreshCachedTransportOAuthToken(ctx context.Context, policy config.PolicyConfig, oauth config.OAuthConfig, tokenURL, clientID, cacheKey, stateDir string, cached cachedTransportToken, httpClient *http.Client, keychainResolver func(string) (string, error)) (string, bool, error) {
	if cached.RefreshFailed {
		return "", false, fmt.Errorf("transport oauth token refresh already failed; reauthorization required")
	}
	if cached.RefreshUsed {
		return "", false, fmt.Errorf("transport oauth token expired after single refresh; reauthorization required")
	}
	if cached.AccessToken == "" || !transportTokenExpiresSoon(cached.ExpiresAt) {
		return "", false, nil
	}
	if cached.RefreshToken == "" {
		return "", false, fmt.Errorf("transport oauth token expired and refresh is unavailable; reauthorization required")
	}
	if transportTokenExpiresSoon(cached.RefreshExpiresAt) {
		return "", false, fmt.Errorf("transport oauth refresh token expired; reauthorization required")
	}

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", cached.RefreshToken)
	form.Set("client_id", clientID)
	if oauth.ClientSecret != nil {
		clientSecret, err := resolveTransportSecretRef(policy, oauth.ClientSecret, keychainResolver)
		if err != nil {
			if storeErr := markCachedTransportRefreshFailed(oauth, cacheKey, stateDir, cached); storeErr != nil {
				return "", false, storeErr
			}
			return "", false, fmt.Errorf("resolve transport oauth clientSecret: %w", err)
		}
		form.Set("client_secret", clientSecret)
	}
	if len(oauth.Scopes) > 0 {
		form.Set("scope", strings.Join(oauth.Scopes, " "))
	}
	if oauth.Audience != "" {
		form.Set("audience", oauth.Audience)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", false, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		if storeErr := markCachedTransportRefreshFailed(oauth, cacheKey, stateDir, cached); storeErr != nil {
			return "", false, storeErr
		}
		return "", false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		if storeErr := markCachedTransportRefreshFailed(oauth, cacheKey, stateDir, cached); storeErr != nil {
			return "", false, storeErr
		}
		return "", false, fmt.Errorf("transport oauth refresh request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var token transportTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		if storeErr := markCachedTransportRefreshFailed(oauth, cacheKey, stateDir, cached); storeErr != nil {
			return "", false, storeErr
		}
		return "", false, err
	}
	if token.AccessToken == "" {
		if storeErr := markCachedTransportRefreshFailed(oauth, cacheKey, stateDir, cached); storeErr != nil {
			return "", false, storeErr
		}
		return "", false, fmt.Errorf("transport oauth token response missing access_token")
	}
	refreshed := cachedTransportTokenFromResponse(token, cached.RefreshToken, true)
	if err := storeCachedTransportToken(oauth, cacheKey, stateDir, refreshed); err != nil {
		return "", false, err
	}
	return refreshed.AccessToken, true, nil
}

func markCachedTransportRefreshFailed(oauth config.OAuthConfig, cacheKey, stateDir string, cached cachedTransportToken) error {
	failed := cached
	failed.RefreshUsed = true
	failed.RefreshFailed = true
	failed.AccessToken = ""
	failed.ExpiresAt = time.Time{}
	return storeCachedTransportToken(oauth, cacheKey, stateDir, failed)
}

func readCachedTransportToken(oauth config.OAuthConfig, cacheKey, stateDir string) (cachedTransportToken, error) {
	if useInMemoryTransportTokenCache(oauth, stateDir) {
		memoryTransportTokenCache.mu.Lock()
		defer memoryTransportTokenCache.mu.Unlock()
		return memoryTransportTokenCache.items[cacheKey], nil
	}
	path := transportTokenCachePath(stateDir, cacheKey)
	if path == "" {
		return cachedTransportToken{}, nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cachedTransportToken{}, nil
	}
	if err != nil {
		return cachedTransportToken{}, err
	}
	var token cachedTransportToken
	if err := json.Unmarshal(data, &token); err != nil {
		return cachedTransportToken{}, err
	}
	return token, nil
}

func storeCachedTransportToken(oauth config.OAuthConfig, cacheKey, stateDir string, token cachedTransportToken) error {
	if token.AccessToken == "" && token.RefreshToken == "" && !token.RefreshUsed && !token.RefreshFailed {
		return nil
	}
	if useInMemoryTransportTokenCache(oauth, stateDir) {
		memoryTransportTokenCache.mu.Lock()
		defer memoryTransportTokenCache.mu.Unlock()
		memoryTransportTokenCache.items[cacheKey] = token
		return nil
	}
	path := transportTokenCachePath(stateDir, cacheKey)
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

func effectiveTransportTokenStorage(oauth config.OAuthConfig) string {
	if oauth.TokenStorage == "memory" {
		return "memory"
	}
	return "instance"
}

func useInMemoryTransportTokenCache(oauth config.OAuthConfig, stateDir string) bool {
	return effectiveTransportTokenStorage(oauth) == "memory" || stateDir == ""
}

func resolveTransportTokenURL(ctx context.Context, oauth config.OAuthConfig, tokenURL string, httpClient *http.Client) (string, error) {
	if tokenURL != "" {
		return tokenURL, nil
	}
	discoveryURL := transportDiscoveryURL(oauth)
	if discoveryURL == "" {
		return "", fmt.Errorf("transport oauth tokenURL is required")
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
		return "", fmt.Errorf("transport oauth discovery failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var discovery transportDiscoveryDocument
	if err := json.NewDecoder(resp.Body).Decode(&discovery); err != nil {
		return "", err
	}
	if discovery.TokenEndpoint == "" {
		return "", fmt.Errorf("transport oauth discovery missing token_endpoint")
	}
	return discovery.TokenEndpoint, nil
}

func unresolvedTransportTokenURL(oauth config.OAuthConfig) string {
	if oauth.TokenURL != "" {
		return oauth.TokenURL
	}
	if oauth.Issuer != "" {
		return oauth.Issuer
	}
	return "discovery"
}

func unresolvedTransportClientID(oauth config.OAuthConfig) string {
	if oauth.ClientID == nil {
		return ""
	}
	return oauth.ClientID.Type + ":" + oauth.ClientID.Value
}

func lockTransportTokenKey(cacheKey string) func() {
	transportTokenKeyLocks.mu.Lock()
	lock, ok := transportTokenKeyLocks.items[cacheKey]
	if !ok {
		lock = &sync.Mutex{}
		transportTokenKeyLocks.items[cacheKey] = lock
	}
	transportTokenKeyLocks.mu.Unlock()
	lock.Lock()
	return lock.Unlock
}

func transportTokenCachePath(stateDir, cacheKey string) string {
	if stateDir == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(cacheKey))
	return filepath.Join(stateDir, "oauth", "mcp-transport-"+hex.EncodeToString(sum[:])+".json")
}

func transportDiscoveryURL(oauth config.OAuthConfig) string {
	if oauth.Issuer == "" {
		return ""
	}
	if strings.HasSuffix(oauth.Issuer, "/.well-known/openid-configuration") {
		return oauth.Issuer
	}
	parsed, err := url.Parse(oauth.Issuer)
	if err != nil {
		return ""
	}
	parsed.Path = path.Join(parsed.Path, "/.well-known/openid-configuration")
	return parsed.String()
}

func resolveTransportSecret(policy config.PolicyConfig, secret config.Secret, keychainResolver func(string) (string, error)) (string, error) {
	return resolveTransportSecretRef(policy, &config.SecretRef{
		Type:    secret.Type,
		Value:   secret.Value,
		Command: append([]string(nil), secret.Command...),
	}, keychainResolver)
}

func resolveTransportSecretRef(policy config.PolicyConfig, secret *config.SecretRef, keychainResolver func(string) (string, error)) (string, error) {
	if secret == nil {
		return "", fmt.Errorf("missing secret reference")
	}
	switch secret.Type {
	case "env":
		return os.Getenv(secret.Value), nil
	case "file":
		data, err := os.ReadFile(secret.Value)
		return string(data), err
	case "osKeychain":
		if keychainResolver == nil {
			keychainResolver = defaultTransportKeychainResolver
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

func defaultTransportKeychainResolver(reference string) (string, error) {
	service, account, err := splitTransportKeychainReference(reference)
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

func splitTransportKeychainReference(reference string) (string, string, error) {
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
