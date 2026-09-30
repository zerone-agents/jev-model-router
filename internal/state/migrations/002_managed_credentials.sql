-- Immutable encrypted revisions preserve credentials for captured request snapshots.
CREATE TABLE managed_credentials (
    revision_id TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL,
    envelope TEXT NOT NULL
);
