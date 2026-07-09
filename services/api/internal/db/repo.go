package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/obs"
)

var ErrUnauthorized = errors.New("db: no matching active token")

type Org struct {
	ID       int64
	Slug     string
	IdpOrgID string
}

type Repo struct{ pool *pgxpool.Pool }

func Open(ctx context.Context, url string) (*Repo, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	return &Repo{pool: pool}, nil
}

func (r *Repo) Close()                         { r.pool.Close() }
func (r *Repo) Ping(ctx context.Context) error { return r.pool.Ping(ctx) }

func (r *Repo) OrgByTokenHash(ctx context.Context, hash string) (org *Org, err error) {
	ctx, span := obs.StartDBSpan(ctx, "db.OrgByTokenHash")
	defer func() { obs.EndSpan(span, err) }()

	const q = `SELECT o.id, o.slug FROM api_keys k
	           JOIN organizations o ON o.id = k.org_id
	           WHERE k.token_hash = $1 AND k.revoked_at IS NULL`
	var o Org
	err = r.pool.QueryRow(ctx, q, hash).Scan(&o.ID, &o.Slug)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrUnauthorized
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}

func (r *Repo) UpsertArtifact(ctx context.Context, orgID int64, hash string, size int64, tag string) (err error) {
	ctx, span := obs.StartDBSpan(ctx, "db.UpsertArtifact")
	defer func() { obs.EndSpan(span, err) }()

	const q = `INSERT INTO cache_artifacts (org_id, hash, size_bytes, artifact_tag)
	           VALUES ($1, $2, $3, NULLIF($4,''))
	           ON CONFLICT (org_id, hash) DO UPDATE
	             SET size_bytes = EXCLUDED.size_bytes,
	                 artifact_tag = EXCLUDED.artifact_tag,
	                 last_accessed_at = now()`
	_, err = r.pool.Exec(ctx, q, orgID, hash, size, tag)
	return err
}

func (r *Repo) ArtifactExists(ctx context.Context, orgID int64, hash string) (exists bool, err error) {
	ctx, span := obs.StartDBSpan(ctx, "db.ArtifactExists")
	defer func() { obs.EndSpan(span, err) }()

	const q = `SELECT EXISTS(SELECT 1 FROM cache_artifacts WHERE org_id=$1 AND hash=$2)`
	err = r.pool.QueryRow(ctx, q, orgID, hash).Scan(&exists)
	return exists, err
}

func (r *Repo) TouchArtifact(ctx context.Context, orgID int64, hash string) (err error) {
	ctx, span := obs.StartDBSpan(ctx, "db.TouchArtifact")
	defer func() { obs.EndSpan(span, err) }()

	const q = `UPDATE cache_artifacts SET last_accessed_at = now() WHERE org_id=$1 AND hash=$2`
	_, err = r.pool.Exec(ctx, q, orgID, hash)
	return err
}

func orgSlugFor(idpOrgID string) string {
	sum := sha256.Sum256([]byte(idpOrgID))
	return "org-" + hex.EncodeToString(sum[:6]) // 12 hex chars, matches ^[a-z0-9-]+$
}

// EnsureOrgByIdpID returns the org for an IdP org id, creating it on first sight.
// Single upsert keyed on organizations.idp_org_id UNIQUE, so concurrent
// first-requests from the same org are safe.
func (r *Repo) EnsureOrgByIdpID(ctx context.Context, idpOrgID, name string) (org *Org, err error) {
	ctx, span := obs.StartDBSpan(ctx, "db.EnsureOrgByIdpID")
	defer func() { obs.EndSpan(span, err) }()

	if name == "" {
		name = idpOrgID
	}
	const q = `INSERT INTO organizations (idp_org_id, slug, name)
	           VALUES ($1, $2, $3)
	           ON CONFLICT (idp_org_id) DO UPDATE SET idp_org_id = EXCLUDED.idp_org_id
	           RETURNING id, slug, idp_org_id`
	var o Org
	err = r.pool.QueryRow(ctx, q, idpOrgID, orgSlugFor(idpOrgID), name).
		Scan(&o.ID, &o.Slug, &o.IdpOrgID)
	if err != nil {
		return nil, err
	}
	return &o, nil
}

type APIKey struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	ProjectID  *int64     `json:"project_id"`
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedAt  time.Time  `json:"created_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
}

// CreateToken stores only the SHA-256 hash (via auth.HashToken upstream);
// the plaintext token is never persisted or logged.
func (r *Repo) CreateToken(ctx context.Context, orgID int64, name, tokenHash string) (id int64, err error) {
	ctx, span := obs.StartDBSpan(ctx, "db.CreateToken")
	defer func() { obs.EndSpan(span, err) }()

	const q = `INSERT INTO api_keys (org_id, name, token_hash) VALUES ($1, $2, $3) RETURNING id`
	err = r.pool.QueryRow(ctx, q, orgID, name, tokenHash).Scan(&id)
	return id, err
}

// ListTokens is org-scoped and never selects token_hash.
func (r *Repo) ListTokens(ctx context.Context, orgID int64) (keys []APIKey, err error) {
	ctx, span := obs.StartDBSpan(ctx, "db.ListTokens")
	defer func() { obs.EndSpan(span, err) }()

	const q = `SELECT id, name, project_id, last_used_at, created_at, revoked_at
	           FROM api_keys WHERE org_id = $1 ORDER BY created_at DESC`
	rows, err := r.pool.Query(ctx, q, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k APIKey
		if err = rows.Scan(&k.ID, &k.Name, &k.ProjectID, &k.LastUsedAt, &k.CreatedAt, &k.RevokedAt); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	err = rows.Err()
	return keys, err
}

// RevokeToken sets revoked_at; org-scoped so a token can't revoke another org's key.
func (r *Repo) RevokeToken(ctx context.Context, orgID, tokenID int64) (ok bool, err error) {
	ctx, span := obs.StartDBSpan(ctx, "db.RevokeToken")
	defer func() { obs.EndSpan(span, err) }()

	const q = `UPDATE api_keys SET revoked_at = now()
	           WHERE id = $1 AND org_id = $2 AND revoked_at IS NULL`
	tag, err := r.pool.Exec(ctx, q, tokenID, orgID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

type Project struct {
	ID        int64     `json:"id"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

func (r *Repo) CreateProject(ctx context.Context, orgID int64, slug, name string) (proj Project, err error) {
	ctx, span := obs.StartDBSpan(ctx, "db.CreateProject")
	defer func() { obs.EndSpan(span, err) }()

	const q = `INSERT INTO projects (org_id, slug, name) VALUES ($1, $2, $3)
	           RETURNING id, slug, name, created_at`
	err = r.pool.QueryRow(ctx, q, orgID, slug, name).Scan(&proj.ID, &proj.Slug, &proj.Name, &proj.CreatedAt)
	return proj, err
}

func (r *Repo) ListProjects(ctx context.Context, orgID int64) (projects []Project, err error) {
	ctx, span := obs.StartDBSpan(ctx, "db.ListProjects")
	defer func() { obs.EndSpan(span, err) }()

	const q = `SELECT id, slug, name, created_at FROM projects WHERE org_id = $1 ORDER BY name`
	rows, err := r.pool.Query(ctx, q, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p Project
		if err = rows.Scan(&p.ID, &p.Slug, &p.Name, &p.CreatedAt); err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	err = rows.Err()
	return projects, err
}

type Stats struct {
	StorageBytes  int64 `json:"storage_bytes"`
	ArtifactCount int64 `json:"artifact_count"`
	Hits          int64 `json:"hits"`
	Misses        int64 `json:"misses"`
	Requests      int64 `json:"requests"`
	BytesUp       int64 `json:"bytes_up"`
	BytesDown     int64 `json:"bytes_down"`
}

func (r *Repo) Stats(ctx context.Context, orgID int64) (s Stats, err error) {
	ctx, span := obs.StartDBSpan(ctx, "db.Stats")
	defer func() { obs.EndSpan(span, err) }()

	const q1 = `SELECT COALESCE(SUM(size_bytes),0), COUNT(*) FROM cache_artifacts WHERE org_id=$1`
	if err = r.pool.QueryRow(ctx, q1, orgID).Scan(&s.StorageBytes, &s.ArtifactCount); err != nil {
		return s, err
	}
	const q2 = `SELECT COALESCE(SUM(hits),0), COALESCE(SUM(misses),0),
	                   COALESCE(SUM(bytes_up),0), COALESCE(SUM(bytes_down),0)
	            FROM usage_daily WHERE org_id=$1`
	if err = r.pool.QueryRow(ctx, q2, orgID).Scan(&s.Hits, &s.Misses, &s.BytesUp, &s.BytesDown); err != nil {
		return s, err
	}
	s.Requests = s.Hits + s.Misses
	return s, nil
}

type StatsPoint struct {
	Day       string `json:"day"` // YYYY-MM-DD
	Hits      int64  `json:"hits"`
	Misses    int64  `json:"misses"`
	BytesUp   int64  `json:"bytes_up"`
	BytesDown int64  `json:"bytes_down"`
}

// StatsSeries returns per-day usage for the last `days` days, oldest first.
// Days with no activity are simply absent (the chart connects across gaps).
// ponytail: gap-filling (zero rows for silent days) left out; add a generate_series
// LEFT JOIN if the chart needs continuous days.
func (r *Repo) StatsSeries(ctx context.Context, orgID int64, days int) (pts []StatsPoint, err error) {
	ctx, span := obs.StartDBSpan(ctx, "db.StatsSeries")
	defer func() { obs.EndSpan(span, err) }()

	const q = `SELECT to_char(day,'YYYY-MM-DD'), hits, misses, bytes_up, bytes_down
	           FROM usage_daily
	           WHERE org_id=$1 AND day >= CURRENT_DATE - $2::int
	           ORDER BY day`
	rows, err := r.pool.Query(ctx, q, orgID, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p StatsPoint
		if err = rows.Scan(&p.Day, &p.Hits, &p.Misses, &p.BytesUp, &p.BytesDown); err != nil {
			return nil, err
		}
		pts = append(pts, p)
	}
	return pts, rows.Err()
}

type Artifact struct {
	Hash           string    `json:"hash"`
	SizeBytes      int64     `json:"size_bytes"`
	Tag            *string   `json:"tag"`
	CreatedAt      time.Time `json:"created_at"`
	LastAccessedAt time.Time `json:"last_accessed_at"`
}

func (r *Repo) ListArtifacts(ctx context.Context, orgID int64, limit, offset int) (artifacts []Artifact, err error) {
	ctx, span := obs.StartDBSpan(ctx, "db.ListArtifacts")
	defer func() { obs.EndSpan(span, err) }()

	const q = `SELECT hash, size_bytes, artifact_tag, created_at, last_accessed_at
	           FROM cache_artifacts WHERE org_id=$1
	           ORDER BY created_at DESC LIMIT $2 OFFSET $3`
	rows, err := r.pool.Query(ctx, q, orgID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var a Artifact
		if err = rows.Scan(&a.Hash, &a.SizeBytes, &a.Tag, &a.CreatedAt, &a.LastAccessedAt); err != nil {
			return nil, err
		}
		artifacts = append(artifacts, a)
	}
	err = rows.Err()
	return artifacts, err
}

// AddUsage accumulates daily usage counters; idempotent within a day via
// ON CONFLICT accumulation (safe to call multiple times per day).
func (r *Repo) AddUsage(ctx context.Context, orgID int64, day time.Time, up, down, hits, misses int64) (err error) {
	ctx, span := obs.StartDBSpan(ctx, "db.AddUsage")
	defer func() { obs.EndSpan(span, err) }()

	const q = `INSERT INTO usage_daily (org_id, day, bytes_up, bytes_down, hits, misses)
	           VALUES ($1, $2::date, $3, $4, $5, $6)
	           ON CONFLICT (org_id, day) DO UPDATE SET
	             bytes_up   = usage_daily.bytes_up   + EXCLUDED.bytes_up,
	             bytes_down = usage_daily.bytes_down + EXCLUDED.bytes_down,
	             hits       = usage_daily.hits       + EXCLUDED.hits,
	             misses     = usage_daily.misses     + EXCLUDED.misses`
	_, err = r.pool.Exec(ctx, q, orgID, day, up, down, hits, misses)
	return err
}

type ExpiredArtifact struct {
	OrgID   int64
	OrgSlug string
	Hash    string
}

// ExpiredArtifacts is batched (limit) so a huge backlog drains over ticks
// instead of loading everything at once. Not org-scoped by design: it's a
// system-wide cron scan, and each row carries its own OrgID/OrgSlug so the
// caller stays tenant-aware when deleting.
func (r *Repo) ExpiredArtifacts(ctx context.Context, cutoff time.Time, limit int) (out []ExpiredArtifact, err error) {
	ctx, span := obs.StartDBSpan(ctx, "db.ExpiredArtifacts")
	defer func() { obs.EndSpan(span, err) }()

	const q = `SELECT a.org_id, o.slug, a.hash
	           FROM cache_artifacts a JOIN organizations o ON o.id = a.org_id
	           WHERE a.last_accessed_at < $1
	           ORDER BY a.last_accessed_at LIMIT $2`
	rows, err := r.pool.Query(ctx, q, cutoff, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var e ExpiredArtifact
		if err = rows.Scan(&e.OrgID, &e.OrgSlug, &e.Hash); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	err = rows.Err()
	return out, err
}

func (r *Repo) DeleteArtifact(ctx context.Context, orgID int64, hash string) (err error) {
	ctx, span := obs.StartDBSpan(ctx, "db.DeleteArtifact")
	defer func() { obs.EndSpan(span, err) }()

	const q = `DELETE FROM cache_artifacts WHERE org_id=$1 AND hash=$2`
	_, err = r.pool.Exec(ctx, q, orgID, hash)
	return err
}
