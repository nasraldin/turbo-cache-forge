# turbo-cache-forge — Phase 1 (Cache API MVP) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A self-hostable Go service that speaks the Turborepo v8 remote-cache protocol, streams artifacts to pluggable storage, authenticates CLI clients with hashed bearer tokens, and exposes Prometheus metrics + health — provable by pointing a real Turborepo repo at it.

**Architecture:** Single Go binary (`chi` router over `net/http`). CLI hot path = validate hashed token → stream bytes to/from a `Storage` backend (filesystem default, S3 optional). Postgres holds orgs, tokens, and artifact metadata; metadata writes stay off the download hot path (fire-and-forget `last_accessed`). Everything runs from `docker compose up` with zero cloud accounts.

**Tech Stack:** Go 1.25 (raised from 1.24 to unblock pgx v5.9.2 / x/crypto v0.52.0 Dependabot updates, both of which require go1.25), chi v5, pgx v5 (+ pgxpool), goose (SQL migrations), aws-sdk-go-v2 (S3 backend), prometheus/client_golang, stdlib `testing`. Postgres 16 in docker-compose.

## Global Constraints

- Go module path: `github.com/nasraldin/turbo-cache-forge/services/api` (placeholder `<org>` = `turbo-cache-forge`; find/replace when the real git org is chosen). All internal imports use this prefix.
- Backend imports **no auth-vendor SDK**. Phase 1 CLI auth is hashed bearer tokens only; OIDC/JWT is Phase 3.
- Storage is accessed **only** through the `storage.Storage` interface — no direct disk/S3 calls in handlers.
- Never buffer a whole artifact in memory: use `io.Copy` / streaming everywhere.
- **One metrics pipeline** (Prometheus). No OTel metrics in Phase 1 (tracing seam is Phase 2).
- All tables carry `org_id`. Tokens are stored only as SHA-256 hex hashes; the plaintext token is shown once at creation.
- Every task ends green (`go test ./...`) and is committed.

---

## File structure (services/api)

```
services/api/
  go.mod
  cmd/server/main.go              wiring: config → db → storage → router → listen
  internal/
    config/config.go              env → Config struct (+ test)
    storage/storage.go            Storage interface, ObjectInfo, ErrNotFound
    storage/filesystem/fs.go      local-disk backend (+ test)
    storage/s3/s3.go              S3 backend (aws-sdk-go-v2)
    storage/storagetest/suite.go  shared conformance suite run by both backends
    db/repo.go                    pgxpool + plain-SQL repository (+ test, DB-gated)
    auth/token.go                 GenerateToken, HashToken (+ test)
    auth/middleware.go            bearer-token middleware, org in context (+ test)
    turbo/handlers.go             status/HEAD/PUT/GET (+ test with fakes)
    turbo/keys.go                 storageKey(orgSlug, hash)
    obs/metrics.go                prometheus collectors + middleware
    obs/health.go                 /live /ready /health
    server/router.go              chi router assembling all of the above
infra/
  migrations/001_initial.sql      goose schema
  docker/docker-compose.yml       postgres + cache-api (fs backend)
  docker/Dockerfile               multi-stage Go build
```

---

## Task 1: Repo foundation + first green health endpoint

**Files:**
- Create: `services/api/go.mod`, `services/api/cmd/server/main.go`, `services/api/internal/server/router.go`, `services/api/internal/obs/health.go`
- Test: `services/api/internal/server/router_test.go`

**Interfaces:**
- Produces: `server.New(deps Deps) http.Handler`; `Deps` struct (fields added in later tasks — start empty). `health.Live(w,r)`, `health.Ready(readyFn func(ctx) error)`.

- [ ] **Step 1: Init module + deps**

Run in `services/api/`:
```bash
go mod init github.com/nasraldin/turbo-cache-forge/services/api
go get github.com/go-chi/chi/v5@latest
```

- [ ] **Step 2: Write the failing test**

`internal/server/router_test.go`:
```go
package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLiveEndpoint(t *testing.T) {
	srv := New(Deps{})
	req := httptest.NewRequest(http.MethodGet, "/live", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /live = %d, want 200", rec.Code)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestLiveEndpoint -v`
Expected: FAIL — `New`/`Deps` undefined.

- [ ] **Step 4: Write minimal implementation**

`internal/obs/health.go`:
```go
package obs

import (
	"context"
	"net/http"
)

// Live reports the process is running.
func Live(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// Ready reports the process can serve traffic; readyFn checks dependencies.
func Ready(readyFn func(context.Context) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if readyFn != nil {
			if err := readyFn(r.Context()); err != nil {
				http.Error(w, err.Error(), http.StatusServiceUnavailable)
				return
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	}
}
```

`internal/server/router.go`:
```go
package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/obs"
)

// Deps holds everything the router needs. Fields are added as tasks land.
type Deps struct{}

func New(_ Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Get("/live", obs.Live)
	r.Get("/ready", obs.Ready(nil))
	r.Get("/health", obs.Ready(nil))
	return r
}
```

`cmd/server/main.go`:
```go
package main

import (
	"log"
	"net/http"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/server"
)

func main() {
	srv := server.New(server.Deps{})
	log.Println("listening on :8080")
	if err := http.ListenAndServe(":8080", srv); err != nil {
		log.Fatal(err)
	}
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./... -v`
Expected: PASS.

- [ ] **Step 6: Commit**
```bash
git add services/api
git commit -m "feat(api): scaffold chi server with health endpoints"
```

---

## Task 2: Config from environment

**Files:**
- Create: `services/api/internal/config/config.go`
- Test: `services/api/internal/config/config_test.go`

**Interfaces:**
- Produces: `config.Config{ Addr, DatabaseURL, StorageBackend, StoragePath, S3Bucket, S3Endpoint, S3Region, MaxUploadBytes int64 }`; `config.Load() (Config, error)`.

- [ ] **Step 1: Write the failing test**

```go
package config

import (
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != ":8080" {
		t.Errorf("Addr = %q, want :8080", c.Addr)
	}
	if c.StorageBackend != "fs" {
		t.Errorf("StorageBackend = %q, want fs", c.StorageBackend)
	}
	if c.MaxUploadBytes != 1<<30 {
		t.Errorf("MaxUploadBytes = %d, want %d", c.MaxUploadBytes, 1<<30)
	}
}

func TestLoadRequiresDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected error when DATABASE_URL is unset")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -v`
Expected: FAIL — `Load` undefined.

- [ ] **Step 3: Write minimal implementation**

```go
package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	Addr           string
	DatabaseURL    string
	StorageBackend string // "fs" | "s3"
	StoragePath    string
	S3Bucket       string
	S3Endpoint     string
	S3Region       string
	MaxUploadBytes int64
}

func Load() (Config, error) {
	c := Config{
		Addr:           env("ADDR", ":8080"),
		DatabaseURL:    os.Getenv("DATABASE_URL"),
		StorageBackend: env("STORAGE_BACKEND", "fs"),
		StoragePath:    env("STORAGE_PATH", "/var/lib/turbo-cache-forge"),
		S3Bucket:       os.Getenv("STORAGE_S3_BUCKET"),
		S3Endpoint:     os.Getenv("STORAGE_S3_ENDPOINT"),
		S3Region:       env("STORAGE_S3_REGION", "auto"),
		MaxUploadBytes: envInt("MAX_UPLOAD_BYTES", 1<<30), // 1 GiB
	}
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("DATABASE_URL is required")
	}
	if c.StorageBackend == "s3" && c.S3Bucket == "" {
		return c, fmt.Errorf("STORAGE_S3_BUCKET is required when STORAGE_BACKEND=s3")
	}
	return c, nil
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int64) int64 {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}
```

- [ ] **Step 4: Run + commit**

Run: `go test ./internal/config/ -v` → PASS
```bash
git add services/api/internal/config
git commit -m "feat(api): env-based config loader"
```

---

## Task 3: Storage interface + filesystem backend

**Files:**
- Create: `services/api/internal/storage/storage.go`, `services/api/internal/storage/filesystem/fs.go`
- Test: `services/api/internal/storage/filesystem/fs_test.go`

**Interfaces:**
- Produces:
```go
type ObjectInfo struct{ Size int64 }
type Storage interface {
	Put(ctx context.Context, key string, r io.Reader) error
	Get(ctx context.Context, key string) (io.ReadCloser, *ObjectInfo, error)
	Head(ctx context.Context, key string) (*ObjectInfo, error)
	Delete(ctx context.Context, key string) error
}
var ErrNotFound = errors.New("storage: object not found")
```
- `filesystem.New(root string) *FS` implements `storage.Storage`.

- [ ] **Step 1: Write the interface file**

`internal/storage/storage.go`:
```go
package storage

import (
	"context"
	"errors"
	"io"
)

type ObjectInfo struct{ Size int64 }

type Storage interface {
	Put(ctx context.Context, key string, r io.Reader) error
	Get(ctx context.Context, key string) (io.ReadCloser, *ObjectInfo, error)
	Head(ctx context.Context, key string) (*ObjectInfo, error)
	Delete(ctx context.Context, key string) error
}

var ErrNotFound = errors.New("storage: object not found")
```

- [ ] **Step 2: Write the failing test**

`internal/storage/filesystem/fs_test.go`:
```go
package filesystem

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/storage"
)

func TestPutGetHead(t *testing.T) {
	fs := New(t.TempDir())
	ctx := context.Background()
	want := []byte("artifact-bytes")

	if err := fs.Put(ctx, "team-a/abc123", bytes.NewReader(want)); err != nil {
		t.Fatal(err)
	}
	info, err := fs.Head(ctx, "team-a/abc123")
	if err != nil || info.Size != int64(len(want)) {
		t.Fatalf("Head = %+v, %v", info, err)
	}
	rc, _, err := fs.Get(ctx, "team-a/abc123")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if !bytes.Equal(got, want) {
		t.Fatalf("Get = %q, want %q", got, want)
	}
}

func TestMissingIsErrNotFound(t *testing.T) {
	fs := New(t.TempDir())
	if _, err := fs.Head(context.Background(), "nope/x"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Head missing = %v, want ErrNotFound", err)
	}
}

func TestRejectsPathTraversal(t *testing.T) {
	fs := New(t.TempDir())
	err := fs.Put(context.Background(), "../../etc/passwd", bytes.NewReader([]byte("x")))
	if err == nil {
		t.Fatal("expected traversal key to be rejected")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/storage/... -v`
Expected: FAIL — `New` undefined.

- [ ] **Step 4: Write minimal implementation**

`internal/storage/filesystem/fs.go`:
```go
package filesystem

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/storage"
)

type FS struct{ root string }

func New(root string) *FS { return &FS{root: root} }

// path resolves key under root and refuses to escape it.
func (f *FS) path(key string) (string, error) {
	clean := filepath.Clean("/" + key) // strips leading .. relative to "/"
	full := filepath.Join(f.root, clean)
	if !strings.HasPrefix(full, filepath.Clean(f.root)+string(os.PathSeparator)) {
		return "", storage.ErrNotFound // path escaped root
	}
	return full, nil
}

func (f *FS) Put(_ context.Context, key string, r io.Reader) error {
	p, err := f.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p) // atomic publish
}

func (f *FS) Get(_ context.Context, key string) (io.ReadCloser, *storage.ObjectInfo, error) {
	p, err := f.path(key)
	if err != nil {
		return nil, nil, err
	}
	file, err := os.Open(p)
	if os.IsNotExist(err) {
		return nil, nil, storage.ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	st, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, nil, err
	}
	return file, &storage.ObjectInfo{Size: st.Size()}, nil
}

func (f *FS) Head(_ context.Context, key string) (*storage.ObjectInfo, error) {
	p, err := f.path(key)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(p)
	if os.IsNotExist(err) {
		return nil, storage.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &storage.ObjectInfo{Size: st.Size()}, nil
}

func (f *FS) Delete(_ context.Context, key string) error {
	p, err := f.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
```

Note: `path()` returning `ErrNotFound` on traversal makes both `Put` (error) and `Head` (not-found) tests pass. `// ponytail: reuse ErrNotFound for the escape case — a distinct ErrBadKey buys nothing a caller acts on differently`.

- [ ] **Step 5: Run + commit**

Run: `go test ./internal/storage/... -v` → PASS
```bash
git add services/api/internal/storage
git commit -m "feat(api): storage interface + filesystem backend"
```

---

## Task 4: S3 backend + shared conformance suite

**Files:**
- Create: `services/api/internal/storage/storagetest/suite.go`, `services/api/internal/storage/s3/s3.go`
- Test: `services/api/internal/storage/s3/s3_test.go`, and refactor `filesystem/fs_test.go` to call the suite

**Interfaces:**
- Produces: `storagetest.Run(t *testing.T, newStore func() storage.Storage)` — the same behavior suite both backends must pass. `s3.New(cfg s3.Config) (*Store, error)`.

- [ ] **Step 1: Extract the shared suite**

`internal/storage/storagetest/suite.go`:
```go
package storagetest

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/storage"
)

// Run exercises the contract every Storage backend must satisfy.
func Run(t *testing.T, newStore func() storage.Storage) {
	t.Helper()
	ctx := context.Background()

	t.Run("put/get/head roundtrip", func(t *testing.T) {
		s := newStore()
		want := []byte("hello-artifact")
		if err := s.Put(ctx, "team-a/h1", bytes.NewReader(want)); err != nil {
			t.Fatal(err)
		}
		info, err := s.Head(ctx, "team-a/h1")
		if err != nil || info.Size != int64(len(want)) {
			t.Fatalf("Head = %+v, %v", info, err)
		}
		rc, _, err := s.Get(ctx, "team-a/h1")
		if err != nil {
			t.Fatal(err)
		}
		defer rc.Close()
		got, _ := io.ReadAll(rc)
		if !bytes.Equal(got, want) {
			t.Fatalf("Get = %q", got)
		}
	})

	t.Run("missing → ErrNotFound", func(t *testing.T) {
		s := newStore()
		if _, err := s.Head(ctx, "team-a/missing"); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})

	t.Run("delete", func(t *testing.T) {
		s := newStore()
		_ = s.Put(ctx, "team-a/del", bytes.NewReader([]byte("x")))
		if err := s.Delete(ctx, "team-a/del"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Head(ctx, "team-a/del"); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("after delete want ErrNotFound, got %v", err)
		}
	})
}
```

Replace the roundtrip/missing cases in `filesystem/fs_test.go` with:
```go
func TestFilesystemConformance(t *testing.T) {
	storagetest.Run(t, func() storage.Storage { return New(t.TempDir()) })
}
```
(Keep `TestRejectsPathTraversal` — it's filesystem-specific.)

- [ ] **Step 2: Write the S3 backend**

Run: `go get github.com/aws/aws-sdk-go-v2/config github.com/aws/aws-sdk-go-v2/service/s3 github.com/aws/aws-sdk-go-v2/feature/s3/manager github.com/aws/aws-sdk-go-v2/credentials`

`internal/storage/s3/s3.go`:
```go
package s3

import (
	"context"
	"errors"
	"io"

	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/storage"
)

type Config struct {
	Bucket, Endpoint, Region, AccessKey, SecretKey string
}

type Store struct {
	bucket   string
	client   *awss3.Client
	uploader *manager.Uploader
}

func New(ctx context.Context, cfg Config) (*Store, error) {
	loaded, err := awscfg.LoadDefaultConfig(ctx,
		awscfg.WithRegion(cfg.Region),
		awscfg.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, "")),
	)
	if err != nil {
		return nil, err
	}
	client := awss3.NewFromConfig(loaded, func(o *awss3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = &cfg.Endpoint // R2 / MinIO
			o.UsePathStyle = true
		}
	})
	return &Store{bucket: cfg.Bucket, client: client, uploader: manager.NewUploader(client)}, nil
}

func (s *Store) Put(ctx context.Context, key string, r io.Reader) error {
	_, err := s.uploader.Upload(ctx, &awss3.PutObjectInput{
		Bucket: &s.bucket, Key: &key, Body: r, // streaming multipart, no full buffer
	})
	return err
}

func (s *Store) Get(ctx context.Context, key string) (io.ReadCloser, *storage.ObjectInfo, error) {
	out, err := s.client.GetObject(ctx, &awss3.GetObjectInput{Bucket: &s.bucket, Key: &key})
	if isNotFound(err) {
		return nil, nil, storage.ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	var size int64
	if out.ContentLength != nil {
		size = *out.ContentLength
	}
	return out.Body, &storage.ObjectInfo{Size: size}, nil
}

func (s *Store) Head(ctx context.Context, key string) (*storage.ObjectInfo, error) {
	out, err := s.client.HeadObject(ctx, &awss3.HeadObjectInput{Bucket: &s.bucket, Key: &key})
	if isNotFound(err) {
		return nil, storage.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var size int64
	if out.ContentLength != nil {
		size = *out.ContentLength
	}
	return &storage.ObjectInfo{Size: size}, nil
}

func (s *Store) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &awss3.DeleteObjectInput{Bucket: &s.bucket, Key: &key})
	return err
}

func isNotFound(err error) bool {
	var nsk *types.NoSuchKey
	var nf *types.NotFound
	return errors.As(err, &nsk) || errors.As(err, &nf)
}
```

- [ ] **Step 3: S3 test, gated on a MinIO container**

`internal/storage/s3/s3_test.go`:
```go
package s3

import (
	"context"
	"os"
	"testing"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/storage"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/storage/storagetest"
)

// Set S3_TEST_ENDPOINT (e.g. http://localhost:9000) + creds to run against MinIO.
func TestS3Conformance(t *testing.T) {
	endpoint := os.Getenv("S3_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("set S3_TEST_ENDPOINT to run S3 conformance tests")
	}
	storagetest.Run(t, func() storage.Storage {
		s, err := New(context.Background(), Config{
			Bucket:    os.Getenv("S3_TEST_BUCKET"),
			Endpoint:  endpoint,
			Region:    "auto",
			AccessKey: os.Getenv("S3_TEST_ACCESS_KEY"),
			SecretKey: os.Getenv("S3_TEST_SECRET_KEY"),
		})
		if err != nil {
			t.Fatal(err)
		}
		return s
	})
}
```

- [ ] **Step 4: Run + commit**

Run: `go test ./internal/storage/... -v` (S3 test skips without env; fs conformance passes)
Expected: PASS + one SKIP.
```bash
git add services/api/internal/storage
git commit -m "feat(api): S3 backend + shared storage conformance suite"
```

---

## Task 5: Database schema + repository

**Files:**
- Create: `infra/migrations/001_initial.sql`, `services/api/internal/db/repo.go`
- Test: `services/api/internal/db/repo_test.go`

**Interfaces:**
- Produces:
```go
type Org struct{ ID int64; Slug string }
func Open(ctx, url string) (*Repo, error)
func (r *Repo) Ping(ctx) error
func (r *Repo) OrgByTokenHash(ctx, hash string) (*Org, error)     // ErrUnauthorized if none/revoked
func (r *Repo) UpsertArtifact(ctx, orgID int64, hash string, size int64, tag string) error
func (r *Repo) ArtifactExists(ctx, orgID int64, hash string) (bool, error)
func (r *Repo) TouchArtifact(ctx, orgID int64, hash string) error
var ErrUnauthorized = errors.New("db: no matching active token")
```

- [ ] **Step 1: Write the migration**

`infra/migrations/001_initial.sql`:
```sql
-- +goose Up
CREATE TABLE organizations (
    id                  BIGSERIAL PRIMARY KEY,
    idp_org_id          TEXT UNIQUE,
    slug                TEXT NOT NULL UNIQUE,
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
```

- [ ] **Step 2: Write the failing test** (DB-gated, like the S3 suite)

Run: `go get github.com/jackc/pgx/v5 github.com/jackc/pgx/v5/pgxpool`

`internal/db/repo_test.go`:
```go
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
```

- [ ] **Step 3: Run to verify it fails/skips**

Run: `go test ./internal/db/ -v`
Expected: SKIP (no `TEST_DATABASE_URL`) — but it must compile, so `Open`/methods must exist. Before impl it FAILS to build.

- [ ] **Step 4: Write minimal implementation**

`internal/db/repo.go`:
```go
package db

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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

func (r *Repo) Close()                     { r.pool.Close() }
func (r *Repo) Ping(ctx context.Context) error { return r.pool.Ping(ctx) }

func (r *Repo) OrgByTokenHash(ctx context.Context, hash string) (*Org, error) {
	const q = `SELECT o.id, o.slug FROM api_keys k
	           JOIN organizations o ON o.id = k.org_id
	           WHERE k.token_hash = $1 AND k.revoked_at IS NULL`
	var o Org
	err := r.pool.QueryRow(ctx, q, hash).Scan(&o.ID, &o.Slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUnauthorized
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}

func (r *Repo) UpsertArtifact(ctx context.Context, orgID int64, hash string, size int64, tag string) error {
	const q = `INSERT INTO cache_artifacts (org_id, hash, size_bytes, artifact_tag)
	           VALUES ($1, $2, $3, NULLIF($4,''))
	           ON CONFLICT (org_id, hash) DO UPDATE
	             SET size_bytes = EXCLUDED.size_bytes,
	                 artifact_tag = EXCLUDED.artifact_tag,
	                 last_accessed_at = now()`
	_, err := r.pool.Exec(ctx, q, orgID, hash, size, tag)
	return err
}

func (r *Repo) ArtifactExists(ctx context.Context, orgID int64, hash string) (bool, error) {
	const q = `SELECT EXISTS(SELECT 1 FROM cache_artifacts WHERE org_id=$1 AND hash=$2)`
	var ok bool
	err := r.pool.QueryRow(ctx, q, orgID, hash).Scan(&ok)
	return ok, err
}

func (r *Repo) TouchArtifact(ctx context.Context, orgID int64, hash string) error {
	const q = `UPDATE cache_artifacts SET last_accessed_at = now() WHERE org_id=$1 AND hash=$2`
	_, err := r.pool.Exec(ctx, q, orgID, hash)
	return err
}
```

- [ ] **Step 5: Verify against a real DB, then commit**

```bash
go install github.com/pressly/goose/v3/cmd/goose@latest
createdb tcf_test  # or via docker exec into the compose postgres
goose -dir infra/migrations postgres "$TEST_DATABASE_URL" up
TEST_DATABASE_URL="postgres://...tcf_test" go test ./internal/db/ -v   # PASS
git add services/api/internal/db infra/migrations
git commit -m "feat(api): schema + pgx repository (orgs, tokens, artifacts)"
```

---

## Task 6: Token generation, hashing, and auth middleware

**Files:**
- Create: `services/api/internal/auth/token.go`, `services/api/internal/auth/middleware.go`
- Test: `services/api/internal/auth/token_test.go`, `services/api/internal/auth/middleware_test.go`

**Interfaces:**
- Produces:
```go
func GenerateToken() (token, hash string, err error)   // token "turbo_<base64url>"
func HashToken(token string) string                    // sha256 hex
type OrgLookup interface{ OrgByTokenHash(ctx, hash string) (*db.Org, error) }
func RequireToken(lookup OrgLookup) func(http.Handler) http.Handler
func OrgFromContext(ctx) (*db.Org, bool)
```

- [ ] **Step 1: Write failing token test**

`internal/auth/token_test.go`:
```go
package auth

import (
	"strings"
	"testing"
)

func TestGenerateTokenRoundTrips(t *testing.T) {
	tok, hash, err := GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(tok, "turbo_") {
		t.Errorf("token %q missing turbo_ prefix", tok)
	}
	if HashToken(tok) != hash {
		t.Error("HashToken(token) != returned hash")
	}
	tok2, _, _ := GenerateToken()
	if tok2 == tok {
		t.Error("tokens must be unique")
	}
}
```

- [ ] **Step 2: Run → FAIL, then implement**

`internal/auth/token.go`:
```go
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

func GenerateToken() (token, hash string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	token = "turbo_" + base64.RawURLEncoding.EncodeToString(b)
	return token, HashToken(token), nil
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
```
`// ponytail: no constant-time compare needed — we look up the SHA-256 of a 256-bit random token by indexed equality; there's no low-entropy secret to time-attack`

Run: `go test ./internal/auth/ -run TestGenerateToken -v` → PASS

- [ ] **Step 3: Write failing middleware test**

`internal/auth/middleware_test.go`:
```go
package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/db"
)

type fakeLookup struct{ hash string }

func (f fakeLookup) OrgByTokenHash(_ context.Context, hash string) (*db.Org, error) {
	if hash == f.hash {
		return &db.Org{ID: 1, Slug: "team-a"}, nil
	}
	return nil, db.ErrUnauthorized
}

func TestRequireToken(t *testing.T) {
	valid := "turbo_secret"
	mw := RequireToken(fakeLookup{hash: HashToken(valid)})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		org, ok := OrgFromContext(r.Context())
		if !ok || org.Slug != "team-a" {
			t.Error("org not in context")
		}
		w.WriteHeader(http.StatusOK)
	}))

	cases := []struct {
		name, header string
		want         int
	}{
		{"valid", "Bearer " + valid, http.StatusOK},
		{"wrong token", "Bearer turbo_nope", http.StatusUnauthorized},
		{"missing header", "", http.StatusUnauthorized},
		{"malformed", "Token xyz", http.StatusUnauthorized},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/x", nil)
			if c.header != "" {
				req.Header.Set("Authorization", c.header)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != c.want {
				t.Errorf("%s: code = %d, want %d", c.name, rec.Code, c.want)
			}
		})
	}
}
```

- [ ] **Step 4: Run → FAIL, then implement**

`internal/auth/middleware.go`:
```go
package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/db"
)

type OrgLookup interface {
	OrgByTokenHash(ctx context.Context, hash string) (*db.Org, error)
}

type ctxKey struct{}

func RequireToken(lookup OrgLookup) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearer(r)
			if !ok {
				http.Error(w, "missing bearer token", http.StatusUnauthorized)
				return
			}
			org, err := lookup.OrgByTokenHash(r.Context(), HashToken(token))
			if err != nil {
				http.Error(w, "invalid token", http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), ctxKey{}, org)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func bearer(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	const p = "Bearer "
	if !strings.HasPrefix(h, p) {
		return "", false
	}
	return strings.TrimPrefix(h, p), true
}

func OrgFromContext(ctx context.Context) (*db.Org, bool) {
	org, ok := ctx.Value(ctxKey{}).(*db.Org)
	return org, ok
}
```

Run: `go test ./internal/auth/ -v` → PASS

- [ ] **Step 5: Commit**
```bash
git add services/api/internal/auth
git commit -m "feat(api): bearer-token auth (hash, generate, middleware)"
```

---

## Task 7: Turborepo protocol handlers

**Files:**
- Create: `services/api/internal/turbo/keys.go`, `services/api/internal/turbo/handlers.go`
- Test: `services/api/internal/turbo/handlers_test.go`

**Interfaces:**
- Consumes: `storage.Storage`, `auth.OrgFromContext`, `db.Org`.
- Produces:
```go
type ArtifactStore interface { // subset of storage.Storage the handlers need
	Put(ctx, key string, r io.Reader) error
	Get(ctx, key string) (io.ReadCloser, *storage.ObjectInfo, error)
	Head(ctx, key string) (*storage.ObjectInfo, error)
}
type MetaRepo interface {
	UpsertArtifact(ctx, orgID int64, hash string, size int64, tag string) error
	ArtifactExists(ctx, orgID int64, hash string) (bool, error)
	TouchArtifact(ctx, orgID int64, hash string) error
}
func NewHandler(store ArtifactStore, repo MetaRepo, maxBytes int64) *Handler
func (h *Handler) Mount(r chi.Router)  // registers /v8/artifacts routes
```

**Protocol note (verify against your Turbo version — the end-to-end run in Task 9 is the real check):** `status`→200 `{"status":"enabled"}`; `PUT`→202 `{"urls":[key]}`; `GET` hit→200 `application/octet-stream` + `Content-Length`; `GET`/`HEAD` miss→404. These match the widely-used self-hosted implementations.

- [ ] **Step 1: Keys helper**

`internal/turbo/keys.go`:
```go
package turbo

// storageKey namespaces artifacts by org so tenants never collide.
func storageKey(orgSlug, hash string) string { return orgSlug + "/" + hash }
```

- [ ] **Step 2: Write the failing handler test**

`internal/turbo/handlers_test.go`:
```go
package turbo

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/auth"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/db"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/storage"
)

// in-memory fakes
type memStore struct {
	mu   sync.Mutex
	data map[string][]byte
}

func newMemStore() *memStore { return &memStore{data: map[string][]byte{}} }
func (m *memStore) Put(_ context.Context, key string, r io.Reader) error {
	b, _ := io.ReadAll(r)
	m.mu.Lock()
	m.data[key] = b
	m.mu.Unlock()
	return nil
}
func (m *memStore) Get(_ context.Context, key string) (io.ReadCloser, *storage.ObjectInfo, error) {
	m.mu.Lock()
	b, ok := m.data[key]
	m.mu.Unlock()
	if !ok {
		return nil, nil, storage.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(b)), &storage.ObjectInfo{Size: int64(len(b))}, nil
}
func (m *memStore) Head(_ context.Context, key string) (*storage.ObjectInfo, error) {
	m.mu.Lock()
	b, ok := m.data[key]
	m.mu.Unlock()
	if !ok {
		return nil, storage.ErrNotFound
	}
	return &storage.ObjectInfo{Size: int64(len(b))}, nil
}

type memRepo struct{ exists bool }

func (m *memRepo) UpsertArtifact(context.Context, int64, string, int64, string) error { return nil }
func (m *memRepo) ArtifactExists(context.Context, int64, string) (bool, error)        { return m.exists, nil }
func (m *memRepo) TouchArtifact(context.Context, int64, string) error                 { return nil }

// helper: build a router with an org already injected into context
func testRouter(store ArtifactStore, repo MetaRepo) http.Handler {
	h := NewHandler(store, repo, 1<<20)
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := auth.WithOrg(req.Context(), &db.Org{ID: 1, Slug: "team-a"})
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	h.Mount(r)
	return r
}

func TestStatus(t *testing.T) {
	r := testRouter(newMemStore(), &memRepo{})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v8/artifacts/status", nil))
	if rec.Code != 200 || !bytes.Contains(rec.Body.Bytes(), []byte(`"enabled"`)) {
		t.Fatalf("status = %d %s", rec.Code, rec.Body)
	}
}

func TestPutThenGetRoundTrip(t *testing.T) {
	store := newMemStore()
	r := testRouter(store, &memRepo{})
	body := []byte("tarball-zst-bytes")

	rec := httptest.NewRecorder()
	put := httptest.NewRequest(http.MethodPut, "/v8/artifacts/hash123?teamId=team-a", bytes.NewReader(body))
	r.ServeHTTP(rec, put)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("PUT = %d, want 202", rec.Code)
	}

	rec = httptest.NewRecorder()
	get := httptest.NewRequest(http.MethodGet, "/v8/artifacts/hash123?teamId=team-a", nil)
	r.ServeHTTP(rec, get)
	if rec.Code != 200 {
		t.Fatalf("GET = %d, want 200", rec.Code)
	}
	if !bytes.Equal(rec.Body.Bytes(), body) {
		t.Fatalf("GET body = %q, want %q", rec.Body.Bytes(), body)
	}
}

func TestGetMissIs404(t *testing.T) {
	r := testRouter(newMemStore(), &memRepo{})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v8/artifacts/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET miss = %d, want 404", rec.Code)
	}
}
```

This test needs `auth.WithOrg` — add it to `internal/auth/middleware.go` (exported constructor used by the real middleware too):
```go
// WithOrg stores org in context (used by RequireToken and by tests).
func WithOrg(ctx context.Context, org *db.Org) context.Context {
	return context.WithValue(ctx, ctxKey{}, org)
}
```
Refactor `RequireToken` to call `WithOrg` instead of `context.WithValue` directly.

- [ ] **Step 3: Run → FAIL, then implement handlers**

`internal/turbo/handlers.go`:
```go
package turbo

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/auth"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/storage"
)

type ArtifactStore interface {
	Put(ctx context.Context, key string, r io.Reader) error
	Get(ctx context.Context, key string) (io.ReadCloser, *storage.ObjectInfo, error)
	Head(ctx context.Context, key string) (*storage.ObjectInfo, error)
}

type MetaRepo interface {
	UpsertArtifact(ctx context.Context, orgID int64, hash string, size int64, tag string) error
	ArtifactExists(ctx context.Context, orgID int64, hash string) (bool, error)
	TouchArtifact(ctx context.Context, orgID int64, hash string) error
}

type Handler struct {
	store    ArtifactStore
	repo     MetaRepo
	maxBytes int64
}

func NewHandler(store ArtifactStore, repo MetaRepo, maxBytes int64) *Handler {
	return &Handler{store: store, repo: repo, maxBytes: maxBytes}
}

func (h *Handler) Mount(r chi.Router) {
	r.Get("/v8/artifacts/status", h.status)
	r.Head("/v8/artifacts/{hash}", h.head)
	r.Put("/v8/artifacts/{hash}", h.put)
	r.Get("/v8/artifacts/{hash}", h.get)
	r.Post("/v8/artifacts/events", h.events) // telemetry sink
}

func (h *Handler) status(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "enabled"})
}

func (h *Handler) events(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK) // no-op sink
}

func (h *Handler) head(w http.ResponseWriter, r *http.Request) {
	org, _ := auth.OrgFromContext(r.Context())
	key := storageKey(org.Slug, chi.URLParam(r, "hash"))
	if _, err := h.store.Head(r.Context(), key); err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) put(w http.ResponseWriter, r *http.Request) {
	org, _ := auth.OrgFromContext(r.Context())
	hash := chi.URLParam(r, "hash")
	key := storageKey(org.Slug, hash)

	body := http.MaxBytesReader(w, r.Body, h.maxBytes)
	if err := h.store.Put(r.Context(), key, body); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			http.Error(w, "artifact too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "upload failed", http.StatusInternalServerError)
		return
	}
	info, err := h.store.Head(r.Context(), key)
	if err != nil {
		http.Error(w, "upload verify failed", http.StatusInternalServerError)
		return
	}
	tag := r.Header.Get("x-artifact-tag")
	if err := h.repo.UpsertArtifact(r.Context(), org.ID, hash, info.Size, tag); err != nil {
		http.Error(w, "metadata write failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string][]string{"urls": {key}})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	org, _ := auth.OrgFromContext(r.Context())
	hash := chi.URLParam(r, "hash")
	key := storageKey(org.Slug, hash)

	rc, info, err := h.store.Get(r.Context(), key)
	if errors.Is(err, storage.ErrNotFound) {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "download failed", http.StatusInternalServerError)
		return
	}
	defer rc.Close()

	// fire-and-forget last_accessed bump — never block the download on the DB
	go func(orgID int64, hash string) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = h.repo.TouchArtifact(ctx, orgID, hash)
	}(org.ID, hash)

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", itoa(info.Size))
	if tag := r.Header.Get("x-artifact-tag"); tag != "" {
		w.Header().Set("x-artifact-tag", tag)
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, rc)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
```
Add `import "strconv"` (grouped). `// ponytail: fire-and-forget touch; batch it only if last_accessed write volume ever shows up in DB metrics`

- [ ] **Step 4: Run + commit**

Run: `go test ./internal/turbo/ ./internal/auth/ -v` → PASS
```bash
git add services/api/internal/turbo services/api/internal/auth
git commit -m "feat(api): Turborepo v8 artifact handlers (status/HEAD/PUT/GET)"
```

---

## Task 8: Prometheus metrics + wire everything into the router

**Files:**
- Create: `services/api/internal/obs/metrics.go`
- Modify: `services/api/internal/server/router.go`, `services/api/cmd/server/main.go`
- Test: `services/api/internal/server/router_test.go` (extend)

**Interfaces:**
- Produces: `obs.Metrics` (Prometheus collectors), `obs.MetricsMiddleware(m) func(http.Handler) http.Handler`, `obs.MetricsHandler() http.Handler`. Router `Deps` gains `Store storage.Storage`, `Repo *db.Repo`, `MaxUploadBytes int64`.

- [ ] **Step 1: Write metrics**

Run: `go get github.com/prometheus/client_golang/prometheus github.com/prometheus/client_golang/prometheus/promhttp`

`internal/obs/metrics.go`:
```go
package obs

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	reg      *prometheus.Registry
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
	CacheHit prometheus.Counter
	CacheMiss prometheus.Counter
	UploadBytes   prometheus.Counter
	DownloadBytes prometheus.Counter
}

func NewMetrics() *Metrics {
	reg := prometheus.NewRegistry()
	m := &Metrics{
		reg: reg,
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total", Help: "HTTP requests by method/route/status",
		}, []string{"method", "route", "status"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "request_duration_seconds", Help: "HTTP request duration",
			Buckets: prometheus.DefBuckets,
		}, []string{"route"}),
		CacheHit:      prometheus.NewCounter(prometheus.CounterOpts{Name: "cache_hits_total"}),
		CacheMiss:     prometheus.NewCounter(prometheus.CounterOpts{Name: "cache_misses_total"}),
		UploadBytes:   prometheus.NewCounter(prometheus.CounterOpts{Name: "cache_upload_bytes_total"}),
		DownloadBytes: prometheus.NewCounter(prometheus.CounterOpts{Name: "cache_download_bytes_total"}),
	}
	reg.MustRegister(m.requests, m.duration, m.CacheHit, m.CacheMiss, m.UploadBytes, m.DownloadBytes)
	return m
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}

// Middleware records count + duration. Uses chi RoutePattern to avoid label cardinality blowup.
func (m *Metrics) Middleware(routePattern func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: 200}
			next.ServeHTTP(sw, r)
			route := routePattern(r)
			m.requests.WithLabelValues(r.Method, route, strconv.Itoa(sw.status)).Inc()
			m.duration.WithLabelValues(route).Observe(time.Since(start).Seconds())
		})
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}
```

The Turbo handlers increment `CacheHit`/`CacheMiss`/`UploadBytes`/`DownloadBytes`. Pass `*obs.Metrics` into `NewHandler` (extend its signature to `NewHandler(store, repo, maxBytes, metrics *obs.Metrics)`; store it on `Handler`), and:
- in `get`: on `ErrNotFound` → `metrics.CacheMiss.Inc()`; on success → `metrics.CacheHit.Inc()` and `metrics.DownloadBytes.Add(float64(info.Size))`.
- in `put`: after successful upsert → `metrics.UploadBytes.Add(float64(info.Size))`.

**Update both existing callers of `NewHandler` (signature changed from 3→4 args):**
- `internal/turbo/handlers_test.go` → `testRouter`: build `m := obs.NewMetrics()` and call `NewHandler(store, repo, 1<<20, m)`.
- `internal/server/router.go`: pass the router's `m` — `turbo.NewHandler(d.Store, d.Repo, d.MaxUploadBytes, m)` (already shown in Step 2).

Re-run `go test ./internal/turbo/ -v` after the signature change to confirm the handler tests still pass.

- [ ] **Step 2: Wire the router**

`internal/server/router.go`:
```go
package server

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/auth"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/db"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/obs"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/storage"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/turbo"
)

type Deps struct {
	Store          storage.Storage
	Repo           *db.Repo
	MaxUploadBytes int64
}

func New(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)

	m := obs.NewMetrics()
	r.Use(m.Middleware(func(req *http.Request) string {
		if rc := chi.RouteContext(req.Context()); rc != nil && rc.RoutePattern() != "" {
			return rc.RoutePattern()
		}
		return "unknown"
	}))

	// ops endpoints (unauthenticated)
	r.Get("/live", obs.Live)
	r.Get("/health", readyHandler(d))
	r.Get("/ready", readyHandler(d))
	r.Handle("/metrics", m.Handler())

	// authenticated Turbo protocol
	if d.Repo != nil {
		th := turbo.NewHandler(d.Store, d.Repo, d.MaxUploadBytes, m)
		r.Group(func(pr chi.Router) {
			pr.Use(auth.RequireToken(d.Repo))
			th.Mount(pr)
		})
	}
	return r
}

func readyHandler(d Deps) http.HandlerFunc {
	return obs.Ready(func(ctx context.Context) error {
		if d.Repo != nil {
			return d.Repo.Ping(ctx)
		}
		return nil
	})
}
```
Note: `auth.RequireToken(d.Repo)` works because `*db.Repo` satisfies `auth.OrgLookup` (it has `OrgByTokenHash`).

`cmd/server/main.go` (final wiring):
```go
package main

import (
	"context"
	"log"
	"net/http"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/config"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/db"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/server"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/storage"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/storage/filesystem"
	s3store "github.com/nasraldin/turbo-cache-forge/services/api/internal/storage/s3"
)

func main() {
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	repo, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer repo.Close()

	var store storage.Storage
	switch cfg.StorageBackend {
	case "s3":
		store, err = s3store.New(ctx, s3store.Config{
			Bucket: cfg.S3Bucket, Endpoint: cfg.S3Endpoint, Region: cfg.S3Region,
			AccessKey: getenv("STORAGE_S3_ACCESS_KEY"), SecretKey: getenv("STORAGE_S3_SECRET_KEY"),
		})
	default:
		store = filesystem.New(cfg.StoragePath)
	}
	if err != nil {
		log.Fatal(err)
	}

	srv := server.New(server.Deps{Store: store, Repo: repo, MaxUploadBytes: cfg.MaxUploadBytes})
	log.Printf("turbo-cache-forge listening on %s (backend=%s)", cfg.Addr, cfg.StorageBackend)
	if err := http.ListenAndServe(cfg.Addr, srv); err != nil {
		log.Fatal(err)
	}
}

func getenv(k string) string { return os.Getenv(k) } // add "os" import
```

- [ ] **Step 3: Extend router test**

Add to `router_test.go`:
```go
func TestMetricsEndpoint(t *testing.T) {
	srv := New(Deps{}) // no repo → Turbo routes skipped, metrics still up
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics = %d, want 200", rec.Code)
	}
}
```

- [ ] **Step 4: Run + commit**

Run: `go build ./... && go test ./... -v` → PASS
```bash
git add services/api
git commit -m "feat(api): prometheus metrics + full router wiring"
```

---

## Task 9: Docker self-host + end-to-end Turborepo verification

**Files:**
- Create: `infra/docker/Dockerfile`, `infra/docker/docker-compose.yml`, `.env.example`, `README.md` (quickstart)

- [ ] **Step 1: Dockerfile (multi-stage)**

`infra/docker/Dockerfile`:
```dockerfile
FROM golang:1.24 AS build
WORKDIR /src
COPY services/api/go.mod services/api/go.sum ./services/api/
RUN cd services/api && go mod download
COPY services/api ./services/api
RUN cd services/api && CGO_ENABLED=0 go build -o /out/server ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/server /server
COPY infra/migrations /migrations
EXPOSE 8080
ENTRYPOINT ["/server"]
```

- [ ] **Step 2: docker-compose (postgres + api, fs backend, no cloud)**

`infra/docker/docker-compose.yml`:
```yaml
services:
  postgres:
    image: postgres:16
    environment:
      POSTGRES_USER: tcf
      POSTGRES_PASSWORD: tcf
      POSTGRES_DB: tcf
    ports: ["5432:5432"]
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U tcf"]
      interval: 2s
      timeout: 3s
      retries: 20

  migrate:
    image: ghcr.io/pressly/goose:latest
    depends_on:
      postgres: { condition: service_healthy }
    volumes: ["../migrations:/migrations"]
    command: ["-dir", "/migrations", "postgres",
      "postgres://tcf:tcf@postgres:5432/tcf?sslmode=disable", "up"]

  cache-api:
    build:
      context: ../..
      dockerfile: infra/docker/Dockerfile
    depends_on:
      migrate: { condition: service_completed_successfully }
    environment:
      DATABASE_URL: postgres://tcf:tcf@postgres:5432/tcf?sslmode=disable
      STORAGE_BACKEND: fs
      STORAGE_PATH: /data
    volumes: ["cache-data:/data"]
    ports: ["8080:8080"]

volumes:
  cache-data:
```

`.env.example` (documents every knob):
```env
ADDR=:8080
DATABASE_URL=postgres://tcf:tcf@localhost:5432/tcf?sslmode=disable
STORAGE_BACKEND=fs           # fs | s3
STORAGE_PATH=/var/lib/turbo-cache-forge
# --- S3/R2 (only when STORAGE_BACKEND=s3) ---
STORAGE_S3_BUCKET=
STORAGE_S3_ENDPOINT=         # https://<acct>.r2.cloudflarestorage.com  | http://localhost:9000 (MinIO)
STORAGE_S3_REGION=auto
STORAGE_S3_ACCESS_KEY=
STORAGE_S3_SECRET_KEY=
MAX_UPLOAD_BYTES=1073741824
```

- [ ] **Step 3: Bring it up + seed a token**
```bash
docker compose -f infra/docker/docker-compose.yml up -d --build
# wait for health, then seed an org + token:
docker compose -f infra/docker/docker-compose.yml exec postgres \
  psql -U tcf -d tcf -c \
  "INSERT INTO organizations (slug,name) VALUES ('my-team','My Team');"
# generate a token hash: run a tiny Go snippet or reuse GenerateToken via a scratch main;
# for the manual path, pick token 'turbo_dev' and insert its sha256:
#   printf 'turbo_dev' | shasum -a 256
docker compose -f infra/docker/docker-compose.yml exec postgres \
  psql -U tcf -d tcf -c \
  "INSERT INTO api_keys (org_id,name,token_hash) SELECT id,'dev','<sha256-hex>' FROM organizations WHERE slug='my-team';"
```

- [ ] **Step 4: End-to-end protocol check (the real acceptance test)**

```bash
# status
curl -s -H "Authorization: Bearer turbo_dev" \
  "http://localhost:8080/v8/artifacts/status"          # {"status":"enabled"}

# upload
echo "fake-artifact" | curl -s -X PUT --data-binary @- \
  -H "Authorization: Bearer turbo_dev" \
  "http://localhost:8080/v8/artifacts/abc123?teamId=my-team"   # 202 {"urls":[...]}

# download (from a "second machine" = fresh curl, no local cache)
curl -s -H "Authorization: Bearer turbo_dev" \
  "http://localhost:8080/v8/artifacts/abc123?teamId=my-team"   # -> "fake-artifact"
```

Then the **true** end-to-end, against a real Turborepo repo:
```bash
export TURBO_API=http://localhost:8080
export TURBO_TOKEN=turbo_dev
export TURBO_TEAM=my-team
turbo run build --remote-only          # run 1: MISS → uploads
rm -rf node_modules/.cache/turbo .turbo # clear local cache
turbo run build --remote-only          # run 2: HIT → downloads (FULL TURBO)
```

- [ ] **Step 5: Commit**
```bash
git add infra .env.example README.md
git commit -m "feat: docker-compose self-host + Turborepo e2e verification"
```

---

## Self-review notes (coverage against the design spec)

- **Milestones 1–7 mapped:** repo foundation (T1), config (T2), storage abstraction + fs/s3 (T3–T4), database (T5), token auth (T6), Turbo protocol (T7), observability (T8), docker self-host + e2e (T9).
- **Streaming/no-buffer:** fs uses `io.Copy` to a temp file; s3 uses `manager.Uploader`; GET streams via `io.Copy`. No `io.ReadAll` on artifact bodies in production paths (only in the in-memory test fake).
- **DB off the hot path:** GET's `TouchArtifact` is a fire-and-forget goroutine with its own context.
- **Tenant isolation:** `storageKey(orgSlug, hash)` + `UNIQUE(org_id, hash)`; a token resolves to exactly one org.
- **One metrics pipeline:** Prometheus only; OTel deferred to Phase 2 per the spec.
- **Vendor-neutral backend:** no auth SDK; tokens are hashed bearer strings.

## Deferred to later phases (do NOT build here)
- OTel tracing seam, batch/async metadata flush tuning, load test → **Phase 2**.
- Token/project/org management API (`/api/v1`), OIDC/JWT, cleanup cron → **Phase 3**.
- Dashboard → **Phase 4**. CLI → **Phase 5**.

## Verification checklist (run before calling Phase 1 done)
1. `go build ./... && go test ./...` green (S3 + DB suites skip without env; that's expected).
2. `docker compose up` on a clean machine yields a working cache with **no cloud accounts**.
3. `turbo run build --remote-only` shows MISS→HIT across a local-cache wipe (FULL TURBO).
4. `/metrics` shows `cache_hits_total` incrementing; `/ready` returns 503 when Postgres is stopped.
5. A token for `team-a` cannot read `team-b`'s artifacts (seed two orgs; cross-request → 404/401).
