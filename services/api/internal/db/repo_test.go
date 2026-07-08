package db

import (
	"context"
	"errors"
	"os"
	"testing"
)

// Set TEST_DATABASE_URL (points at a migrated test DB) to run these.
func testRepo(t *testing.T) *Repo {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL to run db tests")
	}
	r, err := Open(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	return r
}

func TestTokenLookupAndArtifactUpsert(t *testing.T) {
	r := testRepo(t)
	ctx := context.Background()
	// seed an org + active token (hash of "turbo_test")
	var orgID int64
	err := r.pool.QueryRow(ctx,
		`INSERT INTO organizations (slug, name) VALUES ('team-a','A') RETURNING id`).Scan(&orgID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.pool.Exec(ctx,
		`INSERT INTO api_keys (org_id, name, token_hash) VALUES ($1,'ci','deadbeef')`, orgID)
	if err != nil {
		t.Fatal(err)
	}

	org, err := r.OrgByTokenHash(ctx, "deadbeef")
	if err != nil || org.Slug != "team-a" {
		t.Fatalf("OrgByTokenHash = %+v, %v", org, err)
	}
	if _, err := r.OrgByTokenHash(ctx, "nope"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("unknown token want ErrUnauthorized, got %v", err)
	}

	if err := r.UpsertArtifact(ctx, orgID, "h1", 42, ""); err != nil {
		t.Fatal(err)
	}
	ok, err := r.ArtifactExists(ctx, orgID, "h1")
	if err != nil || !ok {
		t.Fatalf("ArtifactExists = %v, %v", ok, err)
	}
}
