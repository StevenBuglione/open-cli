-- Admin control-plane persistence schema

-- Sources represent external API sources (OpenAPI specs, etc.)
CREATE TABLE IF NOT EXISTS admin_sources (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    display_name TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('draft', 'validated', 'publishable', 'archived')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- Bundles represent access packages that can be assigned to principals
CREATE TABLE IF NOT EXISTS admin_bundles (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

-- Bundle assignments link bundles to principals (users or groups)
CREATE TABLE IF NOT EXISTS admin_bundle_assignments (
    id TEXT PRIMARY KEY,
    bundle_id TEXT NOT NULL REFERENCES admin_bundles(id) ON DELETE CASCADE,
    principal_type TEXT NOT NULL CHECK (principal_type IN ('user', 'group')),
    principal_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE(bundle_id, principal_type, principal_id)
);

-- Revisions track versioned snapshots of bundle configurations
CREATE TABLE IF NOT EXISTS admin_revisions (
    id TEXT PRIMARY KEY,
    bundle_id TEXT NOT NULL REFERENCES admin_bundles(id) ON DELETE CASCADE,
    status TEXT NOT NULL CHECK (status IN ('draft', 'published')),
    created_by TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    published_at TIMESTAMPTZ,
    snapshot_hash TEXT NOT NULL,
    snapshot_data TEXT NOT NULL
);

-- Index for finding active (published) revisions by bundle
CREATE INDEX IF NOT EXISTS idx_revisions_bundle_status 
    ON admin_revisions(bundle_id, status, published_at DESC);

-- Audit events track all admin actions for compliance and change history
CREATE TABLE IF NOT EXISTS admin_audit_events (
    id TEXT PRIMARY KEY,
    timestamp TIMESTAMPTZ NOT NULL,
    admin_id TEXT NOT NULL,
    action TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    changes JSONB,
    success BOOLEAN NOT NULL,
    error_message TEXT
);

-- Indexes for efficient audit queries
CREATE INDEX IF NOT EXISTS idx_audit_timestamp ON admin_audit_events(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_audit_admin ON admin_audit_events(admin_id);
CREATE INDEX IF NOT EXISTS idx_audit_resource ON admin_audit_events(resource_type, resource_id);
