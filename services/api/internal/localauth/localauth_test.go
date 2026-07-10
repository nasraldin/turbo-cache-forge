package localauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/auth"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/db"
)

type fakeRepo struct{ lastIdp, lastName string }

func (f *fakeRepo) EnsureOrgByIdpID(_ context.Context, idpOrgID, name string) (*db.Org, error) {
	f.lastIdp, f.lastName = idpOrgID, name
	return &db.Org{ID: 42, Slug: "org-test", IdpOrgID: idpOrgID}, nil
}

func newTestAuth(t *testing.T, repo OrgProvisioner) *Authenticator {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("hunter2"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(Config{RootUsername: "root", PasswordHash: hash, Secret: []byte("s3cret"), TTL: time.Hour}, repo)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestLoginSuccess(t *testing.T) {
	a := newTestAuth(t, &fakeRepo{})
	tok, exp, err := a.Login("root", "hunter2")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if tok == "" || !exp.After(time.Now()) {
		t.Fatalf("bad token/exp: %q %v", tok, exp)
	}
}

func TestLoginRejectsWrongPassword(t *testing.T) {
	a := newTestAuth(t, &fakeRepo{})
	if _, _, err := a.Login("root", "nope"); err != ErrInvalidCredentials {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
}

func TestLoginRejectsUnknownUsername(t *testing.T) {
	a := newTestAuth(t, &fakeRepo{})
	if _, _, err := a.Login("admin", "hunter2"); err != ErrInvalidCredentials {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
}

func TestMiddlewareInjectsOrgForValidToken(t *testing.T) {
	repo := &fakeRepo{}
	a := newTestAuth(t, repo)
	tok, _, _ := a.Login("root", "hunter2")

	var gotOrg *db.Org
	h := a.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotOrg, _ = auth.OrgFromContext(r.Context())
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	h.ServeHTTP(httptest.NewRecorder(), req)

	if gotOrg == nil || gotOrg.ID != 42 {
		t.Fatalf("org not injected: %+v", gotOrg)
	}
	if repo.lastIdp != "local:root" || repo.lastName != "root" {
		t.Fatalf("EnsureOrgByIdpID called with (%q,%q), want (local:root, root)", repo.lastIdp, repo.lastName)
	}
}

func TestMiddlewareRejectsMissingAndBadTokens(t *testing.T) {
	a := newTestAuth(t, &fakeRepo{})
	h := a.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))

	for _, tc := range []struct{ name, authz string }{
		{"missing", ""},
		{"garbage", "Bearer garbage"},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil)
		if tc.authz != "" {
			req.Header.Set("Authorization", tc.authz)
		}
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s: code = %d, want 401", tc.name, rec.Code)
		}
	}
}

func TestLoginHandler(t *testing.T) {
	a := newTestAuth(t, &fakeRepo{})
	h := LoginHandler(a)

	t.Run("good creds -> token", func(t *testing.T) {
		body := strings.NewReader(`{"username":"root","password":"hunter2"}`)
		rec := httptest.NewRecorder()
		h(rec, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", body))
		if rec.Code != http.StatusOK {
			t.Fatalf("code = %d, want 200; body=%s", rec.Code, rec.Body)
		}
		var out struct {
			Token     string `json:"token"`
			ExpiresAt string `json:"expires_at"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Token == "" {
			t.Fatalf("bad body: %s (%v)", rec.Body, err)
		}
	})

	t.Run("bad creds -> 401", func(t *testing.T) {
		body := strings.NewReader(`{"username":"root","password":"nope"}`)
		rec := httptest.NewRecorder()
		h(rec, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", body))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("code = %d, want 401", rec.Code)
		}
	})
}
