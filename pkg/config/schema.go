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
		if shouldSkipSchemaDiagnostic(item.Description()) {
			continue
		}
		path := normalizePath(item.Context().String(), item.Description())
		if path == "(root)" {
			path = ""
		}
		if path == "" {
			path = rootPathForDescription(item.Description())
		}
		diagnostics = append(diagnostics, Diagnostic{
			Path:    path,
			Message: item.Description(),
		})
	}
	return &ValidationError{Diagnostics: diagnostics}
}

func shouldSkipSchemaDiagnostic(message string) bool {
	return strings.HasPrefix(message, "Must validate")
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
	if strings.Contains(message, `"sources"`) {
		return "sources"
	}
	return ""
}

func validateConfig(cfg Config) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := validateDocument(data, false); err != nil {
		return err
	}
	return validateMCPConfig(cfg)
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
	switch typed := value.(type) {
	case map[string]any:
		delete(typed, "required")
		delete(typed, "minProperties")
		for _, nested := range typed {
			relaxSchema(nested)
		}
	case []any:
		for _, nested := range typed {
			relaxSchema(nested)
		}
	}
}
