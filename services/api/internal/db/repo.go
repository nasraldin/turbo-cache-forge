package db

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/obs"
)

var ErrUnauthorized = errors.New("db: no matching active token")

type Org struct {
	ID   int64
	Slug string
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
