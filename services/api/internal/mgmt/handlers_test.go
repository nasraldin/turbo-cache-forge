package mgmt

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/klauspost/compress/zstd"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/auth"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/db"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/storage"
)

type fakeRepo struct {
	created       db.APIKey
	revokedOK     bool
	tokens        []db.APIKey
	projects      []db.Project
	stats         db.Stats
	statsSeries   []db.StatsPoint
	seriesDaysGot int
	artifacts     []db.Artifact

	getArtifact    db.Artifact
	getArtifactErr error
	hashes         []string
	deletedHash    string
}

func (f *fakeRepo) CreateToken(_ context.Context, _ int64, name, _ string) (int64, error) {
	f.created = db.APIKey{ID: 99, Name: name}
	return 99, nil
}
func (f *fakeRepo) ListTokens(context.Context, int64) ([]db.APIKey, error) { return f.tokens, nil }
func (f *fakeRepo) RevokeToken(context.Context, int64, int64) (bool, error) { return f.revokedOK, nil }
func (f *fakeRepo) CreateProject(_ context.Context, _ int64, slug, name string) (db.Project, error) {
	return db.Project{ID: 1, Slug: slug, Name: name}, nil
}
func (f *fakeRepo) ListProjects(context.Context, int64) ([]db.Project, error)      { return f.projects, nil }
func (f *fakeRepo) Stats(context.Context, int64) (db.Stats, error)                 { return f.stats, nil }
func (f *fakeRepo) StatsSeries(_ context.Context, _ int64, days int) ([]db.StatsPoint, error) {
	f.seriesDaysGot = days
	return f.statsSeries, nil
}
func (f *fakeRepo) ListArtifacts(context.Context, int64, int, int) ([]db.Artifact, error) {
	return f.artifacts, nil
}
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

// router injects a fixed org into context, mimicking oidcauth.Middleware.
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

func TestCreateTokenReturnsPlaintextOnce(t *testing.T) {
	repo := &fakeRepo{}
	r := testRouter(repo)
	rec := httptest.NewRecorder()
	body := bytes.NewBufferString(`{"name":"ci"}`)
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/tokens", body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /tokens = %d, want 201", rec.Code)
	}
	var resp struct {
		ID    int64  `json:"id"`
		Name  string `json:"name"`
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Token == "" || resp.ID != 99 || resp.Name != "ci" {
		t.Fatalf("resp = %+v", resp)
	}
	// stored value must be the HASH of the returned plaintext, never the plaintext
	if auth.HashToken(resp.Token) == resp.Token {
		t.Fatal("token appears unhashed")
	}
}

func TestRevokeToken(t *testing.T) {
	r := testRouter(&fakeRepo{revokedOK: true})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/v1/tokens/99", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /tokens/99 = %d, want 204", rec.Code)
	}

	rec = httptest.NewRecorder()
	r2 := testRouter(&fakeRepo{revokedOK: false}) // unknown/cross-org id
	r2.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/v1/tokens/1234", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("DELETE unknown = %d, want 404", rec.Code)
	}
}

func TestListTokensHasNoSecret(t *testing.T) {
	repo := &fakeRepo{tokens: []db.APIKey{{ID: 1, Name: "ci"}}}
	r := testRouter(repo)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/tokens", nil))
	if rec.Code != http.StatusOK || bytes.Contains(rec.Body.Bytes(), []byte("token_hash")) {
		t.Fatalf("list leaked secret or bad status: %d %s", rec.Code, rec.Body)
	}
}

func TestCreateProjectValidatesSlug(t *testing.T) {
	r := testRouter(&fakeRepo{})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/projects",
		bytes.NewBufferString(`{"slug":"Bad Slug","name":"x"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad slug = %d, want 400", rec.Code)
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/projects",
		bytes.NewBufferString(`{"slug":"web","name":"Web"}`)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("good slug = %d, want 201", rec.Code)
	}
}

func TestListProjectsOrgScoped(t *testing.T) {
	repo := &fakeRepo{projects: []db.Project{{ID: 1, Slug: "web", Name: "Web"}}}
	r := testRouter(repo)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil))
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"web"`)) {
		t.Fatalf("list projects = %d %s", rec.Code, rec.Body)
	}
}

func TestStatsAndArtifacts(t *testing.T) {
	repo := &fakeRepo{
		stats:     db.Stats{StorageBytes: 100, Hits: 4, Misses: 1, Requests: 5},
		artifacts: []db.Artifact{{Hash: "h1", SizeBytes: 10}},
	}
	r := testRouter(repo)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil))
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"storage_bytes":100`)) {
		t.Fatalf("stats = %d %s", rec.Code, rec.Body)
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/artifacts?limit=10", nil))
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"hash":"h1"`)) {
		t.Fatalf("artifacts = %d %s", rec.Code, rec.Body)
	}
}

func TestStatsTimeseries(t *testing.T) {
	repo := &fakeRepo{statsSeries: []db.StatsPoint{
		{Day: "2026-07-01", Hits: 40, Misses: 5, BytesUp: 10, BytesDown: 20},
		{Day: "2026-07-02", Hits: 50, Misses: 5, BytesUp: 10, BytesDown: 20},
	}}
	r := testRouter(repo)

	// explicit days is passed through to the repo untouched
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/stats/timeseries?days=7", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /stats/timeseries?days=7 = %d", rec.Code)
	}
	if repo.seriesDaysGot != 7 {
		t.Fatalf("days passed to repo = %d, want 7", repo.seriesDaysGot)
	}
	var pts []db.StatsPoint
	if err := json.Unmarshal(rec.Body.Bytes(), &pts); err != nil {
		t.Fatalf("bad json: %v (%s)", err, rec.Body)
	}
	if len(pts) != 2 || pts[0].Day != "2026-07-01" {
		t.Fatalf("pts = %+v", pts)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"bytes_down":20`)) {
		t.Fatalf("expected snake_case keys, got %s", rec.Body)
	}

	// missing days defaults to 30
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/stats/timeseries", nil))
	if rec.Code != http.StatusOK || repo.seriesDaysGot != 30 {
		t.Fatalf("default days = %d (code %d), want 30", repo.seriesDaysGot, rec.Code)
	}

	// oversized days clamps to 365
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/stats/timeseries?days=9999", nil))
	if rec.Code != http.StatusOK || repo.seriesDaysGot != 365 {
		t.Fatalf("clamp days = %d (code %d), want 365", repo.seriesDaysGot, rec.Code)
	}

	// zero/negative days clamps to the floor of 1
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/stats/timeseries?days=-5", nil))
	if rec.Code != http.StatusOK || repo.seriesDaysGot != 1 {
		t.Fatalf("clamp negative days = %d (code %d), want 1", repo.seriesDaysGot, rec.Code)
	}

	// non-numeric days is rejected outright rather than silently defaulted
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/stats/timeseries?days=abc", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad days = %d, want 400", rec.Code)
	}
}

func TestListArtifactsClampsAndDefaults(t *testing.T) {
	repo := &fakeRepo{artifacts: []db.Artifact{{Hash: "h1", SizeBytes: 10}}}
	r := testRouter(repo)

	// no limit/offset given -> default limit 50, offset 0
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/artifacts", nil))
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"limit":50`)) {
		t.Fatalf("default limit = %d %s", rec.Code, rec.Body)
	}

	// limit above cap gets clamped to 200
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/artifacts?limit=99999", nil))
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"limit":200`)) {
		t.Fatalf("clamp limit = %d %s", rec.Code, rec.Body)
	}

	// negative limit gets clamped to the floor of 1
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/artifacts?limit=-5", nil))
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"limit":1`)) {
		t.Fatalf("clamp negative limit = %d %s", rec.Code, rec.Body)
	}

	// non-numeric limit is rejected outright rather than silently coerced
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/artifacts?limit=abc", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad limit = %d, want 400", rec.Code)
	}

	// non-numeric offset is likewise rejected
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/artifacts?offset=abc", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad offset = %d, want 400", rec.Code)
	}
}

func TestMgmtHandlersRequireOrg(t *testing.T) {
	h := NewHandler(&fakeRepo{}, &fakeStore{})
	r := chi.NewRouter()
	r.Route("/api/v1", func(pr chi.Router) { h.Mount(pr) }) // no org-injecting middleware

	cases := []struct {
		method, path string
	}{
		{http.MethodGet, "/api/v1/tokens"},
		{http.MethodDelete, "/api/v1/tokens/1"},
		{http.MethodPost, "/api/v1/projects"},
		{http.MethodGet, "/api/v1/projects"},
		{http.MethodGet, "/api/v1/stats"},
		{http.MethodGet, "/api/v1/stats/timeseries"},
		{http.MethodGet, "/api/v1/artifacts"},
		{http.MethodGet, "/api/v1/artifacts/abc123"},
		{http.MethodGet, "/api/v1/artifacts/abc123/download"},
		{http.MethodDelete, "/api/v1/artifacts/abc123"},
		{http.MethodDelete, "/api/v1/artifacts"},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(c.method, c.path, bytes.NewBufferString(`{}`)))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s missing org = %d, want 401", c.method, c.path, rec.Code)
		}
	}
}

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
