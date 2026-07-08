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
