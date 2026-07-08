-- +goose Up
CREATE TABLE organizations (
    id                  BIGSERIAL PRIMARY KEY,
    idp_org_id          TEXT UNIQUE,
    slug                TEXT NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9-]+$'),
    name                TEXT NOT NULL,
    plan                TEXT NOT NULL DEFAULT 'free',
    storage_limit_bytes BIGINT NOT NULL DEFAULT 0, -- 0 = unlimited (Phase 1: unenforced)
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE projects (
    id         BIGSERIAL PRIMARY KEY,
    org_id     BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    slug       TEXT NOT NULL,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, slug)
);

CREATE TABLE api_keys (
    id           BIGSERIAL PRIMARY KEY,
    org_id       BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id   BIGINT REFERENCES projects(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    token_hash   TEXT NOT NULL UNIQUE,
    last_used_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at   TIMESTAMPTZ
);

CREATE TABLE cache_artifacts (
    id               BIGSERIAL PRIMARY KEY,
    org_id           BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id       BIGINT REFERENCES projects(id) ON DELETE SET NULL, -- nullable: Turbo sends no project
    hash             TEXT NOT NULL,
    size_bytes       BIGINT NOT NULL,
    artifact_tag     TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_accessed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, hash)
);

-- +goose Down
DROP TABLE cache_artifacts;
DROP TABLE api_keys;
DROP TABLE projects;
DROP TABLE organizations;
