# turbo-cache-forge — Phase 2 (Concurrency & heavy-cache hardening) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prove the Phase 1 cache API survives real parallel CI load and large artifacts, give it an OTel tracing seam and Sentry error reporting without adding a second metrics pipeline, add the batch existence endpoint reserved in Phase 1, and clear the Phase-1 follow-up backlog. No new product surface — this is depth on what exists.

**Architecture (unchanged from Phase 1):** Single Go binary (`chi` router over `net/http`). CLI hot path = validate hashed token → stream bytes to/from a `storage.Storage` backend (filesystem default, S3 optional). Postgres holds orgs, tokens, and artifact metadata; metadata writes stay off the download hot path. Phase 2 adds two cross-cutting seams around that same shape: an OTel tracer (no-op unless `OTEL_EXPORTER_OTLP_ENDPOINT` is set) wrapping storage + DB calls, and Sentry (no-op unless `SENTRY_DSN` is set) capturing panics and storage/DB 5xx errors. Neither seam changes the hot-path request flow.

**Tech Stack:** Go 1.25 (`services/api/go.mod` already reads `go 1.25.0`; this plan verifies it, does not re-bump it — see Task 1), chi v5, pgx v5, aws-sdk-go-v2 (S3 backend), prometheus/client_golang (+ `prometheus/testutil` for counter assertions), stdlib `testing` + `net/http/httptest`. New: `go.opentelemetry.io/otel` + `otel/sdk` + `otel/exporters/otlp/otlptrace/otlptracehttp` + `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp`; `github.com/getsentry/sentry-go` (+ its `/http` subpackage). Postgres 16 in docker-compose (unchanged).

## Global Constraints

All Phase 1 constraints still hold (see `docs/ROADMAP.md`'s **Cross-phase invariants** — read it before starting; nothing here may regress it):

- Go module path: `github.com/nasraldin/turbo-cache-forge/services/api`. All internal imports use this prefix.
- Backend imports **no auth-vendor SDK**. Hashed bearer tokens only; OIDC/JWT is Phase 3.
- Storage is accessed **only** through the `storage.Storage` interface — no direct disk/S3 calls in handlers. New wrappers (e.g. the Task 8 tracing decorator) still implement `storage.Storage`, they don't bypass it.
- Never buffer a whole artifact in memory: `io.Copy` / streaming everywhere. The Task 7 load test's job is to *prove* this under concurrency, not just assert it in prose.
- **One metrics pipeline** (Prometheus). OTel in this phase is **tracing only** — it must never register a metrics exporter, meter provider, or a second `/metrics`-shaped endpoint.
- **DB off the download hot path** — `TouchArtifact` stays a fire-and-forget goroutine on a detached context; nothing in this phase makes GET wait on Postgres.
- Tokens stored only as SHA-256 hex; tenant isolation via `storageKey(orgSlug, hash)` + `validHash` + `UNIQUE(org_id, hash)`.
- Every task ends green (`go test ./...`) and is committed.

Phase-2-specific additions to those invariants:

- **OTel tracing is opt-in and free when off.** `obs.InitTracer` only registers a real `TracerProvider` when `OTEL_EXPORTER_OTLP_ENDPOINT` is set; otherwise otel's own built-in no-op provider stays in place globally. There is no separate `TRACING_ENABLED` flag to keep in sync with the endpoint var — one env var is the single on/off switch.
- **Sentry is opt-in and free when off.** `obs.InitSentry` only calls `sentry.Init` when `SENTRY_DSN` is set. `sentry-go`'s package-level `CaptureException` is a safe no-op when `Init` was never called, so `obs.CaptureError` is unconditionally safe to call from every error path, gated or not.
- **Sentry reports 5xx, not 4xx.** Only storage/DB errors that produce a 500 (and panics) go to Sentry. Client mistakes (bad hash → 400, oversize upload → 413, bad auth → 401) are not bugs and must never be reported.
- **The load test never runs in the default `go test ./...`.** It lives behind a `//go:build loadtest` build tag so CI stays fast; it's a deliberate, separately-invoked verification tool (see Task 7 and the Verification section).
- **The batch endpoint's request body cap is independent of `MaxUploadBytes`.** A hash list is never artifact-sized; capping it separately (1 MiB) stops it from being a JSON-parsing denial-of-service vector regardless of how large `MAX_UPLOAD_BYTES` is configured.

---

## File structure (services/api) — Phase 2 deltas only

```
services/api/
  go.mod                                       (+ otel/otel-sdk/otlptracehttp/otelhttp, sentry-go deps)
  cmd/server/main.go                           modify: InitTracer/InitSentry wiring, storage.WithTracing
  internal/
    obs/
      tracing.go                               new: InitTracer, StartStorageSpan/StartDBSpan, EndSpan
      tracing_test.go                          new
      sentry.go                                new: InitSentry, CaptureError
      sentry_test.go                           new
      metrics.go                               modify: statusWriter.ReadFrom passthrough (sendfile fast path)
      metrics_test.go                          new
    storage/
      tracing.go                               new: WithTracing(Storage) Storage decorator
      tracing_test.go                          new
      s3/s3.go                                 modify: credentialOptions() skip-when-empty, ContentLength dedupe
      s3/s3_test.go                            modify: + TestCredentialOptionsSkipStaticWhenBothEmpty
    db/repo.go                                 modify: named returns + span on all 4 methods
    db/repo_test.go                            modify: + TestRepoMethodsEmitSpans (DB-gated, same skip pattern)
    auth/token.go                              modify: restore the ponytail rationale comment
    turbo/handlers.go                          modify: batchExists handler + route, PUT orphan compensation,
                                                Sentry CaptureError calls on 5xx branches
    turbo/handlers_test.go                     modify: testutil.ToFloat64 assertions, batch endpoint tests,
                                                orphan-compensation test, testRouter returns *obs.Metrics too
    turbo/loadtest_test.go                     new (//go:build loadtest) — concurrency + heavy-artifact suite
    server/router.go                           modify: otelhttp.NewHandler wrap, sentryhttp middleware
infra/
  docker/Dockerfile                            modify: pin goose to a released version, drop GOTOOLCHAIN=auto
.env.example                                   modify: + OTEL_EXPORTER_OTLP_ENDPOINT, SENTRY_DSN
README.md                                      modify: load test invocation + observability env vars
```

---

## Task 1: Toolchain confirmation + pin `goose` (Docker reproducibility)

**Files:**
- Verify: `services/api/go.mod`
- Modify: `infra/docker/Dockerfile`

**Interfaces:** none (build/infra only).

Phase 1 left the Docker `goose` build stage on `@latest` + `GOTOOLCHAIN=auto`, flagged in the backlog as non-reproducible. Separately, `docs/ROADMAP.md`'s cross-phase invariants already state Go 1.25 as the toolchain floor and `services/api/go.mod` already reads `go 1.25.0` — **no version bump is needed in this task**, only confirmation, so this task is pure housekeeping with no TDD cycle.

- [ ] **Step 1: Confirm the toolchain is already 1.25**

Run:
```bash
cd services/api && go version && head -3 go.mod
```
Expected: installed `go` ≥ 1.25, and `go.mod`'s `go 1.25.0` line unchanged. If either is not true, stop and re-scope this task — do not silently downgrade or upgrade go.mod as a side effect of Docker work.

- [ ] **Step 2: Pin `goose` instead of `@latest`**

Current `infra/docker/Dockerfile` goose stage:
```dockerfile
FROM golang:1.25 AS goose
ENV GOTOOLCHAIN=auto
RUN go install github.com/pressly/goose/v3/cmd/goose@latest
COPY infra/migrations /migrations
ENTRYPOINT ["goose"]
```

**Decision:** pin to `goose/v3@v3.24.1` (latest stable release compatible with Go 1.25 at plan-authoring time) and drop `GOTOOLCHAIN=auto` — it was a Phase-1 workaround for `@latest`'s unpredictable `go.mod` floor; with a pinned release and a known-compatible base image it's dead weight. Before executing, run `go list -m -versions github.com/pressly/goose/v3` (or check the GitHub releases page) and bump the pin if a newer patch exists — never re-introduce `@latest`.

```dockerfile
FROM golang:1.25 AS goose
RUN go install github.com/pressly/goose/v3/cmd/goose@v3.24.1
COPY infra/migrations /migrations
ENTRYPOINT ["goose"]
```

- [ ] **Step 3: Verify the build is reproducible**

```bash
docker build -f infra/docker/Dockerfile --target goose -t tcf-goose:test .
docker run --rm tcf-goose:test --version   # prints the pinned version, not "@latest" drift
```

- [ ] **Step 4: Commit**
```bash
git add infra/docker/Dockerfile
git commit -m "chore(docker): pin goose to a released version, drop GOTOOLCHAIN=auto"
```

---

## Task 2: Backlog cleanup — S3 credential-chain fallback, `ContentLength` dedupe, `token.go` comment

**Files:**
- Modify: `services/api/internal/storage/s3/s3.go`
- Test: `services/api/internal/storage/s3/s3_test.go`
- Modify: `services/api/internal/auth/token.go`

**Interfaces:**
- Produces: `credentialOptions(accessKey, secretKey string) []func(*awscfg.LoadOptions) error` — an unexported helper `New` composes into its options list.

Three small, independent, no-behavior-change-except-one-bugfix items from the Phase-1 backlog, grouped because none needs its own task-sized ceremony.

### 2a — S3 `New`: skip static creds when both keys are empty

Today `New` unconditionally calls `awscfg.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""))`, which overrides the SDK's default credential chain even when both are `""`. That's fine for R2/MinIO (always keyed) but breaks AWS deployments that rely on an IAM role.

- [ ] **Step 1: Write the failing test**

Add to `internal/storage/s3/s3_test.go`:
```go
func TestCredentialOptionsSkipStaticWhenBothEmpty(t *testing.T) {
	if got := len(credentialOptions("", "")); got != 0 {
		t.Fatalf("credentialOptions(\"\",\"\") returned %d options, want 0 (fall back to SDK default chain: env vars, IAM role, ~/.aws/credentials)", got)
	}
	if got := len(credentialOptions("ak", "sk")); got != 1 {
		t.Fatalf("credentialOptions(\"ak\",\"sk\") returned %d options, want 1 (static provider)", got)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/storage/s3/... -run TestCredentialOptions -v`
Expected: FAIL — `credentialOptions` undefined.

- [ ] **Step 3: Implement**

In `internal/storage/s3/s3.go`, replace the credential wiring in `New`:
```go
// credentialOptions returns a static-credentials load option only when at
// least one key is non-empty. When both are empty, New falls back to the
// SDK's default credential chain (env vars, shared config, EC2/ECS IAM
// role) — required for AWS deployments that don't hand out static keys.
func credentialOptions(accessKey, secretKey string) []func(*awscfg.LoadOptions) error {
	if accessKey == "" && secretKey == "" {
		return nil
	}
	return []func(*awscfg.LoadOptions) error{
		awscfg.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
	}
}

func New(ctx context.Context, cfg Config) (*Store, error) {
	opts := append([]func(*awscfg.LoadOptions) error{awscfg.WithRegion(cfg.Region)},
		credentialOptions(cfg.AccessKey, cfg.SecretKey)...)
	loaded, err := awscfg.LoadDefaultConfig(ctx, opts...)
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
```

### 2b — Dedupe `ContentLength` extraction

`Get` and `Head` each repeat the same 3-line `*int64` nil-check. Extract it:
```go
func contentLength(cl *int64) int64 {
	if cl == nil {
		return 0
	}
	return *cl
}
```
Replace both extraction sites with `size := contentLength(out.ContentLength)`.

- [ ] **Step 4: Run + commit (2a + 2b)**

Run: `go test ./internal/storage/... -v` → PASS (S3 conformance test still skips without `S3_TEST_ENDPOINT`, unaffected).
```bash
git add services/api/internal/storage/s3
git commit -m "fix(api): S3 New skips static creds when unset; dedupe ContentLength extraction"
```

### 2c — Restore the `token.go` ponytail rationale comment

The Phase 1 plan documented why `HashToken` doesn't need a constant-time compare; the shipped code lost the comment along the way. Restore it above `HashToken` in `internal/auth/token.go`:
```go
// ponytail: no constant-time compare needed — we look up the SHA-256 hash of
// a 256-bit random token by indexed equality against api_keys.token_hash;
// there is no low-entropy secret here for a timing attack to extract.
func HashToken(token string) string {
```

- [ ] **Step 5: Verify + commit**

Run: `go build ./... && go test ./internal/auth/... -v` → PASS (comment-only change).
```bash
git add services/api/internal/auth/token.go
git commit -m "docs(api): restore ponytail rationale for token hash comparison"
```

---

## Task 3: Assert metric counter values (not just HTTP status)

**Files:**
- Modify: `services/api/internal/turbo/handlers_test.go`

**Interfaces:**
- Changes: `testRouter(store ArtifactStore, repo MetaRepo) (http.Handler, *obs.Metrics)` — was `http.Handler` only; callers now get the metrics object to assert on.

Phase 1's `TestPutThenGetRoundTrip`/`TestGetMissIs404` only assert HTTP status codes, so a swapped `CacheHit`/`CacheMiss` increment wouldn't be caught. `prometheus/client_golang/prometheus/testutil` (already vended transitively via the existing `client_golang` dependency — no new `go get`) fixes that.

- [ ] **Step 1: Write the failing assertions**

Change `testRouter`'s signature and the 3 call sites in `handlers_test.go`:
```go
func testRouter(store ArtifactStore, repo MetaRepo) (http.Handler, *obs.Metrics) {
	m := obs.NewMetrics()
	h := NewHandler(store, repo, 1<<20, m)
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := auth.WithOrg(req.Context(), &db.Org{ID: 1, Slug: "team-a"})
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	h.Mount(r)
	return r, m
}
```

Update `TestStatus` to `r, _ := testRouter(...)`. Extend `TestPutThenGetRoundTrip` and `TestGetMissIs404`:
```go
import "github.com/prometheus/client_golang/prometheus/testutil"

func TestPutThenGetRoundTrip(t *testing.T) {
	store := newMemStore()
	r, m := testRouter(store, &memRepo{})
	body := []byte("tarball-zst-bytes")

	rec := httptest.NewRecorder()
	put := httptest.NewRequest(http.MethodPut, "/v8/artifacts/hash123?teamId=team-a", bytes.NewReader(body))
	r.ServeHTTP(rec, put)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("PUT = %d, want 202", rec.Code)
	}
	if got := testutil.ToFloat64(m.UploadBytes); got != float64(len(body)) {
		t.Fatalf("UploadBytes = %v, want %d", got, len(body))
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
	if got := testutil.ToFloat64(m.CacheHit); got != 1 {
		t.Fatalf("CacheHit = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.CacheMiss); got != 0 {
		t.Fatalf("CacheMiss = %v, want 0 — this GET was a hit, not a miss", got)
	}
	if got := testutil.ToFloat64(m.DownloadBytes); got != float64(len(body)) {
		t.Fatalf("DownloadBytes = %v, want %d", got, len(body))
	}
}

func TestGetMissIs404(t *testing.T) {
	r, m := testRouter(newMemStore(), &memRepo{})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v8/artifacts/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET miss = %d, want 404", rec.Code)
	}
	if got := testutil.ToFloat64(m.CacheMiss); got != 1 {
		t.Fatalf("CacheMiss = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.CacheHit); got != 0 {
		t.Fatalf("CacheHit = %v, want 0 — this GET was a miss, not a hit", got)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/turbo/... -v`
Expected: FAIL to compile — `testRouter` signature mismatch at the 3 existing call sites, and `testutil` unresolved until imported.

- [ ] **Step 3: Fix remaining call sites + confirm green**

Run: `go test ./internal/turbo/... -v` → PASS.

- [ ] **Step 4: Commit**
```bash
git add services/api/internal/turbo/handlers_test.go
git commit -m "test(api): assert cache hit/miss/byte counters, not just HTTP status"
```

---

## Task 4: `GET` `io.ReaderFrom` fast path (undo the metrics wrapper's sendfile regression)

**Files:**
- Modify: `services/api/internal/obs/metrics.go`
- Test: `services/api/internal/obs/metrics_test.go` (new)

**Interfaces:**
- `statusWriter` gains `ReadFrom(io.Reader) (int64, error)`.

`get()`'s final `io.Copy(w, rc)` would use the kernel sendfile/splice fast path when `rc` is an `*os.File` and the underlying `http.response` (which implements `io.ReaderFrom`) is the direct write target. But the metrics middleware wraps every response in `*statusWriter`, which only embeds the `http.ResponseWriter` **interface** — method promotion is based on the field's static type, so embedding doesn't make `*statusWriter` satisfy `io.ReaderFrom` even though the concrete writer underneath does. `io.Copy` then silently falls back to a generic buffered copy loop. This costs throughput, not correctness — worth fixing under a "heavy-cache hardening" phase.

- [ ] **Step 1: Write the failing test**

`internal/obs/metrics_test.go`:
```go
package obs

import (
	"bytes"
	"io"
	"net/http/httptest"
	"testing"
)

// fakeReaderFromWriter proves whether io.Copy actually used the ReadFrom
// fast path (as *http.response would for sendfile) instead of falling back
// to a byte-by-byte copy loop through Write.
type fakeReaderFromWriter struct {
	httptest.ResponseRecorder
	readFromCalled bool
	written        []byte
}

func (f *fakeReaderFromWriter) ReadFrom(r io.Reader) (int64, error) {
	f.readFromCalled = true
	b, err := io.ReadAll(r)
	f.written = b
	return int64(len(b)), err
}

func TestStatusWriterForwardsReaderFrom(t *testing.T) {
	fw := &fakeReaderFromWriter{}
	sw := &statusWriter{ResponseWriter: fw, status: 200}

	src := bytes.NewReader([]byte("payload-bytes"))
	n, err := io.Copy(sw, src) // io.Copy type-asserts sw itself for io.ReaderFrom
	if err != nil {
		t.Fatal(err)
	}
	if !fw.readFromCalled {
		t.Fatal("io.Copy did not use the ReaderFrom fast path — statusWriter must forward it")
	}
	if n != int64(len("payload-bytes")) || !bytes.Equal(fw.written, []byte("payload-bytes")) {
		t.Fatalf("copied %q (%d bytes), want %q", fw.written, n, "payload-bytes")
	}
}

func TestStatusWriterReadFromFallsBackWithoutReaderFrom(t *testing.T) {
	rec := httptest.NewRecorder() // *httptest.ResponseRecorder itself has no ReadFrom
	sw := &statusWriter{ResponseWriter: rec, status: 200}
	n, err := io.Copy(sw, bytes.NewReader([]byte("x")))
	if err != nil || n != 1 || rec.Body.String() != "x" {
		t.Fatalf("fallback copy = %d, %v, body=%q", n, err, rec.Body.String())
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/obs/... -run TestStatusWriter -v`
Expected: `TestStatusWriterForwardsReaderFrom` FAILS (`readFromCalled` stays false — `io.Copy` doesn't see `*statusWriter` as an `io.ReaderFrom` yet). The fallback test passes already (that's the current, un-regressed behavior).

- [ ] **Step 3: Implement**

Add to `internal/obs/metrics.go`:
```go
// ReadFrom forwards to the wrapped ResponseWriter's ReadFrom when it has one
// (http.response does, enabling the kernel sendfile/splice fast path for
// *os.File sources). Embedding a bare http.ResponseWriter interface value
// does NOT promote this method — promotion is based on the field's static
// (interface) type, not whatever concrete writer sits behind it at runtime —
// so this passthrough has to be explicit.
func (s *statusWriter) ReadFrom(r io.Reader) (int64, error) {
	if rf, ok := s.ResponseWriter.(io.ReaderFrom); ok {
		return rf.ReadFrom(r)
	}
	return io.Copy(writerOnly{s.ResponseWriter}, r)
}

// writerOnly strips every interface but io.Writer so the fallback io.Copy
// above can't recurse back into statusWriter.ReadFrom.
type writerOnly struct{ io.Writer }
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/obs/... ./internal/server/... -v` → PASS.

- [ ] **Step 5: Commit**
```bash
git add services/api/internal/obs
git commit -m "perf(api): forward io.ReaderFrom through statusWriter for GET's sendfile fast path"
```

---

## Task 5: PUT store→metadata orphan compensation

**Files:**
- Modify: `services/api/internal/turbo/handlers.go`
- Test: `services/api/internal/turbo/handlers_test.go`

**Interfaces:**
- Changes: `ArtifactStore` gains `Delete(ctx context.Context, key string) error` (the 4th method of `storage.Storage`, already implemented by `filesystem.FS` and `s3.Store` — no production backend changes needed). Test fakes `memStore`/`spyStore` get a `Delete` method.

**Decision:** compensate eagerly with a best-effort delete rather than leaving a note-only "self-heals on retry." Reasoning: `get()` never consults the DB before reading storage (only `TouchArtifact` touches the DB, fire-and-forget, after the read) — so a stored-but-unrecorded object is *not* inaccessible, it's **untrackable**: invisible to Phase 3's quota accounting and LRU/TTL cleanup sweep (both query `cache_artifacts`), i.e. a permanent storage leak, not a harmless orphan. Turbo's client sees the PUT's non-2xx response and doesn't mark the hash cached, so it will just re-upload on a future build — there's no client-visible retry to coordinate with, making an eager compensating delete strictly safer than waiting for a repair sweep that doesn't exist yet.

- [ ] **Step 1: Write the failing test**

Add to `internal/turbo/handlers_test.go`:
```go
// memStore gains Delete (required once ArtifactStore does):
func (m *memStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	delete(m.data, key)
	m.mu.Unlock()
	return nil
}

// spyStore gains a Delete passthrough + call flag:
// (add `deleteCalled bool` to the existing spyStore struct)
func (s *spyStore) Delete(ctx context.Context, key string) error {
	s.deleteCalled = true
	return s.memStore.Delete(ctx, key)
}

type failingUpsertRepo struct{ memRepo }

func (f *failingUpsertRepo) UpsertArtifact(context.Context, int64, string, int64, string) error {
	return errors.New("db unavailable")
}

func TestPutCompensatesStorageWhenMetadataWriteFails(t *testing.T) {
	store := newSpyStore()
	m := obs.NewMetrics()
	h := NewHandler(store, &failingUpsertRepo{}, 1<<20, m)

	rec := httptest.NewRecorder()
	req := requestWithHash(http.MethodPut, "a1b2c3")
	req.Body = io.NopCloser(bytes.NewReader([]byte("payload")))
	h.put(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("PUT with failing metadata write = %d, want 500", rec.Code)
	}
	if !store.deleteCalled {
		t.Fatal("expected a compensating Delete after UpsertArtifact failure")
	}
	if _, err := store.Head(context.Background(), "team-a/a1b2c3"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("object should have been deleted after metadata failure, Head err = %v", err)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/turbo/... -v`
Expected: FAIL to compile — `ArtifactStore` (as currently typed) has no `Delete`, so `spyStore`/`memStore` don't need one yet and `h.store.Delete` in the handler doesn't exist; also `store.deleteCalled` doesn't exist until the struct field is added.

- [ ] **Step 3: Implement**

Widen the interface and the handler in `internal/turbo/handlers.go`:
```go
type ArtifactStore interface {
	Put(ctx context.Context, key string, r io.Reader) error
	Get(ctx context.Context, key string) (io.ReadCloser, *storage.ObjectInfo, error)
	Head(ctx context.Context, key string) (*storage.ObjectInfo, error)
	Delete(ctx context.Context, key string) error
}
```

In `put()`, replace the `UpsertArtifact` error branch:
```go
	tag := r.Header.Get("x-artifact-tag")
	if err := h.repo.UpsertArtifact(r.Context(), org.ID, hash, info.Size, tag); err != nil {
		// Best-effort compensating delete — see Task 5's Decision note in the
		// Phase 2 plan for why this is eager rather than a repair-sweep TODO.
		if delErr := h.store.Delete(r.Context(), key); delErr != nil {
			log.Printf("turbo: put %s: compensating delete after metadata failure also failed: %v", key, delErr)
		}
		http.Error(w, "metadata write failed", http.StatusInternalServerError)
		return
	}
```
Add `"log"` to the import block.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/turbo/... -v` → PASS (all prior tests still pass since `filesystem.FS` and `s3.Store` already implement `Delete`).

- [ ] **Step 5: Commit**
```bash
git add services/api/internal/turbo/handlers.go services/api/internal/turbo/handlers_test.go
git commit -m "fix(api): compensate storage with a best-effort delete when PUT metadata write fails"
```

---

## Task 6: Batch existence endpoint `POST /v8/artifacts`

**Files:**
- Modify: `services/api/internal/turbo/handlers.go`
- Test: `services/api/internal/turbo/handlers_test.go`

**Interfaces:**
- Produces: `POST /v8/artifacts` — request `{"hashes": ["<hash>", ...]}`, response `{"hashes": {"<hash>": {"exists": bool}, ...}}`.

**Decision:** the response omits artifact `size`. `MetaRepo.ArtifactExists` — the method this endpoint is contractually built on, reserved in Phase 1 for exactly this — reports existence only; sourcing size too would need a new batched SQL query (`size_bytes WHERE hash = ANY($1)`), which is YAGNI until a real client asks for it. A bounded loop of N `ArtifactExists` calls is acceptable here because this endpoint runs once per build up front, not on the download hot path — it does not violate the "DB off the hot path" invariant (that invariant is specifically about the GET/download path).

**Decision:** cap the batch independently — `batchBodyMaxBytes = 1<<20` (1 MiB) regardless of `MaxUploadBytes`, and `maxBatchHashes = 1000` — so a hash-list body can never become a resource-exhaustion vector no matter how large artifact uploads are configured to be.

- [ ] **Step 1: Write the failing tests**

Add to `internal/turbo/handlers_test.go`:
```go
import (
	"encoding/json"
	"fmt"
	"strings"
)

// hashSetRepo lets a test control ArtifactExists per-hash instead of the
// single blanket bool memRepo offers.
type hashSetRepo struct {
	memRepo
	exists map[string]bool
}

func (r *hashSetRepo) ArtifactExists(_ context.Context, _ int64, hash string) (bool, error) {
	return r.exists[hash], nil
}

func TestBatchExists(t *testing.T) {
	repo := &hashSetRepo{exists: map[string]bool{"h1": true}}
	r, _ := testRouter(newMemStore(), repo)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v8/artifacts", strings.NewReader(`{"hashes":["h1","h2"]}`))
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("POST /v8/artifacts = %d, want 200: %s", rec.Code, rec.Body)
	}
	var got batchResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Hashes["h1"].Exists || got.Hashes["h2"].Exists {
		t.Fatalf("batch response = %+v, want h1=true h2=false", got.Hashes)
	}
}

func TestBatchExistsRejectsEmptyOrOversizedList(t *testing.T) {
	r, _ := testRouter(newMemStore(), &memRepo{})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v8/artifacts", strings.NewReader(`{"hashes":[]}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty hashes = %d, want 400", rec.Code)
	}

	big := make([]string, 1001)
	for i := range big {
		big[i] = fmt.Sprintf("h%d", i)
	}
	payload, _ := json.Marshal(batchRequest{Hashes: big})
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v8/artifacts", bytes.NewReader(payload)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("1001 hashes = %d, want 400", rec.Code)
	}
}

func TestBatchExistsRejectsHostileHash(t *testing.T) {
	r, _ := testRouter(newMemStore(), &memRepo{})
	rec := httptest.NewRecorder()
	body := `{"hashes":["../team-b/secret"]}`
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v8/artifacts", strings.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("hostile hash = %d, want 400", rec.Code)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/turbo/... -v`
Expected: FAIL to compile — `batchResponse`/`batchRequest` undefined, route not mounted.

- [ ] **Step 3: Implement**

Add to `internal/turbo/handlers.go`:
```go
const (
	maxBatchHashes    = 1000
	batchBodyMaxBytes = 1 << 20 // 1 MiB — a hash list never needs an artifact-sized body
)

type batchRequest struct {
	Hashes []string `json:"hashes"`
}

type batchArtifact struct {
	Exists bool `json:"exists"`
}

type batchResponse struct {
	Hashes map[string]batchArtifact `json:"hashes"`
}

// batchExists lets a client ask which of many hashes are already cached in
// one round trip instead of one HEAD per hash — the intended consumer of
// MetaRepo.ArtifactExists, reserved for exactly this in Phase 1.
func (h *Handler) batchExists(w http.ResponseWriter, r *http.Request) {
	org, _ := auth.OrgFromContext(r.Context())

	body := http.MaxBytesReader(w, r.Body, batchBodyMaxBytes)
	defer body.Close()

	var req batchRequest
	if err := json.NewDecoder(body).Decode(&req); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if len(req.Hashes) == 0 || len(req.Hashes) > maxBatchHashes {
		http.Error(w, fmt.Sprintf("hashes must contain 1..%d entries", maxBatchHashes), http.StatusBadRequest)
		return
	}

	out := make(map[string]batchArtifact, len(req.Hashes))
	for _, hash := range req.Hashes {
		if !validHash(hash) {
			http.Error(w, "invalid hash: "+hash, http.StatusBadRequest)
			return
		}
		exists, err := h.repo.ArtifactExists(r.Context(), org.ID, hash)
		if err != nil {
			obs.CaptureError(err) // wired in Task 9; safe no-op until then
			http.Error(w, "lookup failed", http.StatusInternalServerError)
			return
		}
		out[hash] = batchArtifact{Exists: exists}
	}
	writeJSON(w, http.StatusOK, batchResponse{Hashes: out})
}
```
Add `"fmt"` to the import block. Register the route in `Mount`:
```go
	r.Post("/v8/artifacts", h.batchExists)
```

Note: the `obs.CaptureError` call here is written now but only becomes a real no-op-vs-real-report switch once Task 9 lands `obs.CaptureError`; since it's unconditionally safe to call before Sentry is initialized (see the Global Constraints note), this ordering is fine and avoids touching this handler again in Task 9.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/turbo/... -v` → PASS.

- [ ] **Step 5: Commit**
```bash
git add services/api/internal/turbo/handlers.go services/api/internal/turbo/handlers_test.go
git commit -m "feat(api): batch artifact existence endpoint (POST /v8/artifacts)"
```

---

## Task 7: Concurrency & heavy-cache load test

**Files:**
- New: `services/api/internal/turbo/loadtest_test.go` (`//go:build loadtest`)

**Interfaces:** none new — exercises the existing `NewHandler`/`Mount` over a real `httptest.Server` + real `filesystem.Storage` backend.

**Decision:** the harness uses the real filesystem backend (a `t.TempDir()`) plus the existing in-memory `memRepo` fake, not Postgres. The point of this suite is proving concurrency-safety of the HTTP+storage path (streaming, atomic rename, `MaxBytesReader`), which is backend-agnostic; DB metadata writes are already fire-and-forget off this path per the cross-phase invariant, so wiring real Postgres here would add setup cost without testing anything new.

**Decision (concrete numbers, resolving ROADMAP's open question):** 64 concurrent goroutines for the HTTP-level concurrency tests; a 200 MiB artifact with a 32 MiB heap-growth ceiling for the flat-memory test (large enough that a full-buffer regression would be unmistakable against the ceiling; generous enough to absorb ordinary Go runtime/HTTP buffering noise without masking a real one).

**Decision:** this file is excluded from the default `go test ./...` via a build tag (not merely an env var check) so CI never accidentally pays for it, and so `go vet`/`go build ./...` in Task 1–6 never needed to compile it either.

- [ ] **Step 1: Write the load test file**

`internal/turbo/loadtest_test.go`:
```go
//go:build loadtest

package turbo

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/auth"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/db"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/obs"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/storage/filesystem"
)

func newLoadTestServer(t *testing.T, maxBytes int64) (*httptest.Server, string) {
	t.Helper()
	dir := t.TempDir()
	store := filesystem.New(dir)
	m := obs.NewMetrics()
	h := NewHandler(store, &memRepo{}, maxBytes, m)

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := auth.WithOrg(req.Context(), &db.Org{ID: 1, Slug: "team-a"})
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	h.Mount(r)

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, dir
}

// TestConcurrentDistinctHashes fires 64 goroutines each PUTting then GETting
// its own hash+payload, and asserts every readback's sha256 matches exactly
// — proving the storage layer never cross-contaminates keys under load.
func TestConcurrentDistinctHashes(t *testing.T) {
	const workers = 64
	srv, _ := newLoadTestServer(t, 10<<20)

	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			hash := fmt.Sprintf("hash%03d", i)
			payload := bytes.Repeat([]byte(fmt.Sprintf("%d-", i)), 1000)
			sum := sha256.Sum256(payload)

			put, _ := http.NewRequest(http.MethodPut, srv.URL+"/v8/artifacts/"+hash, bytes.NewReader(payload))
			resp, err := http.DefaultClient.Do(put)
			if err != nil {
				errs <- err
				return
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusAccepted {
				errs <- fmt.Errorf("worker %d: PUT status = %d", i, resp.StatusCode)
				return
			}

			get, _ := http.NewRequest(http.MethodGet, srv.URL+"/v8/artifacts/"+hash, nil)
			resp, err = http.DefaultClient.Do(get)
			if err != nil {
				errs <- err
				return
			}
			defer resp.Body.Close()
			got, _ := io.ReadAll(resp.Body)
			if sha256.Sum256(got) != sum {
				errs <- fmt.Errorf("worker %d: readback corrupted (sha256 mismatch)", i)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// TestConcurrentSameHashIdempotent PUTs the exact same hash+identical
// payload from 32 goroutines, interleaved with 32 concurrent GETs. The
// filesystem backend publishes each PUT via write-to-tempfile + atomic
// os.Rename (internal/storage/filesystem/fs.go), so every GET must see
// either nothing (raced ahead of the first rename) or one COMPLETE copy of
// the payload — never a partial/torn write.
func TestConcurrentSameHashIdempotent(t *testing.T) {
	const writers, readers = 32, 32
	srv, _ := newLoadTestServer(t, 10<<20)
	const hash = "shared-hash"
	payload := bytes.Repeat([]byte("stable-payload-"), 2000)

	var wg sync.WaitGroup
	errs := make(chan error, writers+readers)

	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest(http.MethodPut, srv.URL+"/v8/artifacts/"+hash, bytes.NewReader(payload))
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				errs <- err
				return
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusAccepted {
				errs <- fmt.Errorf("PUT status = %d", resp.StatusCode)
			}
		}()
	}
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := http.Get(srv.URL + "/v8/artifacts/" + hash)
			if err != nil {
				errs <- err
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusNotFound {
				return // fine: this GET raced ahead of every writer's first rename
			}
			if resp.StatusCode != http.StatusOK {
				errs <- fmt.Errorf("GET status = %d", resp.StatusCode)
				return
			}
			got, _ := io.ReadAll(resp.Body)
			if !bytes.Equal(got, payload) {
				errs <- fmt.Errorf("GET returned %d bytes, want a complete %d-byte payload (torn read)", len(got), len(payload))
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	resp, err := http.Get(srv.URL + "/v8/artifacts/" + hash)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("final GET failed: %v, status %v", err, resp)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	if !bytes.Equal(got, payload) {
		t.Fatal("final content corrupted")
	}
}

// TestFlatMemoryOnLargeArtifact proves PUT/GET never buffers a whole
// artifact server-side: both source and verification are file/hash-streamed
// on the client side too, so a heap-growth spike can only come from the
// server goroutine.
//
// Decision: 200 MiB artifact, 32 MiB growth ceiling — see this task's
// Decision note in the plan.
func TestFlatMemoryOnLargeArtifact(t *testing.T) {
	const size = 200 << 20
	const growthCeiling = 32 << 20
	srv, _ := newLoadTestServer(t, size+(1<<20))

	srcPath := filepath.Join(t.TempDir(), "big-src")
	sum := writeDeterministicFile(t, srcPath, size)

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	src, err := os.Open(srcPath)
	if err != nil {
		t.Fatal(err)
	}
	put, _ := http.NewRequest(http.MethodPut, srv.URL+"/v8/artifacts/big", src)
	put.ContentLength = size
	resp, err := http.DefaultClient.Do(put)
	src.Close()
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("PUT status = %d", resp.StatusCode)
	}

	resp, err = http.Get(srv.URL + "/v8/artifacts/big")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	h := sha256.New()
	n, err := io.Copy(h, resp.Body) // streamed verification: no full-body buffer client-side either
	if err != nil {
		t.Fatal(err)
	}
	if n != size {
		t.Fatalf("downloaded %d bytes, want %d", n, size)
	}
	if got := h.Sum(nil); !bytes.Equal(got, sum) {
		t.Fatal("downloaded artifact content does not match uploaded content (checksum mismatch)")
	}

	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	if grown := int64(after.HeapAlloc) - int64(before.HeapAlloc); grown > growthCeiling {
		t.Fatalf("heap grew by %d bytes (ceiling %d) while streaming a %d-byte artifact — full-buffer regression suspected", grown, growthCeiling, size)
	}
}

// writeDeterministicFile writes size bytes of non-repeating filler (so a
// sparse-file trick can't hide a bug) to path and returns its sha256.
func writeDeterministicFile(t *testing.T, path string, size int) []byte {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	h := sha256.New()
	w := io.MultiWriter(f, h)
	buf := make([]byte, 1<<20)
	for written := 0; written < size; {
		n := len(buf)
		if remaining := size - written; remaining < n {
			n = remaining
		}
		for i := 0; i < n; i++ {
			buf[i] = byte((written + i) * 2654435761 >> 8)
		}
		if _, err := w.Write(buf[:n]); err != nil {
			t.Fatal(err)
		}
		written += n
	}
	return h.Sum(nil)
}

// TestConcurrentOversizedPutsAll413 fires 32 parallel oversized PUTs and
// asserts MaxBytesReader's 413 cap holds for every one under concurrency,
// and that no partial object is ever committed to storage — fs.Put only
// os.Rename's after a fully successful io.Copy (internal/storage/filesystem/fs.go),
// so an aborted upload must leave zero artifacts behind, not a truncated one.
func TestConcurrentOversizedPutsAll413(t *testing.T) {
	const cap_ = 1 << 20 // 1 MiB
	const oversized = 5 << 20
	const workers = 32
	srv, dir := newLoadTestServer(t, cap_)

	var wg sync.WaitGroup
	codes := make(chan int, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			payload := bytes.Repeat([]byte{'x'}, oversized)
			req, _ := http.NewRequest(http.MethodPut,
				fmt.Sprintf("%s/v8/artifacts/oversized%d", srv.URL, i), bytes.NewReader(payload))
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Error(err)
				return
			}
			resp.Body.Close()
			codes <- resp.StatusCode
		}(i)
	}
	wg.Wait()
	close(codes)
	for code := range codes {
		if code != http.StatusRequestEntityTooLarge {
			t.Errorf("oversized PUT status = %d, want 413", code)
		}
	}

	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && len(info.Name()) > 0 && info.Name()[0] == '.' {
			t.Errorf("leftover temp file after an aborted oversized PUT: %s", path)
		}
		return nil
	})
}
```

- [ ] **Step 2: Run it**

```bash
go test -tags loadtest -race ./internal/turbo/... -run 'TestConcurrent|TestFlatMemory' -v
```
Expected: all PASS. `-race` is not optional here — concurrency is the entire point of this suite. Note timing (this is the baseline the Verification section's re-run compares against).

- [ ] **Step 3: Confirm the default suite is unaffected**

```bash
go build ./... && go test ./...
```
Expected: PASS, and fast — this file did not compile into the default build (no `loadtest` tag).

- [ ] **Step 4: Commit**
```bash
git add services/api/internal/turbo/loadtest_test.go
git commit -m "test(api): build-tag-gated concurrency + heavy-artifact load test suite"
```

---

## Task 8: OpenTelemetry tracing seam

**Files:**
- Modify: `services/api/go.mod`
- New: `services/api/internal/obs/tracing.go`, `services/api/internal/obs/tracing_test.go`
- New: `services/api/internal/storage/tracing.go`, `services/api/internal/storage/tracing_test.go`
- Modify: `services/api/internal/db/repo.go`, `services/api/internal/db/repo_test.go`
- Modify: `services/api/internal/server/router.go`, `services/api/cmd/server/main.go`

**Interfaces:**
- Produces: `obs.InitTracer(ctx) (shutdown func(context.Context) error, err error)`, `obs.StartStorageSpan(ctx, name) (context.Context, trace.Span)`, `obs.StartDBSpan(ctx, name) (context.Context, trace.Span)`, `obs.EndSpan(span, err)`.
- Produces: `storage.WithTracing(inner Storage) Storage` — a decorator, applied once in `main.go` regardless of backend.

**Decision:** OTLP over HTTP (`otlptracehttp`), not gRPC. One fewer dependency, and it speaks to any OTLP-HTTP collector (Tempo, Jaeger, an `otel-collector`) without a long-lived `grpc.ClientConn` for a self-hosted single binary to manage.

**Decision:** `InitTracer` only builds a real `TracerProvider` when `OTEL_EXPORTER_OTLP_ENDPOINT` is set; when unset, it does nothing and returns a no-op shutdown. otel's own package default is already a no-op `TracerProvider` until something calls `otel.SetTracerProvider` — so "off" costs nothing, and there is no bespoke no-op wrapper to maintain.

**Decision:** instrument storage via a single `storage.WithTracing` decorator (applied uniformly to whichever backend `main.go` constructs) rather than editing `fs.go` and `s3.go` separately — DRY, and it can never drift out of sync between the two backends. DB spans are added directly in `repo.go`'s four methods since `*db.Repo` is already the DB access boundary.

- [ ] **Step 1: Add dependencies**

```bash
cd services/api
go get go.opentelemetry.io/otel
go get go.opentelemetry.io/otel/sdk
go get go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp
go get go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp
```

- [ ] **Step 2: Write the failing tracer test**

`internal/obs/tracing_test.go`:
```go
package obs

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestInitTracerNoopWhenEndpointUnset(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Cleanup(func() { otel.SetTracerProvider(noop.NewTracerProvider()) })

	shutdown, err := InitTracer(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("no-op shutdown should never error, got %v", err)
	}

	_, span := StartStorageSpan(context.Background(), "test-span")
	defer span.End()
	if span.SpanContext().IsValid() {
		t.Fatal("expected an invalid (no-op) span context when tracing is off")
	}
}

func TestInitTracerRegistersRealProviderWhenEndpointSet(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:4318")
	t.Cleanup(func() { otel.SetTracerProvider(noop.NewTracerProvider()) })

	shutdown, err := InitTracer(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	_, span := StartStorageSpan(context.Background(), "test-span")
	if !span.SpanContext().IsValid() {
		t.Fatal("expected a valid span context once a real TracerProvider is registered — export failures to an unreachable collector happen async and must not affect this")
	}
	span.End()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = shutdown(ctx) // best-effort; nothing is actually listening on :4318 in this test
}
```

- [ ] **Step 3: Run to verify it fails**

Run: `go test ./internal/obs/... -run TestInitTracer -v`
Expected: FAIL to compile — `InitTracer`/`StartStorageSpan` undefined.

- [ ] **Step 4: Implement `obs/tracing.go`**

```go
package obs

import (
	"context"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

var (
	storageTracer = otel.Tracer("turbo-cache-forge/storage")
	dbTracer      = otel.Tracer("turbo-cache-forge/db")
)

// InitTracer wires a real OTLP/HTTP tracer provider when
// OTEL_EXPORTER_OTLP_ENDPOINT is set; otherwise it does nothing, leaving
// otel's built-in no-op global TracerProvider in place so every
// StartStorageSpan/StartDBSpan call elsewhere is a true zero-cost no-op by
// default. This env var is the ONLY tracing on/off switch.
func InitTracer(ctx context.Context) (shutdown func(context.Context) error, err error) {
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
		return func(context.Context) error { return nil }, nil
	}

	exp, err := otlptracehttp.New(ctx) // reads OTEL_EXPORTER_OTLP_* env vars itself
	if err != nil {
		return nil, err
	}
	res, err := resource.Merge(resource.Default(),
		resource.NewSchemaless(attribute.String("service.name", "turbo-cache-forge")))
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	return tp.Shutdown, nil
}

func StartStorageSpan(ctx context.Context, name string) (context.Context, trace.Span) {
	return storageTracer.Start(ctx, name)
}

func StartDBSpan(ctx context.Context, name string) (context.Context, trace.Span) {
	return dbTracer.Start(ctx, name)
}

// EndSpan records err (if any) and ends span. Called via defer at every span
// call site so storage/db instrumentation doesn't repeat this dance.
func EndSpan(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}
```

Run: `go test ./internal/obs/... -run TestInitTracer -v` → PASS.

- [ ] **Step 5: Write the failing storage-tracing test**

`internal/storage/tracing_test.go`:
```go
package storage

import (
	"bytes"
	"context"
	"io"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace/noop"
)

type fakeStore struct{}

func (f *fakeStore) Put(context.Context, string, io.Reader) error { return nil }
func (f *fakeStore) Get(context.Context, string) (io.ReadCloser, *ObjectInfo, error) {
	return io.NopCloser(bytes.NewReader(nil)), &ObjectInfo{}, nil
}
func (f *fakeStore) Head(context.Context, string) (*ObjectInfo, error) { return &ObjectInfo{}, nil }
func (f *fakeStore) Delete(context.Context, string) error              { return nil }

func TestWithTracingEmitsSpans(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(noop.NewTracerProvider()) })

	traced := WithTracing(&fakeStore{})
	_ = traced.Put(context.Background(), "team-a/h1", bytes.NewReader([]byte("x")))
	_, _, _ = traced.Get(context.Background(), "team-a/h1")
	_, _ = traced.Head(context.Background(), "team-a/h1")
	_ = traced.Delete(context.Background(), "team-a/h1")

	spans := exp.GetSpans()
	if len(spans) != 4 {
		t.Fatalf("got %d spans, want 4 (Put/Get/Head/Delete)", len(spans))
	}
	seen := map[string]bool{}
	for _, s := range spans {
		seen[s.Name] = true
	}
	for _, want := range []string{"storage.Put", "storage.Get", "storage.Head", "storage.Delete"} {
		if !seen[want] {
			t.Errorf("missing span %q", want)
		}
	}
}
```

- [ ] **Step 6: Run to verify it fails, then implement `storage/tracing.go`**

```go
package storage

import (
	"context"
	"io"

	"go.opentelemetry.io/otel/attribute"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/obs"
)

type tracingStorage struct{ inner Storage }

// WithTracing wraps any Storage backend so every call emits a span — a
// no-op span, and effectively free, unless obs.InitTracer has registered a
// real TracerProvider. Apply this once in main.go regardless of which
// backend (fs or s3) is configured; both get tracing with no
// backend-specific instrumentation to keep in sync.
func WithTracing(inner Storage) Storage { return &tracingStorage{inner: inner} }

func (t *tracingStorage) Put(ctx context.Context, key string, r io.Reader) error {
	ctx, span := obs.StartStorageSpan(ctx, "storage.Put")
	span.SetAttributes(attribute.String("cache.key", key))
	err := t.inner.Put(ctx, key, r)
	obs.EndSpan(span, err)
	return err
}

func (t *tracingStorage) Get(ctx context.Context, key string) (io.ReadCloser, *ObjectInfo, error) {
	ctx, span := obs.StartStorageSpan(ctx, "storage.Get")
	span.SetAttributes(attribute.String("cache.key", key))
	rc, info, err := t.inner.Get(ctx, key)
	obs.EndSpan(span, err)
	return rc, info, err
}

func (t *tracingStorage) Head(ctx context.Context, key string) (*ObjectInfo, error) {
	ctx, span := obs.StartStorageSpan(ctx, "storage.Head")
	span.SetAttributes(attribute.String("cache.key", key))
	info, err := t.inner.Head(ctx, key)
	obs.EndSpan(span, err)
	return info, err
}

func (t *tracingStorage) Delete(ctx context.Context, key string) error {
	ctx, span := obs.StartStorageSpan(ctx, "storage.Delete")
	span.SetAttributes(attribute.String("cache.key", key))
	err := t.inner.Delete(ctx, key)
	obs.EndSpan(span, err)
	return err
}
```

Run: `go test ./internal/storage/... -v` → PASS.

- [ ] **Step 7: Instrument `db/repo.go`**

Convert the 4 exported methods to named returns so a single `defer` can record the span outcome:
```go
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
```
Add the `obs` import to `repo.go`.

Extend `internal/db/repo_test.go` (same `TEST_DATABASE_URL`-gated skip pattern as the rest of this file):
```go
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
```
(Add the `go.opentelemetry.io/otel`, `.../sdk/trace`, `.../sdk/trace/tracetest`, `.../trace/noop` imports to `repo_test.go`.)

- [ ] **Step 8: Wrap the router with `otelhttp`**

In `internal/server/router.go`, wrap the final return:
```go
import "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

func New(d Deps) http.Handler {
	r := chi.NewRouter()
	// ... existing wiring unchanged ...
	return otelhttp.NewHandler(r, "turbo-cache-forge")
}
```
This is safe when no `TracerProvider` is registered (the default) — `otelhttp` uses the global no-op provider and adds effectively zero overhead. Re-run `go test ./internal/server/... -v` to confirm existing router tests still pass unwrapped-vs-wrapped.

- [ ] **Step 9: Wire `main.go`**

```go
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	shutdownTracer, err := obs.InitTracer(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownTracer(shutCtx)
	}()

	// ... existing repo.Open / storage backend selection unchanged ...

	store = storage.WithTracing(store) // applies to whichever backend was selected above
```
Add `"time"` and the `obs` import to `main.go`.

- [ ] **Step 10: Run + commit**

Run: `go build ./... && go test ./... -v` → PASS.
```bash
git add services/api/go.mod services/api/go.sum services/api/internal/obs services/api/internal/storage/tracing.go services/api/internal/storage/tracing_test.go services/api/internal/db services/api/internal/server services/api/cmd/server
git commit -m "feat(api): OTel tracing seam (no-op unless OTEL_EXPORTER_OTLP_ENDPOINT set)"
```

---

## Task 9: Sentry — panics + storage/DB error capture

**Files:**
- Modify: `services/api/go.mod`
- New: `services/api/internal/obs/sentry.go`, `services/api/internal/obs/sentry_test.go`
- Modify: `services/api/internal/server/router.go`, `services/api/internal/turbo/handlers.go`, `services/api/cmd/server/main.go`

**Interfaces:**
- Produces: `obs.InitSentry() (flush func(), err error)`, `obs.CaptureError(err error)`.

**Decision:** `SENTRY_DSN` is an operational secret supplied at deploy time (same pattern as `DATABASE_URL`) — provisioning a real Sentry account/project is a one-time manual step outside this plan's code scope, resolving ROADMAP's open question. `SENTRY_DSN` unset means Sentry is fully inert; nothing here blocks on having a real account yet.

**Decision:** use the official `github.com/getsentry/sentry-go/http` (`sentryhttp`) middleware for panic capture instead of hand-rolling a second recover(). Mount it **after** (i.e., inside/closer to the handler than) chi's `middleware.Recoverer` with `Repanic: true`: `sentryhttp` recovers first (as the panic unwinds), reports to Sentry, then re-panics outward so `Recoverer` still produces the actual 500 response — Sentry gets the event without changing what the client sees.

- [ ] **Step 1: Add the dependency**

```bash
cd services/api
go get github.com/getsentry/sentry-go
```
(`github.com/getsentry/sentry-go/http` is a subpackage of the same module — no separate `go get`.)

- [ ] **Step 2: Write the failing test**

`internal/obs/sentry_test.go`:
```go
package obs

import (
	"errors"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
)

// fakeTransport captures events instead of sending them over the network.
// If your installed sentry-go version's Transport interface also requires
// FlushWithContext(ctx) bool, add that method here too (added in some
// sentry-go releases; Flush/Configure/SendEvent below is the long-stable
// baseline) — `go doc github.com/getsentry/sentry-go.Transport` confirms
// the exact set for whatever version `go get` resolved.
type fakeTransport struct{ events []*sentry.Event }

func (f *fakeTransport) Configure(sentry.ClientOptions)  {}
func (f *fakeTransport) SendEvent(e *sentry.Event)       { f.events = append(f.events, e) }
func (f *fakeTransport) Flush(time.Duration) bool        { return true }

func TestCaptureErrorNoopWithoutInit(t *testing.T) {
	// No sentry.Init anywhere in this test — CaptureError must not panic
	// and must not require a DSN to be safe to call.
	CaptureError(errors.New("boom"))
}

func TestCaptureErrorSendsEventWhenInitialized(t *testing.T) {
	ft := &fakeTransport{}
	if err := sentry.Init(sentry.ClientOptions{
		Dsn:       "https://public@example.com/1",
		Transport: ft,
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sentry.CurrentHub().BindClient(nil) })

	CaptureError(errors.New("db unavailable"))
	sentry.Flush(time.Second)

	if len(ft.events) != 1 {
		t.Fatalf("got %d events, want 1", len(ft.events))
	}
}
```

- [ ] **Step 3: Run to verify it fails**

Run: `go test ./internal/obs/... -run TestCaptureError -v`
Expected: FAIL to compile — `CaptureError` undefined.

- [ ] **Step 4: Implement `obs/sentry.go`**

```go
package obs

import (
	"os"
	"time"

	"github.com/getsentry/sentry-go"
)

// InitSentry configures the Sentry SDK when SENTRY_DSN is set; otherwise it
// does nothing. sentry-go's package-level CaptureException is a safe no-op
// when Init was never called — there is no separate "enabled" flag to check
// — so CaptureError below is unconditionally safe to call from every error
// path, gated or not.
func InitSentry() (flush func(), err error) {
	dsn := os.Getenv("SENTRY_DSN")
	if dsn == "" {
		return func() {}, nil
	}
	if err := sentry.Init(sentry.ClientOptions{Dsn: dsn}); err != nil {
		return nil, err
	}
	return func() { sentry.Flush(2 * time.Second) }, nil
}

// CaptureError reports err to Sentry (a no-op if InitSentry was never called
// with a DSN). Call it only where a storage/DB error becomes a 5xx response
// — that's the "storage/DB errors" surface Phase 2 scopes this to. Client
// mistakes (bad hash, oversized upload, bad auth) are 4xx and must never be
// reported here.
func CaptureError(err error) {
	if err == nil {
		return
	}
	sentry.CaptureException(err)
}
```

Run: `go test ./internal/obs/... -v` → PASS.

- [ ] **Step 5: Mount `sentryhttp` in the router**

In `internal/server/router.go`:
```go
import sentryhttp "github.com/getsentry/sentry-go/http"

func New(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer) // outer: always produces the actual 500 response
	// inner: catches the panic first as it unwinds, reports to Sentry, then
	// repanics outward (Repanic: true) for Recoverer above to still handle.
	r.Use(sentryhttp.New(sentryhttp.Options{Repanic: true}).Handle)
	// ... rest unchanged ...
```

- [ ] **Step 6: Capture storage/DB errors at the 5xx branches**

In `internal/turbo/handlers.go`'s `put()`:
```go
	body := http.MaxBytesReader(w, r.Body, h.maxBytes)
	if err := h.store.Put(r.Context(), key, body); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			http.Error(w, "artifact too large", http.StatusRequestEntityTooLarge)
			return
		}
		obs.CaptureError(err)
		http.Error(w, "upload failed", http.StatusInternalServerError)
		return
	}
	info, err := h.store.Head(r.Context(), key)
	if err != nil {
		obs.CaptureError(err)
		http.Error(w, "upload verify failed", http.StatusInternalServerError)
		return
	}
	tag := r.Header.Get("x-artifact-tag")
	if err := h.repo.UpsertArtifact(r.Context(), org.ID, hash, info.Size, tag); err != nil {
		if delErr := h.store.Delete(r.Context(), key); delErr != nil {
			obs.CaptureError(delErr)
			log.Printf("turbo: put %s: compensating delete after metadata failure also failed: %v", key, delErr)
		}
		obs.CaptureError(err)
		http.Error(w, "metadata write failed", http.StatusInternalServerError)
		return
	}
```
And in `get()`:
```go
	rc, info, err := h.store.Get(r.Context(), key)
	if errors.Is(err, storage.ErrNotFound) {
		h.metrics.CacheMiss.Inc()
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if err != nil {
		obs.CaptureError(err)
		http.Error(w, "download failed", http.StatusInternalServerError)
		return
	}
```
(The `batchExists` handler already calls `obs.CaptureError` from Task 6, forward-declared for exactly this task.)

- [ ] **Step 7: Wire `main.go`**

```go
	flushSentry, err := obs.InitSentry()
	if err != nil {
		log.Fatal(err)
	}
	defer flushSentry()
```
Place this alongside the `InitTracer` wiring from Task 8, both before `db.Open`.

- [ ] **Step 8: Run + commit**

Run: `go build ./... && go test ./... -v` → PASS.
```bash
git add services/api/go.mod services/api/go.sum services/api/internal/obs/sentry.go services/api/internal/obs/sentry_test.go services/api/internal/server/router.go services/api/internal/turbo/handlers.go services/api/cmd/server/main.go
git commit -m "feat(api): Sentry panic + storage/DB error capture (no-op unless SENTRY_DSN set)"
```

---

## Task 10: Final wiring check, docs, backlog resolution

**Files:**
- Modify: `.env.example`, `README.md`

**Interfaces:** none — documentation and a final full-suite verification.

- [ ] **Step 1: Document the two new opt-in env vars**

Append to `.env.example`:
```env
# --- Observability (optional; both default to fully off) ---
OTEL_EXPORTER_OTLP_ENDPOINT=       # e.g. http://localhost:4318 — unset = tracing off (no-op)
SENTRY_DSN=                        # unset = Sentry off (no-op)
```

- [ ] **Step 2: Update the README**

Add a short section after the existing health/observability paragraph:
```markdown
### Optional: tracing + error reporting

Both are fully inert until you set their env var:

- `OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318` — exports spans (storage + DB calls) via OTLP/HTTP to any collector (Tempo, Jaeger, `otel-collector`). Metrics stay Prometheus-only; this is tracing only.
- `SENTRY_DSN=https://...` — reports panics and storage/DB errors that produce a 5xx. 4xx client errors are never reported.

### Concurrency / heavy-artifact load test

Excluded from the default `go test ./...` (build-tag gated) so CI stays fast:
```bash
go test -tags loadtest -race ./internal/turbo/... -v
```
```

- [ ] **Step 3: Full-suite verification**

```bash
go build ./... && go test ./...                                   # default suite, fast, green
go test -tags loadtest -race ./internal/turbo/... -v               # load test suite, green
go vet ./...
```

- [ ] **Step 4: Commit**
```bash
git add .env.example README.md
git commit -m "docs: document OTel/Sentry env vars and the load test invocation"
```

---

## Phase-1 backlog resolution

Per `docs/ROADMAP.md`'s Phase-1 follow-up backlog, each item's Phase 2 disposition:

| Backlog item | Disposition |
|---|---|
| Assert metric counter values (`testutil.ToFloat64`) | **Done** — Task 3 |
| S3 `New`: skip static creds when both keys empty | **Done** — Task 2a |
| Pin `goose` in the Docker goose stage | **Done** — Task 1 |
| GET `io.ReaderFrom`/sendfile fast path | **Done** — Task 4 |
| PUT store→metadata non-atomicity (orphan) | **Done** — Task 5 (eager compensating delete; see its Decision note) |
| `token.go`: restore ponytail rationale comment | **Done** — Task 2c |
| `s3.go`: dedupe `ContentLength` extraction | **Done** — Task 2b |
| `.env.example`: separate compose-vs-bare-metal vars | **Already done** — the current file already separates the compose-only `POSTGRES_*` block (with an explicit note on Docker Compose's `.env`-location gotcha) from the bare-metal `cache-api` vars; no further action needed. |
| Indexes on `cache_artifacts.project_id` / `api_keys.project_id` | **Re-deferred, unchanged reasoning** — no Phase 2 query filters by `project_id` yet; add when Phase 3's `/api/v1` introduces one. |
| _Won't-fix: `fs.path`'s `strings.Contains(key,"..")` broader than segment-aware_ | **Re-deferred, unchanged reasoning** — harmless behind `validHash`, which is the actual boundary check; not touched in Phase 2. |
| _Won't-fix: handler `org` nil-guard_ | **Re-deferred, unchanged reasoning** — theoretical; `RequireToken` always populates it; not touched in Phase 2. |

---

## Self-review notes (coverage against Phase 2 scope)

- **Concurrency proven, not asserted:** Task 7's suite runs real goroutines over a real `httptest.Server` + real filesystem backend with `-race`, covering distinct-hash isolation, same-hash idempotency under interleaved reads, flat memory on a 200 MiB artifact, and the 413 cap holding under concurrent oversized uploads.
- **One metrics pipeline preserved:** Tasks 8–9 add tracing (OTel) and error reporting (Sentry) — neither registers a `MeterProvider`, a second `/metrics` endpoint, or any Prometheus-competing counter. `obs/metrics.go`'s `Metrics` struct is untouched except for the `ReadFrom` passthrough (Task 4), which changes throughput characteristics, not what's measured.
- **DB off the hot path preserved:** Task 8's DB spans wrap the same four methods that already existed; `TouchArtifact` is still the only DB call on the GET path and is still a fire-and-forget goroutine on a detached context — spans don't change that.
- **Tenant isolation preserved:** the batch endpoint (Task 6) reuses `org.ID` from `auth.OrgFromContext` and the existing `validHash` boundary check per hash — no new key-building path was introduced.
- **Both new dependencies are genuinely opt-in:** `OTEL_EXPORTER_OTLP_ENDPOINT` and `SENTRY_DSN` unset means `go build ./... && go test ./...` and a default `docker compose up` behave identically to Phase 1 — verified in Task 8/9's tests (`TestInitTracerNoopWhenEndpointUnset`, `TestCaptureErrorNoopWithoutInit`).
- **Toolchain:** confirmed at 1.25 (Task 1), not re-bumped — `docs/ROADMAP.md`'s cross-phase invariants already reflect this.

## Deferred to later phases (do NOT build here)

- Token/project/org management API (`/api/v1`), OIDC/JWT, cleanup cron, `usage_daily` rollup, indexes on `project_id` → **Phase 3**.
- Dashboard → **Phase 4**. CLI → **Phase 5**.
- Any batched (`size`-returning) variant of the batch existence endpoint → build only if a real client asks for size.

---

## Definition of Done

(Copied verbatim from `docs/ROADMAP.md`'s Phase 2 entry — this is the acceptance bar, not a paraphrase.)

> Parallel load test passes with flat memory; batch endpoint returns correct existence map and a real Turbo client uses it; a trace exports only when the OTLP env var is set; the Phase-1 follow-up backlog is either done or explicitly re-deferred with reasoning.

---

## Verification (prove it end-to-end)

1. **Default suite stays green and fast:**
   ```bash
   go build ./... && go test ./...
   ```
   (S3/DB suites still skip without their env vars, same as Phase 1; the load test file doesn't even compile in here.)

2. **Load test, with race detection:**
   ```bash
   go test -tags loadtest -race ./internal/turbo/... -v
   ```
   All of `TestConcurrentDistinctHashes`, `TestConcurrentSameHashIdempotent`, `TestFlatMemoryOnLargeArtifact`, `TestConcurrentOversizedPutsAll413` pass with no `-race` reports.

3. **Batch endpoint, real client caveat:**
   ```bash
   docker compose -f infra/docker/docker-compose.yml up -d --build
   # seed org/token as in Phase 1's README, then PUT one artifact (hash abc123), leave another absent
   curl -s -X POST -H "Authorization: Bearer turbo_dev" \
     -d '{"hashes":["abc123","doesnotexist"]}' \
     http://localhost:8080/v8/artifacts
   # → {"hashes":{"abc123":{"exists":true},"doesnotexist":{"exists":false}}}
   ```
   **Caveat to flag, not silently paper over:** stock Turborepo CLI v8 has no documented call to a batch-exists route — this endpoint is a custom addition ahead of any CLI consumer. The `curl` above stands in for "a real Turbo client" per the ROADMAP's Definition of Done; a genuine CLI integration is future work (candidate for Phase 5's `turbo-cache` CLI or a Turbo-side plugin), not something this backend plan can complete unilaterally.

4. **OTLP smoke — negative (off):** leave `OTEL_EXPORTER_OTLP_ENDPOINT` unset, run `cache-api`, do a PUT/GET, confirm no outbound connection attempts to any collector port.

5. **OTLP smoke — positive (on):** run a minimal collector locally:
   ```yaml
   # otel-collector-config.yaml
   receivers:
     otlp:
       protocols:
         http:
           endpoint: 0.0.0.0:4318
   exporters:
     debug:
       verbosity: detailed
   service:
     pipelines:
       traces:
         receivers: [otlp]
         exporters: [debug]
   ```
   ```bash
   docker run --rm -p 4318:4318 -v $PWD/otel-collector-config.yaml:/etc/otelcol/config.yaml \
     otel/opentelemetry-collector:0.110.0
   OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318 <run cache-api>
   # PUT + GET an artifact
   ```
   Confirm the collector's `debug` exporter logs spans named `storage.Put`, `storage.Get`, `db.UpsertArtifact`, etc.

6. **Sentry smoke:** set `SENTRY_DSN` to a real (or sentry.io test) project DSN, force a storage error (e.g. point `STORAGE_PATH` at a read-only directory) and confirm the event appears in the project's Issues stream, and that a plain 400 (hostile hash) does **not** produce an event.

7. **No regression on Phase 1's acceptance checklist:** re-run `docker compose up` MISS→HIT, `/ready` 503 with Postgres stopped, cross-tenant 404/401 — all still hold.
