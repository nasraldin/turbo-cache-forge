# turbo-cache-forge — Phase 3 (Management API `/api/v1` + OIDC/JWT) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** let humans (and later the dashboard/CLI) manage orgs, projects, and tokens over an authenticated, versioned HTTP API — without coupling the backend to any auth vendor. JWTs from any OIDC provider (Clerk / Keycloak / ZITADEL / Auth.js) authenticate `/api/v1`; the JWT org claim maps to `organizations.idp_org_id` with just-in-time org creation. Tokens minted here are the same hashed bearer tokens the CLI cache path already consumes, so create→use→revoke is provable end-to-end. Adds a `usage_daily` rollup for real stats and an in-process cleanup cron for retention.

**Architecture:** Two auth worlds stay physically separate at the package level. The **cache world** (`/v8/artifacts`) keeps using `internal/auth` (hashed bearer only, no vendor SDK, unchanged). The **management world** (`/api/v1`) gets a new package `internal/oidcauth` that imports the generic `github.com/coreos/go-oidc/v3/oidc` library (provider discovery + JWKS + signature/issuer/audience/expiry verification) — it is mounted ONLY on the `/api/v1` chi group. Both worlds converge on the same `db.Org` in request context (`auth.WithOrg` / `auth.OrgFromContext`) so downstream handlers are auth-source-agnostic. Per-org hit/miss/bytes are accumulated in memory on the cache hot path (never a DB write) and flushed to `usage_daily` by a rollup ticker; a cleanup ticker deletes expired artifacts from storage + DB.

**Tech Stack:** Go 1.25 (current module `go 1.25.0`), chi v5, pgx v5, goose (SQL migrations). New deps: `github.com/coreos/go-oidc/v3` (OIDC/JWKS/JWT verification — provider-agnostic, NOT a vendor SDK) and its transitive `github.com/go-jose/go-jose/v4` (used in tests to mint signed JWTs + a static JWKS, so tests never touch the network). `github.com/flowchartsman/swaggerui` embeds the Swagger UI dist and serves a hand-written spec with no codegen step. Prometheus stays the single metrics pipeline (untouched).

## Global Constraints

- Go module path: `github.com/nasraldin/turbo-cache-forge/services/api`. All internal imports use this prefix.
- **Two auth worlds, never mixed.** The cache path (`internal/auth`, `internal/turbo`) imports **no** OIDC/vendor SDK — verified by `go list` in Task 10. OIDC/JWT lives only in `internal/oidcauth` and is applied only to `/api/v1`.
- Tokens are stored only as SHA-256 hex hashes; the plaintext is returned exactly once at creation (`auth.GenerateToken`) and never logged. Token creation on `/api/v1` reuses the existing `auth.GenerateToken` / `auth.HashToken` — no second token scheme.
- **JWT validation is a trust boundary:** signature, issuer, audience, and expiry are all verified by go-oidc against a JWKS; unsigned/unverified claims are never trusted. No `SkipIssuerCheck` / `SkipClientIDCheck` / `SkipExpiryCheck` in production config.
- **DB off the cache hot path** stays true: per-org usage is an in-memory atomic-ish (mutex) increment on GET/PUT; the only DB writes for usage happen in the rollup ticker on a detached context.
- **One metrics pipeline** (Prometheus). Usage rollup is product data in Postgres, not a second metrics system.
- Storage is touched only through `storage.Storage` (cleanup uses `store.Delete`). Tenant key building reuses the one canonical `turbo.StorageKey`.
- Turbo protocol stays at `/v8/artifacts` (client-dictated). Versioning lives on `/api/v1` only.
- All new tables carry `org_id`. Every management query is org-scoped (`WHERE org_id = $1`) — no cross-tenant read/write.
- Every task ends green (`go test ./...`; DB/JWKS suites self-contained) and is committed.

---

## File structure (Phase 3 additions)

```
services/api/
  cmd/server/main.go              MODIFY: build accumulator, oidc authenticator, start rollup+cleanup goroutines
  internal/
    config/config.go              MODIFY: OIDC_* + retention/interval envs
    db/repo.go                    MODIFY: management + provisioning + stats + usage + cleanup queries
    oidcauth/oidc.go              NEW: OIDC verifier + JWT middleware (go-oidc), JIT org creation
    oidcauth/oidc_test.go         NEW: RSA/JWKS table tests (valid/expired/wrong-iss/wrong-aud/missing-org) + JIT
    mgmt/handlers.go              NEW: /api/v1 tokens, projects, stats, artifacts
    mgmt/handlers_test.go         NEW: handler tests with fake repo + injected org context
    usage/accumulator.go          NEW: in-memory per-org hit/miss/bytes counters
    usage/rollup.go               NEW: drain accumulator → usage_daily; ticker loop
    usage/rollup_test.go          NEW
    cleanup/cleanup.go            NEW: expired-artifact ticker (storage.Delete + DB delete)
    cleanup/cleanup_test.go       NEW
    turbo/keys.go                 MODIFY: export StorageKey (was storageKey)
    turbo/handlers.go             MODIFY: take *usage.Accumulator; record hit/miss/bytes
    server/router.go              MODIFY: mount /api/v1 group + swagger; pass accumulator to turbo
    openapi/openapi.yaml          NEW: hand-written spec for /api/v1 + /v8 protocol
    openapi/embed.go              NEW: go:embed spec bytes
infra/
  migrations/002_usage_and_indexes.sql   NEW: usage_daily + deferred FK indexes
  docker/docker-compose.keycloak.yml     NEW: self-hosted Keycloak for e2e (Task 10)
```

---

## Task 1: Migration 002 — `usage_daily` + deferred indexes

**Files:**
- Create: `infra/migrations/002_usage_and_indexes.sql`

**Interfaces:** none (schema only). New table `usage_daily(org_id, day, bytes_up, bytes_down, hits, misses)` keyed `(org_id, day)`; indexes deferred from the Phase-1 follow-up backlog.

**Decision:** `usage_daily` is a per-day rollup keyed `PRIMARY KEY (org_id, day)` so the rollup ticker can `INSERT … ON CONFLICT … DO UPDATE SET col = usage_daily.col + EXCLUDED.col` (accumulate, idempotent within a day). `day` is a `DATE` in UTC. Add the `cache_artifacts.last_accessed_at` index the cleanup scan needs, plus the two FK indexes (`cache_artifacts.project_id`, `api_keys.project_id`) the Phase-1 backlog deferred to "Phase 3+".

- [ ] **Step 1: Write the migration**

`infra/migrations/002_usage_and_indexes.sql`:
```sql
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
```

- [ ] **Step 2: Apply against the test DB + verify round-trips**

```bash
goose -dir infra/migrations postgres "$TEST_DATABASE_URL" up
goose -dir infra/migrations postgres "$TEST_DATABASE_URL" down   # verify Down works
goose -dir infra/migrations postgres "$TEST_DATABASE_URL" up     # back to head
```
Expected: `up` then `down` then `up` all succeed with no error (proves the Down block is correct).

- [ ] **Step 3: Commit**
```bash
git add infra/migrations/002_usage_and_indexes.sql
git commit -m "feat(db): migration 002 — usage_daily rollup table + deferred indexes"
```

---

## Task 2: Config — OIDC + retention/interval env

**Files:**
- Modify: `services/api/internal/config/config.go`
- Test: `services/api/internal/config/config_test.go` (extend)

**Interfaces:**
- `Config` gains `OIDCIssuer, OIDCJWKSURL, OIDCAudience, OIDCOrgClaim string`, `RetentionDays int`, `RollupIntervalSec, CleanupIntervalSec int`.

**Decision:** OIDC is **optional**. A zero-cloud self-host that only wants the cache path leaves `OIDC_ISSUER` unset and `/api/v1` is simply not mounted (mirrors the existing `d.Repo != nil` guard). Validation: if `OIDC_ISSUER` is set, `OIDC_AUDIENCE` is required (audience check must never be a no-op). `OIDC_ORG_CLAIM` defaults to `org_id`. Retention defaults to 30 days; intervals default to 300s (rollup) and 3600s (cleanup).

- [ ] **Step 1: Extend the failing test**

Add to `internal/config/config_test.go`:
```go
func TestLoadOIDCOptional(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("OIDC_ISSUER", "")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.OIDCOrgClaim != "org_id" {
		t.Errorf("OIDCOrgClaim default = %q, want org_id", c.OIDCOrgClaim)
	}
	if c.RetentionDays != 30 {
		t.Errorf("RetentionDays default = %d, want 30", c.RetentionDays)
	}
}

func TestLoadOIDCRequiresAudience(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("OIDC_ISSUER", "https://issuer.example")
	t.Setenv("OIDC_AUDIENCE", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected error: OIDC_AUDIENCE required when OIDC_ISSUER set")
	}
}
```

- [ ] **Step 2: Run → FAIL, then implement**

Add fields to `Config`:
```go
	OIDCIssuer         string
	OIDCJWKSURL        string
	OIDCAudience       string
	OIDCOrgClaim       string
	RetentionDays      int
	RollupIntervalSec  int
	CleanupIntervalSec int
```

In `Load()`, after the existing assignments and before the `return`:
```go
	c.OIDCIssuer = os.Getenv("OIDC_ISSUER")
	c.OIDCJWKSURL = os.Getenv("OIDC_JWKS_URL")
	c.OIDCAudience = os.Getenv("OIDC_AUDIENCE")
	c.OIDCOrgClaim = env("OIDC_ORG_CLAIM", "org_id")
	c.RetentionDays = int(envInt("RETENTION_DAYS", 30))
	c.RollupIntervalSec = int(envInt("USAGE_ROLLUP_INTERVAL_SEC", 300))
	c.CleanupIntervalSec = int(envInt("CLEANUP_INTERVAL_SEC", 3600))

	if c.OIDCIssuer != "" && c.OIDCAudience == "" {
		return c, fmt.Errorf("OIDC_AUDIENCE is required when OIDC_ISSUER is set")
	}
```
`// ponytail: OIDC optional — cache-only self-hosts skip it entirely; no dashboard deps forced on them.`

- [ ] **Step 3: Run + commit**
```bash
go test ./internal/config/ -v   # PASS
git add services/api/internal/config
git commit -m "feat(config): OIDC + retention/interval env knobs"
```

---

## Task 3: Repository — provisioning, tokens, projects, stats, usage, cleanup queries

**Files:**
- Modify: `services/api/internal/db/repo.go`
- Test: `services/api/internal/db/repo_test.go` (extend — DB-gated like Phase 1)

**Interfaces (added to `*Repo`):**
```go
func (r *Repo) EnsureOrgByIdpID(ctx, idpOrgID, name string) (*Org, error) // JIT org creation
type APIKey struct{ ID int64; Name string; ProjectID *int64; LastUsedAt, CreatedAt time.Time; RevokedAt *time.Time }
func (r *Repo) CreateToken(ctx, orgID int64, name, tokenHash string) (int64, error)
func (r *Repo) ListTokens(ctx, orgID int64) ([]APIKey, error)   // NEVER returns token_hash
func (r *Repo) RevokeToken(ctx, orgID, tokenID int64) (bool, error)
type Project struct{ ID int64; Slug, Name string; CreatedAt time.Time }
func (r *Repo) CreateProject(ctx, orgID int64, slug, name string) (Project, error)
func (r *Repo) ListProjects(ctx, orgID int64) ([]Project, error)
type Stats struct{ StorageBytes, ArtifactCount, Hits, Misses, Requests, BytesUp, BytesDown int64 }
func (r *Repo) Stats(ctx, orgID int64) (Stats, error)
type Artifact struct{ Hash string; SizeBytes int64; Tag *string; CreatedAt, LastAccessedAt time.Time }
func (r *Repo) ListArtifacts(ctx, orgID int64, limit, offset int) ([]Artifact, error)
func (r *Repo) AddUsage(ctx, orgID int64, day time.Time, up, down, hits, misses int64) error
type ExpiredArtifact struct{ OrgID int64; OrgSlug, Hash string }
func (r *Repo) ExpiredArtifacts(ctx, cutoff time.Time, limit int) ([]ExpiredArtifact, error)
func (r *Repo) DeleteArtifact(ctx, orgID int64, hash string) error
```

**Decision:** JIT org creation keys on the existing `organizations.idp_org_id UNIQUE`. Slug is derived **deterministically and collision-safely** from the idp id: `"org-" + hex(sha256(idpOrgID))[:12]` — always satisfies the `slug ~ '^[a-z0-9-]+$'` CHECK, never collides across distinct idp ids, no retry loop. `EnsureOrgByIdpID` is a single upsert (`ON CONFLICT (idp_org_id) DO UPDATE … RETURNING`) so concurrent first-requests from the same org are safe. `Org` grows an `IdpOrgID` field for completeness but the cache path never reads it.

- [ ] **Step 1: Extend the failing DB test**

Add to `internal/db/repo_test.go`:
```go
func TestEnsureOrgAndManagement(t *testing.T) {
	r := testRepo(t)
	ctx := context.Background()

	org, err := r.EnsureOrgByIdpID(ctx, "idp-org-abc", "Acme")
	if err != nil {
		t.Fatal(err)
	}
	if org.Slug == "" || org.ID == 0 {
		t.Fatalf("EnsureOrgByIdpID = %+v", org)
	}
	// idempotent: same idp id → same org row
	again, err := r.EnsureOrgByIdpID(ctx, "idp-org-abc", "Acme Renamed")
	if err != nil || again.ID != org.ID {
		t.Fatalf("re-ensure = %+v, %v (want id %d)", again, err, org.ID)
	}

	// tokens: create → list (no secret) → revoke
	id, err := r.CreateToken(ctx, org.ID, "ci", "hash-xyz")
	if err != nil || id == 0 {
		t.Fatalf("CreateToken = %d, %v", id, err)
	}
	keys, err := r.ListTokens(ctx, org.ID)
	if err != nil || len(keys) != 1 || keys[0].Name != "ci" {
		t.Fatalf("ListTokens = %+v, %v", keys, err)
	}
	ok, err := r.RevokeToken(ctx, org.ID, id)
	if err != nil || !ok {
		t.Fatalf("RevokeToken = %v, %v", ok, err)
	}
	// revoked token no longer authenticates the cache path
	if _, err := r.OrgByTokenHash(ctx, "hash-xyz"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("revoked token lookup = %v, want ErrUnauthorized", err)
	}
	// cross-org revoke is a no-op
	other, _ := r.EnsureOrgByIdpID(ctx, "idp-org-other", "Other")
	id2, _ := r.CreateToken(ctx, org.ID, "k2", "hash-2")
	if ok, _ := r.RevokeToken(ctx, other.ID, id2); ok {
		t.Fatal("cross-org revoke must not succeed")
	}

	// projects
	p, err := r.CreateProject(ctx, org.ID, "web", "Web App")
	if err != nil || p.ID == 0 {
		t.Fatalf("CreateProject = %+v, %v", p, err)
	}
	ps, _ := r.ListProjects(ctx, org.ID)
	if len(ps) != 1 {
		t.Fatalf("ListProjects = %+v", ps)
	}

	// usage + stats
	_ = r.UpsertArtifact(ctx, org.ID, "h1", 100, "")
	if err := r.AddUsage(ctx, org.ID, time.Now().UTC(), 100, 200, 3, 1); err != nil {
		t.Fatal(err)
	}
	_ = r.AddUsage(ctx, org.ID, time.Now().UTC(), 0, 50, 1, 0) // accumulates same day
	st, err := r.Stats(ctx, org.ID)
	if err != nil {
		t.Fatal(err)
	}
	if st.StorageBytes != 100 || st.Hits != 4 || st.Misses != 1 || st.BytesDown != 250 {
		t.Fatalf("Stats = %+v", st)
	}

	arts, err := r.ListArtifacts(ctx, org.ID, 10, 0)
	if err != nil || len(arts) != 1 || arts[0].Hash != "h1" {
		t.Fatalf("ListArtifacts = %+v, %v", arts, err)
	}
}

func TestExpiredArtifacts(t *testing.T) {
	r := testRepo(t)
	ctx := context.Background()
	org, _ := r.EnsureOrgByIdpID(ctx, "idp-exp", "Exp")
	_ = r.UpsertArtifact(ctx, org.ID, "old", 10, "")
	// force last_accessed_at into the past
	_, _ = r.pool.Exec(ctx,
		`UPDATE cache_artifacts SET last_accessed_at = now() - interval '90 days' WHERE org_id=$1 AND hash='old'`, org.ID)

	exp, err := r.ExpiredArtifacts(ctx, time.Now().Add(-24*time.Hour), 100)
	if err != nil || len(exp) == 0 {
		t.Fatalf("ExpiredArtifacts = %+v, %v", exp, err)
	}
	if exp[0].OrgSlug != org.Slug || exp[0].Hash != "old" {
		t.Fatalf("expired[0] = %+v", exp[0])
	}
	if err := r.DeleteArtifact(ctx, org.ID, "old"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := r.ArtifactExists(ctx, org.ID, "old"); ok {
		t.Fatal("artifact should be gone after DeleteArtifact")
	}
}
```
Add imports `"time"` and (already present) `"errors"` to the test file.

- [ ] **Step 2: Run → FAIL (build), then implement**

Add to `Org`:
```go
type Org struct {
	ID       int64
	Slug     string
	IdpOrgID string
}
```

Append to `internal/db/repo.go` (add imports `crypto/sha256`, `encoding/hex`, `time`):
```go
func orgSlugFor(idpOrgID string) string {
	sum := sha256.Sum256([]byte(idpOrgID))
	return "org-" + hex.EncodeToString(sum[:6]) // 12 hex chars, matches ^[a-z0-9-]+$
}

// EnsureOrgByIdpID returns the org for an IdP org id, creating it on first sight.
func (r *Repo) EnsureOrgByIdpID(ctx context.Context, idpOrgID, name string) (*Org, error) {
	if name == "" {
		name = idpOrgID
	}
	const q = `INSERT INTO organizations (idp_org_id, slug, name)
	           VALUES ($1, $2, $3)
	           ON CONFLICT (idp_org_id) DO UPDATE SET idp_org_id = EXCLUDED.idp_org_id
	           RETURNING id, slug, idp_org_id`
	var o Org
	err := r.pool.QueryRow(ctx, q, idpOrgID, orgSlugFor(idpOrgID), name).
		Scan(&o.ID, &o.Slug, &o.IdpOrgID)
	if err != nil {
		return nil, err
	}
	return &o, nil
}

type APIKey struct {
	ID         int64
	Name       string
	ProjectID  *int64
	LastUsedAt *time.Time
	CreatedAt  time.Time
	RevokedAt  *time.Time
}

func (r *Repo) CreateToken(ctx context.Context, orgID int64, name, tokenHash string) (int64, error) {
	const q = `INSERT INTO api_keys (org_id, name, token_hash) VALUES ($1, $2, $3) RETURNING id`
	var id int64
	err := r.pool.QueryRow(ctx, q, orgID, name, tokenHash).Scan(&id)
	return id, err
}

func (r *Repo) ListTokens(ctx context.Context, orgID int64) ([]APIKey, error) {
	const q = `SELECT id, name, project_id, last_used_at, created_at, revoked_at
	           FROM api_keys WHERE org_id = $1 ORDER BY created_at DESC` // token_hash never selected
	rows, err := r.pool.Query(ctx, q, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIKey
	for rows.Next() {
		var k APIKey
		if err := rows.Scan(&k.ID, &k.Name, &k.ProjectID, &k.LastUsedAt, &k.CreatedAt, &k.RevokedAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// RevokeToken sets revoked_at; org-scoped so a token can't revoke another org's key.
func (r *Repo) RevokeToken(ctx context.Context, orgID, tokenID int64) (bool, error) {
	const q = `UPDATE api_keys SET revoked_at = now()
	           WHERE id = $1 AND org_id = $2 AND revoked_at IS NULL`
	tag, err := r.pool.Exec(ctx, q, tokenID, orgID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

type Project struct {
	ID        int64
	Slug      string
	Name      string
	CreatedAt time.Time
}

func (r *Repo) CreateProject(ctx context.Context, orgID int64, slug, name string) (Project, error) {
	const q = `INSERT INTO projects (org_id, slug, name) VALUES ($1, $2, $3)
	           RETURNING id, slug, name, created_at`
	var p Project
	err := r.pool.QueryRow(ctx, q, orgID, slug, name).Scan(&p.ID, &p.Slug, &p.Name, &p.CreatedAt)
	return p, err
}

func (r *Repo) ListProjects(ctx context.Context, orgID int64) ([]Project, error) {
	const q = `SELECT id, slug, name, created_at FROM projects WHERE org_id = $1 ORDER BY name`
	rows, err := r.pool.Query(ctx, q, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Slug, &p.Name, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

type Stats struct {
	StorageBytes  int64
	ArtifactCount int64
	Hits          int64
	Misses        int64
	Requests      int64
	BytesUp       int64
	BytesDown     int64
}

func (r *Repo) Stats(ctx context.Context, orgID int64) (Stats, error) {
	var s Stats
	const q1 = `SELECT COALESCE(SUM(size_bytes),0), COUNT(*) FROM cache_artifacts WHERE org_id=$1`
	if err := r.pool.QueryRow(ctx, q1, orgID).Scan(&s.StorageBytes, &s.ArtifactCount); err != nil {
		return s, err
	}
	const q2 = `SELECT COALESCE(SUM(hits),0), COALESCE(SUM(misses),0),
	                   COALESCE(SUM(bytes_up),0), COALESCE(SUM(bytes_down),0)
	            FROM usage_daily WHERE org_id=$1`
	if err := r.pool.QueryRow(ctx, q2, orgID).Scan(&s.Hits, &s.Misses, &s.BytesUp, &s.BytesDown); err != nil {
		return s, err
	}
	s.Requests = s.Hits + s.Misses
	return s, nil
}

type Artifact struct {
	Hash           string
	SizeBytes      int64
	Tag            *string
	CreatedAt      time.Time
	LastAccessedAt time.Time
}

func (r *Repo) ListArtifacts(ctx context.Context, orgID int64, limit, offset int) ([]Artifact, error) {
	const q = `SELECT hash, size_bytes, artifact_tag, created_at, last_accessed_at
	           FROM cache_artifacts WHERE org_id=$1
	           ORDER BY created_at DESC LIMIT $2 OFFSET $3`
	rows, err := r.pool.Query(ctx, q, orgID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Artifact
	for rows.Next() {
		var a Artifact
		if err := rows.Scan(&a.Hash, &a.SizeBytes, &a.Tag, &a.CreatedAt, &a.LastAccessedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *Repo) AddUsage(ctx context.Context, orgID int64, day time.Time, up, down, hits, misses int64) error {
	const q = `INSERT INTO usage_daily (org_id, day, bytes_up, bytes_down, hits, misses)
	           VALUES ($1, $2::date, $3, $4, $5, $6)
	           ON CONFLICT (org_id, day) DO UPDATE SET
	             bytes_up   = usage_daily.bytes_up   + EXCLUDED.bytes_up,
	             bytes_down = usage_daily.bytes_down + EXCLUDED.bytes_down,
	             hits       = usage_daily.hits       + EXCLUDED.hits,
	             misses     = usage_daily.misses     + EXCLUDED.misses`
	_, err := r.pool.Exec(ctx, q, orgID, day, up, down, hits, misses)
	return err
}

type ExpiredArtifact struct {
	OrgID   int64
	OrgSlug string
	Hash    string
}

func (r *Repo) ExpiredArtifacts(ctx context.Context, cutoff time.Time, limit int) ([]ExpiredArtifact, error) {
	const q = `SELECT a.org_id, o.slug, a.hash
	           FROM cache_artifacts a JOIN organizations o ON o.id = a.org_id
	           WHERE a.last_accessed_at < $1
	           ORDER BY a.last_accessed_at LIMIT $2`
	rows, err := r.pool.Query(ctx, q, cutoff, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ExpiredArtifact
	for rows.Next() {
		var e ExpiredArtifact
		if err := rows.Scan(&e.OrgID, &e.OrgSlug, &e.Hash); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *Repo) DeleteArtifact(ctx context.Context, orgID int64, hash string) error {
	const q = `DELETE FROM cache_artifacts WHERE org_id=$1 AND hash=$2`
	_, err := r.pool.Exec(ctx, q, orgID, hash)
	return err
}
```

- [ ] **Step 3: Run + commit**
```bash
go build ./... && TEST_DATABASE_URL="postgres://...tcf_test" go test ./internal/db/ -v   # PASS
git add services/api/internal/db
git commit -m "feat(db): management, provisioning, stats, usage, and cleanup queries"
```

---

## Task 4: OIDC/JWT verifier + middleware (`internal/oidcauth`)

**Files:**
- Create: `services/api/internal/oidcauth/oidc.go`, `services/api/internal/oidcauth/oidc_test.go`

**Interfaces:**
```go
type Config struct{ Issuer, JWKSURL, Audience, OrgClaim string }
type OrgProvisioner interface {
	EnsureOrgByIdpID(ctx context.Context, idpOrgID, name string) (*db.Org, error)
}
func New(ctx context.Context, cfg Config, repo OrgProvisioner) (*Authenticator, error)
func (a *Authenticator) Middleware(next http.Handler) http.Handler
```

**Decision:** Verification uses `github.com/coreos/go-oidc/v3/oidc`. Prefer an **explicit JWKS URL** (`oidc.NewRemoteKeySet` — no discovery round-trip, provider-agnostic); fall back to issuer discovery (`oidc.NewProvider(...).VerifierContext(...)`) only when `JWKSURL` is empty. `RemoteKeySet` fetches lazily on first `Verify`, so construction never blocks on the network and startup stays resilient. The verifier enforces signature + issuer + audience (`oidc.Config{ClientID: Audience}`) + expiry — no `Skip*` flags. The org claim (`OrgClaim`, default `org_id`) is read from the **verified** token only, then mapped to `organizations.idp_org_id` via `EnsureOrgByIdpID` (just-in-time creation). Org lands in context via the shared `auth.WithOrg`, so `/api/v1` handlers read it with the same `auth.OrgFromContext` the cache path uses.

Tests mint real RS256 JWTs and serve a static JWKS from an `httptest.Server` (loopback, no external IdP) using `github.com/go-jose/go-jose/v4` (already in the module graph via go-oidc). Table cases: valid→200, expired→401, wrong-issuer→401, wrong-audience→401, missing-org-claim→401; plus JIT provisioning for a previously-unknown org.

- [ ] **Step 1: Add deps**
```bash
cd services/api
go get github.com/coreos/go-oidc/v3@latest
go get github.com/go-jose/go-jose/v4@latest   # transitive of go-oidc; pin for test use
```

- [ ] **Step 2: Write the failing test**

`internal/oidcauth/oidc_test.go`:
```go
package oidcauth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/auth"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/db"
)

const (
	testIssuer = "https://issuer.test"
	testAud    = "turbo-cache-forge"
)

// fakeProvisioner records the last idp org id it was asked to ensure.
type fakeProvisioner struct{ lastIdp string; fail bool }

func (f *fakeProvisioner) EnsureOrgByIdpID(_ context.Context, idpOrgID, _ string) (*db.Org, error) {
	if f.fail {
		return nil, context.DeadlineExceeded
	}
	f.lastIdp = idpOrgID
	return &db.Org{ID: 7, Slug: "org-test", IdpOrgID: idpOrgID}, nil
}

// harness spins up a static JWKS server + a signer over a fresh RSA key.
type harness struct {
	signer  jose.Signer
	jwksSrv *httptest.Server
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pub := jose.JSONWebKey{Key: key.Public(), KeyID: "test-key", Algorithm: "RS256", Use: "sig"}
	jwks := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{pub}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jwks)
	}))
	t.Cleanup(srv.Close)
	sig, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: key, KeyID: "test-key"}},
		(&jose.SignerOptions{}).WithType("JWT"))
	if err != nil {
		t.Fatal(err)
	}
	return &harness{signer: sig, jwksSrv: srv}
}

func (h *harness) mint(t *testing.T, iss, aud string, exp time.Time, extra map[string]any) string {
	t.Helper()
	claims := jwt.Claims{
		Issuer:   iss,
		Subject:  "user-1",
		Audience: jwt.Audience{aud},
		Expiry:   jwt.NewNumericDate(exp),
		IssuedAt: jwt.NewNumericDate(time.Now()),
	}
	tok, err := jwt.Signed(h.signer).Claims(claims).Claims(extra).Serialize()
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func (h *harness) authenticator(repo OrgProvisioner) *Authenticator {
	keySet := oidc.NewRemoteKeySet(context.Background(), h.jwksSrv.URL)
	verifier := oidc.NewVerifier(testIssuer, keySet, &oidc.Config{ClientID: testAud})
	return &Authenticator{verifier: verifier, orgClaim: "org_id", repo: repo}
}

func TestMiddlewareTable(t *testing.T) {
	h := newHarness(t)
	prov := &fakeProvisioner{}
	a := h.authenticator(prov)

	future := time.Now().Add(time.Hour)
	past := time.Now().Add(-time.Hour)
	orgClaim := map[string]any{"org_id": "idp-org-42"}

	cases := []struct {
		name  string
		token string
		want  int
	}{
		{"valid", h.mint(t, testIssuer, testAud, future, orgClaim), http.StatusOK},
		{"expired", h.mint(t, testIssuer, testAud, past, orgClaim), http.StatusUnauthorized},
		{"wrong issuer", h.mint(t, "https://evil.test", testAud, future, orgClaim), http.StatusUnauthorized},
		{"wrong audience", h.mint(t, testIssuer, "someone-else", future, orgClaim), http.StatusUnauthorized},
		{"missing org claim", h.mint(t, testIssuer, testAud, future, map[string]any{}), http.StatusUnauthorized},
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if org, ok := auth.OrgFromContext(r.Context()); !ok || org.ID != 7 {
			t.Error("org not injected into context")
		}
		w.WriteHeader(http.StatusOK)
	})
	handler := a.Middleware(next)

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil)
			req.Header.Set("Authorization", "Bearer "+c.token)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != c.want {
				t.Fatalf("%s: code = %d, want %d", c.name, rec.Code, c.want)
			}
		})
	}
}

func TestJITProvisioning(t *testing.T) {
	h := newHarness(t)
	prov := &fakeProvisioner{}
	a := h.authenticator(prov)
	tok := h.mint(t, testIssuer, testAud, time.Now().Add(time.Hour), map[string]any{"org_id": "brand-new-org"})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	a.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("JIT valid token = %d, want 200", rec.Code)
	}
	if prov.lastIdp != "brand-new-org" {
		t.Fatalf("EnsureOrgByIdpID called with %q, want brand-new-org", prov.lastIdp)
	}
}

func TestMissingBearer(t *testing.T) {
	h := newHarness(t)
	a := h.authenticator(&fakeProvisioner{})
	rec := httptest.NewRecorder()
	a.Middleware(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no header = %d, want 401", rec.Code)
	}
}
```

- [ ] **Step 3: Run → FAIL, then implement**

`internal/oidcauth/oidc.go`:
```go
// Package oidcauth authenticates /api/v1 (dashboard/management humans) with OIDC/JWT.
// It imports go-oidc and is mounted ONLY on /api/v1 — the cache path (internal/auth,
// internal/turbo) must never import this package. Two auth worlds, never mixed.
package oidcauth

import (
	"context"
	"net/http"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/auth"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/db"
)

type Config struct {
	Issuer   string
	JWKSURL  string
	Audience string
	OrgClaim string // JWT claim holding the IdP org id; default "org_id"
}

type OrgProvisioner interface {
	EnsureOrgByIdpID(ctx context.Context, idpOrgID, name string) (*db.Org, error)
}

type Authenticator struct {
	verifier *oidc.IDTokenVerifier
	orgClaim string
	repo     OrgProvisioner
}

func New(ctx context.Context, cfg Config, repo OrgProvisioner) (*Authenticator, error) {
	orgClaim := cfg.OrgClaim
	if orgClaim == "" {
		orgClaim = "org_id"
	}
	var verifier *oidc.IDTokenVerifier
	oc := &oidc.Config{ClientID: cfg.Audience} // enforces audience; signature+expiry+issuer are default-on
	if cfg.JWKSURL != "" {
		keySet := oidc.NewRemoteKeySet(ctx, cfg.JWKSURL)
		verifier = oidc.NewVerifier(cfg.Issuer, keySet, oc)
	} else {
		provider, err := oidc.NewProvider(ctx, cfg.Issuer) // discovery finds jwks_uri
		if err != nil {
			return nil, err
		}
		verifier = provider.VerifierContext(ctx, oc)
	}
	return &Authenticator{verifier: verifier, orgClaim: orgClaim, repo: repo}, nil
}

func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, ok := bearer(r)
		if !ok {
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}
		idt, err := a.verifier.Verify(r.Context(), raw) // signature + issuer + audience + expiry
		if err != nil {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
		var claims map[string]any
		if err := idt.Claims(&claims); err != nil {
			http.Error(w, "invalid claims", http.StatusUnauthorized)
			return
		}
		idpOrg, _ := claims[a.orgClaim].(string)
		if idpOrg == "" {
			http.Error(w, "missing org claim", http.StatusUnauthorized)
			return
		}
		name, _ := claims["org_name"].(string) // best-effort display name; falls back to idp id
		org, err := a.repo.EnsureOrgByIdpID(r.Context(), idpOrg, name)
		if err != nil {
			http.Error(w, "org provisioning failed", http.StatusInternalServerError)
			return
		}
		next.ServeHTTP(w, r.WithContext(auth.WithOrg(r.Context(), org)))
	})
}

func bearer(r *http.Request) (string, bool) {
	const p = "Bearer "
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, p) {
		return "", false
	}
	return strings.TrimPrefix(h, p), true
}
```
`// ponytail: reuse auth.WithOrg/OrgFromContext so /api/v1 handlers are auth-source-agnostic — no parallel context key.`

- [ ] **Step 4: Run + commit**
```bash
go test ./internal/oidcauth/ -v   # PASS (no network — static JWKS via httptest)
git add services/api/internal/oidcauth services/api/go.mod services/api/go.sum
git commit -m "feat(oidcauth): OIDC/JWT middleware with JWKS verification + JIT org creation"
```

---

## Task 5: `/api/v1` token endpoints (create / list / revoke)

**Files:**
- Create: `services/api/internal/mgmt/handlers.go`, `services/api/internal/mgmt/handlers_test.go`

**Interfaces:**
```go
type Repo interface {
	CreateToken(ctx, orgID int64, name, tokenHash string) (int64, error)
	ListTokens(ctx, orgID int64) ([]db.APIKey, error)
	RevokeToken(ctx, orgID, tokenID int64) (bool, error)
	CreateProject(ctx, orgID int64, slug, name string) (db.Project, error)
	ListProjects(ctx, orgID int64) ([]db.Project, error)
	Stats(ctx, orgID int64) (db.Stats, error)
	ListArtifacts(ctx, orgID int64, limit, offset int) ([]db.Artifact, error)
}
func NewHandler(repo Repo) *Handler
func (h *Handler) Mount(r chi.Router) // registers /tokens, /projects, /stats, /artifacts
```

**Decision:** Handlers read the org from context (`auth.OrgFromContext`) — populated by `oidcauth.Middleware`, so `mgmt` never imports `oidcauth` (clean layering). Token creation reuses `auth.GenerateToken`: the plaintext is returned **once** in the 201 body, only the hash is stored. This task builds the whole `mgmt.Handler` (interface + Mount + shared helpers) but only implements/tests the token routes; projects/stats/artifacts land in Task 6.

- [ ] **Step 1: Write the failing token test**

`internal/mgmt/handlers_test.go`:
```go
package mgmt

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/auth"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/db"
)

type fakeRepo struct {
	created   db.APIKey
	revokedOK bool
	tokens    []db.APIKey
	projects  []db.Project
	stats     db.Stats
	artifacts []db.Artifact
}

func (f *fakeRepo) CreateToken(_ context.Context, _ int64, name, _ string) (int64, error) {
	f.created = db.APIKey{ID: 99, Name: name}
	return 99, nil
}
func (f *fakeRepo) ListTokens(context.Context, int64) ([]db.APIKey, error) { return f.tokens, nil }
func (f *fakeRepo) RevokeToken(context.Context, int64, int64) (bool, error) { return f.revokedOK, nil }
func (f *fakeRepo) CreateProject(_ context.Context, _ int64, slug, name string) (db.Project, error) {
	return db.Project{ID: 1, Slug: slug, Name: name}, nil
}
func (f *fakeRepo) ListProjects(context.Context, int64) ([]db.Project, error)      { return f.projects, nil }
func (f *fakeRepo) Stats(context.Context, int64) (db.Stats, error)                 { return f.stats, nil }
func (f *fakeRepo) ListArtifacts(context.Context, int64, int, int) ([]db.Artifact, error) {
	return f.artifacts, nil
}

// router injects a fixed org into context, mimicking oidcauth.Middleware.
func testRouter(repo Repo) http.Handler {
	h := NewHandler(repo)
	r := chi.NewRouter()
	r.Route("/api/v1", func(pr chi.Router) {
		pr.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				ctx := auth.WithOrg(req.Context(), &db.Org{ID: 7, Slug: "org-test"})
				next.ServeHTTP(w, req.WithContext(ctx))
			})
		})
		h.Mount(pr)
	})
	return r
}

func TestCreateTokenReturnsPlaintextOnce(t *testing.T) {
	repo := &fakeRepo{}
	r := testRouter(repo)
	rec := httptest.NewRecorder()
	body := bytes.NewBufferString(`{"name":"ci"}`)
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/tokens", body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /tokens = %d, want 201", rec.Code)
	}
	var resp struct {
		ID    int64  `json:"id"`
		Name  string `json:"name"`
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Token == "" || resp.ID != 99 || resp.Name != "ci" {
		t.Fatalf("resp = %+v", resp)
	}
	// stored value must be the HASH of the returned plaintext, never the plaintext
	if auth.HashToken(resp.Token) == resp.Token {
		t.Fatal("token appears unhashed")
	}
}

func TestRevokeToken(t *testing.T) {
	r := testRouter(&fakeRepo{revokedOK: true})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/v1/tokens/99", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /tokens/99 = %d, want 204", rec.Code)
	}

	rec = httptest.NewRecorder()
	r2 := testRouter(&fakeRepo{revokedOK: false}) // unknown/cross-org id
	r2.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/v1/tokens/1234", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("DELETE unknown = %d, want 404", rec.Code)
	}
}

func TestListTokensHasNoSecret(t *testing.T) {
	repo := &fakeRepo{tokens: []db.APIKey{{ID: 1, Name: "ci"}}}
	r := testRouter(repo)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/tokens", nil))
	if rec.Code != http.StatusOK || bytes.Contains(rec.Body.Bytes(), []byte("token_hash")) {
		t.Fatalf("list leaked secret or bad status: %d %s", rec.Code, rec.Body)
	}
}
```

- [ ] **Step 2: Run → FAIL, then implement**

`internal/mgmt/handlers.go`:
```go
package mgmt

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/auth"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/db"
)

type Repo interface {
	CreateToken(ctx context.Context, orgID int64, name, tokenHash string) (int64, error)
	ListTokens(ctx context.Context, orgID int64) ([]db.APIKey, error)
	RevokeToken(ctx context.Context, orgID, tokenID int64) (bool, error)
	CreateProject(ctx context.Context, orgID int64, slug, name string) (db.Project, error)
	ListProjects(ctx context.Context, orgID int64) ([]db.Project, error)
	Stats(ctx context.Context, orgID int64) (db.Stats, error)
	ListArtifacts(ctx context.Context, orgID int64, limit, offset int) ([]db.Artifact, error)
}

type Handler struct{ repo Repo }

func NewHandler(repo Repo) *Handler { return &Handler{repo: repo} }

func (h *Handler) Mount(r chi.Router) {
	r.Post("/tokens", h.createToken)
	r.Get("/tokens", h.listTokens)
	r.Delete("/tokens/{id}", h.revokeToken)
	r.Post("/projects", h.createProject)
	r.Get("/projects", h.listProjects)
	r.Get("/stats", h.stats)
	r.Get("/artifacts", h.listArtifacts)
}

func (h *Handler) createToken(w http.ResponseWriter, r *http.Request) {
	org, ok := auth.OrgFromContext(r.Context())
	if !ok {
		http.Error(w, "no org", http.StatusUnauthorized)
		return
	}
	var in struct{ Name string `json:"name"` }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	token, hash, err := auth.GenerateToken()
	if err != nil {
		http.Error(w, "token generation failed", http.StatusInternalServerError)
		return
	}
	id, err := h.repo.CreateToken(r.Context(), org.ID, in.Name, hash)
	if err != nil {
		http.Error(w, "create failed", http.StatusInternalServerError)
		return
	}
	// plaintext returned exactly once; only the hash is persisted
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "name": in.Name, "token": token})
}

func (h *Handler) listTokens(w http.ResponseWriter, r *http.Request) {
	org, _ := auth.OrgFromContext(r.Context())
	keys, err := h.repo.ListTokens(r.Context(), org.ID)
	if err != nil {
		http.Error(w, "list failed", http.StatusInternalServerError)
		return
	}
	out := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		out = append(out, map[string]any{
			"id": k.ID, "name": k.Name, "created_at": k.CreatedAt,
			"last_used_at": k.LastUsedAt, "revoked_at": k.RevokedAt,
		}) // never includes token_hash
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) revokeToken(w http.ResponseWriter, r *http.Request) {
	org, _ := auth.OrgFromContext(r.Context())
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	ok, err := h.repo.RevokeToken(r.Context(), org.ID, id)
	if err != nil {
		http.Error(w, "revoke failed", http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "not found", http.StatusNotFound) // unknown or another org's token
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
```
Note: `createProject`, `listProjects`, `stats`, `listArtifacts` are referenced by `Mount` — add temporary stubs returning 501 so the package compiles, then fill them in Task 6:
```go
func (h *Handler) createProject(w http.ResponseWriter, r *http.Request)  { http.Error(w, "todo", http.StatusNotImplemented) }
func (h *Handler) listProjects(w http.ResponseWriter, r *http.Request)   { http.Error(w, "todo", http.StatusNotImplemented) }
func (h *Handler) stats(w http.ResponseWriter, r *http.Request)          { http.Error(w, "todo", http.StatusNotImplemented) }
func (h *Handler) listArtifacts(w http.ResponseWriter, r *http.Request)  { http.Error(w, "todo", http.StatusNotImplemented) }
```

- [ ] **Step 3: Run + commit**
```bash
go test ./internal/mgmt/ -v   # token tests PASS
git add services/api/internal/mgmt
git commit -m "feat(mgmt): /api/v1 token create/list/revoke (plaintext once, hash stored)"
```

---

## Task 6: `/api/v1` projects, stats, artifacts

**Files:**
- Modify: `services/api/internal/mgmt/handlers.go` (replace the four stubs)
- Test: `services/api/internal/mgmt/handlers_test.go` (extend)

**Decision:** `GET /artifacts` is paginated via `?limit=&offset=` with a hard cap (`limit` clamped to 1..200, default 50) so a caller can't ask for an unbounded scan. Project slug is validated against the same `^[a-z0-9-]+$` shape the DB CHECK enforces on org slugs, returning 400 before hitting the DB.

- [ ] **Step 1: Extend the failing test**

Add to `internal/mgmt/handlers_test.go`:
```go
func TestCreateProjectValidatesSlug(t *testing.T) {
	r := testRouter(&fakeRepo{})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/projects",
		bytes.NewBufferString(`{"slug":"Bad Slug","name":"x"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad slug = %d, want 400", rec.Code)
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/projects",
		bytes.NewBufferString(`{"slug":"web","name":"Web"}`)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("good slug = %d, want 201", rec.Code)
	}
}

func TestStatsAndArtifacts(t *testing.T) {
	repo := &fakeRepo{
		stats:     db.Stats{StorageBytes: 100, Hits: 4, Misses: 1, Requests: 5},
		artifacts: []db.Artifact{{Hash: "h1", SizeBytes: 10}},
	}
	r := testRouter(repo)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil))
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"storage_bytes":100`)) {
		t.Fatalf("stats = %d %s", rec.Code, rec.Body)
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/artifacts?limit=10", nil))
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"h1"`)) {
		t.Fatalf("artifacts = %d %s", rec.Code, rec.Body)
	}
}
```

- [ ] **Step 2: Run → FAIL, then replace the stubs**

Add `regexp` import and a package-level slug regex, then implement:
```go
var slugRe = regexp.MustCompile(`^[a-z0-9-]+$`)

func (h *Handler) createProject(w http.ResponseWriter, r *http.Request) {
	org, _ := auth.OrgFromContext(r.Context())
	var in struct {
		Slug string `json:"slug"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Name == "" || !slugRe.MatchString(in.Slug) {
		http.Error(w, "slug must match ^[a-z0-9-]+$ and name is required", http.StatusBadRequest)
		return
	}
	p, err := h.repo.CreateProject(r.Context(), org.ID, in.Slug, in.Name)
	if err != nil {
		http.Error(w, "create failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (h *Handler) listProjects(w http.ResponseWriter, r *http.Request) {
	org, _ := auth.OrgFromContext(r.Context())
	ps, err := h.repo.ListProjects(r.Context(), org.ID)
	if err != nil {
		http.Error(w, "list failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, ps)
}

func (h *Handler) stats(w http.ResponseWriter, r *http.Request) {
	org, _ := auth.OrgFromContext(r.Context())
	s, err := h.repo.Stats(r.Context(), org.ID)
	if err != nil {
		http.Error(w, "stats failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"storage_bytes": s.StorageBytes, "artifact_count": s.ArtifactCount,
		"hits": s.Hits, "misses": s.Misses, "requests": s.Requests,
		"bytes_up": s.BytesUp, "bytes_down": s.BytesDown,
	})
}

func (h *Handler) listArtifacts(w http.ResponseWriter, r *http.Request) {
	org, _ := auth.OrgFromContext(r.Context())
	limit := clampInt(r.URL.Query().Get("limit"), 50, 1, 200)
	offset := clampInt(r.URL.Query().Get("offset"), 0, 0, 1<<31)
	arts, err := h.repo.ListArtifacts(r.Context(), org.ID, limit, offset)
	if err != nil {
		http.Error(w, "list failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"limit": limit, "offset": offset, "artifacts": arts})
}

func clampInt(s string, def, lo, hi int) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}
```

- [ ] **Step 3: Run + commit**
```bash
go test ./internal/mgmt/ -v   # PASS
git add services/api/internal/mgmt
git commit -m "feat(mgmt): /api/v1 projects, stats, and paginated artifacts"
```

---

## Task 7: Usage accumulator + rollup ticker

**Files:**
- Create: `services/api/internal/usage/accumulator.go`, `services/api/internal/usage/rollup.go`, `services/api/internal/usage/rollup_test.go`
- Modify: `services/api/internal/turbo/handlers.go`, `services/api/internal/turbo/keys.go`, and the two `NewHandler` callers

**Interfaces:**
```go
type Accumulator struct{ … }
func New() *Accumulator
func (a *Accumulator) Hit(orgID, bytesDown int64)
func (a *Accumulator) Miss(orgID int64)
func (a *Accumulator) Upload(orgID, bytesUp int64)
func (a *Accumulator) Drain() map[int64]Delta
type Sink interface{ AddUsage(ctx, orgID int64, day time.Time, up, down, hits, misses int64) error }
func Rollup(ctx context.Context, acc *Accumulator, sink Sink) error
func Run(ctx context.Context, acc *Accumulator, sink Sink, interval time.Duration)
```

**Decision:** Per-org hit/miss/bytes cannot come from the global Prometheus counters, and incrementing the DB on the cache hot path violates a cross-phase invariant. So the cache handler does the cheapest possible thing — a mutex-guarded in-memory increment (`Accumulator`) — and a rollup ticker **drains** the accumulated deltas and folds them into `usage_daily` on a detached context. Storage-used stays a live `SUM(size_bytes)` (Task 3), so it needs no accumulation. This is the minimum that yields real per-org stats without a queue or a hot-path DB write.

- [ ] **Step 1: Write the accumulator + rollup test**

`internal/usage/rollup_test.go`:
```go
package usage

import (
	"context"
	"sync"
	"testing"
	"time"
)

type fakeSink struct {
	mu   sync.Mutex
	rows map[int64]Delta
}

func (f *fakeSink) AddUsage(_ context.Context, orgID int64, _ time.Time, up, down, hits, misses int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rows == nil {
		f.rows = map[int64]Delta{}
	}
	f.rows[orgID] = Delta{Up: up, Down: down, Hits: hits, Misses: misses}
	return nil
}

func TestAccumulateAndRollup(t *testing.T) {
	acc := New()
	acc.Hit(7, 100)
	acc.Hit(7, 50)
	acc.Miss(7)
	acc.Upload(7, 200)
	acc.Hit(9, 10)

	sink := &fakeSink{}
	if err := Rollup(context.Background(), acc, sink); err != nil {
		t.Fatal(err)
	}
	if got := sink.rows[7]; got.Hits != 2 || got.Misses != 1 || got.Down != 150 || got.Up != 200 {
		t.Fatalf("org 7 rollup = %+v", got)
	}
	if got := sink.rows[9]; got.Hits != 1 || got.Down != 10 {
		t.Fatalf("org 9 rollup = %+v", got)
	}
	// drained: a second rollup with no new activity writes nothing
	sink2 := &fakeSink{}
	_ = Rollup(context.Background(), acc, sink2)
	if len(sink2.rows) != 0 {
		t.Fatalf("expected empty after drain, got %+v", sink2.rows)
	}
}

func TestConcurrentAccumulate(t *testing.T) {
	acc := New()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); acc.Hit(1, 1) }()
	}
	wg.Wait()
	if d := acc.Drain()[1]; d.Hits != 100 || d.Down != 100 {
		t.Fatalf("race lost updates: %+v", d)
	}
}
```

- [ ] **Step 2: Run → FAIL, then implement**

`internal/usage/accumulator.go`:
```go
package usage

import "sync"

type Delta struct {
	Up, Down, Hits, Misses int64
}

type Accumulator struct {
	mu sync.Mutex
	m  map[int64]*Delta
}

func New() *Accumulator { return &Accumulator{m: map[int64]*Delta{}} }

func (a *Accumulator) at(orgID int64) *Delta {
	d := a.m[orgID]
	if d == nil {
		d = &Delta{}
		a.m[orgID] = d
	}
	return d
}

func (a *Accumulator) Hit(orgID, bytesDown int64) {
	a.mu.Lock()
	d := a.at(orgID)
	d.Hits++
	d.Down += bytesDown
	a.mu.Unlock()
}

func (a *Accumulator) Miss(orgID int64) {
	a.mu.Lock()
	a.at(orgID).Misses++
	a.mu.Unlock()
}

func (a *Accumulator) Upload(orgID, bytesUp int64) {
	a.mu.Lock()
	d := a.at(orgID)
	d.Up += bytesUp
	a.mu.Unlock()
}

// Drain returns accumulated deltas and resets. Values are copied out under lock.
func (a *Accumulator) Drain() map[int64]Delta {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make(map[int64]Delta, len(a.m))
	for id, d := range a.m {
		out[id] = *d
	}
	a.m = map[int64]*Delta{}
	return out
}
```
`// ponytail: one global mutex; contention is a non-issue at 4 int64 adds per cache op. Shard only if a profile ever says so.`

`internal/usage/rollup.go`:
```go
package usage

import (
	"context"
	"time"
)

type Sink interface {
	AddUsage(ctx context.Context, orgID int64, day time.Time, up, down, hits, misses int64) error
}

// Rollup drains the accumulator into the sink under today's UTC date.
func Rollup(ctx context.Context, acc *Accumulator, sink Sink) error {
	day := time.Now().UTC()
	for orgID, d := range acc.Drain() {
		if err := sink.AddUsage(ctx, orgID, day, d.Up, d.Down, d.Hits, d.Misses); err != nil {
			return err
		}
	}
	return nil
}

// Run rolls up on an interval until ctx is cancelled, then does a final drain.
func Run(ctx context.Context, acc *Accumulator, sink Sink, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = Rollup(context.Background(), acc, sink) // flush remaining on shutdown
			return
		case <-t.C:
			_ = Rollup(ctx, acc, sink)
		}
	}
}
```

- [ ] **Step 3: Wire the accumulator into the turbo handler**

Export the key builder — `internal/turbo/keys.go`:
```go
package turbo

// StorageKey namespaces artifacts by org so tenants never collide.
func StorageKey(orgSlug, hash string) string { return orgSlug + "/" + hash }
```
Replace the internal `storageKey(...)` calls in `handlers.go` with `StorageKey(...)`.

In `internal/turbo/handlers.go`: add field `usage *usage.Accumulator` to `Handler`, extend the constructor signature, and record usage next to the existing Prometheus increments:
```go
func NewHandler(store ArtifactStore, repo MetaRepo, maxBytes int64, metrics *obs.Metrics, acc *usage.Accumulator) *Handler {
	return &Handler{store: store, repo: repo, maxBytes: maxBytes, metrics: metrics, usage: acc}
}
```
- in `put`, after the successful upsert (where `metrics.UploadBytes.Add` already runs): `h.usage.Upload(org.ID, info.Size)`
- in `get`, on `ErrNotFound` (next to `metrics.CacheMiss.Inc()`): `h.usage.Miss(org.ID)`
- in `get`, on success (next to `metrics.CacheHit.Inc()`): `h.usage.Hit(org.ID, info.Size)`

Update the two callers (signature 5→6):
- `internal/turbo/handlers_test.go` → `testRouter`: pass `usage.New()`.
- `internal/server/router.go`: pass `d.Usage` (added in Task 10).

- [ ] **Step 4: Run + commit**
```bash
go build ./... && go test ./internal/usage/ ./internal/turbo/ -v   # PASS
git add services/api/internal/usage services/api/internal/turbo
git commit -m "feat(usage): in-memory per-org accumulator + usage_daily rollup ticker"
```

---

## Task 8: Cleanup cron (retention)

**Files:**
- Create: `services/api/internal/cleanup/cleanup.go`, `services/api/internal/cleanup/cleanup_test.go`

**Interfaces:**
```go
type Store interface{ Delete(ctx context.Context, key string) error }
type Repo interface {
	ExpiredArtifacts(ctx context.Context, cutoff time.Time, limit int) ([]db.ExpiredArtifact, error)
	DeleteArtifact(ctx context.Context, orgID int64, hash string) error
}
func RunOnce(ctx context.Context, repo Repo, store Store, retention time.Duration) (int, error)
func Run(ctx context.Context, repo Repo, store Store, retention, interval time.Duration)
```

**Decision:** An in-process ticker (NOT a queue) deletes artifacts whose `last_accessed_at` is older than `retention`. Order matters: **delete the storage object first, then the DB row.** If the storage delete fails we skip the DB delete and retry next tick (the row is still discoverable); if the object is already gone, `storage.Delete` is idempotent (both fs and s3 backends treat missing as success), so we still drop the row. The storage key is built with the canonical `turbo.StorageKey` (tenant-isolation invariant: one key builder). Batched via `ExpiredArtifacts(..., limit)` so a huge backlog is drained over several ticks without loading everything at once.

- [ ] **Step 1: Write the failing test**

`internal/cleanup/cleanup_test.go`:
```go
package cleanup

import (
	"context"
	"testing"
	"time"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/db"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/turbo"
)

type fakeStore struct{ deleted []string }

func (f *fakeStore) Delete(_ context.Context, key string) error {
	f.deleted = append(f.deleted, key)
	return nil
}

type fakeRepo struct {
	expired  []db.ExpiredArtifact
	dbDeletes []string
}

func (f *fakeRepo) ExpiredArtifacts(context.Context, time.Time, int) ([]db.ExpiredArtifact, error) {
	out := f.expired
	f.expired = nil // second call returns none (drained)
	return out, nil
}
func (f *fakeRepo) DeleteArtifact(_ context.Context, orgID int64, hash string) error {
	f.dbDeletes = append(f.dbDeletes, hash)
	return nil
}

func TestRunOnceDeletesObjectThenRow(t *testing.T) {
	store := &fakeStore{}
	repo := &fakeRepo{expired: []db.ExpiredArtifact{{OrgID: 7, OrgSlug: "org-test", Hash: "old"}}}

	n, err := RunOnce(context.Background(), repo, store, 30*24*time.Hour)
	if err != nil || n != 1 {
		t.Fatalf("RunOnce = %d, %v", n, err)
	}
	wantKey := turbo.StorageKey("org-test", "old")
	if len(store.deleted) != 1 || store.deleted[0] != wantKey {
		t.Fatalf("storage deleted = %v, want [%s]", store.deleted, wantKey)
	}
	if len(repo.dbDeletes) != 1 || repo.dbDeletes[0] != "old" {
		t.Fatalf("db deleted = %v", repo.dbDeletes)
	}
}
```

- [ ] **Step 2: Run → FAIL, then implement**

`internal/cleanup/cleanup.go`:
```go
package cleanup

import (
	"context"
	"log"
	"time"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/db"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/turbo"
)

const batchLimit = 500

type Store interface {
	Delete(ctx context.Context, key string) error
}

type Repo interface {
	ExpiredArtifacts(ctx context.Context, cutoff time.Time, limit int) ([]db.ExpiredArtifact, error)
	DeleteArtifact(ctx context.Context, orgID int64, hash string) error
}

// RunOnce deletes one batch of expired artifacts (object first, then DB row).
func RunOnce(ctx context.Context, repo Repo, store Store, retention time.Duration) (int, error) {
	cutoff := time.Now().Add(-retention)
	rows, err := repo.ExpiredArtifacts(ctx, cutoff, batchLimit)
	if err != nil {
		return 0, err
	}
	var n int
	for _, a := range rows {
		key := turbo.StorageKey(a.OrgSlug, a.Hash)
		if err := store.Delete(ctx, key); err != nil {
			log.Printf("cleanup: storage delete %s failed, will retry: %v", key, err)
			continue // leave the DB row so it is retried next tick
		}
		if err := repo.DeleteArtifact(ctx, a.OrgID, a.Hash); err != nil {
			log.Printf("cleanup: db delete org=%d hash=%s failed: %v", a.OrgID, a.Hash, err)
			continue
		}
		n++
	}
	return n, nil
}

func Run(ctx context.Context, repo Repo, store Store, retention, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n, err := RunOnce(ctx, repo, store, retention); err != nil {
				log.Printf("cleanup: batch failed: %v", err)
			} else if n > 0 {
				log.Printf("cleanup: removed %d expired artifacts", n)
			}
		}
	}
}
```
`// ponytail: object-then-row; a mid-crash leaves a DB row with no object (self-heals — next tick's storage.Delete is a no-op and drops the row). The reverse (orphan object) is what we avoid.`

- [ ] **Step 3: Run + commit**
```bash
go test ./internal/cleanup/ -v   # PASS
git add services/api/internal/cleanup
git commit -m "feat(cleanup): in-process retention ticker (object-first delete)"
```

---

## Task 9: OpenAPI spec + Swagger UI

**Files:**
- Create: `services/api/internal/openapi/openapi.yaml`, `services/api/internal/openapi/embed.go`

**Interfaces:**
```go
//go:embed openapi.yaml
var Spec []byte
func Handler() http.Handler // embedded Swagger UI serving Spec
```

**Decision:** Hand-write `openapi.yaml` and serve it with `github.com/flowchartsman/swaggerui`, which embeds the Swagger UI dist and takes the spec as bytes. Rationale (one line): a hand-written spec avoids the `swaggo/swag` codegen/build step and annotation drift, while `flowchartsman/swaggerui` vendors the UI assets so the docs page is fully self-contained and needs no CDN. The spec documents both `/api/v1` (JWT bearer) and the Turbo `/v8/artifacts` protocol (token bearer) so the two auth worlds are visible in one place; the raw spec is also served at `/api/v1/openapi.yaml`.

- [ ] **Step 1: Add dep**
```bash
cd services/api && go get github.com/flowchartsman/swaggerui@latest
```

- [ ] **Step 2: Write the spec**

`internal/openapi/openapi.yaml` (abridged — cover every route added in this phase plus the Turbo protocol):
```yaml
openapi: 3.0.3
info:
  title: turbo-cache-forge API
  version: "1.0"
  description: >
    Two auth worlds. /v8/artifacts (Turbo protocol) uses hashed bearer tokens.
    /api/v1 (management) uses OIDC-issued JWT bearer tokens.
servers:
  - url: /
components:
  securitySchemes:
    turboToken:
      type: http
      scheme: bearer
      description: Hashed API token (turbo_...) for the cache path.
    oidcJWT:
      type: http
      scheme: bearer
      bearerFormat: JWT
      description: OIDC-issued JWT for /api/v1.
paths:
  /api/v1/tokens:
    post:
      summary: Create an API token (plaintext returned once)
      security: [{ oidcJWT: [] }]
      requestBody:
        required: true
        content:
          application/json:
            schema: { type: object, required: [name], properties: { name: { type: string } } }
      responses:
        "201":
          description: Created
          content:
            application/json:
              schema:
                type: object
                properties:
                  id: { type: integer }
                  name: { type: string }
                  token: { type: string, description: shown once, store securely }
    get:
      summary: List API tokens (no secrets)
      security: [{ oidcJWT: [] }]
      responses: { "200": { description: OK } }
  /api/v1/tokens/{id}:
    delete:
      summary: Revoke an API token
      security: [{ oidcJWT: [] }]
      parameters: [{ name: id, in: path, required: true, schema: { type: integer } }]
      responses: { "204": { description: Revoked }, "404": { description: Not found } }
  /api/v1/projects:
    post:
      summary: Create a project
      security: [{ oidcJWT: [] }]
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [slug, name]
              properties: { slug: { type: string, pattern: "^[a-z0-9-]+$" }, name: { type: string } }
      responses: { "201": { description: Created } }
    get:
      summary: List projects
      security: [{ oidcJWT: [] }]
      responses: { "200": { description: OK } }
  /api/v1/stats:
    get:
      summary: Org usage stats (storage, hit/miss, requests)
      security: [{ oidcJWT: [] }]
      responses: { "200": { description: OK } }
  /api/v1/artifacts:
    get:
      summary: List cache artifacts (paginated)
      security: [{ oidcJWT: [] }]
      parameters:
        - { name: limit, in: query, schema: { type: integer, default: 50, maximum: 200 } }
        - { name: offset, in: query, schema: { type: integer, default: 0 } }
      responses: { "200": { description: OK } }
  /v8/artifacts/status:
    get:
      summary: Turbo remote-cache status
      security: [{ turboToken: [] }]
      responses: { "200": { description: '{"status":"enabled"}' } }
  /v8/artifacts/{hash}:
    put:
      summary: Upload an artifact
      security: [{ turboToken: [] }]
      parameters: [{ name: hash, in: path, required: true, schema: { type: string } }]
      responses: { "202": { description: Accepted }, "413": { description: Too large } }
    get:
      summary: Download an artifact
      security: [{ turboToken: [] }]
      parameters: [{ name: hash, in: path, required: true, schema: { type: string } }]
      responses: { "200": { description: octet-stream }, "404": { description: Miss } }
```

- [ ] **Step 3: Embed + serve**

`internal/openapi/embed.go`:
```go
package openapi

import (
	_ "embed"
	"net/http"

	"github.com/flowchartsman/swaggerui"
)

//go:embed openapi.yaml
var Spec []byte

// Handler serves the embedded Swagger UI rendering Spec.
func Handler() http.Handler { return swaggerui.Handler(Spec) }
```
`// ponytail: hand-written spec, no swag codegen; swaggerui embeds the UI dist so no CDN and no build step.`

- [ ] **Step 4: Build + commit** (route wiring/serving verified in Task 10)
```bash
go build ./...   # confirms embed + dep resolve
git add services/api/internal/openapi services/api/go.mod services/api/go.sum
git commit -m "feat(openapi): hand-written spec + embedded Swagger UI"
```

---

## Task 10: Wire `/api/v1` into the router + main; end-to-end verification

**Files:**
- Modify: `services/api/internal/server/router.go`, `services/api/cmd/server/main.go`, `.env.example`
- Create: `infra/docker/docker-compose.keycloak.yml`

**Interfaces:**
- `server.Deps` gains `Auth *oidcauth.Authenticator`, `Usage *usage.Accumulator`.

**Decision:** `/api/v1` is mounted only when `d.Auth != nil` (OIDC configured) — mirrors the existing `d.Repo != nil` gate, so cache-only self-hosts are unaffected. The Turbo group is untouched except for receiving `d.Usage`. Swagger + the raw spec are served **unauthenticated** at `/api/v1/docs/*` and `/api/v1/openapi.yaml` (public API docs). `main` constructs the accumulator, the authenticator (when configured), and launches the rollup + cleanup goroutines with a cancelable context tied to shutdown.

- [ ] **Step 1: Wire the router**

`internal/server/router.go` — add imports (`mgmt`, `oidcauth`, `openapi`, `usage`, `net/http`) and extend `Deps` + `New`:
```go
type Deps struct {
	Store          storage.Storage
	Repo           *db.Repo
	MaxUploadBytes int64
	Usage          *usage.Accumulator
	Auth           *oidcauth.Authenticator
}
```
In `New(d Deps)`, after the metrics setup and ops endpoints, before the Turbo group, pass the accumulator to the Turbo handler:
```go
	if d.Repo != nil {
		th := turbo.NewHandler(d.Store, d.Repo, d.MaxUploadBytes, m, d.Usage)
		r.Group(func(pr chi.Router) {
			pr.Use(auth.RequireToken(d.Repo))
			th.Mount(pr)
		})
	}

	// Management API (OIDC/JWT) + docs — mounted only when OIDC is configured.
	if d.Auth != nil && d.Repo != nil {
		mh := mgmt.NewHandler(d.Repo)
		r.Route("/api/v1", func(ar chi.Router) {
			// public docs
			ar.Get("/openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/yaml")
				_, _ = w.Write(openapi.Spec)
			})
			ar.Handle("/docs/*", http.StripPrefix("/api/v1/docs", openapi.Handler()))
			// authenticated management routes
			ar.Group(func(pr chi.Router) {
				pr.Use(d.Auth.Middleware)
				mh.Mount(pr)
			})
		})
	}
```
Note: `*db.Repo` satisfies both `mgmt.Repo` and `oidcauth.OrgProvisioner` (all methods added in Task 3) — no adapters.

- [ ] **Step 2: Wire main**

`cmd/server/main.go` — build accumulator, authenticator, background jobs. Add after `store` is built:
```go
	acc := usage.New()

	var authn *oidcauth.Authenticator
	if cfg.OIDCIssuer != "" {
		authn, err = oidcauth.New(ctx, oidcauth.Config{
			Issuer:   cfg.OIDCIssuer,
			JWKSURL:  cfg.OIDCJWKSURL,
			Audience: cfg.OIDCAudience,
			OrgClaim: cfg.OIDCOrgClaim,
		}, repo)
		if err != nil {
			log.Fatalf("oidc init: %v", err)
		}
		log.Printf("management API enabled at /api/v1 (issuer=%s)", cfg.OIDCIssuer)
	}

	// background jobs share a context cancelled on shutdown
	bg, cancel := context.WithCancel(context.Background())
	defer cancel()
	go usage.Run(bg, acc, repo, time.Duration(cfg.RollupIntervalSec)*time.Second)
	go cleanup.Run(bg, repo, store,
		time.Duration(cfg.RetentionDays)*24*time.Hour,
		time.Duration(cfg.CleanupIntervalSec)*time.Second)

	srv := server.New(server.Deps{
		Store: store, Repo: repo, MaxUploadBytes: cfg.MaxUploadBytes,
		Usage: acc, Auth: authn,
	})
```
Add imports: `time`, `internal/usage`, `internal/cleanup`, `internal/oidcauth`. `*db.Repo` satisfies `usage.Sink` (`AddUsage`) and `cleanup.Repo` (`ExpiredArtifacts`/`DeleteArtifact`).

- [ ] **Step 3: Document env + verify auth-world separation**

Append to `.env.example`:
```env
# --- Management API (OIDC/JWT) — optional; leave OIDC_ISSUER empty for cache-only ---
OIDC_ISSUER=              # e.g. http://localhost:8081/realms/turbo (Keycloak)
OIDC_JWKS_URL=            # optional explicit JWKS; else discovered from issuer
OIDC_AUDIENCE=turbo-cache-forge
OIDC_ORG_CLAIM=org_id     # JWT claim carrying the IdP org id
RETENTION_DAYS=30
USAGE_ROLLUP_INTERVAL_SEC=300
CLEANUP_INTERVAL_SEC=3600
```

Prove the cache path imports no OIDC/vendor SDK (cross-phase invariant, mechanically):
```bash
go list -deps ./internal/turbo ./internal/auth | grep -E 'go-oidc|go-jose' && \
  echo "FAIL: cache path pulled in an auth SDK" || echo "OK: cache path is SDK-free"
```
Expected: `OK: cache path is SDK-free`.

- [ ] **Step 4: Build + full test suite**
```bash
go build ./... && go test ./... -v
```
Expected: PASS (DB suite skips without `TEST_DATABASE_URL`; oidcauth/usage/cleanup/mgmt all self-contained).

- [ ] **Step 5: End-to-end with a self-hosted Keycloak (no paid IdP)**

`infra/docker/docker-compose.keycloak.yml`:
```yaml
services:
  keycloak:
    image: quay.io/keycloak/keycloak:26.0
    command: ["start-dev", "--http-port=8081"]
    environment:
      KC_BOOTSTRAP_ADMIN_USERNAME: admin
      KC_BOOTSTRAP_ADMIN_PASSWORD: admin
    ports: ["8081:8081"]
```
Bring it up alongside the API, then create a realm `turbo`, a confidential client `turbo-cache-forge` (audience), and a **token mapper** that emits an `org_id` claim (hardcoded value `demo-org` for the test user, or a user attribute). Point the API at it:
```bash
docker compose -f infra/docker/docker-compose.yml \
               -f infra/docker/docker-compose.keycloak.yml up -d --build
# API env: OIDC_ISSUER=http://keycloak:8081/realms/turbo  OIDC_AUDIENCE=turbo-cache-forge
```

**Acceptance run (proves the Definition of Done):**
```bash
# 1) get a real JWT from Keycloak
JWT=$(curl -s -X POST \
  "http://localhost:8081/realms/turbo/protocol/openid-connect/token" \
  -d grant_type=password -d client_id=turbo-cache-forge -d client_secret=<secret> \
  -d username=demo -d password=demo -d scope=openid | jq -r .access_token)

# 2) JWT authenticates /api/v1 (JIT-creates the org on first call)
curl -s -H "Authorization: Bearer $JWT" http://localhost:8080/api/v1/stats   # 200 JSON

# 3) create a cache token via the management API (plaintext ONCE)
TOK=$(curl -s -X POST -H "Authorization: Bearer $JWT" \
  -H 'Content-Type: application/json' -d '{"name":"ci"}' \
  http://localhost:8080/api/v1/tokens | jq -r .token)

# 4) the token works on the CACHE path
echo hi | curl -s -X PUT --data-binary @- \
  -H "Authorization: Bearer $TOK" http://localhost:8080/v8/artifacts/e2ehash  # 202
curl -s -H "Authorization: Bearer $TOK" \
  http://localhost:8080/v8/artifacts/e2ehash                                  # "hi"

# 5) revoke it via management API, then confirm it's rejected on the cache path
TID=$(curl -s -H "Authorization: Bearer $JWT" http://localhost:8080/api/v1/tokens | jq -r '.[0].id')
curl -s -o /dev/null -w '%{http_code}\n' -X DELETE \
  -H "Authorization: Bearer $JWT" http://localhost:8080/api/v1/tokens/$TID    # 204
curl -s -o /dev/null -w '%{http_code}\n' \
  -H "Authorization: Bearer $TOK" http://localhost:8080/v8/artifacts/e2ehash  # 401

# 6) OpenAPI served
curl -s http://localhost:8080/api/v1/openapi.yaml | head -3                   # openapi: 3.0.3 ...
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:8080/api/v1/docs/   # 200 (Swagger UI)
```

**CI variant (no Keycloak):** the same middleware is exercised hermetically by `internal/oidcauth/oidc_test.go` (static JWKS via `httptest`), so the JWT trust boundary is covered without any IdP dependency.

- [ ] **Step 6: Commit**
```bash
git add services/api infra .env.example
git commit -m "feat(api): mount /api/v1 (OIDC) + jobs; keycloak e2e + auth-world separation check"
```

---

## Self-review notes (coverage against ROADMAP Phase 3)

- **OIDC/JWT middleware** (T4): go-oidc, `OIDC_ISSUER`/`OIDC_JWKS_URL`/`OIDC_AUDIENCE`, provider-agnostic, JWT org claim → `idp_org_id`, table tests over a local RSA/JWKS harness (valid/expired/wrong-iss/wrong-aud/missing-org) — no network.
- **`/api/v1` endpoints** (T5–T6): tokens create/list/revoke (plaintext once, hash stored, org-scoped revoke), projects create/list, stats, paginated artifacts. Org bootstrap = **just-in-time** on first valid JWT (T3 `EnsureOrgByIdpID`).
- **Migration 002** (T1): `usage_daily` + the deferred FK indexes + cleanup/list indexes, goose Up/Down verified.
- **Usage rollup** (T7): in-memory accumulator on the cache hot path (no DB write there) → ticker folds into `usage_daily` → feeds `/api/v1/stats`.
- **Cleanup cron** (T8): in-process ticker, object-first delete via `storage.Delete` + DB row, retention via env; not a queue.
- **OpenAPI/Swagger** (T9): hand-written spec + embedded Swagger UI (`flowchartsman/swaggerui`), covers `/api/v1` and `/v8`.
- **Wiring** (T10): `/api/v1` alongside Turbo, Turbo stays at `/v8/artifacts`.

**Cross-phase invariants honored:** two auth worlds physically separated (`go list` check in T10); tokens hashed, plaintext once; DB off the cache hot path (usage is in-memory there); one metrics pipeline (Prometheus untouched — usage_daily is product data); storage only via the interface; one canonical `turbo.StorageKey`; every management query org-scoped; Go 1.25.

## Deferred (do NOT build here)
- Quota **enforcement** (columns exist, stay unenforced) → North star.
- Dashboard UI → Phase 4. CLI → Phase 5.
- Redis hot-metadata / rate-limit / presigned egress → North star (build on measured need).

## Verification checklist (run before calling Phase 3 done)
1. `go build ./... && go test ./...` green; `go list` shows the cache path free of `go-oidc`/`go-jose`.
2. A **real Keycloak JWT** authenticates `/api/v1/stats` and JIT-creates the org row (`SELECT * FROM organizations WHERE idp_org_id='demo-org'`).
3. **Token lifecycle:** create via `/api/v1/tokens` → use on `/v8/artifacts` (PUT 202 / GET 200) → revoke via `/api/v1/tokens/{id}` → cache path returns **401**.
4. **Cleanup:** seed an artifact with `last_accessed_at` in the past → after a `cleanup.RunOnce`, both the storage object and the `cache_artifacts` row are gone.
5. **Stats real numbers:** after a few PUT/GET, wait one rollup interval → `/api/v1/stats` shows non-zero `hits`/`misses`/`storage_bytes`.
6. **OpenAPI served:** `/api/v1/openapi.yaml` returns the spec and `/api/v1/docs/` renders Swagger UI.
7. **Tenant isolation:** a JWT for org A cannot list/revoke org B's tokens (org-scoped queries); a token minted for A cannot read B's artifacts.
