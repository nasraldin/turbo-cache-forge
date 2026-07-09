# turbo-cache-forge — Roadmap & Status

Canonical status doc. **Read this first** before working on any phase. It tracks *what / why / status* for the whole project; each phase's *how* (task-by-task TDD steps) lives in a per-phase plan under `docs/superpowers/plans/`, generated at that phase's kickoff.

- **Design/spec:** [`docs/superpowers/specs/2026-07-08-turbo-cache-forge-design.md`](superpowers/specs/2026-07-08-turbo-cache-forge-design.md)
- **Repo:** github.com/nasraldin/turbo-cache-forge
- **➡️ Current status, how to run locally, and next options:** [`docs/HANDOFF.md`](./HANDOFF.md) — **all 5 phases Done & merged (2026-07-09).**

---

## Status dashboard

| Phase | Title | Status | Plan | Notes |
|------|-------|--------|------|-------|
| 1 | Cache API MVP | ✅ **Done** | [phase-1 plan](superpowers/plans/2026-07-08-turbo-cache-forge-phase1.md) | PR #1 merged → `main` (`1e90bfa`). Live `docker compose up` MISS→HIT proven. |
| 2 | Concurrency & heavy-cache hardening | ✅ **Done** | [phase-2 plan](superpowers/plans/2026-07-08-phase-2-hardening.md) | 10 tasks on branch `phase-2-hardening`. Load test flat-memory proven, batch endpoint, OTel+Sentry opt-in seams, backlog cleared. |
| 3 | Management API + OIDC/JWT | ✅ **Done** | [phase-3 plan](superpowers/plans/2026-07-08-phase-3-management-api-oidc.md) | 10 tasks on branch `phase-3-management-api-oidc`. `/api/v1` (OIDC), usage rollup, cleanup cron, OpenAPI. Live DB + hermetic JWKS verified. |
| 4 | Dashboard | ✅ **Done** | [phase-4 plan](superpowers/plans/2026-07-08-phase-4-dashboard.md) | 11 tasks on branch `phase-4-dashboard`. Next.js 15 monorepo, 9 pages, hit-meter signature, thin `/api/v1/stats/timeseries` + ECharts trend, gated Playwright e2e, Docker image. Final whole-branch review: READY TO MERGE, all 6 cross-phase invariants hold. |
| 5 | CLI (`turbo-cache`) | ✅ **Done** | [phase-5 plan](superpowers/plans/2026-07-08-phase-5-cli.md) | 9 tasks on branch `phase-5-cli`. Own Go module, `login` (OIDC device flow) / `token create` / `project create` / `stats` / `doctor`. Stdlib-only HTTP client, compile-time boundary from storage/DB. Final whole-branch review: READY TO MERGE, all 6 cross-phase invariants hold. |
| — | North star | 💤 Deferred | — | analytics → policies → distributed → enterprise. |

Legend: ✅ done · 🟡 in progress · ⬜ not started · 💤 deferred (no near-term work).

### How to work a phase
1. Read this doc + the design spec + the previous phase's plan.
2. Run `superpowers:brainstorming` if scope is fuzzy, then `superpowers:writing-plans` to produce `docs/superpowers/plans/<date>-phase-N-*.md` (task-by-task, TDD, like Phase 1).
3. Execute with `superpowers:subagent-driven-development` (per-task review + final whole-branch review).
4. **Update this dashboard + the phase's status + check off its Definition-of-Done** when it lands.

---

## Cross-phase invariants (must hold in EVERY phase — do not regress)

These were established in Phase 1 and are load-bearing. Any change that violates one is a defect.

- **Streaming only** — never buffer a whole artifact in memory (`io.Copy` / `manager.Uploader`). The only `io.ReadAll` of a body is in test fakes.
- **DB off the download hot path** — `last_accessed` and similar are fire-and-forget on a *detached* context; a slow DB never slows a cache hit.
- **Two auth worlds, never mixed** — the CLI cache path is hashed-bearer-token only and imports NO auth-vendor SDK. OIDC/JWT (Phase 3) is for dashboard humans only.
- **Built-in auth provider** — `AUTH_MODE=builtin` runs a single root user (username/password → first-party HS256 JWT) as an alternative to OIDC. It upholds "two auth worlds": `internal/localauth` mounts only on `/api/v1`; the cache path never imports it. Exclusive with `oidc`.
- **Tokens stored only as SHA-256 hex** — plaintext shown once at creation, never logged.
- **Tenant isolation is layered** — `org_slug/hash` key + `validHash` boundary check + DB `CHECK(slug ~ '^[a-z0-9-]+$')` + `UNIQUE(org_id, hash)`. Any new key-building path must validate both sides.
- **One metrics pipeline** — Prometheus. OTel is tracing-only, a no-op unless `OTEL_EXPORTER_OTLP_ENDPOINT` is set.
- **Storage via the `storage.Storage` interface only** — no direct disk/S3 calls in handlers. New backends must pass `storagetest.Run`.
- **Toolchain:** Go 1.25 (raised from 1.24 for aws-sdk-go-v2); pgx no longer pinned — go1.24-compat pin removed now that the module floor is 1.25.
- **SaaS-ready shape:** `org_id` on every table; quota columns present but unenforced until a phase turns them on.

---

## Phase 1 — Cache API MVP ✅ DONE

**Shipped (PR #1, merged `1e90bfa`).** Full detail: the [phase-1 plan](superpowers/plans/2026-07-08-turbo-cache-forge-phase1.md).

- chi server + `/live` `/ready` `/health`; env config loader.
- `storage.Storage` interface + `filesystem` (default, zero-cloud) and `s3` (R2/S3/B2) backends behind a shared conformance suite.
- Postgres schema (goose) + pgx repository (orgs, hashed tokens, artifact metadata).
- Bearer-token auth (sha256 hash) + middleware.
- Turborepo v8 handlers: `status` / `HEAD` / `PUT` / `GET` / `events`, with `validHash` boundary validation.
- Prometheus `/metrics` + full router/main wiring.
- Docker self-host: non-root distroless (uid 65532), self-contained goose migrate, Postgres unexposed, pinned tags.

**Definition of Done — all met:** ✅ real `docker compose up` MISS→HIT roundtrip · ✅ `/ready` 200 against real Postgres · ✅ hostile hash → 400 · ✅ unauth → 401 · ✅ streaming/isolation/off-hot-path verified in the final whole-branch review (no Critical findings).

---

## Phase 2 — Concurrency & heavy-cache hardening ✅ DONE

**Shipped on branch `phase-2-hardening` (10 tasks, per-task + final whole-branch review, no Critical findings).** Full detail: the [phase-2 plan](superpowers/plans/2026-07-08-phase-2-hardening.md).

**Definition of Done — all met:** ✅ parallel load test passes with flat memory (`-race`; ~67 KiB heap growth on a 200 MiB artifact vs a 32 MiB ceiling) · ✅ batch endpoint `POST /v8/artifacts` returns a correct existence map (a `curl` client stands in for a real Turbo client — stock Turbo v8 has no documented batch-exists call; genuine CLI integration is future work) · ✅ a trace exports only when `OTEL_EXPORTER_OTLP_ENDPOINT` is set (OTel is tracing-only — no second metrics pipeline) · ✅ Sentry reports 5xx + panics only when `SENTRY_DSN` is set · ✅ Phase-1 follow-up backlog cleared or explicitly re-deferred (below). Final review confirmed the GET sendfile fast path survives the otelhttp+sentryhttp middleware stack.

**Goal:** make the cache survive real parallel CI load and large artifacts, and clear the Phase-1 follow-up backlog. No new product surface — this is depth on what exists.

**Scope / deliverables:**
- **Batch existence endpoint** `POST /v8/artifacts` → `{ hashes: {<hash>: {size, ...}} }`. This is the intended consumer of the already-present `MetaRepo.ArtifactExists` (kept in Phase 1 for exactly this).
- **Load test** — N parallel `PUT`/`GET` of same and different hashes from multiple clients; assert no memory blowup (flat RSS on multi-hundred-MB artifacts), no corruption, idempotent same-hash writes.
- **`MaxBytesReader` cap** verified end-to-end (413 on oversize) under concurrency.
- **OTel tracing seam** — initialize a no-op tracer provider unless `OTEL_EXPORTER_OTLP_ENDPOINT` is set; spans on storage + DB calls only. (Metrics stay Prometheus — do NOT add a second metrics pipeline.)
- **Sentry** wired for panics / storage / DB errors (backend).
- **Clear the follow-up backlog** (see list at bottom) — especially: assert metric counter values, S3 default-cred-chain fallback, pin goose, `io.ReaderFrom` fast-path for GET, PUT store→metadata note/repair.

**Dependencies:** none beyond Phase 1.

**Definition of Done:** parallel load test passes with flat memory; batch endpoint returns correct existence map and a real Turbo client uses it; a trace exports only when the OTLP env var is set; the Phase-1 follow-up backlog is either done or explicitly re-deferred with reasoning.

**Open questions:** target concurrency/artifact-size numbers for the load test? Sentry DSN/account?

---

## Phase 3 — Management API (`/api/v1`) + OIDC/JWT ✅ DONE

**Shipped on branch `phase-3-management-api-oidc` (10 tasks, per-task + final whole-branch review, no Critical findings).** Full detail: the [phase-3 plan](superpowers/plans/2026-07-08-phase-3-management-api-oidc.md).

**Definition of Done — met:** ✅ JWT trust boundary enforced (go-oidc: signature+issuer+audience+expiry, no `Skip*`, empty-audience fails closed) — proven hermetically with real RS256 JWTs against a real JWKS (Task 4 table tests + Task 10 router integration test); a real-Keycloak e2e is documented (`infra/docker/docker-compose.keycloak.yml` + acceptance steps) as the manual verification · ✅ token create→use→revoke is a single org-bound scheme (plaintext once, hash stored; cross-org read structurally impossible) · ✅ cleanup cron (object-then-row, idempotent, batched) · ✅ usage rollup feeds `/api/v1/stats` (in-memory on the hot path, ticker to `usage_daily`; re-absorbs deltas on partial write failure) · ✅ OpenAPI + Swagger UI served · ✅ live Postgres 16 used for migration 002 round-trip + repo + router integration tests. Two auth worlds stay import-separated (`go list` verified: cache path is SDK-free).

**Open questions resolved:** IdP is provider-agnostic (go-oidc/JWKS; Keycloak used for the e2e harness — no vendor SDK). Quota *enforcement* stays OFF (columns exist, unenforced) — deferred to North star.

**Goal:** let humans (and later the dashboard/CLI) manage orgs, projects, and tokens over an authenticated, versioned API — without coupling the backend to any auth vendor.

**Scope / deliverables:**
- **OIDC/JWT middleware** — validate JWTs against a JWKS URL (`OIDC_ISSUER` / `OIDC_JWKS_URL`); provider-agnostic (Clerk / Keycloak / ZITADEL / Auth.js). Backend imports NO vendor SDK. JWT org claim → `organizations.idp_org_id`.
- **`/api/v1` endpoints** (JWT-protected): create/revoke tokens (return plaintext once), create projects/orgs, read stats (storage used, hit/miss, requests), list artifacts.
- **Usage rollup** — `usage_daily` table + a rollup job feeding stats.
- **Cleanup** — LRU/TTL as an in-process ticker cron (`DELETE WHERE last_accessed < …`), NOT a queue.
- **OpenAPI/Swagger** for `/api/v1` and the Turbo protocol.

**Dependencies:** Phase 2 (stable core). Keeps the Turbo protocol at `/v8/artifacts` (client-dictated); versioning lives on `/api/v1` only.

**Definition of Done:** a JWT from at least one real IdP authenticates `/api/v1`; token create/revoke works end-to-end (revoked token → 401 on the cache path); cleanup cron removes expired artifacts; stats endpoints return real numbers; OpenAPI served.

**Open questions:** which IdP first (Clerk default)? Quota *enforcement* on/off in this phase (columns already exist)?

---

## Phase 4 — Dashboard ✅ DONE

**Goal:** a focused management + observability UI over `/api/v1`.

**Scope / deliverables:** Next.js 15 + TS + Tailwind 4 + hand-written shadcn-style primitives + TanStack Query v5 + Clerk login (browser only), in a pnpm+Turborepo monorepo (`apps/dashboard`, `packages/types`, `packages/api-client`). Nine pages: Overview (hit-meter + storage/requests/work-saved tiles), Projects, Cache Statistics (one ECharts hit/miss trend), Artifacts (offset-paginated), API Keys (create shows plaintext once / revoke), Team (Clerk), Storage Usage, Settings; Billing stubbed. Deployable via `docker compose` with `NEXT_PUBLIC_API_URL`.

**Dependencies:** Phase 3 (`/api/v1` is the only backend the dashboard talks to — never storage directly).

**Definition of Done — met:** ✅ Clerk browser login → JWT is the sole `/api/v1` auth (`useApiClient` bridge; no bare fetch) · ✅ live stats from `/api/v1` on Overview/Statistics/Storage with genuine loading/error/empty states (error names `NEXT_PUBLIC_API_URL`) · ✅ token create shows the plaintext **once** (dialog-local state, wiped on close, never re-fetched/cached), revoke flips status to Revoked · ✅ new thin `GET /api/v1/stats/timeseries` (org-scoped, `parseClampedInt` days, off the cache hot path) feeds the one ECharts trend · ✅ dashboard talks **only** to `/api/v1` (grep-verified: no `/v8`, storage, DB, localStorage) — the two auth worlds stay separate · ✅ snake_case end-to-end (Go tags → `@tcf/types` → components) · ✅ `docker build` succeeds with `NEXT_PUBLIC_*` (incl. Clerk publishable key) baked as build ARGs; `/sign-in` serves. Gated Playwright token-lifecycle e2e authored (login→stats→create→revoke), run on `E2E_CLERK_*` + a live stack. Final whole-branch review (opus): all 6 cross-phase invariants hold, no blocking findings.

**First-live-boot check (carried):** Clerk's default session token must carry the org claim the Phase-3 API reads (`OrgFromContext`); if not, add a JWT template — every `/api/v1` call 401s otherwise despite a valid login.

**Follow-ups (non-blocking):** `aria-label` on the Projects create-form inputs; per-row (not global) revoke pending state; `generate_series` gap-fill for silent days in the trend.

---

## Phase 5 — CLI (`turbo-cache`) ✅ DONE

**Goal:** first-class self-host ergonomics.

**Scope / deliverables:** `login` (RFC 8628 OIDC device flow), `token create`, `project create`, `stats`, `doctor` (checks config + connectivity). Thin stdlib-`net/http` client over `/api/v1`, in its OWN Go module (`services/cli`, separate go.mod) — the module boundary makes "never touches storage/DB" a compile-time fact.

**Dependencies:** Phase 3 (`/api/v1`). The plan's "provisional API shapes" were reconciled against the now-shipped Phase 3/4 endpoints (snake_case `Stats`, `{slug,name}` project body, plaintext-once token).

**Definition of Done — met:** ✅ `turbo-cache doctor` diagnoses a misconfigured self-host — reports config-file (0600) / API-URL / server-reachable / auth independently, bounded 5s client (can't hang), and reports a malformed API URL as a failed check rather than panicking · ✅ `token create` prints a plaintext `TURBO_TOKEN` shown once and never persisted by the CLI (hermetically verified against `httptest`; the live cache-path PUT/GET is the documented end-to-end check once a real IdP is wired) · ✅ two tokens never confused: OIDC JWT (from `login`) stored 0600 for `/api/v1`, hashed cache token printed once for Turborepo · ✅ config precedence (flag>env>file) resolved once via `config.Pick`/`resolveClient` · ✅ `login` is the only IdP-touching command; every other command talks only to the server · ✅ cross-compiles for linux/darwin/windows × amd64/arm64 (goreleaser + plain `go build`; version ldflags injection proven). Final whole-branch review: all 6 cross-phase invariants hold, no blocking findings.

**Follow-ups (non-blocking, next small PR):** wire `signal.NotifyContext` in `main.go` + make `oidcdevice.PollToken`'s sleep context-aware (Ctrl-C during `login`); `url.JoinPath` in `Discover`; recognize a `TURBO_CACHE_TOKEN` shape mismatch in `doctor`.

---

## North star (deferred — build only on measured need)

cache analytics → cache policies (retention/quota *enforcement*) → distributed / multi-region cache → enterprise (SSO, audit logs, real billing). Redis hot-metadata/rate-limit and presigned-URL egress offload slot in here when the single-node API becomes the ceiling. **Do not build any of this speculatively.**

---

## Phase-1 follow-up backlog (non-blocking; pull into Phase 2)

Logged during Phase 1 review, approved as non-blocking. Check off as addressed.

- [x] **Assert metric counter values** in turbo tests (`testutil.ToFloat64`) — **done, Phase 2 Task 3.**
- [x] **S3 `New`: skip static creds when both keys empty** — **done, Phase 2 Task 2a** (`credentialOptions` falls back to the SDK default chain).
- [x] **Pin `goose`** in the Docker `goose` build stage — **done, Phase 2 Task 1** (pinned `v3.27.2`, dropped `GOTOOLCHAIN=auto`).
- [x] **GET `io.ReaderFrom`/sendfile fast path** — **done, Phase 2 Task 4** (`statusWriter.ReadFrom` passthrough; final review confirmed it survives the otelhttp/sentryhttp stack).
- [x] **PUT store→metadata non-atomicity** — **done, Phase 2 Task 5** (eager best-effort compensating `Delete`; chosen over a repair-sweep note because the orphan is untrackable, not inaccessible).
- [x] **`token.go`** — restore the ponytail rationale comment — **done, Phase 2 Task 2c.**
- [x] **`s3.go`** — dedupe the `ContentLength` size extraction in `Get`/`Head` — **done, Phase 2 Task 2b.**
- [x] **`.env.example`** — separate compose-only vs bare-metal vars — **already separated; Phase 2 Task 10 added the OTel/Sentry opt-in block.**
- [ ] **Indexes** on `cache_artifacts.project_id` / `api_keys.project_id` — **re-deferred to Phase 3** (no Phase 2 query filters by `project_id`; add when `/api/v1` introduces one).
- [ ] _Won't-fix unless it bites:_ `fs.path`'s `strings.Contains(key,"..")` is broader than a segment-aware check (harmless behind `validHash`); handler `org` nil-guard (theoretical — `RequireToken` always populates it).

_Already fixed in Phase 1 (not pending): slug `CHECK` constraint, `.env`/seed-snippet docs, HEAD error-handling consistency._
