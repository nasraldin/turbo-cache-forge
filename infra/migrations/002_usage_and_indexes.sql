-- +goose Up
CREATE TABLE usage_daily (
    org_id     BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    day        DATE   NOT NULL,
    bytes_up   BIGINT NOT NULL DEFAULT 0,
    bytes_down BIGINT NOT NULL DEFAULT 0,
    hits       BIGINT NOT NULL DEFAULT 0,
    misses     BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (org_id, day)
);

-- deferred from Phase 1 follow-up backlog: FK indexes now that Phase 3 filters by project
CREATE INDEX idx_cache_artifacts_project_id ON cache_artifacts (project_id);
CREATE INDEX idx_api_keys_project_id        ON api_keys (project_id);

-- cleanup cron scans by last_accessed_at
CREATE INDEX idx_cache_artifacts_last_accessed ON cache_artifacts (last_accessed_at);

-- /api/v1/artifacts lists newest-first per org
CREATE INDEX idx_cache_artifacts_org_created ON cache_artifacts (org_id, created_at DESC);

-- +goose Down
DROP INDEX idx_cache_artifacts_org_created;
DROP INDEX idx_cache_artifacts_last_accessed;
DROP INDEX idx_api_keys_project_id;
DROP INDEX idx_cache_artifacts_project_id;
DROP TABLE usage_daily;
