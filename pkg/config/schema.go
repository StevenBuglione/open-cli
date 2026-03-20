package config

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xeipuuv/gojsonschema"
)

//go:embed cli.schema.json
var configSchema string

var scopeSchema = mustBuildScopeSchema()

func validateDocument(data []byte, partial bool) error {
	schema := configSchema
	if partial {
		schema = scopeSchema
	}
	result, err := gojsonschema.Validate(
		gojsonschema.NewStringLoader(schema),
		gojsonschema.NewBytesLoader(data),
	)
	if err != nil {
		return err
	}
	if result.Valid() {
		return nil
	}

	diagnostics := make([]Diagnostic, 0, len(result.Errors()))
	for _, item := range result.Errors() {
		path := normalizePath(item.Context().String(), item.Description())
		if path == "(root)" {
			path = ""
		}
		if path == "" {
			path = rootPathForDescription(item.Description())
		}
		if shouldSkipSchemaDiagnostic(path, item.Description()) {
			continue
		}
		diagnostics = append(diagnostics, Diagnostic{
			Path:    path,
			Message: normalizeSchemaMessage(path, item.Description()),
		})
	}
	if len(diagnostics) == 0 {
		return nil
	}
	return &ValidationError{Diagnostics: diagnostics}
}

func normalizeSchemaMessage(path, message string) string {
	if path == "runtime.mode" && strings.Contains(message, `must be one of the following`) {
		return "must be local or remote"
	}
	if message != "False always fails validation" {
		return message
	}
	switch {
	case strings.HasSuffix(path, ".transport"):
		return "is only allowed for mcp sources"
	case strings.HasSuffix(path, ".disabledTools"):
		return "is only allowed for mcp sources"
	case strings.HasSuffix(path, ".oauth"):
		return "is only allowed for mcp sources"
	case strings.HasSuffix(path, ".uri"):
		return "is not allowed for mcp sources"
	default:
		return message
	}
}

func shouldSkipSchemaDiagnostic(path, message string) bool {
	if strings.HasPrefix(message, "Must validate") {
		return true
	}
	if message == "Must be greater than or equal to 1" {
		switch path {
		case "runtime.local.heartbeatSeconds", "runtime.local.missedHeartbeatLimit":
			return true
		}
	}
	return false
}

func normalizePath(context, message string) string {
	path := strings.TrimPrefix(context, "(root).")
	if context == "(root)" {
		path = ""
	}
	if property := requiredPropertyName(message); property != "" {
		if path == "" {
			return property
		}
		return path + "." + property
	}
	if path != "" {
		return path
	}
	return rootPathForDescription(message)
}

func requiredPropertyName(message string) string {
	const suffix = " is required"
	if !strings.HasSuffix(message, suffix) {
		return ""
	}
	return strings.Trim(strings.TrimSuffix(message, suffix), `"`)
}

func rootPathForDescription(message string) string {
	if strings.Contains(message, `"cli"`) {
		return "cli"
	}
	if strings.Contains(message, `"mode"`) {
		return "mode"
	}
	if strings.Contains(message, `"runtime"`) {
		return "runtime"
	}
	if strings.Contains(message, `"sources"`) {
		return "sources"
	}
	return ""
}

func validateConfig(cfg Config) error {
	diagnostics := []Diagnostic{}
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := validateDocument(data, false); err != nil {
		validationErr, ok := err.(*ValidationError)
		if !ok {
			return err
		}
		diagnostics = append(diagnostics, validationErr.Diagnostics...)
	}
	if err := validateRuntimeConfig(cfg); err != nil {
		validationErr, ok := err.(*ValidationError)
		if !ok {
			return err
		}
		diagnostics = append(diagnostics, validationErr.Diagnostics...)
	}
	if err := validateMCPConfig(cfg); err != nil {
		validationErr, ok := err.(*ValidationError)
		if !ok {
			return err
		}
		diagnostics = append(diagnostics, validationErr.Diagnostics...)
	}
	if len(diagnostics) > 0 {
		return &ValidationError{Diagnostics: diagnostics}
	}
	return nil
}

func validateRuntimeConfig(cfg Config) error {
	if cfg.Runtime == nil {
		return nil
	}
	diagnostics := []Diagnostic{}
	if cfg.Runtime.Local != nil {
		local := cfg.Runtime.Local
		if local.HeartbeatSeconds <= 0 {
			diagnostics = append(diagnostics, Diagnostic{
				Path:    "runtime.local.heartbeatSeconds",
				Message: "must be a positive integer when runtime.local is configured",
			})
		}
		if local.MissedHeartbeatLimit <= 0 {
			diagnostics = append(diagnostics, Diagnostic{
				Path:    "runtime.local.missedHeartbeatLimit",
				Message: "must be a positive integer when runtime.local is configured",
			})
		}
		if local.Shutdown == "manual" && local.SessionScope != "shared-group" {
			diagnostics = append(diagnostics, Diagnostic{
				Path:    "runtime.local.shutdown",
				Message: `manual shutdown requires sessionScope "shared-group"`,
			})
		}
		switch local.SessionScope {
		case "shared-group":
			if local.Share != "group" {
				diagnostics = append(diagnostics, Diagnostic{
					Path:    "runtime.local.share",
					Message: `shared-group requires share "group"`,
				})
			}
			if local.ShareKey == "" {
				diagnostics = append(diagnostics, Diagnostic{
					Path:    "runtime.local.shareKey",
					Message: `required when sessionScope is "shared-group"`,
				})
			}
		case "terminal", "agent":
			if local.Share != "" && local.Share != "exclusive" {
				diagnostics = append(diagnostics, Diagnostic{
					Path:    "runtime.local.share",
					Message: `exclusive sharing is required for terminal and agent scopes`,
				})
			}
			if local.ShareKey != "" {
				diagnostics = append(diagnostics, Diagnostic{
					Path:    "runtime.local.shareKey",
					Message: `shareKey is only allowed when sessionScope is "shared-group"`,
				})
			}
		}
	}
	if cfg.Runtime.Mode == "remote" && (cfg.Runtime.Remote == nil || cfg.Runtime.Remote.URL == "") {
		diagnostics = append(diagnostics, Diagnostic{
			Path:    "runtime.remote.url",
			Message: "required when runtime.mode is remote",
		})
	}
	if cfg.Runtime.Mode == "remote" && cfg.Runtime.Remote != nil && cfg.Runtime.Remote.OAuth != nil {
		oauth := cfg.Runtime.Remote.OAuth
		switch oauth.Mode {
		case "providedToken":
			if oauth.TokenRef == "" {
				diagnostics = append(diagnostics, Diagnostic{
					Path:    "runtime.remote.oauth.tokenRef",
					Message: "required when runtime.remote.oauth.mode is providedToken",
				})
			}
		case "oauthClient":
			if oauth.Client == nil {
				diagnostics = append(diagnostics, Diagnostic{
					Path:    "runtime.remote.oauth.client",
					Message: "required when runtime.remote.oauth.mode is oauthClient",
				})
				break
			}
			if oauth.Client.TokenURL == "" {
				diagnostics = append(diagnostics, Diagnostic{
					Path:    "runtime.remote.oauth.client.tokenURL",
					Message: "required when runtime.remote.oauth.mode is oauthClient",
				})
			}
			if oauth.Client.ClientID == nil {
				diagnostics = append(diagnostics, Diagnostic{
					Path:    "runtime.remote.oauth.client.clientId",
					Message: "required when runtime.remote.oauth.mode is oauthClient",
				})
			}
			if oauth.Client.ClientSecret == nil {
				diagnostics = append(diagnostics, Diagnostic{
					Path:    "runtime.remote.oauth.client.clientSecret",
					Message: "required when runtime.remote.oauth.mode is oauthClient",
				})
			}
		}
	}
	if cfg.Runtime.Server != nil && cfg.Runtime.Server.Auth != nil {
		auth := cfg.Runtime.Server.Auth
		switch runtimeServerAuthValidationProfile(auth) {
		case "oauth2_introspection":
			if auth.Audience == "" {
				diagnostics = append(diagnostics, Diagnostic{
					Path:    "runtime.server.auth.audience",
					Message: "required when runtime.server.auth.validationProfile is oauth2_introspection",
				})
			}
			if auth.IntrospectionURL == "" {
				diagnostics = append(diagnostics, Diagnostic{
					Path:    "runtime.server.auth.introspectionURL",
					Message: "required when runtime.server.auth.validationProfile is oauth2_introspection",
				})
			}
		case "oidc_jwks":
			if auth.Audience == "" {
				diagnostics = append(diagnostics, Diagnostic{
					Path:    "runtime.server.auth.audience",
					Message: "required when runtime.server.auth.validationProfile is oidc_jwks",
				})
			}
			if auth.Issuer == "" {
				diagnostics = append(diagnostics, Diagnostic{
					Path:    "runtime.server.auth.issuer",
					Message: "required when runtime.server.auth.validationProfile is oidc_jwks",
				})
			}
			if auth.JWKSURL == "" {
				diagnostics = append(diagnostics, Diagnostic{
					Path:    "runtime.server.auth.jwksURL",
					Message: "required when runtime.server.auth.validationProfile is oidc_jwks",
				})
			}
		}
	}
	if len(diagnostics) > 0 {
		return &ValidationError{Diagnostics: diagnostics}
	}
	return nil
}

func runtimeServerAuthValidationProfile(auth *RuntimeServerAuthConfig) string {
	if auth == nil {
		return ""
	}
	switch auth.Mode {
	case "oauth2Introspection":
		return "oauth2_introspection"
	default:
		return auth.ValidationProfile
	}
}

func decodeRawConfig(data []byte) (rawConfig, error) {
	var raw rawConfig
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		return rawConfig{}, err
	}
	return raw, nil
}

func mustBuildScopeSchema() string {
	var schema map[string]any
	if err := json.Unmarshal([]byte(configSchema), &schema); err != nil {
		panic(fmt.Sprintf("decode config schema: %v", err))
	}
	relaxSchema(schema)
	data, err := json.Marshal(schema)
	if err != nil {
		panic(fmt.Sprintf("encode scope schema: %v", err))
	}
	return string(data)
}

func relaxSchema(value any) {
	relaxSchemaWithContext(value, "")
}

func relaxSchemaWithContext(value any, parentKey string) {
	switch typed := value.(type) {
	case map[string]any:
		if parentKey != "if" {
			delete(typed, "required")
		}
		delete(typed, "minProperties")
		for key, nested := range typed {
			relaxSchemaWithContext(nested, key)
		}
	case []any:
		for _, nested := range typed {
			relaxSchemaWithContext(nested, parentKey)
		}
	}
}
