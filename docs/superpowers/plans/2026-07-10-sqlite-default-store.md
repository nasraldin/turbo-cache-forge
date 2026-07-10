# SQLite Default Store Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make SQLite the default, zero-setup metadata store so `docker compose up` needs no external database, while keeping Postgres as a first-class opt-in.

**Architecture:** All DB coupling lives in one file (`services/api/internal/db/repo.go`). Rewrite it from `pgxpool` onto the standard `database/sql`, registering two drivers in one binary — `pgx/v5/stdlib` (Postgres) and `modernc.org/sqlite` (pure-Go, no cgo). The engine is chosen from the `DATABASE_URL` scheme; migrations are embedded and run on boot via goose's instance-based `Provider`.

**Tech Stack:** Go 1.25, `database/sql`, `github.com/jackc/pgx/v5/stdlib`, `modernc.org/sqlite`, `github.com/pressly/goose/v3` (library mode), Docker Compose.

## Global Constraints

- **Go version:** `go 1.25.0` (matches `services/api/go.mod`).
- **No cgo:** the API image is `distroless/static` built with `CGO_ENABLED=0`. The SQLite driver MUST be `modernc.org/sqlite` (pure Go). Never `mattn/go-sqlite3`.
- **`pgx/v5/stdlib` is not a new module** — it is a subpackage of the already-required `github.com/jackc/pgx/v5 v5.9.2`. Only two new modules are added: `modernc.org/sqlite` and `github.com/pressly/goose/v3`.
- **snake_case end to end** in SQL and JSON (unchanged).
- **Download hot path stays DB-free** — no new DB reads on GET/HEAD except the existing signature-enforcement path.
- **Placeholders:** author every query with `?`; a `dialect.rebind` converts `?`→`$N` for Postgres. Our queries never contain a literal `?` outside placeholders.
- **Two dialect-parallel migration sets**, same version numbers (`001`, `002`, `003`) and same tables/columns.
- **SQLite pragmas on every connection:** `journal_mode=WAL`, `busy_timeout=5000`, `foreign_keys=1`, and `SetMaxOpenConns(1)`.
- **Default DB when `DATABASE_URL` is empty:** `sqlite:///data/tcf.db`.

---

### Task 1: New dependencies + embedded, dialect-split migrations

**Files:**
- Create: `services/api/internal/db/migrations/migrations.go`
- Create: `services/api/internal/db/migrations/postgres/001_initial.sql` (move from `infra/migrations/001_initial.sql`, unchanged)
- Create: `services/api/internal/db/migrations/postgres/002_usage_and_indexes.sql` (move, unchanged)
- Create: `services/api/internal/db/migrations/postgres/003_token_readonly.sql` (move, unchanged)
- Create: `services/api/internal/db/migrations/sqlite/001_initial.sql`
- Create: `services/api/internal/db/migrations/sqlite/002_usage_and_indexes.sql`
- Create: `services/api/internal/db/migrations/sqlite/003_token_readonly.sql`
- Delete: `infra/migrations/` (the three files move into the module so Go `embed` can reach them)
- Modify: `services/api/go.mod`, `services/api/go.sum` (via `go mod tidy`)
- Test: `services/api/internal/db/migrations/migrations_test.go`

**Interfaces:**
- Produces: package `migrations` exporting `var FS embed.FS` (rooted so it contains `postgres/` and `sqlite/` dirs).

- [ ] **Step 1: Move the Postgres migrations into the module**

```bash
cd /Users/itapps03/Sources/DevSecOps/turbo-cache-forge
mkdir -p services/api/internal/db/migrations/postgres services/api/internal/db/migrations/sqlite
git mv infra/migrations/001_initial.sql          services/api/internal/db/migrations/postgres/001_initial.sql
git mv infra/migrations/002_usage_and_indexes.sql services/api/internal/db/migrations/postgres/002_usage_and_indexes.sql
git mv infra/migrations/003_token_readonly.sql    services/api/internal/db/migrations/postgres/003_token_readonly.sql
rmdir infra/migrations 2>/dev/null || true
```

- [ ] **Step 2: Write the embed file** — `services/api/internal/db/migrations/migrations.go`

```go
// Package migrations embeds the goose SQL migrations for both dialects.
// The API runs them on boot (see db.Repo.Migrate), so no external migrate
// step is required. Files live in dialect subdirs: postgres/ and sqlite/.
package migrations

import "embed"

//go:embed postgres sqlite
var FS embed.FS
```

- [ ] **Step 3: Write the SQLite migrations**

`services/api/internal/db/migrations/sqlite/001_initial.sql`:

```sql
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
```

`services/api/internal/db/migrations/sqlite/002_usage_and_indexes.sql`:

```sql
-- +goose Up
-- day is TEXT 'YYYY-MM-DD' (SQLite has no DATE type; lexicographic order = chronological).
CREATE TABLE usage_daily (
    org_id     INTEGER NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    day        TEXT   NOT NULL,
    bytes_up   INTEGER NOT NULL DEFAULT 0,
    bytes_down INTEGER NOT NULL DEFAULT 0,
    hits       INTEGER NOT NULL DEFAULT 0,
    misses     INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (org_id, day)
);

CREATE INDEX idx_cache_artifacts_project_id ON cache_artifacts (project_id);
CREATE INDEX idx_api_keys_project_id        ON api_keys (project_id);
CREATE INDEX idx_cache_artifacts_last_accessed ON cache_artifacts (last_accessed_at);
CREATE INDEX idx_cache_artifacts_org_created ON cache_artifacts (org_id, created_at DESC);

-- +goose Down
DROP INDEX idx_cache_artifacts_org_created;
DROP INDEX idx_cache_artifacts_last_accessed;
DROP INDEX idx_api_keys_project_id;
DROP INDEX idx_cache_artifacts_project_id;
DROP TABLE usage_daily;
```

`services/api/internal/db/migrations/sqlite/003_token_readonly.sql`:

```sql
-- +goose Up
ALTER TABLE api_keys ADD COLUMN read_only INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE api_keys DROP COLUMN read_only;
```

- [ ] **Step 4: Add dependencies**

```bash
cd services/api
go get modernc.org/sqlite@v1.34.4
go get github.com/pressly/goose/v3@v3.24.1
go mod tidy
```

Expected: `go.mod` gains `modernc.org/sqlite` and `github.com/pressly/goose/v3` in the `require` block; `pgx/v5/stdlib` needs no `go get` (subpackage of existing pgx).

- [ ] **Step 5: Write the failing migrations test** — `services/api/internal/db/migrations/migrations_test.go`

```go
package migrations_test

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/db/migrations"
)

func TestSQLiteMigrationsApply(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	sqlDB, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	sub, err := fs.Sub(migrations.FS, "sqlite")
	if err != nil {
		t.Fatal(err)
	}
	p, err := goose.NewProvider(goose.DialectSQLite3, sqlDB, sub)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Up(context.Background()); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	// every table the repo uses must now exist
	for _, tbl := range []string{"organizations", "projects", "api_keys", "cache_artifacts", "usage_daily"} {
		var name string
		err := sqlDB.QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&name)
		if err != nil {
			t.Fatalf("table %q missing after migrate: %v", tbl, err)
		}
	}
	// read_only column from 003 must exist
	var ro int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('api_keys') WHERE name='read_only'`).Scan(&ro); err != nil || ro != 1 {
		t.Fatalf("read_only column missing: count=%d err=%v", ro, err)
	}
}
```

- [ ] **Step 6: Run it to verify it fails, then passes**

```bash
cd services/api
go test ./internal/db/migrations/ -run TestSQLiteMigrationsApply -v
```

Expected: PASS (all 5 tables + `read_only` present). If it fails on the `embed` directive, confirm `migrations.go` sits in the same dir as `postgres/` and `sqlite/`.

- [ ] **Step 7: Commit**

```bash
git add services/api/internal/db/migrations services/api/go.mod services/api/go.sum
git commit -m "feat(db): embed dialect-split migrations (postgres + sqlite), boot-ready

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Dialect + rebind + `database/sql` connection core

**Files:**
- Create: `services/api/internal/db/dialect.go`
- Test: `services/api/internal/db/dialect_test.go`

**Interfaces:**
- Produces:
  - `type dialect struct { name string; isPG bool; now string }`
  - `func (d dialect) rebind(q string) string`
  - `func dialectFor(driver string) dialect` where `driver` is `"pgx"` or `"sqlite"`.
  - `func parseURL(rawURL string) (driver, dsn string, err error)` — maps a `DATABASE_URL` to a `database/sql` driver name + DSN. `postgres://`/`postgresql://` → `("pgx", <url unchanged>)`; `sqlite:`/`file:`/bare path → `("sqlite", <file dsn with pragmas>)`.

- [ ] **Step 1: Write the failing test** — `services/api/internal/db/dialect_test.go`

```go
package db

import "testing"

func TestRebindPostgres(t *testing.T) {
	d := dialectFor("pgx")
	got := d.rebind("SELECT * FROM t WHERE a=? AND b=? AND c=?")
	want := "SELECT * FROM t WHERE a=$1 AND b=$2 AND c=$3"
	if got != want {
		t.Fatalf("rebind pg = %q, want %q", got, want)
	}
	if d.rebind("x=?::date") != "x=$1::date" {
		t.Fatalf("rebind must keep ::date suffix")
	}
}

func TestRebindSQLiteIsNoop(t *testing.T) {
	d := dialectFor("sqlite")
	q := "SELECT * FROM t WHERE a=? AND b=?"
	if d.rebind(q) != q {
		t.Fatalf("sqlite rebind must be a no-op")
	}
	if d.now != "CURRENT_TIMESTAMP" {
		t.Fatalf("sqlite now = %q, want CURRENT_TIMESTAMP", d.now)
	}
}

func TestParseURL(t *testing.T) {
	cases := []struct {
		in, driver string
	}{
		{"postgres://u:p@h:5432/db?sslmode=disable", "pgx"},
		{"postgresql://u@h/db", "pgx"},
		{"sqlite:///data/tcf.db", "sqlite"},
		{"file:/tmp/x.db", "sqlite"},
		{"/data/tcf.db", "sqlite"},
	}
	for _, c := range cases {
		drv, dsn, err := parseURL(c.in)
		if err != nil {
			t.Fatalf("parseURL(%q) err: %v", c.in, err)
		}
		if drv != c.driver {
			t.Fatalf("parseURL(%q) driver = %q, want %q", c.in, drv, c.driver)
		}
		if drv == "sqlite" && dsn == "" {
			t.Fatalf("parseURL(%q) produced empty sqlite dsn", c.in)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd services/api && go test ./internal/db/ -run 'TestRebind|TestParseURL' -v
```

Expected: FAIL — `dialectFor`, `parseURL` undefined.

- [ ] **Step 3: Write `services/api/internal/db/dialect.go`**

```go
package db

import (
	"fmt"
	"strconv"
	"strings"
)

// dialect carries the few SQL fragments that differ between Postgres and
// SQLite. Everything else in repo.go is portable and shared verbatim.
type dialect struct {
	name string // "postgres" | "sqlite"
	isPG bool
	now  string // current-timestamp expression: now() vs CURRENT_TIMESTAMP
}

func dialectFor(driver string) dialect {
	if driver == "pgx" {
		return dialect{name: "postgres", isPG: true, now: "now()"}
	}
	return dialect{name: "sqlite", isPG: false, now: "CURRENT_TIMESTAMP"}
}

// rebind converts '?' placeholders to '$1,$2,...' for Postgres; SQLite keeps
// '?'. Our queries never contain a literal '?' outside a placeholder, so a
// straight positional scan is safe.
func (d dialect) rebind(q string) string {
	if !d.isPG {
		return q
	}
	var b strings.Builder
	n := 0
	for i := 0; i < len(q); i++ {
		if q[i] == '?' {
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
			continue
		}
		b.WriteByte(q[i])
	}
	return b.String()
}

// parseURL maps a DATABASE_URL to a database/sql driver name and DSN.
//   - postgres:// or postgresql:// -> driver "pgx", DSN = the URL unchanged
//     (pgx/stdlib parses the full URL).
//   - sqlite:, file:, or a bare filesystem path -> driver "sqlite", DSN is a
//     modernc file DSN with WAL + busy_timeout + foreign_keys pragmas.
func parseURL(rawURL string) (driver, dsn string, err error) {
	switch {
	case strings.HasPrefix(rawURL, "postgres://"), strings.HasPrefix(rawURL, "postgresql://"):
		return "pgx", rawURL, nil
	case strings.HasPrefix(rawURL, "sqlite:"):
		return "sqlite", sqliteDSN(strings.TrimPrefix(strings.TrimPrefix(rawURL, "sqlite://"), "sqlite:")), nil
	case strings.HasPrefix(rawURL, "file:"):
		return "sqlite", sqliteDSN(strings.TrimPrefix(rawURL, "file:")), nil
	case strings.HasPrefix(rawURL, "/"), strings.HasPrefix(rawURL, "./"):
		return "sqlite", sqliteDSN(rawURL), nil
	default:
		return "", "", fmt.Errorf("db: unrecognized DATABASE_URL scheme in %q", rawURL)
	}
}

// sqliteDSN builds a modernc.org/sqlite DSN with the pragmas the app relies on.
// path is a plain filesystem path (e.g. /data/tcf.db).
func sqliteDSN(path string) string {
	return "file:" + path +
		"?_pragma=journal_mode(WAL)" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=foreign_keys(1)"
}
```

- [ ] **Step 4: Run to verify it passes**

```bash
cd services/api && go test ./internal/db/ -run 'TestRebind|TestParseURL' -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add services/api/internal/db/dialect.go services/api/internal/db/dialect_test.go
git commit -m "feat(db): add dialect shim, ?->\$N rebind, and DATABASE_URL scheme parser

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Rewrite `repo.go` onto `database/sql` (all 20 queries + Migrate)

This is the core task: replace `pgxpool` with `database/sql`, port every query to `?` + dialect fragments, swap `pgx.ErrNoRows`→`sql.ErrNoRows` and pgx `CommandTag`→`sql.Result`, and add `Repo.Migrate`.

**Files:**
- Modify (full rewrite): `services/api/internal/db/repo.go`
- Modify: `services/api/internal/db/repo_test.go` (default to SQLite; keep Postgres via `TEST_DATABASE_URL`)

**Interfaces:**
- Consumes: `dialect`, `dialectFor`, `parseURL` (Task 2); `migrations.FS` (Task 1).
- Produces (signatures unchanged from today, so all handler interfaces still match):
  - `func Open(ctx context.Context, url string) (*Repo, error)`
  - `func (r *Repo) Migrate(ctx context.Context) error` — **new**; runs the embedded goose set for the repo's dialect.
  - `func (r *Repo) Close()`, `Ping(ctx)`, and the existing 18 data methods with identical signatures.
  - `Repo` struct becomes `struct { db *sql.DB; d dialect }`.

- [ ] **Step 1: Update the test harness first (it defines the contract)** — replace `services/api/internal/db/repo_test.go` in full

```go
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
```

- [ ] **Step 2: Run the tests to confirm they fail against the not-yet-rewritten repo**

```bash
cd services/api && go test ./internal/db/ -run 'TestTokenLookup|TestEnsureOrg|TestExpired' -v
```

Expected: FAIL to compile — `r.db`, `r.d`, `Repo.Migrate`, `setLastAccessedForTest` don't exist yet, and `r.pool` was removed. That's the red state.

- [ ] **Step 3: Rewrite `services/api/internal/db/repo.go` in full**

```go
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
	// ReadOnly is the read-only flag of the *token* used for this request.
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

// ArtifactTag returns the stored x-artifact-tag, or "" if absent/missing.
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
	return "org-" + hex.EncodeToString(sum[:6])
}

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

func (r *Repo) CreateToken(ctx context.Context, orgID int64, name, tokenHash string, readOnly bool) (id int64, err error) {
	ctx, span := obs.StartDBSpan(ctx, "db.CreateToken")
	defer func() { obs.EndSpan(span, err) }()

	q := r.d.rebind(`INSERT INTO api_keys (org_id, name, token_hash, read_only) VALUES (?, ?, ?, ?) RETURNING id`)
	err = r.db.QueryRowContext(ctx, q, orgID, name, tokenHash, readOnly).Scan(&id)
	return id, err
}

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
```

- [ ] **Step 4: Tidy and run the full db package test against SQLite (default)**

```bash
cd services/api
go mod tidy
go vet ./internal/db/...
go test -race ./internal/db/... -v
```

Expected: PASS. If `arts[0].CreatedAt.IsZero()` fails, `modernc.org/sqlite` isn't converting the `DATETIME` column to `time.Time` — pin `modernc.org/sqlite@v1.34.4` (which does) via `go get`, or, as a fallback, change `Artifact`/`APIKey` timestamp scans to go through a `sql.NullString`+`time.Parse("2006-01-02 15:04:05", …)` helper. The declared `DATETIME` type is the trigger for modernc's conversion — keep it.

- [ ] **Step 5: Run the whole api module to confirm handlers still compile against the unchanged method set**

```bash
cd services/api && go build ./... && go test -race ./...
```

Expected: build clean (handler interfaces match — signatures are unchanged); non-DB tests pass.

- [ ] **Step 6: Commit**

```bash
git add services/api/internal/db/repo.go services/api/internal/db/repo_test.go services/api/go.mod services/api/go.sum
git commit -m "feat(db): port repo from pgx to database/sql with sqlite+postgres drivers

One query set with ?->\$N rebind and a small dialect shim (now(), StatsSeries
window, AddUsage day cast, timestamp compare format). Repo.Migrate runs the
embedded goose set on boot. Repo tests now default to a temp-file SQLite; set
TEST_DATABASE_URL to also run against Postgres.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Config — default to SQLite, stop requiring `DATABASE_URL`

**Files:**
- Modify: `services/api/internal/config/config.go:46-56`
- Test: `services/api/internal/config/config_test.go` (create or append)

**Interfaces:**
- Consumes: nothing new.
- Produces: `Config.DatabaseURL` defaults to `sqlite:///data/tcf.db` when the env var is empty; no error on empty.

- [ ] **Step 1: Write the failing test** — `services/api/internal/config/config_test.go`

```go
package config

import (
	"os"
	"testing"
)

func TestDatabaseURLDefaultsToSQLite(t *testing.T) {
	// builtin auth is the compose default; set the minimum so Load() succeeds.
	t.Setenv("DATABASE_URL", "")
	t.Setenv("AUTH_MODE", "builtin")
	t.Setenv("AUTH_ROOT_USERNAME", "root")
	t.Setenv("AUTH_ROOT_PASSWORD", "root")
	_ = os.Unsetenv("OIDC_ISSUER")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() with empty DATABASE_URL should succeed, got %v", err)
	}
	if c.DatabaseURL != "sqlite:///data/tcf.db" {
		t.Fatalf("default DatabaseURL = %q, want sqlite:///data/tcf.db", c.DatabaseURL)
	}
}

func TestDatabaseURLRespectsExplicit(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u:p@h:5432/db")
	t.Setenv("AUTH_MODE", "builtin")
	t.Setenv("AUTH_ROOT_USERNAME", "root")
	t.Setenv("AUTH_ROOT_PASSWORD", "root")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.DatabaseURL != "postgres://u:p@h:5432/db" {
		t.Fatalf("explicit DATABASE_URL not honored: %q", c.DatabaseURL)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd services/api && go test ./internal/config/ -run TestDatabaseURL -v
```

Expected: FAIL — `Load()` returns "DATABASE_URL is required".

- [ ] **Step 3: Edit `config.go`** — replace lines 46 and 54-56

Change line 46 from:

```go
		DatabaseURL:    os.Getenv("DATABASE_URL"),
```

to:

```go
		// Default to a self-migrating SQLite file (zero external setup). Point
		// DATABASE_URL at postgres://… to use Postgres instead.
		DatabaseURL:    env("DATABASE_URL", "sqlite:///data/tcf.db"),
```

Delete the now-obsolete required check (lines 54-56):

```go
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("DATABASE_URL is required")
	}
```

(If `fmt` becomes unused after this, it is still used by other checks in `Load` — leave the import.)

- [ ] **Step 4: Run to verify it passes**

```bash
cd services/api && go test ./internal/config/ -run TestDatabaseURL -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add services/api/internal/config/config.go services/api/internal/config/config_test.go
git commit -m "feat(config): default DATABASE_URL to sqlite:///data/tcf.db (zero-setup)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: Boot self-migration in `main.go`

**Files:**
- Modify: `services/api/cmd/server/main.go:49-53`

**Interfaces:**
- Consumes: `Repo.Migrate(ctx)` (Task 3).

- [ ] **Step 1: Add the migrate call** — in `main.go`, replace the `repo` open block (lines 49-53):

```go
	repo, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer repo.Close()
```

with:

```go
	repo, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer repo.Close()
	// Self-migrate on boot (embedded goose set for the active dialect). Makes
	// the default SQLite deployment need no external migrate step; idempotent.
	if err := repo.Migrate(ctx); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	log.Printf("database ready (%s)", cfg.DatabaseURL)
```

- [ ] **Step 2: Build and smoke-run against a temp SQLite**

```bash
cd services/api && go build -o /tmp/tcf-server ./cmd/server
DATABASE_URL="sqlite:/tmp/tcf-smoke.db" \
  STORAGE_PATH=/tmp/tcf-smoke-data \
  AUTH_MODE=builtin AUTH_ROOT_USERNAME=root AUTH_ROOT_PASSWORD=root \
  ADDR=:18080 /tmp/tcf-server &
sleep 2
curl -s -o /dev/null -w "health -> %{http_code}\n" http://127.0.0.1:18080/health
kill %1 2>/dev/null
ls -la /tmp/tcf-smoke.db
```

Expected: `health -> 200` and `/tmp/tcf-smoke.db` exists (created + migrated on boot with no external DB).

- [ ] **Step 3: Commit**

```bash
git add services/api/cmd/server/main.go
git commit -m "feat(server): self-migrate the database on boot (no migrate step)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: Docker Compose default (SQLite) + Postgres overlay + Dockerfile

**Files:**
- Modify: `infra/docker/docker-compose.yml`
- Create: `infra/docker/docker-compose.postgres.yml`
- Modify: `infra/docker/Dockerfile:15-18` (goose stage path)
- Modify: `infra/docker/.env.example` (if present) / document defaults

**Interfaces:** none (deployment config).

- [ ] **Step 1: Rewrite `infra/docker/docker-compose.yml`** — drop `postgres` and `migrate`; SQLite default

```yaml
services:
  cache-api:
    build:
      context: ../..
      dockerfile: infra/docker/Dockerfile
      target: cache-api
    environment:
      # Default metadata store: a self-migrating SQLite file inside the
      # persisted cache-data volume. No external database, no migrate step.
      # Opt into Postgres with docker-compose.postgres.yml (see README).
      DATABASE_URL: ${DATABASE_URL:-sqlite:///data/tcf.db}
      STORAGE_BACKEND: fs
      STORAGE_PATH: /data
      OIDC_ISSUER: ${OIDC_ISSUER:-}
      OIDC_AUDIENCE: ${OIDC_AUDIENCE:-}
      OIDC_ORG_CLAIM: ${OIDC_ORG_CLAIM:-org_id}
      OIDC_ORG_ENABLED: ${OIDC_ORG_ENABLED:-true}
      CORS_ALLOWED_ORIGINS: ${CORS_ALLOWED_ORIGINS:-http://localhost:3000}
      USAGE_ROLLUP_INTERVAL_SEC: ${USAGE_ROLLUP_INTERVAL_SEC:-15}
      AUTH_MODE: ${AUTH_MODE:-builtin}
      AUTH_ROOT_USERNAME: ${AUTH_ROOT_USERNAME:-root}
      AUTH_ROOT_PASSWORD: ${AUTH_ROOT_PASSWORD:-root}
      AUTH_SECRET: ${AUTH_SECRET:-dev-secret-change-me}
    volumes: ["cache-data:/data"] # holds both artifact blobs and tcf.db
    ports: ["8080:8080"]

  dashboard:
    build:
      context: ../..
      dockerfile: apps/dashboard/Dockerfile
      args:
        NEXT_PUBLIC_API_URL: ${NEXT_PUBLIC_API_URL:-http://localhost:8080}
        NEXT_PUBLIC_CLERK_PUBLISHABLE_KEY: ${NEXT_PUBLIC_CLERK_PUBLISHABLE_KEY:-pk_test_ZXhhbXBsZS5jbGVyay5hY2NvdW50cy5kZXYk}
        NEXT_PUBLIC_ORG_ENABLED: ${NEXT_PUBLIC_ORG_ENABLED:-false}
    depends_on:
      cache-api: { condition: service_started }
    environment:
      NEXT_PUBLIC_CLERK_PUBLISHABLE_KEY: ${NEXT_PUBLIC_CLERK_PUBLISHABLE_KEY:-pk_test_ZXhhbXBsZS5jbGVyay5hY2NvdW50cy5kZXYk}
      CLERK_SECRET_KEY: ${CLERK_SECRET_KEY:-}
    ports: ["3000:3000"]

volumes:
  cache-data:
```

- [ ] **Step 2: Create the Postgres overlay** — `infra/docker/docker-compose.postgres.yml`

```yaml
# Opt into Postgres instead of the default SQLite:
#   docker compose -f infra/docker/docker-compose.yml \
#     -f infra/docker/docker-compose.postgres.yml up -d
# The API self-migrates Postgres on boot (same embedded goose set), so no
# separate migrate service is needed.
services:
  postgres:
    image: postgres:16
    environment:
      POSTGRES_USER: ${POSTGRES_USER:-tcf}
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:-tcf}
      POSTGRES_DB: ${POSTGRES_DB:-tcf}
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U ${POSTGRES_USER:-tcf}"]
      interval: 2s
      timeout: 3s
      retries: 20

  cache-api:
    environment:
      DATABASE_URL: postgres://${POSTGRES_USER:-tcf}:${POSTGRES_PASSWORD:-tcf}@postgres:5432/${POSTGRES_DB:-tcf}?sslmode=disable
    depends_on:
      postgres: { condition: service_healthy }
```

- [ ] **Step 3: Repoint the Dockerfile goose stage** — `infra/docker/Dockerfile` lines 15-18

Change:

```dockerfile
FROM golang:1.25 AS goose
RUN go install github.com/pressly/goose/v3/cmd/goose@v3.27.2
COPY infra/migrations /migrations
ENTRYPOINT ["goose"]
```

to:

```dockerfile
# Optional standalone goose image (e.g. to run Postgres migrations manually).
# The default + overlay both self-migrate on boot, so this is not required to
# run the stack. Ships the Postgres dialect set.
FROM golang:1.25 AS goose
RUN go install github.com/pressly/goose/v3/cmd/goose@v3.27.2
COPY services/api/internal/db/migrations/postgres /migrations
ENTRYPOINT ["goose"]
```

- [ ] **Step 4: Validate both compose configurations**

```bash
cd /Users/itapps03/Sources/DevSecOps/turbo-cache-forge
docker compose -f infra/docker/docker-compose.yml config >/dev/null && echo "default OK"
docker compose -f infra/docker/docker-compose.yml -f infra/docker/docker-compose.postgres.yml config >/dev/null && echo "postgres overlay OK"
```

Expected: both print `OK` (no `postgres`/`migrate` service in the default; overlay adds `postgres` + sets the Postgres `DATABASE_URL`).

- [ ] **Step 5: End-to-end — SQLite-default stack with the example**

```bash
docker compose -f infra/docker/docker-compose.yml up -d --build
# wait for the API, then run the example demo exactly as the Postgres path did:
#   create a token in the dashboard (http://localhost:3000, root/root), then
#   TURBO_API=http://localhost:8080 TURBO_TOKEN=<tok> TURBO_TEAM=root ./apps/example/run-demo.sh
```

Expected: cold build → MISS + upload, warm build → remote HIT (`>>> FULL TURBO`); dashboard Overview shows hit rate / storage / artifacts. No `postgres` container is running (`docker compose ps` shows only `cache-api` + `dashboard`).

- [ ] **Step 6: Commit**

```bash
git add infra/docker/docker-compose.yml infra/docker/docker-compose.postgres.yml infra/docker/Dockerfile
git commit -m "feat(compose): SQLite-default stack; Postgres as an opt-in overlay

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: CI dialect matrix + docs

**Files:**
- Modify: `.github/workflows/ci.yml` (the `go` job)
- Modify: `apps/docs/src/content/docs/getting-started/quickstart.md`, `.../configuration.md`, `.../comparison.md`
- Modify: `README.md`

**Interfaces:** none.

- [ ] **Step 1: Update the `go` job in `.github/workflows/ci.yml`**

Replace the whole `go:` job (lines 25-70) with a matrix that runs the API tests against **both** SQLite (default, no service) and Postgres (service + `TEST_DATABASE_URL`), and the CLI once. The API test path self-migrates via `Repo.Migrate`, so the old "Apply migrations" goose step is removed.

```yaml
  go:
    name: go (${{ matrix.name }})
    runs-on: ubuntu-latest
    strategy:
      fail-fast: false
      matrix:
        include:
          - { name: cli, module: cli, db: none }
          - { name: api-sqlite, module: api, db: sqlite }
          - { name: api-postgres, module: api, db: postgres }
    services:
      postgres:
        image: postgres:16
        env:
          POSTGRES_USER: tcf
          POSTGRES_PASSWORD: tcf
          POSTGRES_DB: tcf
        ports: ["5432:5432"]
        options: >-
          --health-cmd "pg_isready -U tcf"
          --health-interval 2s
          --health-timeout 3s
          --health-retries 20
    steps:
      - uses: actions/checkout@v7
      - uses: actions/setup-go@v6
        with:
          go-version: "1.25"
          cache-dependency-path: services/${{ matrix.module }}/go.sum
      - name: Vet
        working-directory: services/${{ matrix.module }}
        run: go vet ./...
      - name: Test
        working-directory: services/${{ matrix.module }}
        env:
          # api-postgres exercises the Postgres dialect; api-sqlite and cli use
          # the built-in defaults (temp-file SQLite / no DB). The repo tests
          # self-migrate, so no external goose step is needed.
          TEST_DATABASE_URL: ${{ matrix.db == 'postgres' && 'postgres://tcf:tcf@localhost:5432/tcf?sslmode=disable' || '' }}
        run: go test -race ./...
```

(The `postgres` service starts for every matrix leg but is only used by `api-postgres`; that is harmless. The `docker` job's `migrate` image target is unchanged — it still builds the `goose` stage, now sourcing the Postgres migration set from its new path.)

- [ ] **Step 2: Update the comparison table** — `apps/docs/src/content/docs/getting-started/comparison.md`, the "Metadata store" row

Change:

```
| Metadata store | Postgres (projects, usage, orgs) | Managed | None required |
```

to:

```
| Metadata store | **SQLite (default) · Postgres (optional)** | Managed | None required |
```

And in the "Honest caveats" section, remove any implication that a database is required — Turbo Cache Forge now runs with **zero external database** by default (embedded SQLite), matching ducktors on setup while keeping Postgres for scale.

- [ ] **Step 3: Update the README** — the comparison table row and the Quickstart note

In `README.md`, change the `Metadata store` row (added in the comparison table) similarly, and update the Quickstart/What-is-it lines that say metadata "lives in Postgres" to: metadata lives in **SQLite by default (zero setup), or Postgres for scale**.

Replace, in the "What is it?" section:

```
Metadata lives in **Postgres**; artifact blobs go to the **filesystem or any
S3-compatible store** (AWS S3, Cloudflare R2, MinIO).
```

with:

```
Metadata lives in **SQLite by default — zero setup, no external database** —
or **Postgres** when you need multi-node scale; artifact blobs go to the
**filesystem or any S3-compatible store** (AWS S3, Cloudflare R2, MinIO).
```

- [ ] **Step 4: Update the docs quickstart + configuration**

In `apps/docs/src/content/docs/getting-started/quickstart.md`, note that `docker compose up` needs no database and self-migrates SQLite; add the Postgres overlay command. In `apps/docs/src/content/docs/getting-started/configuration.md` (and `reference/environment` if it lists `DATABASE_URL`), document the `DATABASE_URL` schemes: empty → `sqlite:///data/tcf.db`, `sqlite:/path`, `postgres://…`.

- [ ] **Step 5: Build the docs to confirm no broken markdown**

```bash
cd /Users/itapps03/Sources/DevSecOps/turbo-cache-forge
pnpm --filter docs build
```

Expected: pages build clean.

- [ ] **Step 6: Commit**

```bash
git add .github/workflows/ci.yml apps/docs README.md
git commit -m "ci+docs: test both DB dialects; document SQLite-default / Postgres-optional

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Self-Review

**1. Spec coverage:**
- SQLite default / zero external DB → Tasks 4 (default URL), 5 (self-migrate), 6 (compose). ✅
- Postgres first-class opt-in → Tasks 2 (scheme parse), 3 (pgx/stdlib driver), 6 (overlay). ✅
- Unify on `database/sql` + two drivers, no cgo → Task 3 + Global Constraints. ✅
- Query layer (rebind + dialect) → Tasks 2, 3. ✅
- Self-migrate on boot via embedded goose → Tasks 1 (embed), 3 (`Migrate`), 5 (call). ✅
- Concurrency (WAL, busy_timeout, foreign_keys, MaxOpenConns(1)) → Tasks 2 (DSN), 3 (Open). ✅
- Migrations dialect-split → Task 1. ✅
- CI matrix (sqlite + postgres) → Task 7. ✅
- Docs / comparison / README → Task 7. ✅
- Testing against both engines → Task 3 (harness), 7 (matrix). ✅

**2. Placeholder scan:** No "TBD"/"handle errors"/"similar to". Every code step shows complete code. The one conditional ("if IsZero fails, pin modernc") is a concrete, actionable fallback with an exact version, not a placeholder. ✅

**3. Type consistency:** `Repo{db *sql.DB; d dialect}` used consistently (`r.db`, `r.d`) across repo.go and the test harness. `dialectFor(driver string)`, `parseURL(rawURL) (driver, dsn string, err error)`, `Repo.Migrate(ctx)`, `Repo.timeArg(t)`, `Repo.setLastAccessedForTest(...)` — all defined in Task 2/3 and referenced consistently. All 18 public method signatures are unchanged from the current code, so the per-consumer handler interfaces (`MetaRepo`, `mgmt.Repo`, `cleanup.Repo`, `usage.Sink`, `auth.OrgLookup`, `oidcauth.OrgProvisioner`) keep matching without edits. ✅

No issues found requiring a new task.
