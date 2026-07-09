package server

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/db"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/oidcauth"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/usage"
)

// Router-level wiring test for Task 10: /api/v1 mounted with a real
// oidcauth.Authenticator backed by a static in-memory JWKS (same harness
// pattern as internal/oidcauth/oidc_test.go), against a real *db.Repo so
// mgmt handlers and JIT org provisioning run for real. This is the hermetic
// stand-in for the Keycloak e2e described in the Task 10 brief.

const (
	testIssuer = "https://issuer.test"
	testAud    = "turbo-cache-forge"
)

type jwksHarness struct {
	signer  jose.Signer
	jwksSrv *httptest.Server
}

func newJWKSHarness(t *testing.T) *jwksHarness {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pub := jose.JSONWebKey{Key: key.Public(), KeyID: "test-key", Algorithm: "RS256", Use: "sig"}
	jwks := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{pub}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jwks)
	}))
	t.Cleanup(srv.Close)
	sig, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: key, KeyID: "test-key"}},
		(&jose.SignerOptions{}).WithType("JWT"))
	if err != nil {
		t.Fatal(err)
	}
	return &jwksHarness{signer: sig, jwksSrv: srv}
}

func (h *jwksHarness) mint(t *testing.T, iss, aud string, exp time.Time, extra map[string]any) string {
	t.Helper()
	claims := jwt.Claims{
		Issuer:   iss,
		Subject:  "user-1",
		Audience: jwt.Audience{aud},
		Expiry:   jwt.NewNumericDate(exp),
		IssuedAt: jwt.NewNumericDate(time.Now()),
	}
	tok, err := jwt.Signed(h.signer).Claims(claims).Claims(extra).Serialize()
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

// testDBRepo opens a real *db.Repo against TEST_DATABASE_URL (a migrated
// Postgres). Deps.Repo is concrete (*db.Repo), so /api/v1 mounting and JIT
// org provisioning can only be exercised end-to-end against a real DB.
func testDBRepo(t *testing.T) *db.Repo {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL to run the /api/v1 wiring integration test")
	}
	r, err := db.Open(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	return r
}

func TestAPIV1MountedWithAuth(t *testing.T) {
	repo := testDBRepo(t)
	h := newJWKSHarness(t)

	// Long-lived background context, matching main.go's real wiring
	// (never a request context — the key set refreshes in the background).
	authn, err := oidcauth.New(context.Background(), oidcauth.Config{
		Issuer:   testIssuer,
		JWKSURL:  h.jwksSrv.URL,
		Audience: testAud,
		OrgClaim: "org_id",
	}, repo)
	if err != nil {
		t.Fatalf("oidcauth.New: %v", err)
	}

	srv := New(Deps{Repo: repo, Usage: usage.New(), Auth: authn, MaxUploadBytes: 1 << 20})
	validTok := h.mint(t, testIssuer, testAud, time.Now().Add(time.Hour),
		map[string]any{"org_id": "apiv1-wiring-test-org"})

	t.Run("valid JWT reaches mgmt handler, JIT-provisions org", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil)
		req.Header.Set("Authorization", "Bearer "+validTok)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code == http.StatusUnauthorized {
			t.Fatalf("GET /api/v1/stats with valid JWT = 401, want to reach the handler; body=%s", rec.Body)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /api/v1/stats with valid JWT = %d, want 200; body=%s", rec.Code, rec.Body)
		}
		org, err := repo.EnsureOrgByIdpID(context.Background(), "apiv1-wiring-test-org", "")
		if err != nil || org == nil {
			t.Fatalf("expected org already JIT-provisioned by the middleware, EnsureOrgByIdpID = %v, %v", org, err)
		}
	})

	t.Run("no token = 401", func(t *testing.T) {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("GET /api/v1/stats no token = %d, want 401", rec.Code)
		}
	})

	t.Run("invalid token = 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil)
		req.Header.Set("Authorization", "Bearer garbage")
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("GET /api/v1/stats invalid token = %d, want 401", rec.Code)
		}
	})

	t.Run("openapi.yaml is public", func(t *testing.T) {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/openapi.yaml", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /api/v1/openapi.yaml = %d, want 200", rec.Code)
		}
	})

	t.Run("docs UI is public", func(t *testing.T) {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/docs/", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /api/v1/docs/ = %d, want 200", rec.Code)
		}
	})
}

func TestAPIV1NotMountedWithoutAuth(t *testing.T) {
	repo := testDBRepo(t)
	srv := New(Deps{Repo: repo, Usage: usage.New(), MaxUploadBytes: 1 << 20}) // Auth nil: cache-only self-host

	t.Run("api/v1 is 404 when OIDC is not configured", func(t *testing.T) {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("GET /api/v1/stats with Auth nil = %d, want 404", rec.Code)
		}
	})

	t.Run("v8 turbo path is still mounted", func(t *testing.T) {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v8/artifacts/status", nil))
		// no bearer token -> RequireToken rejects with 401 before reaching the
		// handler; a 404 here would mean the route wasn't mounted at all.
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("GET /v8/artifacts/status no token = %d, want 401 (route mounted, auth required)", rec.Code)
		}
	})
}
