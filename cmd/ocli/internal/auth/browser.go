package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	runtimepkg "github.com/StevenBuglione/open-cli/cmd/ocli/internal/runtime"
	embeddedruntime "github.com/StevenBuglione/open-cli/internal/runtime"
	oauthruntime "github.com/StevenBuglione/open-cli/pkg/auth"
	"github.com/StevenBuglione/open-cli/pkg/catalog"
	configpkg "github.com/StevenBuglione/open-cli/pkg/config"
)

// BrowserLoginMetadata holds the OAuth metadata returned by the runtime's
// browser-login configuration endpoint.
type BrowserLoginMetadata struct {
	AuthorizationURL string `json:"authorizationURL"`
	TokenURL         string `json:"tokenURL"`
	ClientID         string `json:"clientId"`
	Audience         string `json:"audience,omitempty"`
}

// BrowserLoginRequest bundles the parameters needed to perform a browser-based
// OAuth login against the runtime.
type BrowserLoginRequest struct {
	Metadata     BrowserLoginMetadata
	Scopes       []string
	Audience     string
	CallbackPort int
	StateDir     string
}

// BrowserLoginTokenAcquirer is the function used to perform the actual
// browser-login flow. Tests can replace it to avoid real browser interaction.
var BrowserLoginTokenAcquirer = AcquireBrowserLoginToken

// FetchHandshake retrieves the runtime handshake info from the given base URL.
func FetchHandshake(baseURL, configPath string) (embeddedruntime.HandshakeInfo, error) {
	return runtimepkg.GetJSON[embeddedruntime.HandshakeInfo](withConfigQuery(strings.TrimRight(baseURL, "/")+"/v1/runtime/info", configPath), "")
}

// FetchBrowserLoginMetadata fetches, validates, and returns the browser-login
// OAuth metadata from the runtime.
func FetchBrowserLoginMetadata(baseURL, endpoint, configPath string) (BrowserLoginMetadata, error) {
	endpointURL, err := ResolveEndpointURL(baseURL, endpoint)
	if err != nil {
		return BrowserLoginMetadata{}, err
	}
	endpointURL = withConfigQuery(endpointURL, configPath)
	req, err := http.NewRequest(http.MethodGet, endpointURL, nil)
	if err != nil {
		return BrowserLoginMetadata{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return BrowserLoginMetadata{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return BrowserLoginMetadata{}, fmt.Errorf("%s", strings.TrimSpace(string(body)))
	}
	var metadata BrowserLoginMetadata
	if err := json.NewDecoder(resp.Body).Decode(&metadata); err != nil {
		return BrowserLoginMetadata{}, err
	}
	if err := ValidateBrowserLoginMetadata(metadata); err != nil {
		return BrowserLoginMetadata{}, err
	}
	return metadata, nil
}

// ResolveEndpointURL resolves a possibly-relative endpoint path against the
// given runtime base URL.
func ResolveEndpointURL(baseURL, endpoint string) (string, error) {
	if strings.TrimSpace(baseURL) == "" {
		return "", fmt.Errorf("runtime URL is required")
	}
	if strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://") {
		return endpoint, nil
	}
	if endpoint == "" {
		endpoint = "/"
	}
	if !strings.HasPrefix(endpoint, "/") {
		endpoint = "/" + endpoint
	}
	return strings.TrimRight(baseURL, "/") + endpoint, nil
}

func withConfigQuery(endpointURL, configPath string) string {
	if strings.TrimSpace(configPath) == "" {
		return endpointURL
	}
	parsed, err := url.Parse(endpointURL)
	if err != nil {
		return endpointURL
	}
	query := parsed.Query()
	query.Set("config", configPath)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// ValidateBrowserLoginMetadata checks that required fields are present.
func ValidateBrowserLoginMetadata(metadata BrowserLoginMetadata) error {
	switch {
	case strings.TrimSpace(metadata.AuthorizationURL) == "":
		return fmt.Errorf("runtime browser login metadata missing authorizationURL")
	case strings.TrimSpace(metadata.TokenURL) == "":
		return fmt.Errorf("runtime browser login metadata missing tokenURL")
	case strings.TrimSpace(metadata.ClientID) == "":
		return fmt.Errorf("runtime browser login metadata missing clientId")
	default:
		return nil
	}
}

// AcquireBrowserLoginToken performs the full browser-based OAuth flow using the
// given request parameters and returns the access token.
func AcquireBrowserLoginToken(request BrowserLoginRequest) (string, error) {
	secret := configpkg.Secret{
		Type: "oauth2",
		OAuthConfig: configpkg.OAuthConfig{
			Mode:             "authorizationCode",
			AuthorizationURL: request.Metadata.AuthorizationURL,
			TokenURL:         request.Metadata.TokenURL,
			ClientID: &configpkg.SecretRef{
				Type:  "literal",
				Value: request.Metadata.ClientID,
			},
			Scopes:       append([]string(nil), request.Scopes...),
			Audience:     request.Audience,
			TokenStorage: "instance",
		},
	}
	if request.CallbackPort > 0 {
		callbackPort := request.CallbackPort
		secret.CallbackPort = &callbackPort
	}
	requirement := catalog.AuthRequirement{
		Type:   "oauth2",
		Scopes: append([]string(nil), request.Scopes...),
		OAuthFlows: []catalog.OAuthFlow{{
			Mode:             "authorizationCode",
			AuthorizationURL: request.Metadata.AuthorizationURL,
			TokenURL:         request.Metadata.TokenURL,
		}},
	}
	return oauthruntime.ResolveOAuthAccessToken(
		context.Background(),
		http.DefaultClient,
		configpkg.PolicyConfig{},
		secret,
		requirement,
		"runtime.browser."+request.Metadata.AuthorizationURL,
		request.StateDir,
		nil,
	)
}
