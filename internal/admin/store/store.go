package store

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/StevenBuglione/open-cli/internal/admin/domain"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

//go:embed schema.sql
var schema string

// Store provides persistence for admin control-plane state
type Store struct {
	db *sql.DB
}

// New creates a new Store with the given database connection
func New(db *sql.DB) *Store {
	return &Store{db: db}
}

// InitSchema initializes the database schema
func (s *Store) InitSchema(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, schema)
	return err
}

// CreateSource creates a new source and returns its ID
func (s *Store) CreateSource(ctx context.Context, input domain.CreateSourceInput) (string, error) {
	id := newID("src")
	now := time.Now()
	_, err := s.db.ExecContext(ctx, `
INSERT INTO admin_sources (id, kind, display_name, status, created_at, updated_at)
VALUES ($1, $2, $3, 'draft', $4, $5)
`, id, input.Kind, input.DisplayName, formatTime(now), formatTime(now))
	if err != nil {
		return "", err
	}
	return id, nil
}

// GetSource retrieves a source by ID
func (s *Store) GetSource(ctx context.Context, id string) (*domain.Source, error) {
	var source domain.Source
	var createdAt, updatedAt string
	err := s.db.QueryRowContext(ctx, `
SELECT id, kind, display_name, status, created_at, updated_at
FROM admin_sources
WHERE id = $1
`, id).Scan(&source.ID, &source.Kind, &source.DisplayName, &source.Status, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	source.CreatedAt = parseTime(createdAt)
	source.UpdatedAt = parseTime(updatedAt)
	return &source, nil
}

// ListSources retrieves all sources
func (s *Store) ListSources(ctx context.Context) ([]domain.Source, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, kind, display_name, status, created_at, updated_at
FROM admin_sources
ORDER BY created_at DESC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sources []domain.Source
	for rows.Next() {
		var source domain.Source
		var createdAt, updatedAt string
		if err := rows.Scan(&source.ID, &source.Kind, &source.DisplayName, &source.Status, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		source.CreatedAt = parseTime(createdAt)
		source.UpdatedAt = parseTime(updatedAt)
		sources = append(sources, source)
	}
	return sources, rows.Err()
}

// UpdateSourceStatus updates the status of a source
func (s *Store) UpdateSourceStatus(ctx context.Context, id string, status string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE admin_sources
SET status = $1, updated_at = $2
WHERE id = $3
`, status, formatTime(time.Now()), id)
	return err
}

// CreateBundle creates a new bundle and returns its ID
func (s *Store) CreateBundle(ctx context.Context, input domain.CreateBundleInput) (string, error) {
	id := newID("bun")
	now := time.Now()
	_, err := s.db.ExecContext(ctx, `
INSERT INTO admin_bundles (id, name, description, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5)
`, id, input.Name, input.Description, formatTime(now), formatTime(now))
	if err != nil {
		return "", err
	}
	return id, nil
}

// GetBundle retrieves a bundle by ID
func (s *Store) GetBundle(ctx context.Context, id string) (*domain.Bundle, error) {
	var bundle domain.Bundle
	var createdAt, updatedAt string
	err := s.db.QueryRowContext(ctx, `
SELECT id, name, description, created_at, updated_at
FROM admin_bundles
WHERE id = $1
`, id).Scan(&bundle.ID, &bundle.Name, &bundle.Description, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	bundle.CreatedAt = parseTime(createdAt)
	bundle.UpdatedAt = parseTime(updatedAt)
	return &bundle, nil
}

// ListBundles retrieves all bundles
func (s *Store) ListBundles(ctx context.Context) ([]domain.Bundle, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, name, description, created_at, updated_at
FROM admin_bundles
ORDER BY created_at DESC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bundles []domain.Bundle
	for rows.Next() {
		var bundle domain.Bundle
		var createdAt, updatedAt string
		if err := rows.Scan(&bundle.ID, &bundle.Name, &bundle.Description, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		bundle.CreatedAt = parseTime(createdAt)
		bundle.UpdatedAt = parseTime(updatedAt)
		bundles = append(bundles, bundle)
	}
	return bundles, rows.Err()
}

// UpdateBundle updates a bundle's fields
func (s *Store) UpdateBundle(ctx context.Context, id string, input domain.UpdateBundleInput) error {
	now := time.Now()
	_, err := s.db.ExecContext(ctx, `
UPDATE admin_bundles
SET name = $1, description = $2, updated_at = $3
WHERE id = $4
`, input.Name, input.Description, formatTime(now), id)
	return err
}

// DeleteBundle deletes a bundle by ID
func (s *Store) DeleteBundle(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM admin_bundles WHERE id = $1`, id)
	return err
}

// CreateBundleAssignment creates a new bundle assignment and returns its ID
func (s *Store) CreateBundleAssignment(ctx context.Context, input domain.CreateBundleAssignmentInput) (string, error) {
	id := newID("bas")
	now := time.Now()
	_, err := s.db.ExecContext(ctx, `
INSERT INTO admin_bundle_assignments (id, bundle_id, principal_type, principal_id, created_at)
VALUES ($1, $2, $3, $4, $5)
`, id, input.BundleID, input.PrincipalType, input.PrincipalID, formatTime(now))
	if err != nil {
		return "", err
	}
	return id, nil
}

// ListBundleAssignments retrieves all assignments for a bundle
func (s *Store) ListBundleAssignments(ctx context.Context, bundleID string) ([]domain.BundleAssignment, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, bundle_id, principal_type, principal_id, created_at
FROM admin_bundle_assignments
WHERE bundle_id = $1
ORDER BY created_at DESC
`, bundleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var assignments []domain.BundleAssignment
	for rows.Next() {
		var assignment domain.BundleAssignment
		var createdAt string
		if err := rows.Scan(&assignment.ID, &assignment.BundleID, &assignment.PrincipalType, &assignment.PrincipalID, &createdAt); err != nil {
			return nil, err
		}
		assignment.CreatedAt = parseTime(createdAt)
		assignments = append(assignments, assignment)
	}
	return assignments, rows.Err()
}

// DeleteBundleAssignment deletes a bundle assignment by ID
func (s *Store) DeleteBundleAssignment(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM admin_bundle_assignments WHERE id = $1`, id)
	return err
}

// newID generates a new ID with the given prefix
func newID(prefix string) string {
	return fmt.Sprintf("%s_%s", prefix, uuid.NewString())
}

// formatTime formats a time for storage
func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

// parseTime parses a stored time string
func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

// Exec executes a query that doesn't return rows (for compiler use)
func (s *Store) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	// Replace ? placeholders with $1, $2, etc. for PostgreSQL compatibility
	query = replacePlaceholders(query)
	return s.db.ExecContext(ctx, query, args...)
}

// Query executes a query that returns rows (for compiler use)
func (s *Store) Query(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	// Replace ? placeholders with $1, $2, etc. for PostgreSQL compatibility
	query = replacePlaceholders(query)
	return s.db.QueryContext(ctx, query, args...)
}

// QueryRow executes a query that returns at most one row (for compiler use)
func (s *Store) QueryRow(ctx context.Context, query string, args ...interface{}) *sql.Row {
	// Replace ? placeholders with $1, $2, etc. for PostgreSQL compatibility
	query = replacePlaceholders(query)
	return s.db.QueryRowContext(ctx, query, args...)
}

// replacePlaceholders replaces ? placeholders with $1, $2, etc. for PostgreSQL
func replacePlaceholders(query string) string {
	// SQLite uses ?, PostgreSQL uses $1, $2, etc.
	// This simple implementation works for basic queries
	result := ""
	paramNum := 1
	for i := 0; i < len(query); i++ {
		if query[i] == '?' {
			result += fmt.Sprintf("$%d", paramNum)
			paramNum++
		} else {
			result += string(query[i])
		}
	}
	return result
}

// CreateAuditEvent creates a new audit event
func (s *Store) CreateAuditEvent(ctx context.Context, event domain.AdminAuditEvent) error {
	changesJSON, err := json.Marshal(event.Changes)
	if err != nil {
		return fmt.Errorf("marshal changes: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO admin_audit_events (id, timestamp, admin_id, action, resource_type, resource_id, changes, success, error_message)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, event.ID, event.Timestamp, event.AdminID, event.Action, event.ResourceType, event.ResourceID, changesJSON, event.Success, event.ErrorMessage)

	return err
}

// ListAuditEvents retrieves audit events based on filter criteria
func (s *Store) ListAuditEvents(ctx context.Context, filter domain.AuditEventFilter) ([]domain.AdminAuditEvent, error) {
	query := `
		SELECT id, timestamp, admin_id, action, resource_type, resource_id, changes, success, error_message
		FROM admin_audit_events
		WHERE 1=1
	`
	args := []interface{}{}
	argIdx := 1

	if filter.AdminID != "" {
		query += fmt.Sprintf(" AND admin_id = $%d", argIdx)
		args = append(args, filter.AdminID)
		argIdx++
	}

	if filter.Action != "" {
		query += fmt.Sprintf(" AND action = $%d", argIdx)
		args = append(args, filter.Action)
		argIdx++
	}

	if filter.ResourceType != "" {
		query += fmt.Sprintf(" AND resource_type = $%d", argIdx)
		args = append(args, filter.ResourceType)
		argIdx++
	}

	if filter.ResourceID != "" {
		query += fmt.Sprintf(" AND resource_id = $%d", argIdx)
		args = append(args, filter.ResourceID)
		argIdx++
	}

	if filter.StartTime != nil {
		query += fmt.Sprintf(" AND timestamp >= $%d", argIdx)
		args = append(args, filter.StartTime)
		argIdx++
	}

	if filter.EndTime != nil {
		query += fmt.Sprintf(" AND timestamp <= $%d", argIdx)
		args = append(args, filter.EndTime)
		argIdx++
	}

	query += " ORDER BY timestamp DESC"

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", argIdx)
		args = append(args, filter.Limit)
		argIdx++
	}

	if filter.Offset > 0 {
		query += fmt.Sprintf(" OFFSET $%d", argIdx)
		args = append(args, filter.Offset)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []domain.AdminAuditEvent
	for rows.Next() {
		var event domain.AdminAuditEvent
		var rawTimestamp interface{}
		var changesJSON []byte
		var errorMessage sql.NullString

		err := rows.Scan(&event.ID, &rawTimestamp, &event.AdminID, &event.Action,
			&event.ResourceType, &event.ResourceID, &changesJSON, &event.Success, &errorMessage)
		if err != nil {
			return nil, err
		}
		if err := assignAuditTimestamp(&event.Timestamp, rawTimestamp); err != nil {
			return nil, err
		}

		if len(changesJSON) > 0 {
			if err := json.Unmarshal(changesJSON, &event.Changes); err != nil {
				return nil, fmt.Errorf("unmarshal changes: %w", err)
			}
		}

		if errorMessage.Valid {
			event.ErrorMessage = errorMessage.String
		}

		events = append(events, event)
	}

	return events, rows.Err()
}

// GetAuditEvent retrieves a specific audit event by ID
func (s *Store) GetAuditEvent(ctx context.Context, id string) (*domain.AdminAuditEvent, error) {
	var event domain.AdminAuditEvent
	var rawTimestamp interface{}
	var changesJSON []byte
	var errorMessage sql.NullString

	err := s.db.QueryRowContext(ctx, `
		SELECT id, timestamp, admin_id, action, resource_type, resource_id, changes, success, error_message
		FROM admin_audit_events
		WHERE id = $1
	`, id).Scan(&event.ID, &rawTimestamp, &event.AdminID, &event.Action,
		&event.ResourceType, &event.ResourceID, &changesJSON, &event.Success, &errorMessage)

	if err != nil {
		return nil, err
	}
	if err := assignAuditTimestamp(&event.Timestamp, rawTimestamp); err != nil {
		return nil, err
	}

	if len(changesJSON) > 0 {
		if err := json.Unmarshal(changesJSON, &event.Changes); err != nil {
			return nil, fmt.Errorf("unmarshal changes: %w", err)
		}
	}

	if errorMessage.Valid {
		event.ErrorMessage = errorMessage.String
	}

	return &event, nil
}

func assignAuditTimestamp(dst *time.Time, value interface{}) error {
	switch v := value.(type) {
	case time.Time:
		*dst = v
		return nil
	case string:
		parsed, err := parseAuditTimestamp(v)
		if err != nil {
			return fmt.Errorf("parse audit timestamp %q: %w", v, err)
		}
		*dst = parsed
		return nil
	case []byte:
		parsed, err := parseAuditTimestamp(string(v))
		if err != nil {
			return fmt.Errorf("parse audit timestamp %q: %w", string(v), err)
		}
		*dst = parsed
		return nil
	default:
		return fmt.Errorf("unsupported audit timestamp type %T", value)
	}
}

func parseAuditTimestamp(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05 -0700 MST",
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	}
	var lastErr error
	for _, layout := range layouts {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed.UTC(), nil
		}
		lastErr = err
	}
	return time.Time{}, lastErr
}
