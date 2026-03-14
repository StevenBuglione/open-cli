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

	"github.com/StevenBuglione/oas-cli-go/pkg/config"
)

type transportTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

type transportDiscoveryDocument struct {
	TokenEndpoint string `json:"token_endpoint"`
}

type cachedTransportToken struct {
	AccessToken string    `json:"accessToken"`
	ExpiresAt   time.Time `json:"expiresAt,omitempty"`
}

var memoryTransportTokenCache = struct {
	mu    sync.Mutex
	items map[string]cachedTransportToken
}{
	items: map[string]cachedTransportToken{},
}

func resolveTransportHeaders(ctx context.Context, source config.Source, secrets map[string]config.Secret, policy config.PolicyConfig, stateDir string, httpClient *http.Client) (map[string]string, error) {
	headers := map[string]string{}
	if source.Transport != nil {
		for key, value := range source.Transport.Headers {
			headers[key] = value
		}
		for headerName, secretKey := range source.Transport.HeaderSecrets {
			secret, ok := secrets[secretKey]
			if !ok {
				return nil, fmt.Errorf("transport header secret %q not found", secretKey)
			}
			value, err := resolveTransportSecret(policy, secret, nil)
			if err != nil {
				return nil, err
			}
			headers[headerName] = strings.TrimSpace(value)
		}
	}
	if source.OAuth != nil {
		token, err := resolveTransportOAuthToken(ctx, policy, *source.OAuth, source.Transport.URL, stateDir, httpClient, nil)
		if err != nil {
			return nil, err
		}
		headers["Authorization"] = "Bearer " + token
	}
	return headers, nil
}

func resolveTransportOAuthToken(ctx context.Context, policy config.PolicyConfig, oauth config.OAuthConfig, transportURL, stateDir string, httpClient *http.Client, keychainResolver func(string) (string, error)) (string, error) {
	if oauth.Mode != "clientCredentials" {
		return "", fmt.Errorf("transport oauth mode %q is not supported yet", oauth.Mode)
	}
	clientID, err := resolveTransportSecretRef(policy, oauth.ClientID, keychainResolver)
	if err != nil {
		return "", fmt.Errorf("resolve transport oauth clientId: %w", err)
	}
	clientSecret, err := resolveTransportSecretRef(policy, oauth.ClientSecret, keychainResolver)
	if err != nil {
		return "", fmt.Errorf("resolve transport oauth clientSecret: %w", err)
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	tokenURL := oauth.TokenURL
	if tokenURL == "" {
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
		tokenURL = discovery.TokenEndpoint
	}
	cacheKey := transportTokenCacheKey(oauth, transportURL, tokenURL, clientID)
	if token, ok, err := loadCachedTransportToken(oauth, cacheKey, stateDir); err != nil {
		return "", err
	} else if ok {
		return token, nil
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
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("transport oauth token request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var token transportTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return "", err
	}
	if token.AccessToken == "" {
		return "", fmt.Errorf("transport oauth token response missing access_token")
	}
	expiresAt := time.Time{}
	if token.ExpiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
	}
	if err := storeCachedTransportToken(oauth, cacheKey, stateDir, cachedTransportToken{AccessToken: token.AccessToken, ExpiresAt: expiresAt}); err != nil {
		return "", err
	}
	return token.AccessToken, nil
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

func loadCachedTransportToken(oauth config.OAuthConfig, cacheKey, stateDir string) (string, bool, error) {
	token, err := readCachedTransportToken(oauth, cacheKey, stateDir)
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

func readCachedTransportToken(oauth config.OAuthConfig, cacheKey, stateDir string) (cachedTransportToken, error) {
	if effectiveTransportTokenStorage(oauth) == "memory" {
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
	if token.AccessToken == "" {
		return nil
	}
	if effectiveTransportTokenStorage(oauth) == "memory" {
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
