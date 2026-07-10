package db

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace/noop"
)

// testRepo returns a migrated repo. Default: a fresh temp-file SQLite (no
// external service). Set TEST_DATABASE_URL to a Postgres URL to also exercise
// the Postgres dialect (used by the CI matrix). Either way the repo is migrated
// via Repo.Migrate, so the test needs no external goose step.
func testRepo(t *testing.T) *Repo {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		url = "sqlite:" + filepath.Join(t.TempDir(), "test.db")
	}
	r, err := Open(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(r.Close)
	return r
}

// seedOrg inserts an organization directly and returns its id, using the
// repo's own dialect for the RETURNING/placeholder differences.
func seedOrg(t *testing.T, r *Repo, slug, name string) int64 {
	t.Helper()
	var id int64
	q := r.d.rebind(`INSERT INTO organizations (slug, name) VALUES (?, ?) RETURNING id`)
	if err := r.db.QueryRowContext(context.Background(), q, slug, name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestTokenLookupAndArtifactUpsert(t *testing.T) {
	r := testRepo(t)
	ctx := context.Background()
	orgID := seedOrg(t, r, "team-a", "A")

	if _, err := r.CreateToken(ctx, orgID, "ci", "deadbeef", false); err != nil {
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
	again, err := r.EnsureOrgByIdpID(ctx, "idp-org-abc", "Acme Renamed")
	if err != nil || again.ID != org.ID {
		t.Fatalf("re-ensure = %+v, %v (want id %d)", again, err, org.ID)
	}

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
	if _, err := r.CreateToken(ctx, org.ID, "ci-ro", "hash-ro", true); err != nil {
		t.Fatalf("CreateToken(readOnly) = %v", err)
	}
	roOrg, err := r.OrgByTokenHash(ctx, "hash-ro")
	if err != nil || !roOrg.ReadOnly {
		t.Fatalf("OrgByTokenHash(read-only).ReadOnly = %v, %v; want true", roOrg, err)
	}
	rwOrg, err := r.OrgByTokenHash(ctx, "hash-xyz")
	if err != nil || rwOrg.ReadOnly {
		t.Fatalf("OrgByTokenHash(read-write).ReadOnly = %v, %v; want false", rwOrg, err)
	}
	ok, err := r.RevokeToken(ctx, org.ID, id)
	if err != nil || !ok {
		t.Fatalf("RevokeToken = %v, %v", ok, err)
	}
	if _, err := r.OrgByTokenHash(ctx, "hash-xyz"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("revoked token lookup = %v, want ErrUnauthorized", err)
	}
	other, _ := r.EnsureOrgByIdpID(ctx, "idp-org-other", "Other")
	id2, _ := r.CreateToken(ctx, org.ID, "k2", "hash-2", false)
	if ok, _ := r.RevokeToken(ctx, other.ID, id2); ok {
		t.Fatal("cross-org revoke must not succeed")
	}

	p, err := r.CreateProject(ctx, org.ID, "web", "Web App")
	if err != nil || p.ID == 0 {
		t.Fatalf("CreateProject = %+v, %v", p, err)
	}
	ps, _ := r.ListProjects(ctx, org.ID)
	if len(ps) != 1 {
		t.Fatalf("ListProjects = %+v", ps)
	}

	_ = r.UpsertArtifact(ctx, org.ID, "h1", 100, "")
	if err := r.AddUsage(ctx, org.ID, time.Now().UTC(), 100, 200, 3, 1); err != nil {
		t.Fatal(err)
	}
	_ = r.AddUsage(ctx, org.ID, time.Now().UTC(), 0, 50, 1, 0)
	st, err := r.Stats(ctx, org.ID)
	if err != nil {
		t.Fatal(err)
	}
	if st.StorageBytes != 100 || st.Hits != 4 || st.Misses != 1 || st.BytesDown != 250 {
		t.Fatalf("Stats = %+v", st)
	}

	// StatsSeries: today's row must come back with a non-empty day string.
	series, err := r.StatsSeries(ctx, org.ID, 7)
	if err != nil || len(series) != 1 || series[0].Day == "" || series[0].Hits != 4 {
		t.Fatalf("StatsSeries = %+v, %v", series, err)
	}

	arts, err := r.ListArtifacts(ctx, org.ID, 10, 0)
	if err != nil || len(arts) != 1 || arts[0].Hash != "h1" {
		t.Fatalf("ListArtifacts = %+v, %v", arts, err)
	}
	// timestamps must round-trip into time.Time (guards the modernc DATETIME conversion)
	if arts[0].CreatedAt.IsZero() {
		t.Fatalf("artifact CreatedAt did not scan into time.Time: %+v", arts[0])
	}
}

func TestExpiredArtifacts(t *testing.T) {
	r := testRepo(t)
	ctx := context.Background()
	org, _ := r.EnsureOrgByIdpID(ctx, "idp-exp", "Exp")
	_ = r.UpsertArtifact(ctx, org.ID, "old", 10, "")
	// back-date last_accessed_at 90 days using the repo's dialect-aware writer.
	if err := r.setLastAccessedForTest(ctx, org.ID, "old", time.Now().Add(-90*24*time.Hour)); err != nil {
		t.Fatal(err)
	}

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
