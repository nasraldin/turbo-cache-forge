-- +goose Up
-- SQLite dialect of 001. Differences vs Postgres: INTEGER PRIMARY KEY
-- AUTOINCREMENT for ids, DATETIME/CURRENT_TIMESTAMP for timestamps, and the
-- slug regex CHECK is dropped (SQLite has no regex operator; slugs are
-- validated in Go before insert).
CREATE TABLE organizations (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    idp_org_id          TEXT UNIQUE,
    slug                TEXT NOT NULL UNIQUE,
    name                TEXT NOT NULL,
    plan                TEXT NOT NULL DEFAULT 'free',
    storage_limit_bytes INTEGER NOT NULL DEFAULT 0,
    created_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE projects (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    org_id     INTEGER NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    slug       TEXT NOT NULL,
    name       TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (org_id, slug)
);

CREATE TABLE api_keys (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    org_id       INTEGER NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id   INTEGER REFERENCES projects(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    token_hash   TEXT NOT NULL UNIQUE,
    last_used_at DATETIME,
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    revoked_at   DATETIME
);

CREATE TABLE cache_artifacts (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    org_id           INTEGER NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id       INTEGER REFERENCES projects(id) ON DELETE SET NULL,
    hash             TEXT NOT NULL,
    size_bytes       INTEGER NOT NULL,
    artifact_tag     TEXT,
    created_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_accessed_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (org_id, hash)
);

-- +goose Down
DROP TABLE cache_artifacts;
DROP TABLE api_keys;
DROP TABLE projects;
DROP TABLE organizations;
