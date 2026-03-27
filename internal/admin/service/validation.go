package service

import (
	"context"

	"github.com/StevenBuglione/open-cli/internal/admin/domain"
)

// Validator validates sources and discovers services/tools
type Validator struct {
}

// NewValidator creates a new validator
func NewValidator() *Validator {
	return &Validator{}
}

// Validate validates a source and returns discovered services and tools
func (v *Validator) Validate(ctx context.Context, source *domain.Source) (*domain.ValidationResult, error) {
	result := &domain.ValidationResult{
		SourceID: source.ID,
		Valid:    true,
		Errors:   []string{},
		Services: []domain.ServiceCandidate{},
		Tools:    []domain.ToolCandidate{},
	}

	// For now, perform basic validation based on kind
	switch source.Kind {
	case "openapi":
		result.Services = append(result.Services, domain.ServiceCandidate{
			Name:        source.DisplayName,
			Description: "OpenAPI service discovered from " + source.DisplayName,
			Endpoints:   5,
		})
	case "mcp":
		result.Tools = append(result.Tools, domain.ToolCandidate{
			Name:        source.DisplayName + " Tool",
			Description: "MCP tool discovered from " + source.DisplayName,
		})
	default:
		result.Valid = false
		result.Errors = append(result.Errors, "unsupported source kind: "+source.Kind)
	}

	return result, nil
}
