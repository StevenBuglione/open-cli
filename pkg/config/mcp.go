package config

import (
	"fmt"
	"strings"
)

type rawMCPTransport struct {
	Type          *string           `json:"type,omitempty"`
	Command       *string           `json:"command,omitempty"`
	Args          []string          `json:"args,omitempty"`
	Env           map[string]string `json:"env,omitempty"`
	URL           *string           `json:"url,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
	HeaderSecrets map[string]string `json:"headerSecrets,omitempty"`
}

type rawOAuthConfig struct {
	Mode             *string    `json:"mode,omitempty"`
	Issuer           *string    `json:"issuer,omitempty"`
	AuthorizationURL *string    `json:"authorizationURL,omitempty"`
	TokenURL         *string    `json:"tokenURL,omitempty"`
	ClientID         *SecretRef `json:"clientId,omitempty"`
	ClientSecret     *SecretRef `json:"clientSecret,omitempty"`
	Scopes           []string   `json:"scopes,omitempty"`
	Audience         *string    `json:"audience,omitempty"`
	Interactive      *bool      `json:"interactive,omitempty"`
	CallbackPort     *int       `json:"callbackPort,omitempty"`
	RedirectURI      *string    `json:"redirectURI,omitempty"`
	TokenStorage     *string    `json:"tokenStorage,omitempty"`
}

type rawMCPServer struct {
	Type          *string           `json:"type,omitempty"`
	Command       *string           `json:"command,omitempty"`
	Args          []string          `json:"args,omitempty"`
	Env           map[string]string `json:"env,omitempty"`
	URL           *string           `json:"url,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
	HeaderSecrets map[string]string `json:"headerSecrets,omitempty"`
	DisabledTools []string          `json:"disabledTools,omitempty"`
	OAuth         *rawOAuthConfig   `json:"oauth,omitempty"`
}

func validateMCPSourceAmbiguity(raw rawConfig, sourceNames, mcpServerNames map[string]struct{}) error {
	var diagnostics []Diagnostic

	for name := range raw.Sources {
		if _, ok := mcpServerNames[name]; ok {
			diagnostics = append(diagnostics, Diagnostic{
				Path:    "sources." + name,
				Message: "is ambiguous with mcpServers." + name,
			})
		}
	}
	for name := range raw.MCPServers {
		if _, ok := raw.Sources[name]; ok {
			diagnostics = append(diagnostics, Diagnostic{
				Path:    "mcpServers." + name,
				Message: "is ambiguous with sources." + name,
			})
			continue
		}
		if _, ok := sourceNames[name]; ok {
			diagnostics = append(diagnostics, Diagnostic{
				Path:    "mcpServers." + name,
				Message: "is ambiguous with sources." + name,
			})
		}
	}
	if len(diagnostics) > 0 {
		return &ValidationError{Diagnostics: diagnostics}
	}

	for name := range raw.Sources {
		sourceNames[name] = struct{}{}
	}
	for name := range raw.MCPServers {
		mcpServerNames[name] = struct{}{}
	}
	return nil
}

func normalizeMCPServers(raw *rawConfig) error {
	if len(raw.MCPServers) == 0 {
		return nil
	}

	if raw.Sources == nil {
		raw.Sources = map[string]rawSource{}
	}

	var diagnostics []Diagnostic
	for name, server := range raw.MCPServers {
		if _, exists := raw.Sources[name]; exists {
			diagnostics = append(diagnostics, Diagnostic{
				Path:    "mcpServers." + name,
				Message: "is ambiguous with sources." + name,
			})
			continue
		}

		raw.Sources[name] = normalizeRawMCPSource(server)
	}
	if len(diagnostics) > 0 {
		return &ValidationError{Diagnostics: diagnostics}
	}

	raw.MCPServers = nil
	return nil
}

func normalizeMCPSources(current Config, raw *rawConfig) error {
	if len(raw.Sources) == 0 {
		return nil
	}
	if raw.Services == nil {
		raw.Services = map[string]rawService{}
	}

	var diagnostics []Diagnostic
	for name, source := range raw.Sources {
		if effectiveSourceType(current.Sources[name], source) != "mcp" {
			continue
		}
		if err := normalizeMCPService(name, current.Services[name], raw); err != nil {
			validationErr, ok := err.(*ValidationError)
			if !ok {
				return err
			}
			diagnostics = append(diagnostics, validationErr.Diagnostics...)
		}
	}
	if len(diagnostics) > 0 {
		return &ValidationError{Diagnostics: diagnostics}
	}
	return nil
}

func normalizeMCPService(name string, current Service, raw *rawConfig) error {
	service, hasRawService := raw.Services[name]
	if hasRawService {
		if service.Source == nil && current.Source == "" {
			service.Source = stringPtr(name)
			raw.Services[name] = service
		}
		return nil
	}

	if current.Source == "" {
		raw.Services[name] = rawService{Source: stringPtr(name)}
	}

	return nil
}

func effectiveSourceType(current Source, raw rawSource) string {
	if raw.Type != nil {
		return *raw.Type
	}
	return current.Type
}

func normalizeRawMCPSource(server rawMCPServer) rawSource {
	source := rawSource{
		Type: stringPtr("mcp"),
	}
	if transport := normalizeRawMCPTransport(server); transport != nil {
		source.Transport = transport
	}
	if server.DisabledTools != nil {
		source.DisabledTools = copyStrings(server.DisabledTools)
	}
	if server.OAuth != nil {
		source.OAuth = cloneRawOAuth(server.OAuth)
	}
	return source
}

func normalizeRawMCPTransport(server rawMCPServer) *rawMCPTransport {
	transport := &rawMCPTransport{}
	hasFields := false

	if server.Type != nil {
		transport.Type = stringPtr(*server.Type)
		hasFields = true
	}
	if server.Command != nil {
		transport.Command = stringPtr(*server.Command)
		hasFields = true
	}
	if server.Args != nil {
		transport.Args = copyStrings(server.Args)
		hasFields = true
	}
	if server.Env != nil {
		transport.Env = copyStringMap(server.Env)
		hasFields = true
	}
	if server.URL != nil {
		transport.URL = stringPtr(*server.URL)
		hasFields = true
	}
	if server.Headers != nil {
		transport.Headers = copyStringMap(server.Headers)
		hasFields = true
	}
	if server.HeaderSecrets != nil {
		transport.HeaderSecrets = copyStringMap(server.HeaderSecrets)
		hasFields = true
	}

	if !hasFields {
		return nil
	}
	return transport
}

func mergeRawTransport(current *MCPTransport, raw *rawMCPTransport) *MCPTransport {
	if raw == nil {
		return current
	}
	if raw.Type != nil {
		if current == nil {
			current = &MCPTransport{}
		} else if current.Type != *raw.Type {
			resetTransportExclusiveFields(current, *raw.Type)
		}
		current.Type = *raw.Type
	} else if current == nil {
		current = &MCPTransport{}
	}

	if raw.Command != nil {
		current.Command = *raw.Command
	}
	if raw.Args != nil {
		current.Args = copyStrings(raw.Args)
	}
	if raw.Env != nil {
		current.Env = copyStringMap(raw.Env)
	}
	if raw.URL != nil {
		current.URL = *raw.URL
	}
	if raw.Headers != nil {
		current.Headers = copyStringMap(raw.Headers)
	}
	if raw.HeaderSecrets != nil {
		current.HeaderSecrets = copyStringMap(raw.HeaderSecrets)
	}

	return current
}

func mergeRawOAuth(current *OAuthConfig, raw *rawOAuthConfig) *OAuthConfig {
	if raw == nil {
		return current
	}
	if raw.Mode != nil {
		if current == nil {
			current = &OAuthConfig{}
		} else if current.Mode != *raw.Mode {
			resetOAuthExclusiveFields(current, *raw.Mode)
		}
		current.Mode = *raw.Mode
	} else if current == nil {
		current = &OAuthConfig{}
	}

	if raw.Issuer != nil {
		current.Issuer = *raw.Issuer
	}
	if raw.AuthorizationURL != nil {
		current.AuthorizationURL = *raw.AuthorizationURL
	}
	if raw.TokenURL != nil {
		current.TokenURL = *raw.TokenURL
	}
	if raw.ClientID != nil {
		current.ClientID = cloneSecretRef(raw.ClientID)
	}
	if raw.ClientSecret != nil {
		current.ClientSecret = cloneSecretRef(raw.ClientSecret)
	}
	if raw.Scopes != nil {
		current.Scopes = copyStrings(raw.Scopes)
	}
	if raw.Audience != nil {
		current.Audience = *raw.Audience
	}
	if raw.Interactive != nil {
		current.Interactive = boolPtr(*raw.Interactive)
	}
	if raw.CallbackPort != nil {
		current.CallbackPort = intPtr(*raw.CallbackPort)
	}
	if raw.RedirectURI != nil {
		current.RedirectURI = *raw.RedirectURI
	}
	if raw.TokenStorage != nil {
		current.TokenStorage = *raw.TokenStorage
	}

	return current
}

func cloneRawOAuth(raw *rawOAuthConfig) *rawOAuthConfig {
	if raw == nil {
		return nil
	}

	clone := &rawOAuthConfig{
		Scopes: copyStrings(raw.Scopes),
	}
	if raw.Mode != nil {
		clone.Mode = stringPtr(*raw.Mode)
	}
	if raw.Issuer != nil {
		clone.Issuer = stringPtr(*raw.Issuer)
	}
	if raw.AuthorizationURL != nil {
		clone.AuthorizationURL = stringPtr(*raw.AuthorizationURL)
	}
	if raw.TokenURL != nil {
		clone.TokenURL = stringPtr(*raw.TokenURL)
	}
	if raw.ClientID != nil {
		clone.ClientID = cloneSecretRef(raw.ClientID)
	}
	if raw.ClientSecret != nil {
		clone.ClientSecret = cloneSecretRef(raw.ClientSecret)
	}
	if raw.Audience != nil {
		clone.Audience = stringPtr(*raw.Audience)
	}
	if raw.Interactive != nil {
		clone.Interactive = boolPtr(*raw.Interactive)
	}
	if raw.CallbackPort != nil {
		clone.CallbackPort = intPtr(*raw.CallbackPort)
	}
	if raw.RedirectURI != nil {
		clone.RedirectURI = stringPtr(*raw.RedirectURI)
	}
	if raw.TokenStorage != nil {
		clone.TokenStorage = stringPtr(*raw.TokenStorage)
	}

	return clone
}

func validateMCPConfig(cfg Config) error {
	var diagnostics []Diagnostic

	for name, source := range cfg.Sources {
		prefix := "sources." + name

		if source.Type != "mcp" {
			if source.Transport != nil {
				diagnostics = append(diagnostics, Diagnostic{
					Path:    prefix + ".transport",
					Message: "is only allowed for mcp sources",
				})
			}
			if len(source.DisabledTools) > 0 {
				diagnostics = append(diagnostics, Diagnostic{
					Path:    prefix + ".disabledTools",
					Message: "is only allowed for mcp sources",
				})
			}
			if source.OAuth != nil {
				diagnostics = append(diagnostics, Diagnostic{
					Path:    prefix + ".oauth",
					Message: "is only allowed for mcp sources",
				})
			}
			continue
		}

		if source.URI != "" {
			diagnostics = append(diagnostics, Diagnostic{
				Path:    prefix + ".uri",
				Message: "is not allowed for mcp sources",
			})
		}

		if source.Transport == nil {
			diagnostics = append(diagnostics, Diagnostic{
				Path:    prefix + ".transport",
				Message: "is required for mcp sources",
			})
			continue
		}
		if source.Transport.Type == "" {
			diagnostics = append(diagnostics, Diagnostic{
				Path:    prefix + ".transport.type",
				Message: "is required for mcp sources",
			})
			continue
		}

		switch source.Transport.Type {
		case "stdio":
			if source.Transport.Command == "" {
				diagnostics = append(diagnostics, Diagnostic{
					Path:    prefix + ".transport.command",
					Message: "is required for stdio transport",
				})
			}
			if source.Transport.URL != "" {
				diagnostics = append(diagnostics, Diagnostic{
					Path:    prefix + ".transport.url",
					Message: "is not allowed for stdio transport",
				})
			}
			if len(source.Transport.Headers) > 0 {
				diagnostics = append(diagnostics, Diagnostic{
					Path:    prefix + ".transport.headers",
					Message: "is not allowed for stdio transport",
				})
			}
			if len(source.Transport.HeaderSecrets) > 0 {
				diagnostics = append(diagnostics, Diagnostic{
					Path:    prefix + ".transport.headerSecrets",
					Message: "is not allowed for stdio transport",
				})
			}
			if source.OAuth != nil {
				diagnostics = append(diagnostics, Diagnostic{
					Path:    prefix + ".oauth",
					Message: "is not allowed for stdio transport",
				})
			}
		case "sse":
			if source.Transport.URL == "" {
				diagnostics = append(diagnostics, Diagnostic{
					Path:    prefix + ".transport.url",
					Message: "is required for sse transport",
				})
			}
			if source.Transport.Command != "" {
				diagnostics = append(diagnostics, Diagnostic{
					Path:    prefix + ".transport.command",
					Message: "is not allowed for sse transport",
				})
			}
			if len(source.Transport.Args) > 0 {
				diagnostics = append(diagnostics, Diagnostic{
					Path:    prefix + ".transport.args",
					Message: "is not allowed for sse transport",
				})
			}
			if len(source.Transport.Env) > 0 {
				diagnostics = append(diagnostics, Diagnostic{
					Path:    prefix + ".transport.env",
					Message: "is not allowed for sse transport",
				})
			}
			if source.OAuth != nil {
				diagnostics = append(diagnostics, Diagnostic{
					Path:    prefix + ".oauth",
					Message: "is not allowed for sse transport",
				})
			}
		case "streamable-http":
			if source.Transport.URL == "" {
				diagnostics = append(diagnostics, Diagnostic{
					Path:    prefix + ".transport.url",
					Message: "is required for streamable-http transport",
				})
			}
			if source.Transport.Command != "" {
				diagnostics = append(diagnostics, Diagnostic{
					Path:    prefix + ".transport.command",
					Message: "is not allowed for streamable-http transport",
				})
			}
			if len(source.Transport.Args) > 0 {
				diagnostics = append(diagnostics, Diagnostic{
					Path:    prefix + ".transport.args",
					Message: "is not allowed for streamable-http transport",
				})
			}
			if len(source.Transport.Env) > 0 {
				diagnostics = append(diagnostics, Diagnostic{
					Path:    prefix + ".transport.env",
					Message: "is not allowed for streamable-http transport",
				})
			}
			diagnostics = append(diagnostics, validateOAuthConfig(prefix+".oauth", source.OAuth)...)
		}

		if source.Transport != nil {
			if key, ok := findHeaderKey(source.Transport.Headers, "Authorization"); ok && source.OAuth != nil {
				diagnostics = append(diagnostics, Diagnostic{
					Path:    prefix + ".transport.headers." + key,
					Message: "is owned by oauth and cannot be set explicitly",
				})
			}
			if key, ok := findHeaderKey(source.Transport.HeaderSecrets, "Authorization"); ok && source.OAuth != nil {
				diagnostics = append(diagnostics, Diagnostic{
					Path:    prefix + ".transport.headerSecrets." + key,
					Message: "is owned by oauth and cannot be set explicitly",
				})
			}
			for secretKey, headerKey := range overlappingHeaderKeys(source.Transport.Headers, source.Transport.HeaderSecrets) {
				diagnostics = append(diagnostics, Diagnostic{
					Path:    prefix + ".transport.headerSecrets." + secretKey,
					Message: fmt.Sprintf("duplicates transport.headers.%s", headerKey),
				})
			}
		}
	}

	if len(diagnostics) > 0 {
		return &ValidationError{Diagnostics: diagnostics}
	}
	return nil
}

func validateOAuthConfig(path string, oauth *OAuthConfig) []Diagnostic {
	if oauth == nil {
		return nil
	}

	var diagnostics []Diagnostic
	switch oauth.Mode {
	case "clientCredentials":
		if oauth.ClientID == nil {
			diagnostics = append(diagnostics, Diagnostic{
				Path:    path + ".clientId",
				Message: "is required for clientCredentials oauth",
			})
		}
		if oauth.ClientSecret == nil {
			diagnostics = append(diagnostics, Diagnostic{
				Path:    path + ".clientSecret",
				Message: "is required for clientCredentials oauth",
			})
		}
		if oauth.Issuer == "" && oauth.TokenURL == "" {
			diagnostics = append(diagnostics, Diagnostic{
				Path:    path,
				Message: "requires issuer or tokenURL for clientCredentials oauth",
			})
		}
		if oauth.AuthorizationURL != "" {
			diagnostics = append(diagnostics, Diagnostic{
				Path:    path + ".authorizationURL",
				Message: "is not allowed for clientCredentials oauth",
			})
		}
		if oauth.Interactive != nil {
			diagnostics = append(diagnostics, Diagnostic{
				Path:    path + ".interactive",
				Message: "is not allowed for clientCredentials oauth",
			})
		}
		if oauth.CallbackPort != nil {
			diagnostics = append(diagnostics, Diagnostic{
				Path:    path + ".callbackPort",
				Message: "is not allowed for clientCredentials oauth",
			})
		}
		if oauth.RedirectURI != "" {
			diagnostics = append(diagnostics, Diagnostic{
				Path:    path + ".redirectURI",
				Message: "is not allowed for clientCredentials oauth",
			})
		}
	case "openIdConnect":
		diagnostics = append(diagnostics, Diagnostic{
			Path:    path + ".mode",
			Message: "is not a direct oauth mode",
		})
	default:
		diagnostics = append(diagnostics, Diagnostic{
			Path:    path + ".mode",
			Message: "must be clientCredentials for mcp transport oauth",
		})
	}
	return diagnostics
}

func copyStrings(values []string) []string {
	if values == nil {
		return nil
	}
	return append([]string(nil), values...)
}

func copyStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}

	clone := make(map[string]string, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}

func cloneSecretRef(secret *SecretRef) *SecretRef {
	if secret == nil {
		return nil
	}

	clone := *secret
	if secret.Command != nil {
		clone.Command = copyStrings(secret.Command)
	}
	return &clone
}

func stringPtr(value string) *string {
	return &value
}

func boolPtr(value bool) *bool {
	return &value
}

func intPtr(value int) *int {
	return &value
}

func isZeroSource(source Source) bool {
	return source.Type == "" &&
		source.URI == "" &&
		!source.Enabled &&
		source.Refresh == nil &&
		source.Transport == nil &&
		len(source.DisabledTools) == 0 &&
		source.OAuth == nil
}

func resetOAuthExclusiveFields(oauth *OAuthConfig, mode string) {
	if mode != "clientCredentials" {
		return
	}

	oauth.AuthorizationURL = ""
	oauth.Interactive = nil
	oauth.CallbackPort = nil
	oauth.RedirectURI = ""
}

func resetTransportExclusiveFields(transport *MCPTransport, transportType string) {
	switch transportType {
	case "stdio":
		transport.URL = ""
		transport.Headers = nil
		transport.HeaderSecrets = nil
	case "sse", "streamable-http":
		transport.Command = ""
		transport.Args = nil
		transport.Env = nil
	}
}

func findHeaderKey(values map[string]string, target string) (string, bool) {
	for key := range values {
		if strings.EqualFold(key, target) {
			return key, true
		}
	}
	return "", false
}

func overlappingHeaderKeys(headers, headerSecrets map[string]string) map[string]string {
	if len(headers) == 0 || len(headerSecrets) == 0 {
		return nil
	}

	headerKeys := make(map[string]string, len(headers))
	for key := range headers {
		headerKeys[strings.ToLower(key)] = key
	}

	overlaps := map[string]string{}
	for key := range headerSecrets {
		if headerKey, ok := headerKeys[strings.ToLower(key)]; ok {
			overlaps[key] = headerKey
		}
	}
	if len(overlaps) == 0 {
		return nil
	}
	return overlaps
}
