# turbo-cache-forge — Handoff & Current Status

_Last updated: 2026-07-09._ Companion to [ROADMAP.md](./ROADMAP.md) (the canonical what/why/status). This doc is the **operational** picture: where the work stands, how to run it locally, and what to do next.

## TL;DR

**All five planned phases are ✅ Done and merged to `main`.** The phased roadmap is complete; the only remaining roadmap item is **North star**, which is deferred ("build only on measured need"). There is no Phase 6 yet — starting one is a deliberate decision, not a default.

| Phase | What | Merged |
|---|---|---|
| 1 | Cache API MVP (`/v8/artifacts`, streaming, Postgres) | PR #1 |
| 2 | Concurrency & heavy-cache hardening | (branch merged) |
| 3 | Management API `/api/v1` + OIDC/JWT, usage rollup, cleanup, OpenAPI | (branch merged) |
| 4 | Dashboard (Next.js 15 monorepo, 9 pages, ECharts trend, Docker) | PR #8 |
| 5 | CLI `turbo-cache` (own Go module, login/token/project/stats/doctor) | PR #10 |

## Repo layout

- `services/api/` — Go cache + management API (own module). Cache path `/v8/artifacts/*` (hashed bearer). Management `/api/v1/*` (OIDC/JWT) — mounted only when `OIDC_ISSUER` is set.
- `apps/dashboard/` + `packages/{types,api-client}` — Next.js 15 dashboard monorepo (pnpm + Turborepo). Talks ONLY to `/api/v1` via a Clerk session JWT.
- `services/cli/` — `turbo-cache` CLI (own module, stdlib HTTP only). Commands: `login` (OIDC device flow), `token create`, `project create`, `stats`, `doctor`.
- `infra/docker/` — `docker-compose.yml` (postgres, migrate, cache-api, dashboard) + `Dockerfile` (multi-target).

## Running it locally

**Env files (git-ignored — hold the Clerk secret; never commit):**
- `infra/docker/.env` — used by docker-compose (Postgres creds, `NEXT_PUBLIC_API_URL`, Clerk keys).
- `apps/dashboard/.env.local` — used by `pnpm --filter dashboard dev` (same values).

Both are seeded with the Clerk instance `loving-toucan-71`. (The publishable key was corrected from a pasted value that had a stray trailing `z`; the canonical key is `pk_test_bG92aW5nLXRvdWNhbi03MS5jbGVyay5hY2NvdW50cy5kZXYk`. If Clerk login misbehaves, copy the exact keys from the Clerk dashboard.)

**Full stack (Docker):**
```bash
docker compose -f infra/docker/docker-compose.yml --env-file infra/docker/.env up -d --build
# Dashboard  → http://localhost:3000   (Clerk login works with the real keys)
# Cache/mgmt API → http://localhost:8080  ( /live, /ready, /v8/artifacts/* )
docker compose -f infra/docker/docker-compose.yml down        # stop
```

**Dashboard only (faster iteration):**
```bash
pnpm install
pnpm --filter dashboard dev        # http://localhost:3000, reads apps/dashboard/.env.local
```

**CLI:**
```bash
cd services/cli && go build -o /tmp/turbo-cache ./cmd/turbo-cache
/tmp/turbo-cache doctor --api http://localhost:8080     # diagnoses config/connectivity/auth
```

### What works out of the box
- Dashboard renders; **Clerk login works** with the real keys; all 9 pages are navigable; loading/empty/error states are exercised.
- Cache path `/v8/artifacts/*` works with a hashed bearer token.
- CLI builds and `doctor` runs against the API.

### The last mile: live `/api/v1` data in the dashboard
The default compose brings the API up **without** OIDC, so `/api/v1` is not mounted and the dashboard's data calls fail into their error states (by design — safe boot). To get **live** stats/projects/tokens/artifacts in the dashboard, three things must line up (this is the carried "first-live-boot" caveat):

1. **Backend OIDC** — set on the `cache-api` service (env): `OIDC_ISSUER=https://loving-toucan-71.clerk.accounts.dev`, `OIDC_AUDIENCE=<your-audience>`, `OIDC_ORG_CLAIM=org_id`. (When `OIDC_ISSUER` is set, `OIDC_AUDIENCE` is required; the API does OIDC discovery at boot — if it can't reach Clerk it will `log.Fatal`, so only enable once reachable.)
2. **Clerk JWT template** — in the Clerk dashboard, create a JWT template whose `aud` claim equals `OIDC_AUDIENCE`. Clerk auto-includes `org_id`/`org_slug`/`org_role` when the user has an **active organization**, so the user must belong to a Clerk org (the backend JIT-creates its org row on first valid call).
3. **Dashboard token** — `apps/dashboard/src/app/api.ts` currently calls `getToken()` (default session token, which has no matching `aud`). Change it to `getToken({ template: "<your-template>" })` so the audience/claims match what the backend verifies.

Until all three are in place, the API rejects the default session token (audience fails closed → 401) and the dashboard shows its error state. This is a small, well-scoped task — happy to implement the dashboard side and wire the compose OIDC env on request.

## Non-blocking follow-ups (from the Phase 4 & 5 final reviews)

- **CLI Ctrl-C:** wire `signal.NotifyContext` in `services/cli/cmd/turbo-cache/main.go` + make `oidcdevice.PollToken`'s sleep context-aware (so Ctrl-C during `login` exits promptly). Latent today (ctx never cancelled).
- **Dashboard a11y:** the Projects create-form inputs rely on placeholder-as-label — add `aria-label`/`<label>`.
- **CLI polish:** `url.JoinPath` in `oidcdevice.Discover`; recognize a `TURBO_CACHE_TOKEN` shape mismatch in `doctor`; the "provisional" doc comment in `apiclient` is stale.
- **Dashboard:** per-row (not global) revoke pending state on API Keys; clipboard `.catch` on token copy.

## Options for the next session

1. **Cleanup PR** — bundle the non-blocking follow-ups above (most valuable: CLI Ctrl-C handling + Projects a11y).
2. **Close the live-data last mile** — implement the dashboard `getToken({template})` change + wire compose OIDC env, and document the Clerk JWT-template/org steps (needs a Clerk-dashboard action from the owner).
3. **Scope a North-star thread into a real Phase 6** — e.g. usage analytics, quota enforcement, or distributed storage. Brainstorm → plan first (don't build speculatively).

## Process notes for the next agent

- Phases were executed with **superpowers:subagent-driven-development** (fresh implementer per task → task review → fix loop → final whole-branch review), then finished with **finishing-a-development-branch** (PR + merge).
- The durable SDD ledger lives at `.superpowers/sdd/progress.md` (git-ignored scratch; `git clean -fdx` destroys it — recover from `git log`). It currently holds the Phase 5 record.
- Cross-phase invariants that must never regress: dashboard talks ONLY to `/api/v1` (never storage/DB/`/v8`); the CLI is its own module and imports no `services/api`/storage/DB; two auth worlds stay separate (cache = hashed bearer, management = OIDC/JWT); tokens stored only hashed, plaintext shown once; streaming-only on the cache hot path.
