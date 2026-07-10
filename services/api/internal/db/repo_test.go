package db

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace/noop"
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

func TestRepoMethodsEmitSpans(t *testing.T) {
	r := testRepo(t)
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(noop.NewTracerProvider()) })

	_ = r.Ping(context.Background())
	_, _ = r.OrgByTokenHash(context.Background(), "nonexistent")

	var sawOrgLookup bool
	for _, s := range exp.GetSpans() {
		if s.Name == "db.OrgByTokenHash" {
			sawOrgLookup = true
		}
	}
	if !sawOrgLookup {
		t.Fatal("expected a db.OrgByTokenHash span")
	}
}

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
	id, err := r.CreateToken(ctx, org.ID, "ci", "hash-xyz", false)
	if err != nil || id == 0 {
		t.Fatalf("CreateToken = %d, %v", id, err)
	}
	keys, err := r.ListTokens(ctx, org.ID)
	if err != nil || len(keys) != 1 || keys[0].Name != "ci" {
		t.Fatalf("ListTokens = %+v, %v", keys, err)
	}
	if keys[0].ReadOnly {
		t.Fatalf("default token should be read-write, got read_only=true")
	}
	// read-only token: the flag persists and rides OrgByTokenHash into the principal.
	if _, err := r.CreateToken(ctx, org.ID, "ci-ro", "hash-ro", true); err != nil {
		t.Fatalf("CreateToken(readOnly) = %v", err)
	}
	roOrg, err := r.OrgByTokenHash(ctx, "hash-ro")
	if err != nil || !roOrg.ReadOnly {
		t.Fatalf("OrgByTokenHash(read-only token).ReadOnly = %v, %v; want true", roOrg, err)
	}
	rwOrg, err := r.OrgByTokenHash(ctx, "hash-xyz")
	if err != nil || rwOrg.ReadOnly {
		t.Fatalf("OrgByTokenHash(read-write token).ReadOnly = %v, %v; want false", rwOrg, err)
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
	id2, _ := r.CreateToken(ctx, org.ID, "k2", "hash-2", false)
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
