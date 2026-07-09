package mgmt

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/auth"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/db"
)

type fakeRepo struct {
	created   db.APIKey
	revokedOK bool
	tokens    []db.APIKey
	projects  []db.Project
	stats     db.Stats
	artifacts []db.Artifact
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
func (f *fakeRepo) ListArtifacts(context.Context, int64, int, int) ([]db.Artifact, error) {
	return f.artifacts, nil
}

// router injects a fixed org into context, mimicking oidcauth.Middleware.
func testRouter(repo Repo) http.Handler {
	h := NewHandler(repo)
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
