package publish

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/StevenBuglione/open-cli/internal/admin/domain"
	"github.com/StevenBuglione/open-cli/internal/admin/store"
	"github.com/google/uuid"
)

// Compiler compiles bundle configurations into runtime-ready snapshots
type Compiler struct {
	store *store.Store
}

// NewCompiler creates a new compiler instance
func NewCompiler(st *store.Store) *Compiler {
	return &Compiler{store: st}
}

// CompileBundle compiles a bundle into a runtime-ready configuration snapshot
func (c *Compiler) CompileBundle(ctx context.Context, bundleID string) (*domain.CompiledSnapshot, error) {
	bundle, err := c.store.GetBundle(ctx, bundleID)
	if err != nil {
		return nil, fmt.Errorf("failed to get bundle: %w", err)
	}

	snapshot := &domain.CompiledSnapshot{
		BundleID:   bundle.ID,
		BundleName: bundle.Name,
		Sources:    make(map[string]domain.SnapshotSource),
		Services:   make(map[string]domain.SnapshotService),
		CompiledAt: time.Now().UTC(),
	}

	// For v1, bundles are empty containers - future versions will link to sources
	// This allows the publish system to work end-to-end while source linking is added later

	return snapshot, nil
}

// CreateRevision creates a new draft revision of a bundle
func (c *Compiler) CreateRevision(ctx context.Context, bundleID, createdBy string) (string, error) {
	// Compile the current bundle state
	snapshot, err := c.CompileBundle(ctx, bundleID)
	if err != nil {
		return "", fmt.Errorf("failed to compile bundle: %w", err)
	}

	// Serialize snapshot
	snapshotData, err := json.Marshal(snapshot)
	if err != nil {
		return "", fmt.Errorf("failed to marshal snapshot: %w", err)
	}

	// Compute hash
	hash := sha256.Sum256(snapshotData)
	snapshotHash := fmt.Sprintf("%x", hash)

	// Generate revision ID
	revisionID := "rev_" + uuid.New().String()

	// Store revision
	query := `
		INSERT INTO admin_revisions 
		(id, bundle_id, status, created_by, created_at, snapshot_hash, snapshot_data)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`
	now := time.Now().UTC()
	_, err = c.store.Exec(ctx, query,
		revisionID,
		bundleID,
		"draft",
		createdBy,
		now.Format(time.RFC3339Nano),
		snapshotHash,
		string(snapshotData),
	)
	if err != nil {
		return "", fmt.Errorf("failed to create revision: %w", err)
	}

	return revisionID, nil
}

// GetRevision retrieves a revision by ID
func (c *Compiler) GetRevision(ctx context.Context, revisionID string) (*domain.Revision, error) {
	query := `
		SELECT id, bundle_id, status, created_by, created_at, published_at, snapshot_hash
		FROM admin_revisions
		WHERE id = ?
	`
	row := c.store.QueryRow(ctx, query, revisionID)

	var revision domain.Revision
	var publishedAt sql.NullString
	var createdAt string
	err := row.Scan(
		&revision.ID,
		&revision.BundleID,
		&revision.Status,
		&revision.CreatedBy,
		&createdAt,
		&publishedAt,
		&revision.SnapshotHash,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("revision not found: %s", revisionID)
		}
		return nil, fmt.Errorf("failed to get revision: %w", err)
	}

	// Parse created_at
	parsedCreatedAt, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return nil, fmt.Errorf("failed to parse created_at: %w", err)
	}
	revision.CreatedAt = parsedCreatedAt

	// Parse published_at if present
	if publishedAt.Valid {
		parsedPublishedAt, err := time.Parse(time.RFC3339Nano, publishedAt.String)
		if err != nil {
			return nil, fmt.Errorf("failed to parse published_at: %w", err)
		}
		revision.PublishedAt = &parsedPublishedAt
	}

	return &revision, nil
}

// PublishRevision marks a revision as published
func (c *Compiler) PublishRevision(ctx context.Context, revisionID string) error {
	now := time.Now().UTC()
	query := `
		UPDATE admin_revisions
		SET status = 'published', published_at = ?
		WHERE id = ?
	`
	result, err := c.store.Exec(ctx, query, now.Format(time.RFC3339Nano), revisionID)
	if err != nil {
		return fmt.Errorf("failed to publish revision: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check affected rows: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("revision not found: %s", revisionID)
	}

	return nil
}

// ListRevisions lists all revisions for a bundle
func (c *Compiler) ListRevisions(ctx context.Context, bundleID string) ([]*domain.Revision, error) {
	query := `
		SELECT id, bundle_id, status, created_by, created_at, published_at, snapshot_hash
		FROM admin_revisions
		WHERE bundle_id = ?
		ORDER BY created_at DESC
	`
	rows, err := c.store.Query(ctx, query, bundleID)
	if err != nil {
		return nil, fmt.Errorf("failed to list revisions: %w", err)
	}
	defer rows.Close()

	var revisions []*domain.Revision
	for rows.Next() {
		var revision domain.Revision
		var publishedAt sql.NullString
		var createdAt string
		err := rows.Scan(
			&revision.ID,
			&revision.BundleID,
			&revision.Status,
			&revision.CreatedBy,
			&createdAt,
			&publishedAt,
			&revision.SnapshotHash,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan revision: %w", err)
		}

		// Parse created_at
		parsedCreatedAt, err := time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, fmt.Errorf("failed to parse created_at: %w", err)
		}
		revision.CreatedAt = parsedCreatedAt

		// Parse published_at if present
		if publishedAt.Valid {
			parsedPublishedAt, err := time.Parse(time.RFC3339Nano, publishedAt.String)
			if err != nil {
				return nil, fmt.Errorf("failed to parse published_at: %w", err)
			}
			revision.PublishedAt = &parsedPublishedAt
		}

		revisions = append(revisions, &revision)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate revisions: %w", err)
	}

	return revisions, nil
}

// GetActiveRevision gets the active (most recently published) revision for a bundle
func (c *Compiler) GetActiveRevision(ctx context.Context, bundleID string) (*domain.Revision, error) {
	query := `
		SELECT id, bundle_id, status, created_by, created_at, published_at, snapshot_hash
		FROM admin_revisions
		WHERE bundle_id = ? AND status = 'published'
		ORDER BY published_at DESC
		LIMIT 1
	`
	row := c.store.QueryRow(ctx, query, bundleID)

	var revision domain.Revision
	var publishedAt sql.NullString
	var createdAt string
	err := row.Scan(
		&revision.ID,
		&revision.BundleID,
		&revision.Status,
		&revision.CreatedBy,
		&createdAt,
		&publishedAt,
		&revision.SnapshotHash,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("no active revision found for bundle: %s", bundleID)
		}
		return nil, fmt.Errorf("failed to get active revision: %w", err)
	}

	// Parse created_at
	parsedCreatedAt, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return nil, fmt.Errorf("failed to parse created_at: %w", err)
	}
	revision.CreatedAt = parsedCreatedAt

	// Parse published_at if present
	if publishedAt.Valid {
		parsedPublishedAt, err := time.Parse(time.RFC3339Nano, publishedAt.String)
		if err != nil {
			return nil, fmt.Errorf("failed to parse published_at: %w", err)
		}
		revision.PublishedAt = &parsedPublishedAt
	}

	return &revision, nil
}

// GetRevisionSnapshot retrieves the compiled snapshot for a revision
func (c *Compiler) GetRevisionSnapshot(ctx context.Context, revisionID string) (*domain.CompiledSnapshot, error) {
	query := `
		SELECT snapshot_data
		FROM admin_revisions
		WHERE id = ?
	`
	row := c.store.QueryRow(ctx, query, revisionID)

	var snapshotData string
	err := row.Scan(&snapshotData)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("revision not found: %s", revisionID)
		}
		return nil, fmt.Errorf("failed to get snapshot: %w", err)
	}

	var snapshot domain.CompiledSnapshot
	if err := json.Unmarshal([]byte(snapshotData), &snapshot); err != nil {
		return nil, fmt.Errorf("failed to unmarshal snapshot: %w", err)
	}

	return &snapshot, nil
}

// DiffRevisions computes the differences between two revisions
func (c *Compiler) DiffRevisions(ctx context.Context, fromRevisionID, toRevisionID string) (*domain.RevisionDiff, error) {
	from, err := c.GetRevisionSnapshot(ctx, fromRevisionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get from revision: %w", err)
	}

	to, err := c.GetRevisionSnapshot(ctx, toRevisionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get to revision: %w", err)
	}

	diff := &domain.RevisionDiff{
		FromRevisionID: fromRevisionID,
		ToRevisionID:   toRevisionID,
	}

	// Compute sources diff
	fromSources := make(map[string]bool)
	for sourceID := range from.Sources {
		fromSources[sourceID] = true
	}

	toSources := make(map[string]bool)
	for sourceID := range to.Sources {
		toSources[sourceID] = true
	}

	for sourceID := range toSources {
		if !fromSources[sourceID] {
			diff.SourcesAdded = append(diff.SourcesAdded, sourceID)
		}
	}

	for sourceID := range fromSources {
		if !toSources[sourceID] {
			diff.SourcesRemoved = append(diff.SourcesRemoved, sourceID)
		} else if from.Sources[sourceID] != to.Sources[sourceID] {
			diff.SourcesChanged = append(diff.SourcesChanged, sourceID)
		}
	}

	// Compute services diff
	fromServices := make(map[string]bool)
	for serviceID := range from.Services {
		fromServices[serviceID] = true
	}

	toServices := make(map[string]bool)
	for serviceID := range to.Services {
		toServices[serviceID] = true
	}

	for serviceID := range toServices {
		if !fromServices[serviceID] {
			diff.ServicesAdded = append(diff.ServicesAdded, serviceID)
		}
	}

	for serviceID := range fromServices {
		if !toServices[serviceID] {
			diff.ServicesRemoved = append(diff.ServicesRemoved, serviceID)
		}
	}

	return diff, nil
}
