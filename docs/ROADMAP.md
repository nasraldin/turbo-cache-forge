# turbo-cache-forge — Roadmap & Status

Canonical status doc. **Read this first** before working on any phase. It tracks *what / why / status* for the whole project; each phase's *how* (task-by-task TDD steps) lives in a per-phase plan under `docs/superpowers/plans/`, generated at that phase's kickoff.

- **Design/spec:** [`docs/superpowers/specs/2026-07-08-turbo-cache-forge-design.md`](superpowers/specs/2026-07-08-turbo-cache-forge-design.md)
- **Repo:** github.com/nasraldin/turbo-cache-forge

---

## Status dashboard

| Phase | Title | Status | Plan | Notes |
|------|-------|--------|------|-------|
| 1 | Cache API MVP | ✅ **Done** | [phase-1 plan](superpowers/plans/2026-07-08-turbo-cache-forge-phase1.md) | PR #1 merged → `main` (`1e90bfa`). Live `docker compose up` MISS→HIT proven. |
| 2 | Concurrency & heavy-cache hardening | ⬜ **Next** | _generate at kickoff_ | Absorbs most Phase-1 follow-ups below. |
| 3 | Management API + OIDC/JWT | ⬜ Planned | _tbd_ | Depends on Phase 2. |
| 4 | Dashboard | ⬜ Planned | _tbd_ | Depends on Phase 3 (`/api/v1`). |
| 5 | CLI (`turbo-cache`) | ⬜ Planned | _tbd_ | Thin client over `/api/v1`. |
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
- **Tokens stored only as SHA-256 hex** — plaintext shown once at creation, never logged.
- **Tenant isolation is layered** — `org_slug/hash` key + `validHash` boundary check + DB `CHECK(slug ~ '^[a-z0-9-]+$')` + `UNIQUE(org_id, hash)`. Any new key-building path must validate both sides.
- **One metrics pipeline** — Prometheus. OTel is tracing-only, a no-op unless `OTEL_EXPORTER_OTLP_ENDPOINT` is set.
- **Storage via the `storage.Storage` interface only** — no direct disk/S3 calls in handlers. New backends must pass `storagetest.Run`.
- **Toolchain:** Go 1.24 (raised from 1.23 for aws-sdk-go-v2); pgx pinned v5.7.6 for go1.24 compat.
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

## Phase 2 — Concurrency & heavy-cache hardening ⬜ NEXT

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

## Phase 3 — Management API (`/api/v1`) + OIDC/JWT ⬜ Planned

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

## Phase 4 — Dashboard ⬜ Planned

**Goal:** a focused management + observability UI over `/api/v1`.

**Scope / deliverables:** Next.js 15 + TS + Tailwind + shadcn/ui + TanStack Query + Clerk login (browser only). Pages: Overview (storage / hit rate / requests), Projects, Cache Statistics, Artifacts, API Keys (create/revoke), Team Members, Storage Usage, Settings; Billing stubbed. Charts: plain numbers first, a couple of ECharts panels only where a trend genuinely helps. Deployable via `docker compose` with `NEXT_PUBLIC_API_URL`.

**Dependencies:** Phase 3 (`/api/v1` is the only backend the dashboard talks to — never storage directly).

**Definition of Done:** log in via Clerk, see live stats from `/api/v1`, create + revoke a token from the UI and confirm it works/stops working on the cache path.

**Open questions:** hosting (Cloudflare Pages vs Vercel)? How much charting in v1?

---

## Phase 5 — CLI (`turbo-cache`) ⬜ Planned

**Goal:** first-class self-host ergonomics.

**Scope / deliverables:** `login` (OIDC device flow), `token create`, `project create`, `stats`, `doctor` (checks config + connectivity). Thin client over `/api/v1`.

**Dependencies:** Phase 3.

**Definition of Done:** `turbo-cache doctor` diagnoses a misconfigured self-host; `token create` yields a working `TURBO_TOKEN`.

---

## North star (deferred — build only on measured need)

cache analytics → cache policies (retention/quota *enforcement*) → distributed / multi-region cache → enterprise (SSO, audit logs, real billing). Redis hot-metadata/rate-limit and presigned-URL egress offload slot in here when the single-node API becomes the ceiling. **Do not build any of this speculatively.**

---

## Phase-1 follow-up backlog (non-blocking; pull into Phase 2)

Logged during Phase 1 review, approved as non-blocking. Check off as addressed.

- [ ] **Assert metric counter values** in turbo tests (`testutil.ToFloat64`) — currently only HTTP status is asserted, so a swapped hit/miss counter wouldn't be caught.
- [ ] **S3 `New`: skip static creds when both keys empty** — currently always overrides the SDK default credential chain; fine for R2/MinIO, breaks AWS IAM-role deployments.
- [ ] **Pin `goose`** in the Docker `goose` build stage (currently `@latest` + `GOTOOLCHAIN=auto`) — for reproducible/offline builds.
- [ ] **GET `io.ReaderFrom`/sendfile fast path** — the metrics `statusWriter` wrapper drops it; streaming still correct, minor throughput cost.
- [ ] **PUT store→metadata non-atomicity** — a failed `UpsertArtifact` after a successful store leaves an orphan object (self-heals on Turbo retry). Add a `// ponytail:` note or a small repair.
- [ ] **`token.go`** — restore the ponytail rationale comment on why constant-time compare isn't needed (doc gap).
- [ ] **`s3.go`** — dedupe the `ContentLength` size extraction in `Get`/`Head` (cosmetic).
- [ ] **`.env.example`** — separate the compose-only vars (`POSTGRES_*`) from the bare-metal `cache-api` vars to reduce confusion.
- [ ] **Indexes** on `cache_artifacts.project_id` / `api_keys.project_id` — add when a query filters by project (Phase 3+).
- [ ] _Won't-fix unless it bites:_ `fs.path`'s `strings.Contains(key,"..")` is broader than a segment-aware check (harmless behind `validHash`); handler `org` nil-guard (theoretical — `RequireToken` always populates it).

_Already fixed in Phase 1 (not pending): slug `CHECK` constraint, `.env`/seed-snippet docs, HEAD error-handling consistency._
