package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/db"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/localauth"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/usage"
)

// builtinauth wiring is hermetic: /auth/config and /auth/login never touch the
// DB, so a zero-value *db.Repo (present, unused) is enough to mount /api/v1.
func newBuiltinServer(t *testing.T) http.Handler {
	t.Helper()
	hash, _ := bcrypt.GenerateFromPassword([]byte("hunter2"), bcrypt.DefaultCost)
	a, err := localauth.New(localauth.Config{
		RootUsername: "root", PasswordHash: hash, Secret: []byte("s3cret"), TTL: time.Hour,
	}, nil) // repo unused by config/login paths
	if err != nil {
		t.Fatal(err)
	}
	return New(Deps{
		Repo: &db.Repo{}, Usage: usage.New(), Auth: a,
		AuthMode: "builtin", OrgEnabled: false, Login: localauth.LoginHandler(a),
	})
}

func TestAuthConfigEndpoint(t *testing.T) {
	srv := newBuiltinServer(t)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/auth/config", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200; body=%s", rec.Code, rec.Body)
	}
	var out struct {
		Mode       string `json:"mode"`
		OrgEnabled bool   `json:"org_enabled"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Mode != "builtin" || out.OrgEnabled {
		t.Fatalf("config = %+v, want {builtin false}", out)
	}
}

func TestAuthLoginEndpoint(t *testing.T) {
	srv := newBuiltinServer(t)

	rec := httptest.NewRecorder()
	body := strings.NewReader(`{"username":"root","password":"hunter2"}`)
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("login code = %d, want 200; body=%s", rec.Code, rec.Body)
	}

	rec = httptest.NewRecorder()
	bad := strings.NewReader(`{"username":"root","password":"nope"}`)
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bad))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad login code = %d, want 401", rec.Code)
	}
}
