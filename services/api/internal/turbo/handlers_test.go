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
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/auth"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/db"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/obs"
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

// spyStore wraps memStore and records whether storage was ever touched, so
// tests can prove a rejected request never reached the backend.
type spyStore struct {
	*memStore
	headCalled, putCalled, getCalled bool
}

func newSpyStore() *spyStore { return &spyStore{memStore: newMemStore()} }
func (s *spyStore) Head(ctx context.Context, key string) (*storage.ObjectInfo, error) {
	s.headCalled = true
	return s.memStore.Head(ctx, key)
}
func (s *spyStore) Put(ctx context.Context, key string, r io.Reader) error {
	s.putCalled = true
	return s.memStore.Put(ctx, key, r)
}
func (s *spyStore) Get(ctx context.Context, key string) (io.ReadCloser, *storage.ObjectInfo, error) {
	s.getCalled = true
	return s.memStore.Get(ctx, key)
}

// requestWithHash builds a request carrying an arbitrary (possibly hostile)
// {hash} route param directly via chi's route context, bypassing any path
// cleaning/segmenting the HTTP stack or router might otherwise apply. This
// reliably exercises the handler with hostile values like "../team-b/secret"
// or "a/b" that would not survive being encoded into a single URL segment.
func requestWithHash(method, hash string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("hash", hash)
	req := httptest.NewRequest(method, "/v8/artifacts/x", nil)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = auth.WithOrg(ctx, &db.Org{ID: 1, Slug: "team-a"})
	return req.WithContext(ctx)
}

func TestHostileHashRejectedBeforeTouchingStore(t *testing.T) {
	hostile := []string{
		"../team-b/secret",
		"..%2Fx",
		"a/b",
		"a..b",
		"..",
		"",
	}

	for _, hash := range hostile {
		t.Run("HEAD/"+hash, func(t *testing.T) {
			store := newSpyStore()
			m := obs.NewMetrics()
			h := NewHandler(store, &memRepo{}, 1<<20, m)
			rec := httptest.NewRecorder()
			h.head(rec, requestWithHash(http.MethodHead, hash))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("HEAD hash=%q = %d, want 400", hash, rec.Code)
			}
			if store.headCalled {
				t.Fatalf("HEAD hash=%q reached store.Head, want rejected before storage", hash)
			}
		})

		t.Run("PUT/"+hash, func(t *testing.T) {
			store := newSpyStore()
			m := obs.NewMetrics()
			h := NewHandler(store, &memRepo{}, 1<<20, m)
			rec := httptest.NewRecorder()
			req := requestWithHash(http.MethodPut, hash)
			req.Body = io.NopCloser(bytes.NewReader([]byte("payload")))
			h.put(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("PUT hash=%q = %d, want 400", hash, rec.Code)
			}
			if store.putCalled {
				t.Fatalf("PUT hash=%q reached store.Put, want rejected before storage", hash)
			}
		})

		t.Run("GET/"+hash, func(t *testing.T) {
			store := newSpyStore()
			m := obs.NewMetrics()
			h := NewHandler(store, &memRepo{}, 1<<20, m)
			rec := httptest.NewRecorder()
			h.get(rec, requestWithHash(http.MethodGet, hash))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("GET hash=%q = %d, want 400", hash, rec.Code)
			}
			if store.getCalled {
				t.Fatalf("GET hash=%q reached store.Get, want rejected before storage", hash)
			}
		})
	}
}

func TestValidHashStillWorksThroughHandlers(t *testing.T) {
	store := newSpyStore()
	m := obs.NewMetrics()
	h := NewHandler(store, &memRepo{}, 1<<20, m)

	rec := httptest.NewRecorder()
	req := requestWithHash(http.MethodPut, "a1b2c3d4e5f6")
	req.Body = io.NopCloser(bytes.NewReader([]byte("payload")))
	h.put(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("PUT valid hash = %d, want 202", rec.Code)
	}
	if !store.putCalled {
		t.Fatalf("PUT valid hash never reached store.Put")
	}

	rec = httptest.NewRecorder()
	h.head(rec, requestWithHash(http.MethodHead, "a1b2c3d4e5f6"))
	if rec.Code != http.StatusOK {
		t.Fatalf("HEAD valid hash = %d, want 200", rec.Code)
	}
}

// helper: build a router with an org already injected into context
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

func TestStatus(t *testing.T) {
	r, _ := testRouter(newMemStore(), &memRepo{})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v8/artifacts/status", nil))
	if rec.Code != 200 || !bytes.Contains(rec.Body.Bytes(), []byte(`"enabled"`)) {
		t.Fatalf("status = %d %s", rec.Code, rec.Body)
	}
}

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
