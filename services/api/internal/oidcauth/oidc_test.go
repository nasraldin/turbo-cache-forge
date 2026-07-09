package oidcauth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/auth"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/db"
)

const (
	testIssuer = "https://issuer.test"
	testAud    = "turbo-cache-forge"
)

// fakeProvisioner records the last idp org id it was asked to ensure.
type fakeProvisioner struct {
	lastIdp string
	fail    bool
}

func (f *fakeProvisioner) EnsureOrgByIdpID(_ context.Context, idpOrgID, _ string) (*db.Org, error) {
	if f.fail {
		return nil, context.DeadlineExceeded
	}
	f.lastIdp = idpOrgID
	return &db.Org{ID: 7, Slug: "org-test", IdpOrgID: idpOrgID}, nil
}

// harness spins up a static JWKS server + a signer over a fresh RSA key.
type harness struct {
	signer  jose.Signer
	jwksSrv *httptest.Server
}

func newHarness(t *testing.T) *harness {
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
	return &harness{signer: sig, jwksSrv: srv}
}

func (h *harness) mint(t *testing.T, iss, aud string, exp time.Time, extra map[string]any) string {
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

func (h *harness) authenticator(repo OrgProvisioner) *Authenticator {
	keySet := oidc.NewRemoteKeySet(context.Background(), h.jwksSrv.URL)
	verifier := oidc.NewVerifier(testIssuer, keySet, &oidc.Config{ClientID: testAud})
	return &Authenticator{verifier: verifier, orgClaim: "org_id", repo: repo}
}

func TestMiddlewareTable(t *testing.T) {
	h := newHarness(t)
	prov := &fakeProvisioner{}
	a := h.authenticator(prov)

	future := time.Now().Add(time.Hour)
	past := time.Now().Add(-time.Hour)
	orgClaim := map[string]any{"org_id": "idp-org-42"}

	cases := []struct {
		name  string
		token string
		want  int
	}{
		{"valid", h.mint(t, testIssuer, testAud, future, orgClaim), http.StatusOK},
		{"expired", h.mint(t, testIssuer, testAud, past, orgClaim), http.StatusUnauthorized},
		{"wrong issuer", h.mint(t, "https://evil.test", testAud, future, orgClaim), http.StatusUnauthorized},
		{"wrong audience", h.mint(t, testIssuer, "someone-else", future, orgClaim), http.StatusUnauthorized},
		{"missing org claim", h.mint(t, testIssuer, testAud, future, map[string]any{}), http.StatusUnauthorized},
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if org, ok := auth.OrgFromContext(r.Context()); !ok || org.ID != 7 {
			t.Error("org not injected into context")
		}
		w.WriteHeader(http.StatusOK)
	})
	handler := a.Middleware(next)

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil)
			req.Header.Set("Authorization", "Bearer "+c.token)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != c.want {
				t.Fatalf("%s: code = %d, want %d", c.name, rec.Code, c.want)
			}
		})
	}
}

func TestJITProvisioning(t *testing.T) {
	h := newHarness(t)
	prov := &fakeProvisioner{}
	a := h.authenticator(prov)
	tok := h.mint(t, testIssuer, testAud, time.Now().Add(time.Hour), map[string]any{"org_id": "brand-new-org"})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	a.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("JIT valid token = %d, want 200", rec.Code)
	}
	if prov.lastIdp != "brand-new-org" {
		t.Fatalf("EnsureOrgByIdpID called with %q, want brand-new-org", prov.lastIdp)
	}
}

func TestMissingBearer(t *testing.T) {
	h := newHarness(t)
	a := h.authenticator(&fakeProvisioner{})
	rec := httptest.NewRecorder()
	a.Middleware(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no header = %d, want 401", rec.Code)
	}
}
