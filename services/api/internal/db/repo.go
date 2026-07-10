package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/pressly/goose/v3"
	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver "pgx"
	_ "modernc.org/sqlite"             // database/sql driver "sqlite"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/db/migrations"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/obs"
)

var ErrUnauthorized = errors.New("db: no matching active token")
var ErrArtifactNotFound = errors.New("db: artifact not found")

type Org struct {
	ID       int64
	Slug     string
	IdpOrgID string
	// ReadOnly is the read-only flag of the *token* used for this request (not a
	// property of the org itself). It rides the per-request principal so the cache
	// handlers can reject writes without extra context plumbing. Only set by
	// OrgByTokenHash; zero (read-write) for the OIDC/mgmt principal.
	ReadOnly bool
}

type Repo struct {
	db *sql.DB
	d  dialect
}

// Open connects using the driver implied by the URL scheme (see parseURL).
// SQLite gets a single connection (WAL + busy_timeout serialize writes);
// Postgres keeps database/sql pool defaults.
func Open(_ context.Context, url string) (*Repo, error) {
	driver, dsn, err := parseURL(url)
	if err != nil {
		return nil, err
	}
	sqlDB, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, err
	}
	if driver == "sqlite" {
		sqlDB.SetMaxOpenConns(1) // ponytail: single writer; multi-node -> Postgres
	}
	return &Repo{db: sqlDB, d: dialectFor(driver)}, nil
}

func (r *Repo) Close()                         { _ = r.db.Close() }
func (r *Repo) Ping(ctx context.Context) error { return r.db.PingContext(ctx) }

// Migrate runs the embedded goose migration set for this repo's dialect. Called
// once on boot; idempotent. Uses goose's instance-based Provider (no globals).
func (r *Repo) Migrate(ctx context.Context) error {
	sub, err := fs.Sub(migrations.FS, r.d.name) // "postgres" | "sqlite"
	if err != nil {
		return err
	}
	gd := goose.DialectPostgres
	if !r.d.isPG {
		gd = goose.DialectSQLite3
	}
	p, err := goose.NewProvider(gd, r.db, sub)
	if err != nil {
		return err
	}
	_, err = p.Up(ctx)
	return err
}

func (r *Repo) OrgByTokenHash(ctx context.Context, hash string) (org *Org, err error) {
	ctx, span := obs.StartDBSpan(ctx, "db.OrgByTokenHash")
	defer func() { obs.EndSpan(span, err) }()

	q := r.d.rebind(`SELECT o.id, o.slug, k.read_only FROM api_keys k
	           JOIN organizations o ON o.id = k.org_id
	           WHERE k.token_hash = ? AND k.revoked_at IS NULL`)
	var o Org
	err = r.db.QueryRowContext(ctx, q, hash).Scan(&o.ID, &o.Slug, &o.ReadOnly)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUnauthorized
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}

func (r *Repo) UpsertArtifact(ctx context.Context, orgID int64, hash string, size int64, tag string) (err error) {
	ctx, span := obs.StartDBSpan(ctx, "db.UpsertArtifact")
	defer func() { obs.EndSpan(span, err) }()

	q := r.d.rebind(fmt.Sprintf(`INSERT INTO cache_artifacts (org_id, hash, size_bytes, artifact_tag)
	           VALUES (?, ?, ?, NULLIF(?,''))
	           ON CONFLICT (org_id, hash) DO UPDATE
	             SET size_bytes = excluded.size_bytes,
	                 artifact_tag = excluded.artifact_tag,
	                 last_accessed_at = %s`, r.d.now))
	_, err = r.db.ExecContext(ctx, q, orgID, hash, size, tag)
	return err
}

func (r *Repo) ArtifactExists(ctx context.Context, orgID int64, hash string) (exists bool, err error) {
	ctx, span := obs.StartDBSpan(ctx, "db.ArtifactExists")
	defer func() { obs.EndSpan(span, err) }()

	q := r.d.rebind(`SELECT EXISTS(SELECT 1 FROM cache_artifacts WHERE org_id=? AND hash=?)`)
	err = r.db.QueryRowContext(ctx, q, orgID, hash).Scan(&exists)
	return exists, err
}

func (r *Repo) TouchArtifact(ctx context.Context, orgID int64, hash string) (err error) {
	ctx, span := obs.StartDBSpan(ctx, "db.TouchArtifact")
	defer func() { obs.EndSpan(span, err) }()

	q := r.d.rebind(fmt.Sprintf(`UPDATE cache_artifacts SET last_accessed_at = %s WHERE org_id=? AND hash=?`, r.d.now))
	_, err = r.db.ExecContext(ctx, q, orgID, hash)
	return err
}

// ArtifactTag returns the stored x-artifact-tag (the client's HMAC signature) for
// an artifact, or "" if the artifact has none / doesn't exist. Read on the download
// path only when REQUIRE_ARTIFACT_SIGNATURE is on, so the default hot path stays
// DB-free. A no-rows result is not an error — an absent tag is "".
func (r *Repo) ArtifactTag(ctx context.Context, orgID int64, hash string) (tag string, err error) {
	ctx, span := obs.StartDBSpan(ctx, "db.ArtifactTag")
	defer func() { obs.EndSpan(span, err) }()

	q := r.d.rebind(`SELECT COALESCE(artifact_tag,'') FROM cache_artifacts WHERE org_id=? AND hash=?`)
	err = r.db.QueryRowContext(ctx, q, orgID, hash).Scan(&tag)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return tag, err
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
	q := r.d.rebind(`INSERT INTO organizations (idp_org_id, slug, name)
	           VALUES (?, ?, ?)
	           ON CONFLICT (idp_org_id) DO UPDATE SET idp_org_id = excluded.idp_org_id
	           RETURNING id, slug, idp_org_id`)
	var o Org
	var idp sql.NullString
	err = r.db.QueryRowContext(ctx, q, idpOrgID, orgSlugFor(idpOrgID), name).
		Scan(&o.ID, &o.Slug, &idp)
	if err != nil {
		return nil, err
	}
	o.IdpOrgID = idp.String
	return &o, nil
}

type APIKey struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	ProjectID  *int64     `json:"project_id"`
	ReadOnly   bool       `json:"read_only"`
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedAt  time.Time  `json:"created_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
}

// CreateToken stores only the SHA-256 hash (via auth.HashToken upstream);
// the plaintext token is never persisted or logged. readOnly marks a token that
// may pull from the cache but never push (enforced in internal/turbo).
func (r *Repo) CreateToken(ctx context.Context, orgID int64, name, tokenHash string, readOnly bool) (id int64, err error) {
	ctx, span := obs.StartDBSpan(ctx, "db.CreateToken")
	defer func() { obs.EndSpan(span, err) }()

	q := r.d.rebind(`INSERT INTO api_keys (org_id, name, token_hash, read_only) VALUES (?, ?, ?, ?) RETURNING id`)
	err = r.db.QueryRowContext(ctx, q, orgID, name, tokenHash, readOnly).Scan(&id)
	return id, err
}

// ListTokens is org-scoped and never selects token_hash.
func (r *Repo) ListTokens(ctx context.Context, orgID int64) (keys []APIKey, err error) {
	ctx, span := obs.StartDBSpan(ctx, "db.ListTokens")
	defer func() { obs.EndSpan(span, err) }()

	q := r.d.rebind(`SELECT id, name, project_id, read_only, last_used_at, created_at, revoked_at
	           FROM api_keys WHERE org_id = ? ORDER BY created_at DESC`)
	rows, err := r.db.QueryContext(ctx, q, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k APIKey
		if err = rows.Scan(&k.ID, &k.Name, &k.ProjectID, &k.ReadOnly, &k.LastUsedAt, &k.CreatedAt, &k.RevokedAt); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// RevokeToken sets revoked_at; org-scoped so a token can't revoke another org's key.
func (r *Repo) RevokeToken(ctx context.Context, orgID, tokenID int64) (ok bool, err error) {
	ctx, span := obs.StartDBSpan(ctx, "db.RevokeToken")
	defer func() { obs.EndSpan(span, err) }()

	q := r.d.rebind(fmt.Sprintf(`UPDATE api_keys SET revoked_at = %s
	           WHERE id = ? AND org_id = ? AND revoked_at IS NULL`, r.d.now))
	res, err := r.db.ExecContext(ctx, q, tokenID, orgID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
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

	q := r.d.rebind(`INSERT INTO projects (org_id, slug, name) VALUES (?, ?, ?)
	           RETURNING id, slug, name, created_at`)
	err = r.db.QueryRowContext(ctx, q, orgID, slug, name).Scan(&proj.ID, &proj.Slug, &proj.Name, &proj.CreatedAt)
	return proj, err
}

func (r *Repo) ListProjects(ctx context.Context, orgID int64) (projects []Project, err error) {
	ctx, span := obs.StartDBSpan(ctx, "db.ListProjects")
	defer func() { obs.EndSpan(span, err) }()

	q := r.d.rebind(`SELECT id, slug, name, created_at FROM projects WHERE org_id = ? ORDER BY name`)
	rows, err := r.db.QueryContext(ctx, q, orgID)
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
	return projects, rows.Err()
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

	q1 := r.d.rebind(`SELECT COALESCE(SUM(size_bytes),0), COUNT(*) FROM cache_artifacts WHERE org_id=?`)
	if err = r.db.QueryRowContext(ctx, q1, orgID).Scan(&s.StorageBytes, &s.ArtifactCount); err != nil {
		return s, err
	}
	q2 := r.d.rebind(`SELECT COALESCE(SUM(hits),0), COALESCE(SUM(misses),0),
	                   COALESCE(SUM(bytes_up),0), COALESCE(SUM(bytes_down),0)
	            FROM usage_daily WHERE org_id=?`)
	if err = r.db.QueryRowContext(ctx, q2, orgID).Scan(&s.Hits, &s.Misses, &s.BytesUp, &s.BytesDown); err != nil {
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

	var q string
	var args []any
	if r.d.isPG {
		q = r.d.rebind(`SELECT to_char(day,'YYYY-MM-DD'), hits, misses, bytes_up, bytes_down
		           FROM usage_daily
		           WHERE org_id=? AND day >= CURRENT_DATE - ?::int
		           ORDER BY day`)
		args = []any{orgID, days}
	} else {
		// day is TEXT 'YYYY-MM-DD'; window bound via date('now','-N days').
		q = `SELECT day, hits, misses, bytes_up, bytes_down
		     FROM usage_daily
		     WHERE org_id=? AND day >= date('now', ?)
		     ORDER BY day`
		args = []any{orgID, fmt.Sprintf("-%d days", days)}
	}
	rows, err := r.db.QueryContext(ctx, q, args...)
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

	q := r.d.rebind(`SELECT hash, size_bytes, artifact_tag, created_at, last_accessed_at
	           FROM cache_artifacts WHERE org_id=?
	           ORDER BY created_at DESC LIMIT ? OFFSET ?`)
	rows, err := r.db.QueryContext(ctx, q, orgID, limit, offset)
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
	return artifacts, rows.Err()
}

// AddUsage accumulates daily usage counters. Postgres casts the day param to
// date; SQLite stores it as a 'YYYY-MM-DD' TEXT key. Both accumulate via
// ON CONFLICT so the call is idempotent within a day.
func (r *Repo) AddUsage(ctx context.Context, orgID int64, day time.Time, up, down, hits, misses int64) (err error) {
	ctx, span := obs.StartDBSpan(ctx, "db.AddUsage")
	defer func() { obs.EndSpan(span, err) }()

	var dayArg any
	var dayExpr string
	if r.d.isPG {
		dayArg, dayExpr = day, "?::date"
	} else {
		dayArg, dayExpr = day.UTC().Format("2006-01-02"), "?"
	}
	q := r.d.rebind(fmt.Sprintf(`INSERT INTO usage_daily (org_id, day, bytes_up, bytes_down, hits, misses)
	           VALUES (?, %s, ?, ?, ?, ?)
	           ON CONFLICT (org_id, day) DO UPDATE SET
	             bytes_up   = usage_daily.bytes_up   + excluded.bytes_up,
	             bytes_down = usage_daily.bytes_down + excluded.bytes_down,
	             hits       = usage_daily.hits       + excluded.hits,
	             misses     = usage_daily.misses     + excluded.misses`, dayExpr))
	_, err = r.db.ExecContext(ctx, q, orgID, dayArg, up, down, hits, misses)
	return err
}

type ExpiredArtifact struct {
	OrgID   int64
	OrgSlug string
	Hash    string
}

// ExpiredArtifacts is batched (limit) and system-wide (not org-scoped). The
// cutoff is bound in a dialect-matching format so the '<' comparison is correct
// on both engines (Postgres timestamptz vs SQLite ISO-8601 text).
func (r *Repo) ExpiredArtifacts(ctx context.Context, cutoff time.Time, limit int) (out []ExpiredArtifact, err error) {
	ctx, span := obs.StartDBSpan(ctx, "db.ExpiredArtifacts")
	defer func() { obs.EndSpan(span, err) }()

	q := r.d.rebind(`SELECT a.org_id, o.slug, a.hash
	           FROM cache_artifacts a JOIN organizations o ON o.id = a.org_id
	           WHERE a.last_accessed_at < ?
	           ORDER BY a.last_accessed_at LIMIT ?`)
	rows, err := r.db.QueryContext(ctx, q, r.timeArg(cutoff), limit)
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
	return out, rows.Err()
}

// timeArg formats a timestamp for a bound parameter: a time.Time for Postgres,
// and the CURRENT_TIMESTAMP text format ('YYYY-MM-DD HH:MM:SS' UTC) for SQLite
// so lexicographic comparison against stored values is chronological.
func (r *Repo) timeArg(t time.Time) any {
	if r.d.isPG {
		return t
	}
	return t.UTC().Format("2006-01-02 15:04:05")
}

func (r *Repo) DeleteArtifact(ctx context.Context, orgID int64, hash string) (err error) {
	ctx, span := obs.StartDBSpan(ctx, "db.DeleteArtifact")
	defer func() { obs.EndSpan(span, err) }()

	q := r.d.rebind(`DELETE FROM cache_artifacts WHERE org_id=? AND hash=?`)
	_, err = r.db.ExecContext(ctx, q, orgID, hash)
	return err
}

func (r *Repo) GetArtifact(ctx context.Context, orgID int64, hash string) (a Artifact, err error) {
	ctx, span := obs.StartDBSpan(ctx, "db.GetArtifact")
	defer func() { obs.EndSpan(span, err) }()

	q := r.d.rebind(`SELECT hash, size_bytes, artifact_tag, created_at, last_accessed_at
	           FROM cache_artifacts WHERE org_id=? AND hash=?`)
	err = r.db.QueryRowContext(ctx, q, orgID, hash).Scan(&a.Hash, &a.SizeBytes, &a.Tag, &a.CreatedAt, &a.LastAccessedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Artifact{}, ErrArtifactNotFound
	}
	return a, err
}

// ListArtifactHashes returns every artifact hash for the org (used to remove
// blobs before a clear-all). Operator-scale counts; read in one query.
func (r *Repo) ListArtifactHashes(ctx context.Context, orgID int64) (hashes []string, err error) {
	ctx, span := obs.StartDBSpan(ctx, "db.ListArtifactHashes")
	defer func() { obs.EndSpan(span, err) }()

	q := r.d.rebind(`SELECT hash FROM cache_artifacts WHERE org_id=?`)
	rows, err := r.db.QueryContext(ctx, q, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var h string
		if err = rows.Scan(&h); err != nil {
			return nil, err
		}
		hashes = append(hashes, h)
	}
	return hashes, rows.Err()
}

func (r *Repo) DeleteAllArtifacts(ctx context.Context, orgID int64) (n int64, err error) {
	ctx, span := obs.StartDBSpan(ctx, "db.DeleteAllArtifacts")
	defer func() { obs.EndSpan(span, err) }()

	q := r.d.rebind(`DELETE FROM cache_artifacts WHERE org_id=?`)
	res, err := r.db.ExecContext(ctx, q, orgID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// setLastAccessedForTest back-dates an artifact's last_accessed_at. Test-only
// helper (exercised by TestExpiredArtifacts); dialect-aware via timeArg.
func (r *Repo) setLastAccessedForTest(ctx context.Context, orgID int64, hash string, t time.Time) error {
	q := r.d.rebind(`UPDATE cache_artifacts SET last_accessed_at = ? WHERE org_id=? AND hash=?`)
	_, err := r.db.ExecContext(ctx, q, r.timeArg(t), orgID, hash)
	return err
}
