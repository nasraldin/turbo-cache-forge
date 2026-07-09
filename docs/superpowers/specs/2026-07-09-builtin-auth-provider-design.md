# Built-in "root user" auth provider — design

**Date:** 2026-07-09
**Status:** Approved (brainstorming); pending implementation plan

## Problem

Today the dashboard authenticates humans only through an external OIDC IdP
(Clerk in practice; Keycloak/ZITADEL/Auth.js also supported). Every developer who
wants to run turbo-cache-forge on their own localhost must first stand up and
configure an auth provider. That is friction for a self-hosted tool.

We want a second, **built-in** auth provider that needs no external IdP: the
operator sets an initial **root user** (username + password) via config and the
dashboard presents a first-party sign-in page. This mirrors how professional
open-source projects (Grafana, GitLab, SonarQube) bootstrap a first admin.

## Decisions (locked during brainstorming)

1. **Single root user only.** One admin identity from config. No `users` table,
   no invitations. Can grow to multi-user later; out of scope now.
2. **Exclusive provider mode.** A single `AUTH_MODE` selects `oidc` **or**
   `builtin` — never both at once. The two auth worlds stay import-separated,
   preserving the ROADMAP invariant ("two auth worlds, never mixed").
3. **Runtime detection by the dashboard.** The Go server is the source of truth;
   the dashboard fetches `GET /api/v1/auth/config` on load and renders the right
   sign-in UI. Flipping mode = change one server env + restart, no dashboard
   rebuild.
4. **First-party JWT session.** A new `internal/localauth` package mints and
   verifies a short-lived HS256 JWT in-process. The dashboard stores it and the
   already provider-agnostic `@tcf/api-client` sends it as `Authorization:
   Bearer` — no api-client change, CLI cache-token path unaffected.

Built-in mode is inherently single-tenant (one root user = one org), so it runs
as the existing **personal / no-org** case.

## Non-goals

- Multi-user management, roles, invitations, password reset flows.
- The CLI's human `login` (OIDC device flow). Cache tokens (`api_keys`) are
  minted in the dashboard and remain independent hashed bearer tokens, so the
  Turborepo cache path keeps working in builtin mode without CLI human-login.
- Coexisting providers / "sign in with password OR SSO" on one page.

## Backend design

### Config (`services/api/internal/config/config.go`)

New fields on `Config`, read in `Load()`. All builtin-only vars are read
regardless of mode but only **required/validated** when `AUTH_MODE=builtin`.

| Env | Meaning | Default |
|-----|---------|---------|
| `AUTH_MODE` | `oidc` \| `builtin` | `oidc` |
| `AUTH_ROOT_USERNAME` | root identity | — (required in builtin) |
| `AUTH_ROOT_PASSWORD` | plaintext root password (bcrypt-hashed in memory at boot) | — |
| `AUTH_ROOT_PASSWORD_HASH` | pre-computed bcrypt hash (alternative to plaintext) | — |
| `AUTH_SECRET` | HS256 signing secret | random per-boot + warning if unset |
| `AUTH_TOKEN_TTL` | session JWT lifetime | `12h` |

Validation in `Load()` when `AUTH_MODE=builtin`:
- `AUTH_ROOT_USERNAME` must be set.
- Exactly one of `AUTH_ROOT_PASSWORD` / `AUTH_ROOT_PASSWORD_HASH` must be set.
- If `AUTH_SECRET` is empty, generate a cryptographically-random secret at boot
  and log a warning that tokens will not survive a restart or span replicas.
- `AUTH_MODE` must be one of the two known values; anything else is a fatal
  config error.

`OIDC_*` vars keep their current meaning and are only consulted when
`AUTH_MODE=oidc`.

### New package `internal/localauth`

Parallel to `internal/oidcauth`. Mounted **only** on `/api/v1`. Imports no auth
vendor SDK — only `golang.org/x/crypto/bcrypt` and a JWT lib (HS256). The cache
path (`internal/auth`, `internal/turbo`) must never import it.

Surface:

- `type Config { RootUsername string; PasswordHash []byte; Secret []byte; TTL time.Duration }`
- `New(cfg Config, repo OrgProvisioner) (*Authenticator, error)` — reuses the
  same `OrgProvisioner` interface (`EnsureOrgByIdpID`) that `oidcauth` uses.
- `(*Authenticator).Login(username, password string) (token string, expiresAt time.Time, err error)`
  - constant-time bcrypt compare against `PasswordHash`; unknown username and
    wrong password are indistinguishable (same error, same timing shape).
  - on success mints HS256 JWT with claims `{ iss:"turbo-cache-forge",
    sub:"local:root", username, iat, exp }`.
- `(*Authenticator).Middleware(next) http.Handler`
  - reads `Authorization: Bearer <jwt>`, verifies signature + `exp` (+ `iss`).
  - resolves the single tenant via `repo.EnsureOrgByIdpID(sub, username)` — same
    provisioning path personal-mode OIDC already uses (`sub` = stable tenant
    key, `username` = display name).
  - injects the org with `auth.WithOrg`. Identical context contract to
    `oidcauth`, so `mgmt/handlers.go` needs **no changes**.

### Endpoints & router (`internal/server/router.go`)

Restructure so `/api/v1` is mounted when **either** provider is configured
(today it mounts only when `OIDC_ISSUER != ""`).

- `GET  /api/v1/auth/config` — **public** → `{ "mode": "builtin"|"oidc", "org_enabled": bool }`.
  Lets the dashboard branch at runtime. (snake_case, per repo convention.)
- `POST /api/v1/auth/login` — **public**, builtin mode only → body
  `{ "username", "password" }` → `{ "token", "expires_at" }`; bad creds → 401.
  In oidc mode this route is **not registered** (→ 404); the dashboard learns
  from `/auth/config` never to call it.
- Authenticated management group: `pr.Use(<active provider>.Middleware)` where
  the active provider is `localauth` or `oidcauth` per `AUTH_MODE`.

CORS: the login endpoint is browser-called, so the dashboard origin must be in
`CORS_ALLOWED_ORIGINS` (existing mechanism, no change).

### Wiring (`services/api/cmd/server/main.go`)

Branch on `cfg.AuthMode`:
- `builtin` → build `localauth.New(...)`, pass to `server.Deps`. Log
  `management API enabled at /api/v1 — BUILTIN MODE: root user <username>`.
- `oidc` → today's `oidcauth.New(...)` path, unchanged.

`server.Deps` gains a provider-agnostic auth handle (an interface both
authenticators satisfy: `Middleware(http.Handler) http.Handler`) plus, in
builtin mode, a reference used to serve `/auth/login`. Keep the seam small.

### Data model

No new tables. Root creds live in env. The tenant reuses the existing
`organizations` upsert on `idp_org_id` (here `idp_org_id = "local:root"` via
`EnsureOrgByIdpID`; `orgSlugFor` already hashes it into a slug).

## Dashboard design

### Runtime auth abstraction

Introduce a `useSession()` seam returning `{ getToken(): Promise<string|null>,
signOut(), user: { username|email } }`. Chosen at runtime after fetching
`/api/v1/auth/config`:

- **oidc** → wraps Clerk. `<ClerkProvider>` is mounted **only** in this branch,
  so builtin deployments need no Clerk publishable key at all.
- **builtin** → a small token store: `login(username,password)` POSTs to
  `/api/v1/auth/login`, saves the JWT in `localStorage`; `getToken()` returns it
  (and treats an expired token as signed-out); `signOut()` clears it.

`useApiClient` (`apps/dashboard/src/app/api.ts`) calls `useSession().getToken()`
instead of Clerk's `getToken()` directly. `@tcf/api-client` is unchanged — it
already just wants a token string.

Because mode is only known at runtime, the app is **built to support both** and
selects on boot. A brief loading gate covers the `/api/v1/auth/config` fetch
before the app shell renders.

### Route protection (`apps/dashboard/src/middleware.ts`)

`clerkMiddleware` runs only when Clerk env is present (oidc). In builtin mode the
middleware is a pass-through and the `(dashboard)` layout guards client-side: no
valid token → redirect to `/sign-in`. Public routes stay `/sign-in`,
`/sign-up`.

### Logged-in chrome (`apps/dashboard/src/app/(dashboard)/layout.tsx`)

Clerk `<UserButton>` / `<OrganizationSwitcher>` render only in oidc mode. In
builtin mode show the root `username` and a plain **Sign out** action. (Org
switcher already hidden when org mode is off; builtin is always no-org.)

### Sign-in page (`apps/dashboard/src/app/sign-in/...`)

A purpose-built page (not Clerk's widget), shown in builtin mode:

- Centered card on `bg-bg`; product mark + name at top.
- Fields: **username**, **password** (with show/hide toggle).
- Submit button with a loading state; disabled while submitting.
- Inline error on 401 ("Invalid username or password").
- Subtle footnote: "self-hosted · built-in auth".
- Consistent with the existing design system: Inter / JetBrains Mono, shadcn
  `components/ui/*` (`Card`, `Input`, `Button`), `bg-bg` tokens.
- States to cover: idle, submitting, error, success→redirect to `/`.

In oidc mode this route continues to render Clerk's `<SignIn/>` (unchanged).

Visual polish will be produced with the `frontend-design` skill during
implementation; this spec fixes the layout intent, fields, and states.

## Security considerations

- Passwords: bcrypt hashing; constant-time compare; unknown-username and
  wrong-password are indistinguishable.
- Session JWT: short TTL (default 12h), HS256 with `AUTH_SECRET`; **no refresh
  token** — expiry means re-login. Ephemeral random secret when unset (with a
  loud warning) so a bare `AUTH_MODE=builtin` still boots for quick local use.
- Startup fails fast if builtin is enabled without a username or password.
- `localStorage` bearer storage is an accepted SPA trade-off for a self-hosted
  single-user tool; short TTL limits exposure. Documented, not hidden.
- Optional (nice-to-have, not required for v1): a lightweight in-memory
  login-attempt throttle on `/api/v1/auth/login`.
- Two auth worlds stay import-separated: `localauth` mounts only on `/api/v1`;
  the cache path never imports it. Verify with the existing `go list` import
  check pattern.

## Testing

**Go (`internal/localauth`):**
- Table tests: mint→verify round-trip; expired token rejected; bad signature
  rejected; wrong password rejected; unknown username rejected; missing/blank
  bearer → 401; token missing tenant → 401.
- Router integration: a minted token reaches a real mgmt handler (e.g.
  `GET /api/v1/stats`) and returns 200; a garbage token returns 401;
  `GET /api/v1/auth/config` returns the right mode unauthenticated.
- Config: `Load()` validation cases (builtin without username / without
  password; unknown `AUTH_MODE`; ephemeral-secret warning path).

**Dashboard:**
- `useSession` builtin store: login saves token, `getToken` returns it, expired
  token reads as signed-out, `signOut` clears it.
- Sign-in page states: idle → submitting → 401 error; success redirect.
- Make the existing Clerk-oriented Playwright e2e mode-aware (guard so it only
  runs in oidc mode; add a builtin happy-path login where feasible).

## Rollout / docs

- `.env.example` (root) and `apps/dashboard/.env.example`: document `AUTH_MODE`
  and the `AUTH_ROOT_*` / `AUTH_SECRET` / `AUTH_TOKEN_TTL` vars, with a builtin
  quickstart block.
- README quickstart: add a "run locally with built-in auth (no IdP)" path.
- ROADMAP: note the built-in provider as an addition that upholds the
  two-auth-worlds invariant.

## Open questions

None blocking. The login throttle is explicitly optional for v1.
