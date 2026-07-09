// Package localauth authenticates /api/v1 (dashboard/management humans) with a
// first-party username/password root user — no external IdP. Like oidcauth it
// is mounted ONLY on /api/v1; the cache path (internal/auth, internal/turbo)
// must never import it. Two auth worlds, never mixed.
package localauth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/auth"
	"github.com/nasraldin/turbo-cache-forge/services/api/internal/db"
)

// rootSubject is the stable tenant key for the single root user. It flows into
// EnsureOrgByIdpID(sub) exactly like the personal-mode OIDC `sub`.
const (
	rootSubject = "local:root"
	issuer      = "turbo-cache-forge"
)

// ErrInvalidCredentials is returned by Login for any bad username/password. The
// two cases are indistinguishable to the caller (and in timing).
var ErrInvalidCredentials = errors.New("invalid username or password")

type OrgProvisioner interface {
	EnsureOrgByIdpID(ctx context.Context, idpOrgID, name string) (*db.Org, error)
}

type Config struct {
	RootUsername string
	PasswordHash []byte // bcrypt hash
	Secret       []byte // HS256 signing secret
	TTL          time.Duration
}

type Authenticator struct {
	cfg  Config
	repo OrgProvisioner
}

func New(cfg Config, repo OrgProvisioner) (*Authenticator, error) {
	if cfg.RootUsername == "" || len(cfg.PasswordHash) == 0 || len(cfg.Secret) == 0 {
		return nil, errors.New("localauth: RootUsername, PasswordHash and Secret are required")
	}
	if cfg.TTL <= 0 {
		cfg.TTL = 12 * time.Hour
	}
	return &Authenticator{cfg: cfg, repo: repo}, nil
}

// Login verifies credentials and mints a session JWT. bcrypt runs on every call
// (dominant cost), so an unknown username and a wrong password take the same
// time and return the same error.
func (a *Authenticator) Login(username, password string) (string, time.Time, error) {
	userOK := subtle.ConstantTimeCompare([]byte(username), []byte(a.cfg.RootUsername)) == 1
	pwErr := bcrypt.CompareHashAndPassword(a.cfg.PasswordHash, []byte(password))
	if !userOK || pwErr != nil {
		return "", time.Time{}, ErrInvalidCredentials
	}
	now := time.Now()
	exp := now.Add(a.cfg.TTL)
	tok := signToken(a.cfg.Secret, claims{
		Iss: issuer, Sub: rootSubject, Username: a.cfg.RootUsername,
		Iat: now.Unix(), Exp: exp.Unix(),
	})
	return tok, exp, nil
}

func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, ok := bearer(r)
		if !ok {
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}
		c, err := verifyToken(a.cfg.Secret, raw, time.Now())
		if err != nil {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
		org, err := a.repo.EnsureOrgByIdpID(r.Context(), c.Sub, c.Username)
		if err != nil {
			http.Error(w, "org provisioning failed", http.StatusInternalServerError)
			return
		}
		next.ServeHTTP(w, r.WithContext(auth.WithOrg(r.Context(), org)))
	})
}

// LoginHandler serves POST /api/v1/auth/login. It is mounted only in builtin mode.
func LoginHandler(a *Authenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		tok, exp, err := a.Login(in.Username, in.Password)
		if err != nil {
			http.Error(w, "invalid username or password", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token":      tok,
			"expires_at": exp.UTC().Format(time.RFC3339),
		})
	}
}

func bearer(r *http.Request) (string, bool) {
	const p = "Bearer "
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, p) {
		return "", false
	}
	return strings.TrimPrefix(h, p), true
}
