package domain

import "time"

// Source represents an external API source (e.g., OpenAPI spec)
type Source struct {
	ID          string
	Kind        string
	DisplayName string
	Status      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// CreateSourceInput contains fields needed to create a source
type CreateSourceInput struct {
	Kind        string
	DisplayName string
}
