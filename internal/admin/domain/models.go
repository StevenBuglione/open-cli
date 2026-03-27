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

// ValidationResult represents the result of validating a source
type ValidationResult struct {
	SourceID string
	Valid    bool
	Errors   []string
	Services []ServiceCandidate
	Tools    []ToolCandidate
}

// ServiceCandidate represents a service discovered from a source
type ServiceCandidate struct {
	Name        string
	Description string
	Endpoints   int
}

// ToolCandidate represents a tool discovered from a source
type ToolCandidate struct {
	Name        string
	Description string
}

// Bundle represents an access package that can be assigned to principals
type Bundle struct {
	ID          string
	Name        string
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// CreateBundleInput contains fields needed to create a bundle
type CreateBundleInput struct {
	Name        string
	Description string
}

// UpdateBundleInput contains fields to update a bundle
type UpdateBundleInput struct {
	Name        string
	Description string
}

// BundleAssignment represents a bundle assigned to a principal (user or group)
type BundleAssignment struct {
	ID            string
	BundleID      string
	PrincipalType string // "user" or "group"
	PrincipalID   string
	CreatedAt     time.Time
}

// CreateBundleAssignmentInput contains fields needed to create an assignment
type CreateBundleAssignmentInput struct {
	BundleID      string
	PrincipalType string
	PrincipalID   string
}

// Revision represents a versioned snapshot of a bundle's configuration
type Revision struct {
	ID           string
	BundleID     string
	Status       string // "draft", "published"
	CreatedBy    string
	CreatedAt    time.Time
	PublishedAt  *time.Time
	SnapshotHash string
}

// CompiledSnapshot represents a runtime-ready configuration snapshot
type CompiledSnapshot struct {
	BundleID   string
	BundleName string
	Sources    map[string]SnapshotSource
	Services   map[string]SnapshotService
	CompiledAt time.Time
}

// SnapshotSource represents a source in a compiled snapshot
type SnapshotSource struct {
	Type    string
	URI     string
	Enabled bool
}

// SnapshotService represents a service in a compiled snapshot
type SnapshotService struct {
	Source string
	Alias  string
}

// RevisionDiff represents the difference between two revisions
type RevisionDiff struct {
	FromRevisionID  string
	ToRevisionID    string
	SourcesAdded    []string
	SourcesRemoved  []string
	SourcesChanged  []string
	ServicesAdded   []string
	ServicesRemoved []string
}

// AdminAuditEvent represents an auditable admin action
type AdminAuditEvent struct {
	ID           string
	Timestamp    time.Time
	AdminID      string
	Action       string // CREATE_BUNDLE, UPDATE_BUNDLE, DELETE_BUNDLE, etc.
	ResourceType string // bundle, source, assignment, revision
	ResourceID   string
	Changes      map[string]interface{} // old/new values for change tracking
	Success      bool
	ErrorMessage string
}

// AuditEventFilter contains filtering criteria for audit event queries
type AuditEventFilter struct {
	AdminID      string
	Action       string
	ResourceType string
	ResourceID   string
	StartTime    *time.Time
	EndTime      *time.Time
	Limit        int
	Offset       int
}
