# Built-in "root user" Auth Provider Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a self-contained username/password auth provider (one "root user" from config) so a developer can run the dashboard locally with no external IdP, selected by a single `AUTH_MODE` env.

**Architecture:** The Go API becomes a tiny first-party token issuer in a new `internal/localauth` package (mints + verifies a short-lived HS256 JWT in-process, bcrypt-checks the root password). It mounts on `/api/v1` exactly where `oidcauth` does today, resolving the single root user to one tenant via the existing personal/no-org `EnsureOrgByIdpID` path. The Next.js dashboard fetches `GET /api/v1/auth/config` at runtime and renders either today's Clerk UI (`oidc`) or a new custom sign-in page (`builtin`) behind a unified `useSession()` seam; the already provider-agnostic `@tcf/api-client` is unchanged.

**Tech Stack:** Go 1.25 (chi, pgx), stdlib `crypto/hmac`+`crypto/sha256` for the HS256 JWT, `golang.org/x/crypto/bcrypt` for passwords; Next.js 15 / React 19, `@clerk/nextjs`, TanStack Query, shadcn `ui/*`, Vitest.

## Global Constraints

- **Two auth worlds, never mixed.** `internal/localauth` mounts ONLY on `/api/v1`. The cache path (`internal/auth`, `internal/turbo`) must never import it. No auth-vendor SDK in the backend.
- **Exclusive mode.** `AUTH_MODE` is `oidc` (default) or `builtin`. Never both live at once.
- **snake_case on the wire** end-to-end (Go JSON tags → `@tcf/types` → dashboard).
- **Built-in mode is single-tenant** (one root user = one org), i.e. the existing personal/no-org case.
- **Token TTL default `12h`; no refresh token** (re-login on expiry).
- **Sign-in copy on bad creds:** exactly `Invalid username or password`.
- Root subject claim value is the constant `local:root`; issuer claim is `turbo-cache-forge`.
- Config env names (verbatim): `AUTH_MODE`, `AUTH_ROOT_USERNAME`, `AUTH_ROOT_PASSWORD`, `AUTH_ROOT_PASSWORD_HASH`, `AUTH_SECRET`, `AUTH_TOKEN_TTL`. Dashboard reads only `NEXT_PUBLIC_API_URL` (already exists) to fetch auth config.

---

## File Structure

**Backend (`services/api`)**
- Create `internal/localauth/token.go` — HS256 JWT mint/verify (stdlib only).
- Create `internal/localauth/localauth.go` — `Authenticator` (Login + Middleware), `Config`, `LoginHandler`.
- Create `internal/localauth/token_test.go`, `internal/localauth/localauth_test.go`.
- Modify `internal/config/config.go` — new auth fields + validation; `internal/config/config_test.go` — cases.
- Modify `internal/server/router.go` — `Auth` becomes an interface; add `AuthMode`/`OrgEnabled`/`Login`; mount `/auth/config` + `/auth/login`. Add `internal/server/builtinauth_test.go`.
- Modify `cmd/server/main.go` — branch on `AUTH_MODE`.

**Dashboard (`apps/dashboard`)**
- Create `src/lib/builtin-auth.ts` — pure token store + login fetch; `src/lib/builtin-auth.test.ts`.
- Create `src/app/session.tsx` — `SessionContext`, `useSession`, `AuthRoot`, `BuiltinSessionProvider`, `ClerkSessionBridge`.
- Create `src/components/builtin-sign-in.tsx` — the custom sign-in form; `src/components/builtin-sign-in.test.tsx`.
- Modify `src/app/layout.tsx` — wrap in `<AuthRoot>` instead of `<ClerkProvider>`.
- Modify `src/app/api.ts` — use `useSession().getToken`.
- Modify `src/middleware.ts` — run Clerk only when `CLERK_SECRET_KEY` present.
- Modify `src/app/(dashboard)/layout.tsx` — chrome branches on mode.
- Modify `src/app/sign-in/[[...sign-in]]/page.tsx` — render custom form in builtin mode.
- Modify `src/app/sign-up/[[...sign-up]]/page.tsx` — redirect to `/sign-in` in builtin mode.

**Docs / env**
- Modify `.env.example`, `apps/dashboard/.env.example`, `README.md`, `docs/ROADMAP.md`.

---

## Task 1: Backend config — AUTH_MODE + root-user + secret fields

**Files:**
- Modify: `services/api/internal/config/config.go`
- Test: `services/api/internal/config/config_test.go`

**Interfaces:**
- Produces: new `Config` fields — `AuthMode string`, `AuthRootUsername string`, `AuthRootPassword string`, `AuthRootPasswordHash string`, `AuthSecret string`, `AuthTokenTTL time.Duration`. `Load()` validates them when `AuthMode=="builtin"`.

- [ ] **Step 1: Write the failing tests**

Append to `services/api/internal/config/config_test.go` (keep existing package clause / imports; add `"time"` and `"os"`/`t.Setenv` as the file already does):

```go
func TestLoadBuiltinAuth(t *testing.T) {
	// base sets the minimum env on the SUBTEST's t, so t.Setenv cleanup is
	// scoped per subtest and never leaks into the next one.
	base := func(t *testing.T) {
		t.Setenv("DATABASE_URL", "postgres://x")
		t.Setenv("AUTH_MODE", "builtin")
	}

	t.Run("defaults ttl to 12h and reads username+password", func(t *testing.T) {
		base(t)
		t.Setenv("AUTH_ROOT_USERNAME", "root")
		t.Setenv("AUTH_ROOT_PASSWORD", "hunter2")
		c, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if c.AuthMode != "builtin" || c.AuthRootUsername != "root" || c.AuthRootPassword != "hunter2" {
			t.Fatalf("unexpected config: %+v", c)
		}
		if c.AuthTokenTTL != 12*time.Hour {
			t.Fatalf("ttl = %v, want 12h", c.AuthTokenTTL)
		}
	})

	t.Run("missing username fails", func(t *testing.T) {
		base(t)
		t.Setenv("AUTH_ROOT_PASSWORD", "hunter2")
		if _, err := Load(); err == nil {
			t.Fatal("expected error for missing AUTH_ROOT_USERNAME")
		}
	})

	t.Run("missing password fails", func(t *testing.T) {
		base(t)
		t.Setenv("AUTH_ROOT_USERNAME", "root")
		if _, err := Load(); err == nil {
			t.Fatal("expected error for missing password")
		}
	})

	t.Run("both password and hash fails", func(t *testing.T) {
		base(t)
		t.Setenv("AUTH_ROOT_USERNAME", "root")
		t.Setenv("AUTH_ROOT_PASSWORD", "hunter2")
		t.Setenv("AUTH_ROOT_PASSWORD_HASH", "$2a$10$abc")
		if _, err := Load(); err == nil {
			t.Fatal("expected error when both password and hash set")
		}
	})

	t.Run("unknown mode fails", func(t *testing.T) {
		t.Setenv("DATABASE_URL", "postgres://x")
		t.Setenv("AUTH_MODE", "ldap")
		if _, err := Load(); err == nil {
			t.Fatal("expected error for unknown AUTH_MODE")
		}
	})

	t.Run("default mode is oidc, no auth vars required", func(t *testing.T) {
		t.Setenv("DATABASE_URL", "postgres://x")
		c, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if c.AuthMode != "oidc" {
			t.Fatalf("default AuthMode = %q, want oidc", c.AuthMode)
		}
	})
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd services/api && go test ./internal/config/ -run TestLoadBuiltinAuth`
Expected: FAIL (compile error: `AuthMode` undefined).

- [ ] **Step 3: Add fields, defaults, validation, and a duration helper**

In `services/api/internal/config/config.go`, add to the `Config` struct (after `OIDCOrgEnabled`):

```go
	AuthMode             string        // "oidc" (default) | "builtin"
	AuthRootUsername     string        // builtin: the single root identity
	AuthRootPassword     string        // builtin: plaintext (bcrypt-hashed at boot); XOR with hash
	AuthRootPasswordHash string        // builtin: precomputed bcrypt hash; XOR with plaintext
	AuthSecret           string        // builtin: HS256 secret; random per-boot if empty
	AuthTokenTTL         time.Duration // builtin: session JWT lifetime (default 12h)
```

Add `"time"` to the import block. In `Load()`, after the OIDC block and before `return c, nil`, add:

```go
	c.AuthMode = env("AUTH_MODE", "oidc")
	if c.AuthMode != "oidc" && c.AuthMode != "builtin" {
		return c, fmt.Errorf("AUTH_MODE must be 'oidc' or 'builtin', got %q", c.AuthMode)
	}
	c.AuthRootUsername = os.Getenv("AUTH_ROOT_USERNAME")
	c.AuthRootPassword = os.Getenv("AUTH_ROOT_PASSWORD")
	c.AuthRootPasswordHash = os.Getenv("AUTH_ROOT_PASSWORD_HASH")
	c.AuthSecret = os.Getenv("AUTH_SECRET")
	c.AuthTokenTTL = envDuration("AUTH_TOKEN_TTL", 12*time.Hour)
	if c.AuthMode == "builtin" {
		if c.AuthRootUsername == "" {
			return c, fmt.Errorf("AUTH_ROOT_USERNAME is required when AUTH_MODE=builtin")
		}
		hasPw, hasHash := c.AuthRootPassword != "", c.AuthRootPasswordHash != ""
		if hasPw == hasHash { // neither, or both
			return c, fmt.Errorf("exactly one of AUTH_ROOT_PASSWORD or AUTH_ROOT_PASSWORD_HASH is required when AUTH_MODE=builtin")
		}
	}
```

Add this helper next to `envBool` at the bottom of the file:

```go
func envDuration(k string, def time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd services/api && go test ./internal/config/`
Expected: PASS (all `TestLoadBuiltinAuth` subtests plus existing config tests).

- [ ] **Step 5: Commit**

```bash
git add services/api/internal/config/
git commit -m "feat(config): add AUTH_MODE + built-in root-user auth vars"
```

---

## Task 2: `internal/localauth` package — JWT mint/verify, Login, Middleware, LoginHandler

**Files:**
- Create: `services/api/internal/localauth/token.go`
- Create: `services/api/internal/localauth/localauth.go`
- Test: `services/api/internal/localauth/token_test.go`
- Test: `services/api/internal/localauth/localauth_test.go`
- Modify: `services/api/go.mod` / `go.sum` (add `golang.org/x/crypto`)

**Interfaces:**
- Consumes: `auth.WithOrg` (`internal/auth`), `db.Org` (`internal/db`).
- Produces:
  - `type OrgProvisioner interface { EnsureOrgByIdpID(ctx context.Context, idpOrgID, name string) (*db.Org, error) }`
  - `type Config struct { RootUsername string; PasswordHash []byte; Secret []byte; TTL time.Duration }`
  - `func New(cfg Config, repo OrgProvisioner) (*Authenticator, error)`
  - `func (*Authenticator) Login(username, password string) (token string, expiresAt time.Time, err error)`
  - `func (*Authenticator) Middleware(next http.Handler) http.Handler`
  - `func LoginHandler(a *Authenticator) http.HandlerFunc`
  - `var ErrInvalidCredentials error`
  - Internal: `signToken(secret []byte, c claims) string`, `verifyToken(secret []byte, tok string, now time.Time) (claims, error)`.

- [ ] **Step 1: Add the bcrypt dependency**

Run:
```bash
cd services/api && go get golang.org/x/crypto/bcrypt@latest
```
Expected: `go.mod` now requires `golang.org/x/crypto` (previously absent).

- [ ] **Step 2: Write the failing token tests**

Create `services/api/internal/localauth/token.go` with just the types/signatures so the package compiles, then the test. First create `token.go`:

```go
package localauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// First-party HS256 JWT. We both sign and verify, so a minimal stdlib
// implementation avoids a JWT dependency while staying standard-shaped.

type claims struct {
	Iss      string `json:"iss"`
	Sub      string `json:"sub"`
	Username string `json:"username"`
	Iat      int64  `json:"iat"`
	Exp      int64  `json:"exp"`
}

var errBadToken = errors.New("invalid token")

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func sign(secret []byte, signingInput string) []byte {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(signingInput))
	return m.Sum(nil)
}

func signToken(secret []byte, c claims) string {
	header := b64([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload, _ := json.Marshal(c)
	signingInput := header + "." + b64(payload)
	return signingInput + "." + b64(sign(secret, signingInput))
}

func verifyToken(secret []byte, tok string, now time.Time) (claims, error) {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return claims{}, errBadToken
	}
	signingInput := parts[0] + "." + parts[1]
	want := sign(secret, signingInput)
	got, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(want, got) {
		return claims{}, errBadToken
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return claims{}, errBadToken
	}
	var c claims
	if err := json.Unmarshal(raw, &c); err != nil {
		return claims{}, errBadToken
	}
	if now.Unix() >= c.Exp {
		return claims{}, errBadToken
	}
	return c, nil
}
```

Create `services/api/internal/localauth/token_test.go`:

```go
package localauth

import (
	"strings"
	"testing"
	"time"
)

func TestSignVerifyRoundTrip(t *testing.T) {
	secret := []byte("s3cret")
	now := time.Unix(1_000_000, 0)
	tok := signToken(secret, claims{Iss: "turbo-cache-forge", Sub: "local:root", Username: "root", Iat: now.Unix(), Exp: now.Add(time.Hour).Unix()})
	c, err := verifyToken(secret, tok, now)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if c.Username != "root" || c.Sub != "local:root" {
		t.Fatalf("claims = %+v", c)
	}
}

func TestVerifyRejectsExpired(t *testing.T) {
	secret := []byte("s3cret")
	now := time.Unix(1_000_000, 0)
	tok := signToken(secret, claims{Exp: now.Unix()}) // exp == now -> expired
	if _, err := verifyToken(secret, tok, now); err == nil {
		t.Fatal("expected expired token to be rejected")
	}
}

func TestVerifyRejectsBadSignature(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	tok := signToken([]byte("right"), claims{Exp: now.Add(time.Hour).Unix()})
	if _, err := verifyToken([]byte("wrong"), tok, now); err == nil {
		t.Fatal("expected wrong-secret verify to fail")
	}
}

func TestVerifyRejectsMalformed(t *testing.T) {
	if _, err := verifyToken([]byte("s"), "not-a-jwt", time.Now()); err == nil {
		t.Fatal("expected malformed token to fail")
	}
	if _, err := verifyToken([]byte("s"), strings.Repeat("a.", 2), time.Now()); err == nil {
		t.Fatal("expected 2-part token to fail")
	}
}
```

- [ ] **Step 3: Run token tests to verify they pass**

Run: `cd services/api && go test ./internal/localauth/ -run 'TestSign|TestVerify'`
Expected: PASS.

- [ ] **Step 4: Write the failing Authenticator tests**

Create `services/api/internal/localauth/localauth_test.go`:

```go
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
```

- [ ] **Step 5: Run to verify it fails**

Run: `cd services/api && go test ./internal/localauth/ -run 'TestLogin|TestMiddleware'`
Expected: FAIL (compile error: `New`, `Login`, `Middleware`, `LoginHandler`, `ErrInvalidCredentials` undefined).

- [ ] **Step 6: Implement `localauth.go`**

Create `services/api/internal/localauth/localauth.go`:

```go
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
```

- [ ] **Step 7: Run all localauth tests to verify they pass**

Run: `cd services/api && go test ./internal/localauth/`
Expected: PASS.

- [ ] **Step 8: Verify the two-worlds import boundary still holds**

Run:
```bash
cd services/api && go list -deps ./internal/turbo/ ./internal/auth/ | grep -c 'internal/localauth' || true
```
Expected: prints `0` (the cache path must not import `localauth`).

- [ ] **Step 9: Commit**

```bash
git add services/api/internal/localauth/ services/api/go.mod services/api/go.sum
git commit -m "feat(localauth): first-party root-user JWT auth (mint/verify/middleware)"
```

---

## Task 3: Router wiring — `Auth` interface, `/auth/config`, `/auth/login`

**Files:**
- Modify: `services/api/internal/server/router.go`
- Test: `services/api/internal/server/builtinauth_test.go` (create)

**Interfaces:**
- Consumes: `localauth.Authenticator`/`LoginHandler` (Task 2), `oidcauth.Authenticator`.
- Produces: `Deps.Auth` is now `type Authenticator interface { Middleware(http.Handler) http.Handler }`; new `Deps.AuthMode string`, `Deps.OrgEnabled bool`, `Deps.Login http.HandlerFunc`. New public routes `GET /api/v1/auth/config` and (when `Login != nil`) `POST /api/v1/auth/login`.

- [ ] **Step 1: Write the failing router test**

Create `services/api/internal/server/builtinauth_test.go`:

```go
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
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd services/api && go test ./internal/server/ -run 'TestAuthConfig|TestAuthLogin'`
Expected: FAIL (compile error: `Deps` has no field `AuthMode`/`Login`; `Auth` type mismatch).

- [ ] **Step 3: Change `Deps` and add the routes**

In `services/api/internal/server/router.go`:

Add the interface and update `Deps` (replace the `Auth *oidcauth.Authenticator` line):

```go
// Authenticator is satisfied by both oidcauth.Authenticator and
// localauth.Authenticator — the router does not care which world is active.
type Authenticator interface {
	Middleware(next http.Handler) http.Handler
}

// Deps holds everything the router needs. Fields are added as tasks land.
type Deps struct {
	Store          storage.Storage
	Repo           *db.Repo
	MaxUploadBytes int64
	Usage          *usage.Accumulator
	Auth           Authenticator    // oidcauth (oidc) or localauth (builtin)
	AuthMode       string           // "oidc" | "builtin" — reported at /api/v1/auth/config
	OrgEnabled     bool             // reported at /api/v1/auth/config
	Login          http.HandlerFunc // non-nil only in builtin mode; serves POST /auth/login
	CORSOrigins    []string
}
```

Remove the now-unused `oidcauth` import from `router.go` (the interface replaces the concrete type). Run `goimports`/`go build` will flag it.

Inside the `if d.Auth != nil && d.Repo != nil {` block, add the public auth routes right after the `ar.Handle("/docs/*", ...)` line and before the authenticated `ar.Group`:

```go
			// public auth discovery — lets the dashboard pick its sign-in UI.
			ar.Get("/auth/config", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"mode":        d.AuthMode,
					"org_enabled": d.OrgEnabled,
				})
			})
			if d.Login != nil { // builtin mode only
				ar.Post("/auth/login", d.Login)
			}
```

Add `"encoding/json"` to the `router.go` imports.

- [ ] **Step 4: Run to verify it passes (and existing server tests still build)**

Run: `cd services/api && go test ./internal/server/ -run 'TestAuthConfig|TestAuthLogin'`
Expected: PASS.

Run: `cd services/api && go build ./... && go vet ./internal/server/`
Expected: no errors. (The existing `apiv1_test.go` still compiles: `*oidcauth.Authenticator` satisfies the new `Authenticator` interface.)

- [ ] **Step 5: Commit**

```bash
git add services/api/internal/server/
git commit -m "feat(server): auth-mode-agnostic Deps + /auth/config and /auth/login routes"
```

---

## Task 4: `main.go` — branch on `AUTH_MODE`

**Files:**
- Modify: `services/api/cmd/server/main.go`

**Interfaces:**
- Consumes: `config.Config` auth fields (Task 1), `localauth` (Task 2), `server.Deps` (Task 3).

- [ ] **Step 1: Add the builtin branch and helpers**

In `services/api/cmd/server/main.go`, add imports: `"crypto/rand"`, `"golang.org/x/crypto/bcrypt"`, and `"github.com/nasraldin/turbo-cache-forge/services/api/internal/localauth"`.

Replace the existing auth-construction block (`var authn *oidcauth.Authenticator ... }` — the whole `if cfg.OIDCIssuer != "" { ... }`) with a mode switch:

```go
	var authn server.Authenticator
	var loginHandler http.HandlerFunc
	switch cfg.AuthMode {
	case "builtin":
		hash := []byte(cfg.AuthRootPasswordHash)
		if len(hash) == 0 {
			hash, err = bcrypt.GenerateFromPassword([]byte(cfg.AuthRootPassword), bcrypt.DefaultCost)
			if err != nil {
				log.Fatalf("bcrypt root password: %v", err)
			}
		}
		secret := []byte(cfg.AuthSecret)
		if len(secret) == 0 {
			secret = randomSecret()
			log.Printf("AUTH_SECRET unset — generated an ephemeral signing secret; " +
				"sessions will not survive a restart or span replicas. Set AUTH_SECRET for stable sessions.")
		}
		la, lerr := localauth.New(localauth.Config{
			RootUsername: cfg.AuthRootUsername, PasswordHash: hash,
			Secret: secret, TTL: cfg.AuthTokenTTL,
		}, repo)
		if lerr != nil {
			log.Fatalf("localauth init: %v", lerr)
		}
		authn, loginHandler = la, localauth.LoginHandler(la)
		log.Printf("management API enabled at /api/v1 — BUILTIN MODE: root user %q (TTL=%s)",
			cfg.AuthRootUsername, cfg.AuthTokenTTL)
	default: // "oidc"
		if cfg.OIDCIssuer != "" {
			oa, oerr := oidcauth.New(ctx, oidcauth.Config{
				Issuer:     cfg.OIDCIssuer,
				JWKSURL:    cfg.OIDCJWKSURL,
				Audience:   cfg.OIDCAudience,
				OrgClaim:   cfg.OIDCOrgClaim,
				OrgEnabled: cfg.OIDCOrgEnabled,
			}, repo)
			if oerr != nil {
				log.Fatalf("oidc init: %v", oerr)
			}
			authn = oa
			if cfg.OIDCOrgEnabled {
				log.Printf("management API enabled at /api/v1 (issuer=%s)", cfg.OIDCIssuer)
			} else {
				log.Printf("management API enabled at /api/v1 (issuer=%s) — PERSONAL MODE: audience check skipped, tenant=sub. "+
					"Only safe when OIDC_ISSUER is dedicated to this app; a shared multi-app issuer lets any of its tokens in.", cfg.OIDCIssuer)
			}
		}
	}
```

Update the `server.New(server.Deps{...})` call to pass the new fields:

```go
	srv := server.New(server.Deps{
		Store: store, Repo: repo, MaxUploadBytes: cfg.MaxUploadBytes,
		Usage: acc, Auth: authn, CORSOrigins: cfg.CORSAllowedOrigins,
		AuthMode:   cfg.AuthMode,
		OrgEnabled: cfg.AuthMode == "oidc" && cfg.OIDCOrgEnabled,
		Login:      loginHandler,
	})
```

Add the helper near `getenv` at the bottom:

```go
func randomSecret() []byte {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		log.Fatalf("generate secret: %v", err)
	}
	return b
}
```

Add `"net/http"` to imports if not already present (it is — `http.ListenAndServe` is used).

- [ ] **Step 2: Build and vet**

Run: `cd services/api && go build ./... && go vet ./...`
Expected: no errors.

- [ ] **Step 3: Manual smoke (build + boot config validation)**

Run (no DB needed to prove the config/boot branch; it will fail at `db.Open`, which is expected and after our branch is constructed — so instead assert the *config* rejects a bad setup):

```bash
cd services/api && \
  DATABASE_URL=postgres://x AUTH_MODE=builtin AUTH_ROOT_USERNAME=root go run ./cmd/server 2>&1 | head -3
```
Expected: fails fast with the config error `exactly one of AUTH_ROOT_PASSWORD or AUTH_ROOT_PASSWORD_HASH is required when AUTH_MODE=builtin` (proves Task 1 validation is wired through `main`). A full runtime smoke against Postgres is covered in Task 5's README quickstart.

- [ ] **Step 4: Commit**

```bash
git add services/api/cmd/server/main.go
git commit -m "feat(server): wire AUTH_MODE=builtin root-user auth in main"
```

---

## Task 5: Docs & env — quickstart, .env examples, ROADMAP

**Files:**
- Modify: `.env.example`
- Modify: `apps/dashboard/.env.example`
- Modify: `README.md`
- Modify: `docs/ROADMAP.md`

**Interfaces:** none (documentation).

- [ ] **Step 1: Document server env vars**

In `.env.example`, add a block (match the file's existing comment style):

```bash
# --- Auth mode -------------------------------------------------------------
# AUTH_MODE selects how dashboard/humans authenticate:
#   oidc     (default) validate JWTs from an external IdP (Clerk/Keycloak/...)
#   builtin  a single root user set here — no external IdP required
AUTH_MODE=oidc

# Built-in auth (only used when AUTH_MODE=builtin):
# AUTH_ROOT_USERNAME=root
# AUTH_ROOT_PASSWORD=change-me            # or set AUTH_ROOT_PASSWORD_HASH (bcrypt) instead
# AUTH_SECRET=                            # HS256 session secret; random per-boot if empty (sessions reset on restart)
# AUTH_TOKEN_TTL=12h                      # session lifetime
```

- [ ] **Step 2: Document the dashboard env (no new var, note runtime detection)**

In `apps/dashboard/.env.example`, add a comment near `NEXT_PUBLIC_API_URL`:

```bash
# The dashboard auto-detects the auth mode at runtime via
# GET ${NEXT_PUBLIC_API_URL}/api/v1/auth/config — no rebuild needed to switch
# between built-in and OIDC. Clerk vars below are only needed for AUTH_MODE=oidc.
```

- [ ] **Step 3: Add a README "run locally with built-in auth" section**

In `README.md`, after the existing quickstart, add:

```markdown
### Run locally with built-in auth (no IdP)

Set a root user on the API and you can sign in to the dashboard with a
username + password — no Clerk/Keycloak needed:

```bash
AUTH_MODE=builtin \
AUTH_ROOT_USERNAME=root \
AUTH_ROOT_PASSWORD=change-me \
AUTH_SECRET=$(openssl rand -hex 32) \
  <your usual `docker compose` / `go run ./cmd/server` invocation>
```

The dashboard detects the mode from `GET /api/v1/auth/config` and shows the
built-in sign-in page automatically. Cache tokens for Turborepo are still
minted in the dashboard exactly as before.
```

- [ ] **Step 4: Note the invariant in ROADMAP**

In `docs/ROADMAP.md`, under the invariants/auth section, add one line:

```markdown
- **Built-in auth provider** — `AUTH_MODE=builtin` runs a single root user (username/password → first-party HS256 JWT) as an alternative to OIDC. It upholds "two auth worlds": `internal/localauth` mounts only on `/api/v1`; the cache path never imports it. Exclusive with `oidc`.
```

- [ ] **Step 5: Commit**

```bash
git add .env.example apps/dashboard/.env.example README.md docs/ROADMAP.md
git commit -m "docs: document AUTH_MODE=builtin root-user auth"
```

---

## Task 6: Dashboard session seam — token store + `useSession` + `AuthRoot`

**Files:**
- Create: `apps/dashboard/src/lib/builtin-auth.ts`
- Test: `apps/dashboard/src/lib/builtin-auth.test.ts`
- Create: `apps/dashboard/src/app/session.tsx`
- Modify: `apps/dashboard/src/app/layout.tsx`

**Interfaces:**
- Produces (`lib/builtin-auth.ts`): `saveToken(token: string): void`, `loadToken(): string | null` (returns null when expired), `clearToken(): void`, `login(baseUrl, username, password): Promise<string>` (throws `Error` with message on 401), `decodeExp(token: string): number | null`.
- Produces (`app/session.tsx`): `useSession(): Session`, `<AuthRoot>`, where
  `interface Session { mode: "oidc" | "builtin"; isLoaded: boolean; isSignedIn: boolean; getToken: () => Promise<string | null>; signOut: () => void; userLabel: string | null; login?: (u: string, p: string) => Promise<void> }`.

- [ ] **Step 1: Write the failing token-store tests**

Create `apps/dashboard/src/lib/builtin-auth.test.ts`:

```ts
import { afterEach, describe, expect, it, vi } from "vitest";
import { clearToken, decodeExp, loadToken, saveToken } from "./builtin-auth";

// Build an unsigned JWT-shaped string with a given exp (seconds).
function fakeJwt(expSec: number): string {
  const b64 = (o: unknown) =>
    btoa(JSON.stringify(o)).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
  return `${b64({ alg: "HS256" })}.${b64({ exp: expSec })}.sig`;
}

afterEach(() => {
  clearToken();
  vi.useRealTimers();
});

describe("builtin-auth token store", () => {
  it("saves and loads a non-expired token", () => {
    const tok = fakeJwt(Math.floor(Date.now() / 1000) + 3600);
    saveToken(tok);
    expect(loadToken()).toBe(tok);
  });

  it("returns null for an expired token", () => {
    const tok = fakeJwt(Math.floor(Date.now() / 1000) - 1);
    saveToken(tok);
    expect(loadToken()).toBeNull();
  });

  it("clearToken removes it", () => {
    saveToken(fakeJwt(Math.floor(Date.now() / 1000) + 3600));
    clearToken();
    expect(loadToken()).toBeNull();
  });

  it("decodeExp reads the exp claim", () => {
    expect(decodeExp(fakeJwt(1234))).toBe(1234);
    expect(decodeExp("garbage")).toBeNull();
  });
});
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd apps/dashboard && pnpm vitest run src/lib/builtin-auth.test.ts`
Expected: FAIL (cannot resolve `./builtin-auth`).

- [ ] **Step 3: Implement the token store**

Create `apps/dashboard/src/lib/builtin-auth.ts`:

```ts
// Built-in-auth session token store. The JWT is a bearer token the SPA must
// read to send as Authorization, so it lives in localStorage (accepted
// trade-off for a self-hosted single-user tool; short TTL limits exposure).
const KEY = "tcf.builtin.token";

export function decodeExp(token: string): number | null {
  const parts = token.split(".");
  if (parts.length !== 3) return null;
  try {
    const json = atob(parts[1].replace(/-/g, "+").replace(/_/g, "/"));
    const claims = JSON.parse(json) as { exp?: number };
    return typeof claims.exp === "number" ? claims.exp : null;
  } catch {
    return null;
  }
}

export function saveToken(token: string): void {
  if (typeof window !== "undefined") window.localStorage.setItem(KEY, token);
}

export function loadToken(): string | null {
  if (typeof window === "undefined") return null;
  const tok = window.localStorage.getItem(KEY);
  if (!tok) return null;
  const exp = decodeExp(tok);
  if (exp === null || Date.now() / 1000 >= exp) {
    window.localStorage.removeItem(KEY);
    return null;
  }
  return tok;
}

export function clearToken(): void {
  if (typeof window !== "undefined") window.localStorage.removeItem(KEY);
}

// login POSTs credentials, stores the returned token, and returns it. Throws
// with a user-facing message on failure.
export async function login(baseUrl: string, username: string, password: string): Promise<string> {
  const res = await fetch(`${baseUrl.replace(/\/$/, "")}/api/v1/auth/login`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ username, password }),
  });
  if (!res.ok) {
    throw new Error(res.status === 401 ? "Invalid username or password" : "Sign-in failed. Try again.");
  }
  const body = (await res.json()) as { token: string };
  saveToken(body.token);
  return body.token;
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd apps/dashboard && pnpm vitest run src/lib/builtin-auth.test.ts`
Expected: PASS.

- [ ] **Step 5: Implement the session context + AuthRoot**

Create `apps/dashboard/src/app/session.tsx`:

```tsx
"use client";
import { ClerkProvider, useAuth, useClerk, useUser } from "@clerk/nextjs";
import { usePathname, useRouter } from "next/navigation";
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { clearToken, loadToken, login as builtinLogin } from "@/lib/builtin-auth";

export interface Session {
  mode: "oidc" | "builtin";
  isLoaded: boolean;
  isSignedIn: boolean;
  getToken: () => Promise<string | null>;
  signOut: () => void;
  userLabel: string | null;
  login?: (username: string, password: string) => Promise<void>;
}

const SessionContext = createContext<Session | null>(null);

export function useSession(): Session {
  const ctx = useContext(SessionContext);
  if (!ctx) throw new Error("useSession must be used within <AuthRoot>");
  return ctx;
}

const API_URL = process.env.NEXT_PUBLIC_API_URL ?? "";

// ClerkSessionBridge is mounted ONLY inside <ClerkProvider> (oidc mode), so its
// Clerk hooks are always active — no conditional-hook violation.
function ClerkSessionBridge({ children }: { children: ReactNode }) {
  const { getToken, isLoaded, isSignedIn } = useAuth();
  const { user } = useUser();
  const clerk = useClerk();
  const value = useMemo<Session>(
    () => ({
      mode: "oidc",
      isLoaded,
      isSignedIn: Boolean(isSignedIn),
      getToken: () => getToken(),
      signOut: () => void clerk.signOut(),
      userLabel: user?.primaryEmailAddress?.emailAddress ?? user?.username ?? null,
    }),
    [getToken, isLoaded, isSignedIn, clerk, user],
  );
  return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>;
}

function BuiltinSessionProvider({ children }: { children: ReactNode }) {
  const router = useRouter();
  const pathname = usePathname();
  const [token, setToken] = useState<string | null>(null);
  const [isLoaded, setLoaded] = useState(false);

  useEffect(() => {
    setToken(loadToken());
    setLoaded(true);
  }, []);

  // Client-side route guard: builtin mode ships no Clerk middleware.
  useEffect(() => {
    if (isLoaded && !token && pathname !== "/sign-in") router.replace("/sign-in");
  }, [isLoaded, token, pathname, router]);

  const value = useMemo<Session>(
    () => ({
      mode: "builtin",
      isLoaded,
      isSignedIn: Boolean(token),
      getToken: async () => loadToken(),
      signOut: () => {
        clearToken();
        setToken(null);
        router.replace("/sign-in");
      },
      userLabel: null,
      login: async (username, password) => {
        const t = await builtinLogin(API_URL, username, password);
        setToken(t);
      },
    }),
    [isLoaded, token, router],
  );
  return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>;
}

// AuthRoot fetches the server's auth mode once, then mounts the matching
// provider. ClerkProvider is only ever mounted in oidc mode, so builtin
// deployments need no Clerk publishable key.
export function AuthRoot({ children }: { children: ReactNode }) {
  const [mode, setMode] = useState<"oidc" | "builtin" | null>(null);

  useEffect(() => {
    let alive = true;
    fetch(`${API_URL.replace(/\/$/, "")}/api/v1/auth/config`)
      .then((r) => (r.ok ? r.json() : Promise.reject(new Error(String(r.status)))))
      .then((cfg: { mode: "oidc" | "builtin" }) => {
        if (alive) setMode(cfg.mode === "builtin" ? "builtin" : "oidc");
      })
      .catch(() => {
        // Fallback: infer from a baked Clerk key, else assume builtin.
        if (alive) setMode(process.env.NEXT_PUBLIC_CLERK_PUBLISHABLE_KEY ? "oidc" : "builtin");
      });
    return () => {
      alive = false;
    };
  }, []);

  if (mode === null) {
    return (
      <div className="grid min-h-screen place-items-center bg-bg text-muted" aria-busy="true">
        Loading…
      </div>
    );
  }
  if (mode === "builtin") {
    return <BuiltinSessionProvider>{children}</BuiltinSessionProvider>;
  }
  return (
    <ClerkProvider>
      <ClerkSessionBridge>{children}</ClerkSessionBridge>
    </ClerkProvider>
  );
}
```

- [ ] **Step 6: Swap `layout.tsx` to use `AuthRoot`**

In `apps/dashboard/src/app/layout.tsx`, replace the `import { ClerkProvider } from "@clerk/nextjs";` with `import { AuthRoot } from "./session";`, and replace the `<ClerkProvider>...</ClerkProvider>` wrapper with `<AuthRoot>...</AuthRoot>` (keep `<html>`, `<body>`, `QueryProvider`, `Toaster` exactly as they are inside).

- [ ] **Step 7: Typecheck + build**

Run: `cd apps/dashboard && pnpm tsc --noEmit`
Expected: no errors.

- [ ] **Step 8: Commit**

```bash
git add apps/dashboard/src/lib/builtin-auth.ts apps/dashboard/src/lib/builtin-auth.test.ts apps/dashboard/src/app/session.tsx apps/dashboard/src/app/layout.tsx
git commit -m "feat(dashboard): runtime auth-mode detection + useSession seam"
```

---

## Task 7: Dashboard wiring — api client, middleware, chrome, sign-up

**Files:**
- Modify: `apps/dashboard/src/app/api.ts`
- Modify: `apps/dashboard/src/middleware.ts`
- Modify: `apps/dashboard/src/app/(dashboard)/layout.tsx`
- Modify: `apps/dashboard/src/app/sign-up/[[...sign-up]]/page.tsx`

**Interfaces:**
- Consumes: `useSession` (Task 6).

- [ ] **Step 1: Route the API client token through `useSession`**

Replace `apps/dashboard/src/app/api.ts` body:

```ts
"use client";
import { createApiClient, type ApiClient } from "@tcf/api-client";
import { useMemo } from "react";
import { useSession } from "./session";

// The ONLY place the session token meets the SDK. Works for either auth mode —
// useSession().getToken() returns a Clerk JWT (oidc) or the built-in JWT.
export function useApiClient(): ApiClient {
  const { getToken } = useSession();
  return useMemo(
    () =>
      createApiClient({
        baseUrl: process.env.NEXT_PUBLIC_API_URL!,
        getToken: () => getToken(),
      }),
    [getToken],
  );
}
```

- [ ] **Step 2: Gate Clerk middleware on the presence of a Clerk secret**

Replace `apps/dashboard/src/middleware.ts`:

```ts
import { clerkMiddleware, createRouteMatcher } from "@clerk/nextjs/server";
import { NextResponse, type NextFetchEvent, type NextRequest } from "next/server";

const isPublic = createRouteMatcher(["/sign-in(.*)", "/sign-up(.*)"]);

// Built lazily so builtin-mode deployments (no Clerk key) never construct it.
let guard: ReturnType<typeof clerkMiddleware> | null = null;
function clerkGuard() {
  return (guard ??= clerkMiddleware(async (auth, req) => {
    if (isPublic(req)) return;
    const { userId } = await auth();
    if (!userId) return NextResponse.redirect(new URL("/sign-in", req.url));
  }));
}

export default function middleware(req: NextRequest, ev: NextFetchEvent) {
  // Built-in auth mode ships no Clerk secret; skip Clerk and let the
  // client-side session guard (BuiltinSessionProvider) handle redirects.
  if (!process.env.CLERK_SECRET_KEY) return NextResponse.next();
  return clerkGuard()(req, ev);
}

export const config = { matcher: ["/((?!_next|.*\\..*).*)", "/"] };
```

- [ ] **Step 3: Branch the logged-in chrome on auth mode**

In `apps/dashboard/src/app/(dashboard)/layout.tsx`:
- Add `import { useSession } from "@/app/session";` and keep the Clerk imports.
- Inside `DashboardLayout`, add `const session = useSession();` and `const isOidc = session.mode === "oidc";`.
- Guard the org switcher: change `{orgEnabled && <OrganizationSwitcher .../>}` to `{isOidc && orgEnabled && <OrganizationSwitcher hidePersonal afterSelectOrganizationUrl="/" />}`.
- Replace the bottom user block:

```tsx
        <div className="mt-auto flex items-center gap-2 px-2">
          {isOidc ? (
            <UserButton />
          ) : (
            <button
              type="button"
              onClick={session.signOut}
              className="flex items-center gap-2 rounded-md px-3 py-2 text-sm text-muted transition-colors hover:bg-surface-2 hover:text-text"
            >
              Sign out
            </button>
          )}
        </div>
```

- [ ] **Step 4: Redirect `/sign-up` to `/sign-in` in builtin mode**

Replace `apps/dashboard/src/app/sign-up/[[...sign-up]]/page.tsx`:

```tsx
"use client";
import { SignUp } from "@clerk/nextjs";
import { redirect } from "next/navigation";
import { useSession } from "@/app/session";

// Built-in auth has no self-registration — there is one root user.
export default function Page() {
  const { mode } = useSession();
  if (mode === "builtin") redirect("/sign-in");
  return (
    <div className="grid min-h-screen place-items-center bg-bg">
      <SignUp />
    </div>
  );
}
```

- [ ] **Step 5: Typecheck**

Run: `cd apps/dashboard && pnpm tsc --noEmit`
Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add apps/dashboard/src/app/api.ts apps/dashboard/src/middleware.ts "apps/dashboard/src/app/(dashboard)/layout.tsx" "apps/dashboard/src/app/sign-up/[[...sign-up]]/page.tsx"
git commit -m "feat(dashboard): route api/middleware/chrome/sign-up through auth mode"
```

---

## Task 8: Built-in sign-in page (UI/UX)

**Files:**
- Create: `apps/dashboard/src/components/builtin-sign-in.tsx`
- Test: `apps/dashboard/src/components/builtin-sign-in.test.tsx`
- Modify: `apps/dashboard/src/app/sign-in/[[...sign-in]]/page.tsx`

**Interfaces:**
- Consumes: `useSession().login` (Task 6), shadcn `ui/{card,input,button}`.
- Produces: `<BuiltinSignIn />` — a self-contained sign-in card.

> **Design step:** Before Step 3, invoke the `frontend-design` skill for the visual pass — a centered card on `bg-bg`, product mark (`Activity` lucide icon in `text-hit`) + `turbo-cache-forge` wordmark, `username` + `password` fields (password with a show/hide toggle), a full-width submit button with a spinner while submitting, an inline error row, and a `self-hosted · built-in auth` footnote. Use the existing design tokens (`bg-bg`, `surface`, `border`, `text`, `muted`) and fonts. The code below is the functional baseline; keep its states and copy, elevate the styling.

- [ ] **Step 1: Write the failing component tests**

Create `apps/dashboard/src/components/builtin-sign-in.test.tsx`:

```tsx
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

const login = vi.fn();
vi.mock("@/app/session", () => ({ useSession: () => ({ mode: "builtin", login }) }));
const replace = vi.fn();
vi.mock("next/navigation", () => ({ useRouter: () => ({ replace }) }));

import { BuiltinSignIn } from "./builtin-sign-in";

afterEach(() => {
  login.mockReset();
  replace.mockReset();
});

function submit() {
  fireEvent.change(screen.getByLabelText(/username/i), { target: { value: "root" } });
  fireEvent.change(screen.getByLabelText(/password/i), { target: { value: "hunter2" } });
  fireEvent.click(screen.getByRole("button", { name: /sign in/i }));
}

describe("BuiltinSignIn", () => {
  it("logs in and redirects to / on success", async () => {
    login.mockResolvedValueOnce(undefined);
    render(<BuiltinSignIn />);
    submit();
    await waitFor(() => expect(login).toHaveBeenCalledWith("root", "hunter2"));
    await waitFor(() => expect(replace).toHaveBeenCalledWith("/"));
  });

  it("shows an inline error on failure", async () => {
    login.mockRejectedValueOnce(new Error("Invalid username or password"));
    render(<BuiltinSignIn />);
    submit();
    expect(await screen.findByText(/invalid username or password/i)).toBeInTheDocument();
    expect(replace).not.toHaveBeenCalled();
  });
});
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd apps/dashboard && pnpm vitest run src/components/builtin-sign-in.test.tsx`
Expected: FAIL (cannot resolve `./builtin-sign-in`).

- [ ] **Step 3: Implement the component (functional baseline; elevate via frontend-design)**

Create `apps/dashboard/src/components/builtin-sign-in.tsx`:

```tsx
"use client";
import { Activity, Eye, EyeOff, Loader2 } from "lucide-react";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { useSession } from "@/app/session";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";

export function BuiltinSignIn() {
  const { login } = useSession();
  const router = useRouter();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [show, setShow] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    setSubmitting(true);
    try {
      await login?.(username, password);
      router.replace("/");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Sign-in failed. Try again.");
      setSubmitting(false);
    }
  }

  return (
    <div className="grid min-h-screen place-items-center bg-bg p-4">
      <Card className="w-full max-w-sm p-8">
        <div className="mb-6 flex items-center gap-2">
          <Activity className="h-5 w-5 text-hit" aria-hidden />
          <span className="text-lg font-semibold tracking-tight text-text">turbo-cache-forge</span>
        </div>
        <form onSubmit={onSubmit} className="flex flex-col gap-4">
          <div className="flex flex-col gap-1.5">
            <label htmlFor="username" className="text-sm text-muted">Username</label>
            <Input
              id="username"
              autoComplete="username"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              required
              autoFocus
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <label htmlFor="password" className="text-sm text-muted">Password</label>
            <div className="relative">
              <Input
                id="password"
                type={show ? "text" : "password"}
                autoComplete="current-password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                required
                className="pr-10"
              />
              <button
                type="button"
                onClick={() => setShow((s) => !s)}
                aria-label={show ? "Hide password" : "Show password"}
                className="absolute inset-y-0 right-0 grid w-10 place-items-center text-muted hover:text-text"
              >
                {show ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
              </button>
            </div>
          </div>
          {error && (
            <p role="alert" className="text-sm text-miss">
              {error}
            </p>
          )}
          <Button type="submit" disabled={submitting} className="w-full">
            {submitting && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
            Sign in
          </Button>
        </form>
        <p className="mt-6 text-center text-xs text-muted">self-hosted · built-in auth</p>
      </Card>
    </div>
  );
}
```

> Note: if the design system has no `text-miss` token, use `text-red-500`. Verify against `apps/dashboard/src/app/globals.css` during the frontend-design step.

- [ ] **Step 4: Render it from the sign-in route in builtin mode**

Replace `apps/dashboard/src/app/sign-in/[[...sign-in]]/page.tsx`:

```tsx
"use client";
import { SignIn } from "@clerk/nextjs";
import { BuiltinSignIn } from "@/components/builtin-sign-in";
import { useSession } from "@/app/session";

export default function Page() {
  const { mode } = useSession();
  if (mode === "builtin") return <BuiltinSignIn />;
  return (
    <div className="grid min-h-screen place-items-center bg-bg">
      <SignIn />
    </div>
  );
}
```

- [ ] **Step 5: Run tests + typecheck to verify pass**

Run: `cd apps/dashboard && pnpm vitest run src/components/builtin-sign-in.test.tsx && pnpm tsc --noEmit`
Expected: PASS, no type errors.

- [ ] **Step 6: Full dashboard check**

Run: `cd apps/dashboard && pnpm lint && pnpm vitest run`
Expected: PASS (whole dashboard suite green).

- [ ] **Step 7: Commit**

```bash
git add apps/dashboard/src/components/builtin-sign-in.tsx apps/dashboard/src/components/builtin-sign-in.test.tsx "apps/dashboard/src/app/sign-in/[[...sign-in]]/page.tsx"
git commit -m "feat(dashboard): built-in username/password sign-in page"
```

---

## Final verification (whole feature)

- [ ] **Backend:** `cd services/api && go build ./... && go vet ./... && go test ./internal/config/ ./internal/localauth/ ./internal/server/ -run 'TestLoad|TestSign|TestVerify|TestLogin|TestMiddleware|TestAuth'`
- [ ] **Import boundary:** `cd services/api && go list -deps ./internal/turbo/ ./internal/auth/ | grep -c 'internal/localauth'` prints `0`.
- [ ] **Dashboard:** `cd apps/dashboard && pnpm tsc --noEmit && pnpm lint && pnpm vitest run`.
- [ ] **Manual end-to-end (optional, needs the stack):** boot with `AUTH_MODE=builtin AUTH_ROOT_USERNAME=root AUTH_ROOT_PASSWORD=change-me AUTH_SECRET=$(openssl rand -hex 32)` + a migrated Postgres, open the dashboard, confirm the built-in sign-in page renders, sign in, mint a token, sign out.
```
