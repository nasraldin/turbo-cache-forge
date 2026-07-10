# Artifacts Admin Enhancements Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the dashboard Artifacts page into a professional admin surface — inspect an artifact's contents, download it, delete single artifacts, and clear the whole cache.

**Architecture:** Four new org-scoped `/api/v1` management endpoints (detail+manifest, download, delete, clear-all) backed by a new pure `artifactview` zstd-tar decoder and three thin repo methods; a redesigned Artifacts page with a details dialog, per-row delete, and a typed-confirmation clear-all — all reusing the redesign's shared UI primitives.

**Tech Stack:** Go (chi, pgx, `archive/tar`, `github.com/klauspost/compress/zstd`), Next.js 15 / React 19 / Tailwind, TanStack Query, Vitest.

## Global Constraints

- **Two-auth-worlds invariant:** new routes live ONLY in the mgmt `/api/v1` auth group (`router.go` `mh.Mount`), NEVER on the `/v8` cache path. `internal/mgmt` must not import `internal/localauth`/`internal/oidcauth`.
- **Org scoping:** every handler resolves its tenant via `auth.OrgFromContext` and returns 401 if absent; every repo/store call is keyed by that org (`org.ID`, and blob key `turbo.StorageKey(org.Slug, hash)`).
- **Delete ordering:** blob first, then DB row — mirrors `internal/cleanup.RunOnce` (an orphan blob is worse than a row without a blob, which self-heals).
- **Decode safety (zip-bomb / DoS):** decode is bounded by `maxEntries=1000`, `maxDecompressed=32 MiB`, `maxPreviewBytes=64 KiB`/file, `maxTotalPreview=512 KiB`. A blob that isn't a decodable zstd-tar yields `{"format":"opaque"}` — never a 500.
- **JSON shape:** snake_case throughout (matches existing `/api/v1`). Empty slices serialize as `[]`, not `null`.
- **Hash validation:** validate `{hash}` with the existing `turbo` hash rules before building any storage key.
- **Frontend:** reuse shared primitives (`Dialog`, `Button`, `Badge`, `.eyebrow`, `.font-data`); light + dark both work; ≥44px mobile hit targets. Preserve the existing Artifacts empty/error copy and the `/no artifacts cached yet/i` + `TURBO_TOKEN` test assertions.

---

### Task 1: `artifactview` zstd-tar decoder (pure package)

**Files:**
- Create: `services/api/internal/artifactview/artifactview.go`
- Test: `services/api/internal/artifactview/artifactview_test.go`
- Modify: `services/api/go.mod` (promote `github.com/klauspost/compress` to a direct require via `go mod tidy`)

**Interfaces:**
- Produces: `artifactview.Decode(r io.Reader) Manifest`; types `Manifest{Format string; TotalEntries int; Truncated bool; Entries []Entry}` and `Entry{Path string; Size int64; IsDir bool; Preview string; Previewable bool}` with snake_case json tags. `Format` is `"zstd-tar"` or `"opaque"`.

- [ ] **Step 1: Write the failing tests**

```go
package artifactview

import (
	"archive/tar"
	"bytes"
	"strconv"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// makeZstdTar builds a zstd-compressed tar from the given regular files and dirs.
func makeZstdTar(t *testing.T, files map[string]string, dirs []string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw, err := zstd.NewWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(zw)
	for _, d := range dirs {
		if err := tw.WriteHeader(&tar.Header{Name: d, Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
			t.Fatal(err)
		}
	}
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func find(m Manifest, path string) (Entry, bool) {
	for _, e := range m.Entries {
		if e.Path == path {
			return e, true
		}
	}
	return Entry{}, false
}

func TestDecodeListsEntriesAndPreviewsText(t *testing.T) {
	blob := makeZstdTar(t, map[string]string{"pkg/dist/index.js": "console.log(1)"}, []string{"pkg/dist/"})
	m := Decode(bytes.NewReader(blob))
	if m.Format != "zstd-tar" {
		t.Fatalf("format = %q, want zstd-tar", m.Format)
	}
	f, ok := find(m, "pkg/dist/index.js")
	if !ok || !f.Previewable || f.Preview != "console.log(1)" {
		t.Fatalf("file entry = %+v, want previewable text", f)
	}
	d, ok := find(m, "pkg/dist/")
	if !ok || !d.IsDir || d.Previewable {
		t.Fatalf("dir entry = %+v, want is_dir & not previewable", d)
	}
}

func TestDecodeBinaryNotPreviewable(t *testing.T) {
	m := Decode(bytes.NewReader(makeZstdTar(t, map[string]string{"a.bin": "ab\x00cd"}, nil)))
	f, _ := find(m, "a.bin")
	if f.Previewable || f.Preview != "" {
		t.Fatalf("binary entry = %+v, want not previewable", f)
	}
}

func TestDecodeOversizedNotPreviewable(t *testing.T) {
	big := strings.Repeat("x", maxPreviewBytes+1)
	m := Decode(bytes.NewReader(makeZstdTar(t, map[string]string{"big.txt": big}, nil)))
	f, _ := find(m, "big.txt")
	if f.Previewable {
		t.Fatalf("oversized entry previewable, want false")
	}
}

func TestDecodeOpaqueOnNonZstd(t *testing.T) {
	if m := Decode(bytes.NewReader([]byte("definitely not a zstd stream"))); m.Format != "opaque" {
		t.Fatalf("format = %q, want opaque", m.Format)
	}
}

func TestDecodeOpaqueOnZstdNonTar(t *testing.T) {
	var buf bytes.Buffer
	zw, _ := zstd.NewWriter(&buf)
	_, _ = zw.Write([]byte("plain text, not a tar"))
	_ = zw.Close()
	if m := Decode(bytes.NewReader(buf.Bytes())); m.Format != "opaque" {
		t.Fatalf("format = %q, want opaque", m.Format)
	}
}

func TestDecodeTruncatesAtEntryCap(t *testing.T) {
	files := make(map[string]string, maxEntries+5)
	for i := 0; i < maxEntries+5; i++ {
		files["f"+strconv.Itoa(i)] = "x"
	}
	m := Decode(bytes.NewReader(makeZstdTar(t, files, nil)))
	if !m.Truncated || len(m.Entries) != maxEntries {
		t.Fatalf("entries=%d truncated=%v, want %d & true", len(m.Entries), m.Truncated, maxEntries)
	}
}
```

(The `strings` import is still used by `TestDecodeOversizedNotPreviewable`.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd services/api && go test ./internal/artifactview/...`
Expected: FAIL — `undefined: Decode` (package has no implementation yet).

- [ ] **Step 3: Write the implementation**

```go
// Package artifactview decodes a Turbo cache artifact (a zstd-compressed tar of
// a task's outputs) into a file manifest with inline text previews, for the
// admin "view contents" surface. It never trusts the blob: anything that isn't a
// decodable zstd-tar (client-encrypted, unknown format) degrades to "opaque".
package artifactview

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"unicode/utf8"

	"github.com/klauspost/compress/zstd"
)

const (
	maxEntries      = 1000      // stop listing beyond this (Truncated=true)
	maxDecompressed = 32 << 20  // 32 MiB total decompressed budget (zip-bomb guard)
	maxPreviewBytes = 64 << 10  // per-file preview cap
	maxTotalPreview = 512 << 10 // total inlined preview budget across all files
)

type Entry struct {
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	IsDir       bool   `json:"is_dir"`
	Preview     string `json:"preview,omitempty"`
	Previewable bool   `json:"previewable"`
}

type Manifest struct {
	Format       string  `json:"format"` // "zstd-tar" | "opaque"
	TotalEntries int     `json:"total_entries"`
	Truncated    bool    `json:"truncated"`
	Entries      []Entry `json:"entries"`
}

func opaque() Manifest { return Manifest{Format: "opaque", Entries: []Entry{}} }

// Decode returns a manifest of the artifact's tar entries. Text files up to
// maxPreviewBytes (within a total maxTotalPreview budget) get an inline UTF-8
// preview; binaries and oversized files are listed without one. Undecodable
// blobs return Format:"opaque" (no error — blobs are stored verbatim).
func Decode(r io.Reader) Manifest {
	zr, err := zstd.NewReader(r)
	if err != nil {
		return opaque()
	}
	defer zr.Close()

	tr := tar.NewReader(io.LimitReader(zr, maxDecompressed+1))
	hdr, err := tr.Next()
	if err != nil {
		return opaque() // not a (zstd) tar
	}

	m := Manifest{Format: "zstd-tar", Entries: []Entry{}}
	totalPreview := 0
	for err == nil {
		if len(m.Entries) >= maxEntries {
			m.Truncated = true
			break
		}
		name := hdr.Name
		e := Entry{
			Path:  name,
			Size:  hdr.Size,
			IsDir: hdr.Typeflag == tar.TypeDir || (len(name) > 0 && name[len(name)-1] == '/'),
		}
		if !e.IsDir && hdr.Typeflag == tar.TypeReg && hdr.Size <= maxPreviewBytes && totalPreview < maxTotalPreview {
			buf := make([]byte, hdr.Size)
			n, rerr := io.ReadFull(tr, buf)
			if rerr == nil || errors.Is(rerr, io.ErrUnexpectedEOF) {
				if b := buf[:n]; isText(b) {
					e.Preview = string(b)
					e.Previewable = true
					totalPreview += n
				}
			}
		}
		m.Entries = append(m.Entries, e)
		m.TotalEntries++
		hdr, err = tr.Next()
	}
	if err != nil && !errors.Is(err, io.EOF) {
		m.Truncated = true // valid start, broke or hit the decompress cap mid-stream
	}
	return m
}

// isText reports whether b is UTF-8 with no NUL byte.
func isText(b []byte) bool {
	if bytes.IndexByte(b, 0) >= 0 {
		return false
	}
	return utf8.Valid(b)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd services/api && go test ./internal/artifactview/... && go mod tidy && go build ./...`
Expected: PASS; `go mod tidy` moves `github.com/klauspost/compress` into the direct `require` block.

- [ ] **Step 5: Commit**

```bash
git add services/api/internal/artifactview services/api/go.mod services/api/go.sum
git commit -m "feat(api): artifactview — bounded zstd-tar decoder with text previews"
```

---

### Task 2: Repo methods for get-by-hash and clear-all

**Files:**
- Modify: `services/api/internal/db/repo.go` (add sentinel + three methods near the existing artifact methods ~line 356)

**Interfaces:**
- Consumes: existing `Artifact` struct (`repo.go:274`), `obs.StartDBSpan`, `pgx.ErrNoRows`.
- Produces: `db.ErrArtifactNotFound`; `(*Repo).GetArtifact(ctx, orgID int64, hash string) (Artifact, error)`; `(*Repo).ListArtifactHashes(ctx, orgID int64) ([]string, error)`; `(*Repo).DeleteAllArtifacts(ctx, orgID int64) (int64, error)`. (`DeleteArtifact` already exists at `repo.go:356`.)

- [ ] **Step 1: Add the sentinel and methods**

Add near the top-level `var ErrUnauthorized` (`repo.go:16`):

```go
var ErrArtifactNotFound = errors.New("db: artifact not found")
```

Add after `DeleteArtifact` (`repo.go:363`):

```go
func (r *Repo) GetArtifact(ctx context.Context, orgID int64, hash string) (a Artifact, err error) {
	ctx, span := obs.StartDBSpan(ctx, "db.GetArtifact")
	defer func() { obs.EndSpan(span, err) }()

	const q = `SELECT hash, size_bytes, artifact_tag, created_at, last_accessed_at
	           FROM cache_artifacts WHERE org_id=$1 AND hash=$2`
	err = r.pool.QueryRow(ctx, q, orgID, hash).Scan(&a.Hash, &a.SizeBytes, &a.Tag, &a.CreatedAt, &a.LastAccessedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Artifact{}, ErrArtifactNotFound
	}
	return a, err
}

// ListArtifactHashes returns every artifact hash for the org (used to remove
// blobs before a clear-all). Operator-scale counts; read in one query.
func (r *Repo) ListArtifactHashes(ctx context.Context, orgID int64) (hashes []string, err error) {
	ctx, span := obs.StartDBSpan(ctx, "db.ListArtifactHashes")
	defer func() { obs.EndSpan(span, err) }()

	const q = `SELECT hash FROM cache_artifacts WHERE org_id=$1`
	rows, err := r.pool.Query(ctx, q, orgID)
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

	const q = `DELETE FROM cache_artifacts WHERE org_id=$1`
	tag, err := r.pool.Exec(ctx, q, orgID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
```

- [ ] **Step 2: Verify it builds and vets**

Run: `cd services/api && go build ./... && go vet ./internal/db/...`
Expected: no output, exit 0. (These SQL methods follow the repo's existing convention of being exercised via handler tests and E2E rather than a standalone DB unit test.)

- [ ] **Step 3: Commit**

```bash
git add services/api/internal/db/repo.go
git commit -m "feat(api): repo GetArtifact, ListArtifactHashes, DeleteAllArtifacts"
```

---

### Task 3: Management endpoints — detail, download, delete, clear-all

**Files:**
- Modify: `services/api/internal/turbo/validate.go` (export a `ValidHash` wrapper)
- Modify: `services/api/internal/mgmt/handlers.go` (Repo interface, Handler struct, NewHandler, Mount, 4 handlers)
- Modify: `services/api/internal/server/router.go:78` (`mgmt.NewHandler(d.Repo, d.Store)`)
- Test: `services/api/internal/mgmt/handlers_test.go` (extend fakeRepo, add fakeStore, update testRouter, new tests)

**Interfaces:**
- Consumes: `artifactview.Decode` (Task 1); `db.ErrArtifactNotFound`, `db.GetArtifact/ListArtifactHashes/DeleteAllArtifacts/DeleteArtifact` (Task 2); `storage.Storage` + `storage.ErrNotFound`; `turbo.StorageKey`, `turbo.ValidHash`; `auth.OrgFromContext`.
- Produces: routes `GET /artifacts/{hash}`, `GET /artifacts/{hash}/download`, `DELETE /artifacts/{hash}`, `DELETE /artifacts`.

- [ ] **Step 1: Export the hash validator**

In `services/api/internal/turbo/validate.go`, add (leave the unexported `validHash` and its callers untouched):

```go
// ValidHash exposes the cache-path hash rules to the management API so admin
// artifact routes reject path-escaping hashes before building a storage key.
func ValidHash(hash string) bool { return validHash(hash) }
```

- [ ] **Step 2: Write the failing handler tests**

In `services/api/internal/mgmt/handlers_test.go`:

Add imports: `"archive/tar"`, `"io"`, `"github.com/klauspost/compress/zstd"`, `"github.com/nasraldin/turbo-cache-forge/services/api/internal/storage"`.

Extend `fakeRepo` (add fields + methods):

```go
// add to fakeRepo struct:
//   getArtifact    db.Artifact
//   getArtifactErr error
//   hashes         []string
//   deletedHash    string

func (f *fakeRepo) GetArtifact(_ context.Context, _ int64, _ string) (db.Artifact, error) {
	if f.getArtifactErr != nil {
		return db.Artifact{}, f.getArtifactErr
	}
	return f.getArtifact, nil
}
func (f *fakeRepo) ListArtifactHashes(context.Context, int64) ([]string, error) { return f.hashes, nil }
func (f *fakeRepo) DeleteAllArtifacts(context.Context, int64) (int64, error) {
	return int64(len(f.hashes)), nil
}
func (f *fakeRepo) DeleteArtifact(_ context.Context, _ int64, hash string) error {
	f.deletedHash = hash
	return nil
}
```

Add a fake store:

```go
type fakeStore struct {
	blobs   map[string][]byte
	deleted []string
}

func (s *fakeStore) Put(context.Context, string, io.Reader) error { return nil }
func (s *fakeStore) Get(_ context.Context, key string) (io.ReadCloser, *storage.ObjectInfo, error) {
	b, ok := s.blobs[key]
	if !ok {
		return nil, nil, storage.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(b)), &storage.ObjectInfo{Size: int64(len(b))}, nil
}
func (s *fakeStore) Head(_ context.Context, key string) (*storage.ObjectInfo, error) {
	b, ok := s.blobs[key]
	if !ok {
		return nil, storage.ErrNotFound
	}
	return &storage.ObjectInfo{Size: int64(len(b))}, nil
}
func (s *fakeStore) Delete(_ context.Context, key string) error {
	s.deleted = append(s.deleted, key)
	delete(s.blobs, key)
	return nil
}
```

Update `testRouter` to thread a store, keeping existing callers working by defaulting:

```go
func testRouter(repo Repo) http.Handler { return testRouterWithStore(repo, &fakeStore{}) }

func testRouterWithStore(repo Repo, store storage.Storage) http.Handler {
	h := NewHandler(repo, store)
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
```

Add a fixture helper + tests:

```go
func zstdTar(t *testing.T, name, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw, _ := zstd.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	_ = tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(content))})
	_, _ = tw.Write([]byte(content))
	_ = tw.Close()
	_ = zw.Close()
	return buf.Bytes()
}

func TestGetArtifactReturnsManifest(t *testing.T) {
	repo := &fakeRepo{getArtifact: db.Artifact{Hash: "abc123", SizeBytes: 10}}
	store := &fakeStore{blobs: map[string][]byte{"org-test/abc123": zstdTar(t, "out/log.txt", "hi")}}
	rec := httptest.NewRecorder()
	testRouterWithStore(repo, store).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/abc123", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET detail = %d, want 200", rec.Code)
	}
	var resp struct {
		Hash    string `json:"hash"`
		Content struct {
			Format  string `json:"format"`
			Entries []struct {
				Path        string `json:"path"`
				Preview     string `json:"preview"`
				Previewable bool   `json:"previewable"`
			} `json:"entries"`
		} `json:"content"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Hash != "abc123" || resp.Content.Format != "zstd-tar" || len(resp.Content.Entries) != 1 {
		t.Fatalf("resp = %+v", resp)
	}
	if e := resp.Content.Entries[0]; e.Path != "out/log.txt" || !e.Previewable || e.Preview != "hi" {
		t.Fatalf("entry = %+v", e)
	}
}

func TestGetArtifactNotFound(t *testing.T) {
	repo := &fakeRepo{getArtifactErr: db.ErrArtifactNotFound}
	rec := httptest.NewRecorder()
	testRouter(repo).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/missing", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET missing = %d, want 404", rec.Code)
	}
}

func TestGetArtifactBadHash(t *testing.T) {
	rec := httptest.NewRecorder()
	testRouter(&fakeRepo{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/bad..hash", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("GET bad hash = %d, want 400", rec.Code)
	}
}

func TestDeleteArtifactRemovesBlobAndRow(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStore{blobs: map[string][]byte{"org-test/abc123": {1, 2, 3}}}
	rec := httptest.NewRecorder()
	testRouterWithStore(repo, store).ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/v1/artifacts/abc123", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE = %d, want 204", rec.Code)
	}
	if repo.deletedHash != "abc123" || len(store.deleted) != 1 || store.deleted[0] != "org-test/abc123" {
		t.Fatalf("row=%q blobDeletes=%v", repo.deletedHash, store.deleted)
	}
}

func TestClearArtifacts(t *testing.T) {
	repo := &fakeRepo{hashes: []string{"h1", "h2"}}
	store := &fakeStore{blobs: map[string][]byte{"org-test/h1": {1}, "org-test/h2": {2}}}
	rec := httptest.NewRecorder()
	testRouterWithStore(repo, store).ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/v1/artifacts", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("clear = %d, want 200", rec.Code)
	}
	var resp struct {
		Deleted int64 `json:"deleted"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Deleted != 2 || len(store.deleted) != 2 {
		t.Fatalf("deleted=%d blobDeletes=%v", resp.Deleted, store.deleted)
	}
}

func TestDownloadArtifactStreamsBlob(t *testing.T) {
	store := &fakeStore{blobs: map[string][]byte{"org-test/abc123": []byte("RAWBYTES")}}
	rec := httptest.NewRecorder()
	testRouterWithStore(&fakeRepo{}, store).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/abc123/download", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "RAWBYTES" {
		t.Fatalf("download = %d body=%q", rec.Code, rec.Body.String())
	}
	if cd := rec.Header().Get("Content-Disposition"); cd == "" {
		t.Fatalf("missing Content-Disposition")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd services/api && go test ./internal/mgmt/...`
Expected: FAIL — `NewHandler` takes one arg / undefined methods (compile error) until Step 4.

- [ ] **Step 4: Implement handlers and wiring**

In `handlers.go`, add imports: `"errors"`, `"fmt"`, `"io"`, `"github.com/nasraldin/turbo-cache-forge/services/api/internal/artifactview"`, `"github.com/nasraldin/turbo-cache-forge/services/api/internal/storage"`, `"github.com/nasraldin/turbo-cache-forge/services/api/internal/turbo"`.

Extend the `Repo` interface (add to the existing block at `handlers.go:18`):

```go
	GetArtifact(ctx context.Context, orgID int64, hash string) (db.Artifact, error)
	ListArtifactHashes(ctx context.Context, orgID int64) ([]string, error)
	DeleteAllArtifacts(ctx context.Context, orgID int64) (int64, error)
	DeleteArtifact(ctx context.Context, orgID int64, hash string) error
```

Replace the Handler type + constructor (`handlers.go:29-30`):

```go
type Handler struct {
	repo  Repo
	store storage.Storage
}

func NewHandler(repo Repo, store storage.Storage) *Handler {
	return &Handler{repo: repo, store: store}
}
```

Add to `Mount` (after the existing `r.Get("/artifacts", h.listArtifacts)`):

```go
	r.Get("/artifacts/{hash}", h.getArtifact)
	r.Get("/artifacts/{hash}/download", h.downloadArtifact)
	r.Delete("/artifacts/{hash}", h.deleteArtifact)
	r.Delete("/artifacts", h.clearArtifacts)
```

Add the four handlers (near `listArtifacts`):

```go
func (h *Handler) getArtifact(w http.ResponseWriter, r *http.Request) {
	org, ok := auth.OrgFromContext(r.Context())
	if !ok {
		http.Error(w, "no org", http.StatusUnauthorized)
		return
	}
	hash := chi.URLParam(r, "hash")
	if !turbo.ValidHash(hash) {
		http.Error(w, "bad hash", http.StatusBadRequest)
		return
	}
	a, err := h.repo.GetArtifact(r.Context(), org.ID, hash)
	if errors.Is(err, db.ErrArtifactNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "get failed", http.StatusInternalServerError)
		return
	}
	// Blob is optional: a row can outlive its blob (cleanup deletes blob first).
	content := artifactview.Manifest{Format: "opaque", Entries: []artifactview.Entry{}}
	if rc, _, gerr := h.store.Get(r.Context(), turbo.StorageKey(org.Slug, hash)); gerr == nil {
		defer rc.Close()
		content = artifactview.Decode(rc)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"hash": a.Hash, "size_bytes": a.SizeBytes, "tag": a.Tag,
		"created_at": a.CreatedAt, "last_accessed_at": a.LastAccessedAt,
		"content": content,
	})
}

func (h *Handler) downloadArtifact(w http.ResponseWriter, r *http.Request) {
	org, ok := auth.OrgFromContext(r.Context())
	if !ok {
		http.Error(w, "no org", http.StatusUnauthorized)
		return
	}
	hash := chi.URLParam(r, "hash")
	if !turbo.ValidHash(hash) {
		http.Error(w, "bad hash", http.StatusBadRequest)
		return
	}
	rc, info, err := h.store.Get(r.Context(), turbo.StorageKey(org.Slug, hash))
	if errors.Is(err, storage.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "get failed", http.StatusInternalServerError)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", hash+".tar.zst"))
	if info != nil {
		w.Header().Set("Content-Length", strconv.FormatInt(info.Size, 10))
	}
	_, _ = io.Copy(w, rc)
}

func (h *Handler) deleteArtifact(w http.ResponseWriter, r *http.Request) {
	org, ok := auth.OrgFromContext(r.Context())
	if !ok {
		http.Error(w, "no org", http.StatusUnauthorized)
		return
	}
	hash := chi.URLParam(r, "hash")
	if !turbo.ValidHash(hash) {
		http.Error(w, "bad hash", http.StatusBadRequest)
		return
	}
	// blob first, then row — mirrors cleanup.RunOnce (avoids orphan blobs).
	if err := h.store.Delete(r.Context(), turbo.StorageKey(org.Slug, hash)); err != nil {
		http.Error(w, "delete failed", http.StatusInternalServerError)
		return
	}
	if err := h.repo.DeleteArtifact(r.Context(), org.ID, hash); err != nil {
		http.Error(w, "delete failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) clearArtifacts(w http.ResponseWriter, r *http.Request) {
	org, ok := auth.OrgFromContext(r.Context())
	if !ok {
		http.Error(w, "no org", http.StatusUnauthorized)
		return
	}
	hashes, err := h.repo.ListArtifactHashes(r.Context(), org.ID)
	if err != nil {
		http.Error(w, "clear failed", http.StatusInternalServerError)
		return
	}
	for _, hh := range hashes {
		// Best-effort per blob; a failed blob delete leaves its row for the
		// cleanup cron, but we still clear the rows below.
		_ = h.store.Delete(r.Context(), turbo.StorageKey(org.Slug, hh))
	}
	n, err := h.repo.DeleteAllArtifacts(r.Context(), org.ID)
	if err != nil {
		http.Error(w, "clear failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": n})
}
```

In `router.go:78`, change `mh := mgmt.NewHandler(d.Repo)` to:

```go
			mh := mgmt.NewHandler(d.Repo, d.Store)
```

- [ ] **Step 5: Run tests + build to verify they pass**

Run: `cd services/api && go test ./internal/mgmt/... && go build ./...`
Expected: PASS; whole module builds.

- [ ] **Step 6: Commit**

```bash
git add services/api/internal/mgmt services/api/internal/turbo/validate.go services/api/internal/server/router.go
git commit -m "feat(api): artifact detail/manifest, download, delete, and clear-all endpoints"
```

---

### Task 4: Dashboard types + API client methods

**Files:**
- Modify: `packages/types/src/index.ts`
- Modify: `packages/api-client/src/client.ts`
- Test: `packages/api-client/src/client.test.ts`

**Interfaces:**
- Produces: types `ArtifactEntry`, `ArtifactContent`, `ArtifactDetail`, `ClearArtifactsResult`; client methods `getArtifact(hash)`, `deleteArtifact(hash)`, `clearArtifacts()`, `getArtifactBlob(hash)`.

- [ ] **Step 1: Add the types**

Append to `packages/types/src/index.ts`:

```ts
// One tar entry inside GET /api/v1/artifacts/{hash}.content
export interface ArtifactEntry {
  path: string;
  size: number;
  is_dir: boolean;
  preview?: string; // present only when previewable
  previewable: boolean;
}

// Decoded contents of an artifact. "opaque" = not a decodable Turbo zstd-tar.
export interface ArtifactContent {
  format: "zstd-tar" | "opaque";
  total_entries: number;
  truncated: boolean;
  entries: ArtifactEntry[];
}

// GET /api/v1/artifacts/{hash} — list metadata plus decoded contents.
export interface ArtifactDetail extends Artifact {
  content: ArtifactContent;
}

// 200 body of DELETE /api/v1/artifacts (clear-all).
export interface ClearArtifactsResult {
  deleted: number;
}
```

- [ ] **Step 2: Write the failing client tests**

Append to `packages/api-client/src/client.test.ts`:

```ts
  it("GETs /api/v1/artifacts/{hash} detail", async () => {
    const detail = {
      hash: "abc", size_bytes: 10, tag: null,
      created_at: "2026-07-01T00:00:00Z", last_accessed_at: "2026-07-01T00:00:00Z",
      content: { format: "zstd-tar", total_entries: 0, truncated: false, entries: [] },
    };
    const fetchMock = mockFetch(detail);
    vi.stubGlobal("fetch", fetchMock);
    const client = createApiClient({ baseUrl: base, getToken: async () => "jwt" });

    const got = await client.getArtifact("abc");

    expect(got.content.format).toBe("zstd-tar");
    expect(fetchMock.mock.calls[0][0]).toBe(`${base}/api/v1/artifacts/abc`);
  });

  it("DELETEs a single artifact (204)", async () => {
    const fetchMock = mockFetch(undefined, { status: 204 });
    vi.stubGlobal("fetch", fetchMock);
    const client = createApiClient({ baseUrl: base, getToken: async () => "jwt" });

    await client.deleteArtifact("abc");

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe(`${base}/api/v1/artifacts/abc`);
    expect(init.method).toBe("DELETE");
  });

  it("DELETEs all artifacts and returns the count", async () => {
    const fetchMock = mockFetch({ deleted: 3 });
    vi.stubGlobal("fetch", fetchMock);
    const client = createApiClient({ baseUrl: base, getToken: async () => "jwt" });

    const res = await client.clearArtifacts();

    expect(res.deleted).toBe(3);
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe(`${base}/api/v1/artifacts`);
    expect(init.method).toBe("DELETE");
  });

  it("downloads an artifact blob with the JWT attached", async () => {
    const blob = new Blob(["RAW"]);
    const fetchMock = vi.fn(async () => ({ ok: true, status: 200, blob: async () => blob }));
    vi.stubGlobal("fetch", fetchMock);
    const client = createApiClient({ baseUrl: base, getToken: async () => "jwt" });

    const got = await client.getArtifactBlob("abc");

    expect(got).toBe(blob);
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe(`${base}/api/v1/artifacts/abc/download`);
    expect((init.headers as Record<string, string>).Authorization).toBe("Bearer jwt");
  });
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd packages/api-client && pnpm test`
Expected: FAIL — `getArtifact`/`deleteArtifact`/`clearArtifacts`/`getArtifactBlob` are not functions.

- [ ] **Step 4: Implement the client methods**

Update the type import at the top of `packages/api-client/src/client.ts`:

```ts
import type {
  ArtifactDetail,
  ArtifactsPage,
  ClearArtifactsResult,
  CreatedToken,
  Project,
  Stats,
  StatsPoint,
  Token,
} from "@tcf/types";
```

Add inside the returned object (after `listArtifacts`):

```ts
    getArtifact: (hash: string) => request<ArtifactDetail>(`/artifacts/${encodeURIComponent(hash)}`),
    deleteArtifact: (hash: string) =>
      request<void>(`/artifacts/${encodeURIComponent(hash)}`, { method: "DELETE" }),
    clearArtifacts: () => request<ClearArtifactsResult>("/artifacts", { method: "DELETE" }),
    // Raw download needs the Bearer header, so it can't be a bare <a href>. The
    // caller turns the Blob into an object-URL download.
    getArtifactBlob: async (hash: string): Promise<Blob> => {
      const token = await opts.getToken();
      const res = await fetch(`${root}/artifacts/${encodeURIComponent(hash)}/download`, {
        headers: token ? { Authorization: `Bearer ${token}` } : {},
      });
      if (!res.ok) {
        const text = await res.text().catch(() => res.statusText);
        throw new ApiError(res.status, text || `request failed: ${res.status}`);
      }
      return res.blob();
    },
```

- [ ] **Step 5: Run tests + typecheck to verify they pass**

Run: `cd packages/api-client && pnpm test` and `cd apps/dashboard && ./node_modules/.bin/tsc --noEmit`
Expected: PASS; dashboard still typechecks against the new client/types.

- [ ] **Step 6: Commit**

```bash
git add packages/types/src/index.ts packages/api-client/src/client.ts packages/api-client/src/client.test.ts
git commit -m "feat(dashboard): api-client artifact detail/download/delete/clear methods + types"
```

---

### Task 5: Artifacts page — summary, details dialog, delete, clear-all

**Files:**
- Create: `apps/dashboard/src/components/artifact-detail-dialog.tsx`
- Create: `apps/dashboard/src/components/clear-artifacts-dialog.tsx`
- Modify: `apps/dashboard/src/app/(dashboard)/artifacts/page.tsx`
- Test: `apps/dashboard/src/app/(dashboard)/artifacts/page.test.tsx`

**Interfaces:**
- Consumes: `useApiClient()` → `listArtifacts`, `getStats`, `getArtifact`, `deleteArtifact`, `clearArtifacts`, `getArtifactBlob`; shared `Dialog*`, `Button`, `Badge`, `PageHeader`, `formatBytes`.

- [ ] **Step 1: Create the ArtifactDetailDialog**

`apps/dashboard/src/components/artifact-detail-dialog.tsx`:

```tsx
"use client";
import { useQuery } from "@tanstack/react-query";
import { FileText, Folder, Loader2 } from "lucide-react";
import { useApiClient } from "@/app/api";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { formatBytes } from "@/lib/format";

// Details + decoded contents of one artifact. Controlled by the parent (open
// when a hash is selected); the detail query runs only while open.
export function ArtifactDetailDialog({
  hash,
  onClose,
}: {
  hash: string | null;
  onClose: () => void;
}) {
  const api = useApiClient();
  const { data, isLoading, isError } = useQuery({
    queryKey: ["artifact", hash],
    queryFn: () => api.getArtifact(hash!),
    enabled: !!hash,
  });

  async function download() {
    if (!hash) return;
    const blob = await api.getArtifactBlob(hash);
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `${hash}.tar.zst`;
    a.click();
    URL.revokeObjectURL(url);
  }

  return (
    <Dialog open={!!hash} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>Artifact contents</DialogTitle>
        </DialogHeader>
        {isError ? (
          <p role="alert" className="text-sm text-danger">
            Couldn&apos;t load this artifact.
          </p>
        ) : isLoading || !data ? (
          <div className="flex items-center gap-2 text-sm text-muted">
            <Loader2 className="h-4 w-4 animate-spin" aria-hidden /> Loading…
          </div>
        ) : (
          <div className="space-y-4">
            <dl className="grid grid-cols-2 gap-x-4 gap-y-2 text-sm">
              <dt className="text-muted">Hash</dt>
              <dd className="font-data truncate" title={data.hash}>{data.hash}</dd>
              <dt className="text-muted">Size</dt>
              <dd className="font-data">{formatBytes(data.size_bytes)}</dd>
              <dt className="text-muted">Created</dt>
              <dd className="font-data">{new Date(data.created_at).toLocaleString()}</dd>
              <dt className="text-muted">Last accessed</dt>
              <dd className="font-data">{new Date(data.last_accessed_at).toLocaleString()}</dd>
            </dl>

            {data.content.format === "opaque" ? (
              <p className="rounded-md border border-border bg-surface-2 px-3 py-2 text-sm text-muted">
                Encrypted or non-Turbo artifact — download to inspect.
              </p>
            ) : (
              <div className="space-y-1">
                <p className="eyebrow">
                  Contents{data.content.truncated ? " (truncated)" : ""}
                </p>
                <ul className="max-h-72 space-y-1 overflow-y-auto rounded-md border border-border p-2">
                  {data.content.entries.map((e) => (
                    <li key={e.path} className="text-sm">
                      <div className="flex items-center justify-between gap-2">
                        <span className="flex min-w-0 items-center gap-2">
                          {e.is_dir ? (
                            <Folder className="h-4 w-4 shrink-0 text-faint" aria-hidden />
                          ) : (
                            <FileText className="h-4 w-4 shrink-0 text-faint" aria-hidden />
                          )}
                          <span className="font-data truncate" title={e.path}>{e.path}</span>
                        </span>
                        {!e.is_dir && (
                          <span className="font-data shrink-0 text-muted">{formatBytes(e.size)}</span>
                        )}
                      </div>
                      {e.previewable && e.preview && (
                        <pre className="font-data mt-1 max-h-40 overflow-auto rounded bg-surface-2 p-2 text-xs text-text">
                          {e.preview}
                        </pre>
                      )}
                    </li>
                  ))}
                </ul>
              </div>
            )}

            <div className="flex justify-end">
              <Button variant="outline" onClick={() => void download()}>
                Download
              </Button>
            </div>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
```

- [ ] **Step 2: Create the ClearArtifactsDialog**

`apps/dashboard/src/components/clear-artifacts-dialog.tsx`:

```tsx
"use client";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";

const PHRASE = "delete all";

// Destructive: wipes every artifact for the org. Gated on typing the phrase so
// it can't be triggered by a stray click.
export function ClearArtifactsDialog({
  clearArtifacts,
  onCleared,
  disabled,
}: {
  clearArtifacts: () => Promise<{ deleted: number }>;
  onCleared: () => void;
  disabled?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [phrase, setPhrase] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit() {
    setBusy(true);
    try {
      await clearArtifacts();
      onCleared();
      setOpen(false);
      setPhrase("");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={(o) => { setOpen(o); if (!o) setPhrase(""); }}>
      <DialogTrigger asChild>
        <Button variant="destructive" disabled={disabled}>Clear all</Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader><DialogTitle>Clear all artifacts</DialogTitle></DialogHeader>
        <form className="space-y-3" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
          <p className="text-sm text-muted">
            This permanently deletes every cached artifact for this organization. Type{" "}
            <code className="font-data text-text">{PHRASE}</code> to confirm.
          </p>
          <Input
            aria-label="Confirmation phrase"
            value={phrase}
            onChange={(e) => setPhrase(e.target.value)}
          />
          <Button type="submit" variant="destructive" disabled={busy || phrase !== PHRASE}>
            Delete everything
          </Button>
        </form>
      </DialogContent>
    </Dialog>
  );
}
```

- [ ] **Step 3: Rewrite the Artifacts page**

`apps/dashboard/src/app/(dashboard)/artifacts/page.tsx`:

```tsx
"use client";
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Eye, Trash2 } from "lucide-react";
import { useState } from "react";
import { useApiClient } from "@/app/api";
import { ArtifactDetailDialog } from "@/components/artifact-detail-dialog";
import { ClearArtifactsDialog } from "@/components/clear-artifacts-dialog";
import { DataTable, type Column } from "@/components/data-table";
import { PageHeader } from "@/components/page-header";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import { formatBytes } from "@/lib/format";
import type { Artifact } from "@tcf/types";

const LIMIT = 50;

// ponytail: middle-truncate a hash inline (a1b2c3…9f3c).
const shortHash = (h: string) => (h.length > 18 ? `${h.slice(0, 10)}…${h.slice(-6)}` : h);

export default function ArtifactsPage() {
  const api = useApiClient();
  const qc = useQueryClient();
  const [offset, setOffset] = useState(0);
  const [detailHash, setDetailHash] = useState<string | null>(null);
  const [pendingDelete, setPendingDelete] = useState<string | null>(null);

  const { data, isLoading, isError, isFetching } = useQuery({
    queryKey: ["artifacts", offset],
    queryFn: () => api.listArtifacts({ limit: LIMIT, offset }),
    placeholderData: keepPreviousData,
  });
  const stats = useQuery({ queryKey: ["stats"], queryFn: () => api.getStats() });

  const refresh = () => {
    void qc.invalidateQueries({ queryKey: ["artifacts"] });
    void qc.invalidateQueries({ queryKey: ["stats"] });
  };
  const del = useMutation({
    mutationFn: (hash: string) => api.deleteArtifact(hash),
    onSuccess: () => { setPendingDelete(null); refresh(); },
  });

  const arts = data?.artifacts ?? [];
  const hasNext = arts.length === LIMIT;
  const hasPrev = offset > 0;

  const columns: Column<Artifact>[] = [
    { header: "Hash", cell: (a) => <code className="font-data text-sm" title={a.hash}>{shortHash(a.hash)}</code> },
    { header: "Size", cell: (a) => <span className="font-data">{formatBytes(a.size_bytes)}</span> },
    { header: "Tag", cell: (a) => (a.tag ? <Badge>{a.tag}</Badge> : <span className="text-muted">—</span>) },
    { header: "Created", cell: (a) => <span className="font-data text-muted">{new Date(a.created_at).toLocaleDateString()}</span> },
    { header: "Last accessed", cell: (a) => <span className="font-data text-muted">{new Date(a.last_accessed_at).toLocaleString()}</span> },
    {
      header: "",
      cell: (a) => (
        <div className="flex justify-end gap-1">
          <Button size="sm" variant="ghost" aria-label={`View ${a.hash}`} onClick={() => setDetailHash(a.hash)}>
            <Eye className="h-4 w-4" aria-hidden />
          </Button>
          <Button size="sm" variant="ghost" aria-label={`Delete ${a.hash}`} onClick={() => setPendingDelete(a.hash)}>
            <Trash2 className="h-4 w-4 text-danger" aria-hidden />
          </Button>
        </div>
      ),
    },
  ];

  return (
    <div>
      <PageHeader
        eyebrow="Monitor"
        title="Artifacts"
        description="Cached build outputs stored for this organization."
        actions={
          <ClearArtifactsDialog
            clearArtifacts={() => api.clearArtifacts()}
            onCleared={refresh}
            disabled={(stats.data?.artifact_count ?? 0) === 0}
          />
        }
      />

      {stats.data && (
        <div className="mb-5 flex flex-wrap gap-x-8 gap-y-1">
          <div>
            <span className="eyebrow">Artifacts</span>
            <p className="font-data text-lg text-text">{stats.data.artifact_count.toLocaleString()}</p>
          </div>
          <div>
            <span className="eyebrow">Total size</span>
            <p className="font-data text-lg text-text">{formatBytes(stats.data.storage_bytes)}</p>
          </div>
        </div>
      )}

      {isError ? (
        <p role="alert" className="rounded-md border border-danger/30 bg-danger/10 px-4 py-3 text-sm text-danger">
          Couldn&apos;t reach the cache API. Check that NEXT_PUBLIC_API_URL points at a running Turbo Cache Forge.
        </p>
      ) : isLoading ? (
        <div className="space-y-2">
          {Array.from({ length: 6 }).map((_, i) => <Skeleton key={i} className="h-10 w-full" />)}
        </div>
      ) : (
        <>
          <DataTable
            columns={columns}
            rows={arts}
            empty="No artifacts cached yet. Run a build with TURBO_TOKEN set and they'll show up here."
          />
          {(hasPrev || hasNext) && (
            <div className="mt-4 flex items-center gap-2">
              <Button variant="outline" disabled={!hasPrev || isFetching} onClick={() => setOffset((o) => Math.max(0, o - LIMIT))}>Prev</Button>
              <Button variant="outline" disabled={!hasNext || isFetching} onClick={() => setOffset((o) => o + LIMIT)}>Next</Button>
            </div>
          )}
        </>
      )}

      <ArtifactDetailDialog hash={detailHash} onClose={() => setDetailHash(null)} />

      <Dialog open={!!pendingDelete} onOpenChange={(o) => !o && setPendingDelete(null)}>
        <DialogContent>
          <DialogHeader><DialogTitle>Delete artifact</DialogTitle></DialogHeader>
          <p className="text-sm text-muted">
            Remove this cached artifact? A later build will re-upload it on the next miss.
          </p>
          <div className="flex justify-end gap-2">
            <Button variant="outline" onClick={() => setPendingDelete(null)}>Cancel</Button>
            <Button variant="destructive" disabled={del.isPending} onClick={() => pendingDelete && del.mutate(pendingDelete)}>Delete</Button>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}
```

- [ ] **Step 4: Update the page tests**

Rewrite `apps/dashboard/src/app/(dashboard)/artifacts/page.test.tsx` so the mocked client provides every method the page now calls, and add coverage for the details dialog and the typed clear-all gate. Keep the existing pagination + empty-state assertions.

```tsx
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import ArtifactsPage from "./page";

const listArtifacts = vi.fn();
const getStats = vi.fn();
const getArtifact = vi.fn();
const deleteArtifact = vi.fn();
const clearArtifacts = vi.fn();
const getArtifactBlob = vi.fn();
vi.mock("@/app/api", () => ({
  useApiClient: () => ({ listArtifacts, getStats, getArtifact, deleteArtifact, clearArtifacts, getArtifactBlob }),
}));

const art = (hash: string, size: number, tag: string | null) => ({
  hash, size_bytes: size, tag,
  created_at: "2026-07-01T00:00:00Z", last_accessed_at: "2026-07-02T00:00:00Z",
});
const STATS = { storage_bytes: 2048, artifact_count: 2, hits: 0, misses: 0, requests: 0, bytes_up: 0, bytes_down: 0 };

function wrap(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

beforeEach(() => {
  vi.clearAllMocks();
  getStats.mockResolvedValue(STATS);
});

describe("ArtifactsPage", () => {
  it("shows a page of artifacts and pages forward with offset", async () => {
    const full = [art("aaaaaaaa11111111", 4096, null),
      ...Array.from({ length: 49 }, (_, i) => art(`h${i}`, 1024, null))];
    listArtifacts
      .mockResolvedValueOnce({ limit: 50, offset: 0, artifacts: full })
      .mockResolvedValueOnce({ limit: 50, offset: 50, artifacts: [art("bbbbbbbb22222222", 8192, "build")] });

    wrap(<ArtifactsPage />);
    expect(await screen.findByText("4 KiB")).toBeInTheDocument();
    const next = screen.getByRole("button", { name: /next/i });
    expect(next).toBeEnabled();
    expect(screen.getByRole("button", { name: /prev/i })).toBeDisabled();

    await userEvent.click(next);
    expect(await screen.findByText("build")).toBeInTheDocument();
    expect(listArtifacts).toHaveBeenLastCalledWith({ limit: 50, offset: 50 });
    await waitFor(() => expect(screen.getByRole("button", { name: /next/i })).toBeDisabled());
    expect(screen.getByRole("button", { name: /prev/i })).toBeEnabled();
  });

  it("shows the empty state when there are no artifacts", async () => {
    listArtifacts.mockResolvedValueOnce({ limit: 50, offset: 0, artifacts: [] });
    wrap(<ArtifactsPage />);
    await waitFor(() => expect(screen.getByText(/no artifacts cached yet/i)).toBeInTheDocument());
  });

  it("opens the details dialog and shows the decoded manifest", async () => {
    listArtifacts.mockResolvedValueOnce({ limit: 50, offset: 0, artifacts: [art("abcd1234efgh5678", 100, null)] });
    getArtifact.mockResolvedValueOnce({
      ...art("abcd1234efgh5678", 100, null),
      content: { format: "zstd-tar", total_entries: 1, truncated: false,
        entries: [{ path: "out/log.txt", size: 5, is_dir: false, preview: "hello", previewable: true }] },
    });
    wrap(<ArtifactsPage />);
    await userEvent.click(await screen.findByRole("button", { name: /view abcd1234efgh5678/i }));
    expect(await screen.findByText("out/log.txt")).toBeInTheDocument();
    expect(screen.getByText("hello")).toBeInTheDocument();
  });

  it("gates clear-all on the typed confirmation phrase", async () => {
    listArtifacts.mockResolvedValueOnce({ limit: 50, offset: 0, artifacts: [art("h", 1, null)] });
    clearArtifacts.mockResolvedValueOnce({ deleted: 2 });
    wrap(<ArtifactsPage />);
    await userEvent.click(await screen.findByRole("button", { name: /clear all/i }));
    const confirm = await screen.findByRole("button", { name: /delete everything/i });
    expect(confirm).toBeDisabled();
    await userEvent.type(screen.getByLabelText(/confirmation phrase/i), "delete all");
    expect(confirm).toBeEnabled();
    await userEvent.click(confirm);
    await waitFor(() => expect(clearArtifacts).toHaveBeenCalled());
  });
});
```

- [ ] **Step 5: Run tests + typecheck**

Run: `cd apps/dashboard && ./node_modules/.bin/tsc --noEmit && ./node_modules/.bin/vitest run`
Expected: PASS — all suites green (the four Artifacts tests plus the rest of the dashboard suite).

- [ ] **Step 6: Commit**

```bash
git add apps/dashboard/src/components/artifact-detail-dialog.tsx apps/dashboard/src/components/clear-artifacts-dialog.tsx apps/dashboard/src/app/\(dashboard\)/artifacts/page.tsx apps/dashboard/src/app/\(dashboard\)/artifacts/page.test.tsx
git commit -m "feat(dashboard): Artifacts page — details/content viewer, per-row delete, clear-all"
```

---

## Final verification (after all tasks)

- [ ] `cd services/api && go build ./... && go test ./...` — all Go tests green.
- [ ] `cd apps/dashboard && ./node_modules/.bin/tsc --noEmit && ./node_modules/.bin/vitest run` — tsc + all vitest green; `cd packages/api-client && pnpm test` green.
- [ ] Rebuild the Docker `cache-api` + `dashboard` images and E2E against the live stack: create a token, run `apps/example/run-demo.sh` to populate artifacts, then in the dashboard open an artifact's details (verify the manifest + `turbo-build.log` preview render), delete one artifact (verify it disappears and the count drops), and clear-all with the typed phrase (verify empty state + `storage_bytes` → 0). Confirms the real repo SQL + storage delete paths that the unit tests exercise only through fakes.

## Deferred (out of scope for this plan)

- OpenAPI (`internal/openapi`) documentation for the four new routes — docs-only; add in a follow-up to keep this plan focused on the working feature.
