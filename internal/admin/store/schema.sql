-- Admin control-plane persistence schema

-- Sources represent external API sources (OpenAPI specs, etc.)
CREATE TABLE IF NOT EXISTS admin_sources (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    display_name TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

-- Bundles group multiple sources together for versioned publication
CREATE TABLE IF NOT EXISTS admin_bundles (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

-- Bundle assignments link sources to bundles
CREATE TABLE IF NOT EXISTS admin_bundle_assignments (
    id TEXT PRIMARY KEY,
    bundle_id TEXT NOT NULL REFERENCES admin_bundles(id),
    source_id TEXT NOT NULL REFERENCES admin_sources(id),
    created_at TIMESTAMP NOT NULL,
    UNIQUE(bundle_id, source_id)
);
