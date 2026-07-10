-- +goose Up
-- day is TEXT 'YYYY-MM-DD' (SQLite has no DATE type; lexicographic order = chronological).
CREATE TABLE usage_daily (
    org_id     INTEGER NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    day        TEXT   NOT NULL,
    bytes_up   INTEGER NOT NULL DEFAULT 0,
    bytes_down INTEGER NOT NULL DEFAULT 0,
    hits       INTEGER NOT NULL DEFAULT 0,
    misses     INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (org_id, day)
);

CREATE INDEX idx_cache_artifacts_project_id ON cache_artifacts (project_id);
CREATE INDEX idx_api_keys_project_id        ON api_keys (project_id);
CREATE INDEX idx_cache_artifacts_last_accessed ON cache_artifacts (last_accessed_at);
CREATE INDEX idx_cache_artifacts_org_created ON cache_artifacts (org_id, created_at DESC);

-- +goose Down
DROP INDEX idx_cache_artifacts_org_created;
DROP INDEX idx_cache_artifacts_last_accessed;
DROP INDEX idx_api_keys_project_id;
DROP INDEX idx_cache_artifacts_project_id;
DROP TABLE usage_daily;
