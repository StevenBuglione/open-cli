package openapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	mcpclient "github.com/StevenBuglione/oas-cli-go/pkg/mcp/client"
	"github.com/getkin/kin-openapi/openapi3"
)

func BuildDocument(serviceID, sourceID, transport string, tools []mcpclient.ToolDescriptor, disabledTools []string) (*openapi3.T, error) {
	disabled := map[string]struct{}{}
	for _, name := range disabledTools {
		disabled[name] = struct{}{}
	}

	document := &openapi3.T{
		OpenAPI: "3.1.0",
		Info: &openapi3.Info{
			Title:   serviceID,
			Version: "mcp",
		},
		Paths: openapi3.NewPaths(),
	}

	usedPaths := map[string]string{}
	usedOperationIDs := map[string]string{}
	serviceSlug := slugify(serviceID)
	if serviceSlug == "" {
		serviceSlug = "service"
	}

	for _, tool := range tools {
		if tool.Name == "" {
			return nil, fmt.Errorf("mcp tool name is required")
		}
		if _, blocked := disabled[tool.Name]; blocked {
			continue
		}

		operationID := uniqueValue(tool.Name, usedOperationIDs, tool.Name)
		pathSlug := slugify(tool.Name)
		if pathSlug == "" {
			pathSlug = "tool"
		}
		pathValue := uniqueValue(pathSlug, usedPaths, tool.Name)

		schemaRef, inputWrapped, err := schemaForToolInput(tool.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("mcp tool %q: %w", tool.Name, err)
		}

		operation := &openapi3.Operation{
			OperationID: operationID,
			Summary:     tool.Description,
			Description: tool.Description,
			Tags:        []string{serviceID},
			Extensions: map[string]any{
				"x-oascli-backend": map[string]any{
					"kind":         "mcp",
					"sourceId":     sourceID,
					"toolName":     tool.Name,
					"transport":    transport,
					"inputWrapped": inputWrapped,
				},
			},
			RequestBody: &openapi3.RequestBodyRef{
				Value: &openapi3.RequestBody{
					Required: schemaRef != nil,
					Content:  openapi3.NewContentWithJSONSchema(schemaRef.Value),
				},
			},
		}
		document.Paths.Set("/_mcp/"+serviceSlug+"/"+pathValue, &openapi3.PathItem{
			Post: operation,
		})
	}

	return document, nil
}

func schemaForToolInput(raw map[string]any) (*openapi3.SchemaRef, bool, error) {
	if len(raw) == 0 {
		return openapi3.NewObjectSchema().NewRef(), false, nil
	}

	data, err := json.Marshal(raw)
	if err != nil {
		return nil, false, err
	}

	var schemaRef openapi3.SchemaRef
	if err := json.Unmarshal(data, &schemaRef); err != nil {
		return nil, false, err
	}
	if schemaRef.Value == nil {
		return openapi3.NewObjectSchema().NewRef(), false, nil
	}
	if schemaRef.Value.Type != nil && schemaRef.Value.Type.Is("object") {
		return &schemaRef, false, nil
	}

	wrapper := openapi3.NewObjectSchema().
		WithPropertyRef("input", &schemaRef).
		WithRequired([]string{"input"})
	return wrapper.NewRef(), true, nil
}

func uniqueValue(base string, used map[string]string, toolName string) string {
	if _, ok := used[base]; !ok {
		used[base] = toolName
		return base
	}
	sum := sha256.Sum256([]byte(toolName))
	unique := base + "-" + hex.EncodeToString(sum[:4])
	used[unique] = toolName
	return unique
}

func slugify(value string) string {
	if value == "" {
		return ""
	}
	result := make([]rune, 0, len(value))
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'A' && r <= 'Z':
			if len(result) > 0 && !lastDash {
				result = append(result, '-')
			}
			result = append(result, r+('a'-'A'))
			lastDash = false
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			result = append(result, r)
			lastDash = false
		case r == '-' || r == '_' || r == ' ' || r == '.':
			if len(result) > 0 && !lastDash {
				result = append(result, '-')
				lastDash = true
			}
		}
	}
	return string(bytesTrimDash(result))
}

func bytesTrimDash(value []rune) []rune {
	start := 0
	for start < len(value) && value[start] == '-' {
		start++
	}
	end := len(value)
	for end > start && value[end-1] == '-' {
		end--
	}
	return value[start:end]
}
