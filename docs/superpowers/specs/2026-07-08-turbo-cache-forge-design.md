# Turborepo Remote Cache — Self-hosted, SaaS-ready

## Context

We want a self-hosted Turborepo Remote Cache that our own projects (dev machines + CI)
point at today, and that can become a multi-tenant SaaS later **without a rewrite**.

The core product is small: Turborepo's remote-cache protocol is ~6 HTTP endpoints, and the
CLI authenticates with a **static bearer token** scoped by team. The hot path is
*validate token → stream bytes to/from object storage*. Everything else (dashboard, orgs,
billing, workers) is scaffolding layered around those endpoints.

Decisions locked with the user:
- **Goal:** internal now, structured for a clean SaaS path (`org_id` everywhere, hashed
  tokens, quota columns present but unenforced).
- **Storage:** a `Storage` interface with two backends — `filesystem` (single-node, homelab,
  tests: **no MinIO needed**) and `s3` (`aws-sdk-go-v2`, so R2 / S3 / B2 swap by env var).
- **Auth is vendor-neutral:** the Go API validates **JWTs against a JWKS URL** (OIDC). Works
  with Clerk, Keycloak, ZITADEL, Auth.js — the backend never imports a vendor SDK. Dashboard
  may still use Clerk.
- **Observability from v1:** native Prometheus `/metrics` + health endpoints. OpenTelemetry
  **tracing** wired as a no-op unless `OTEL_EXPORTER_OTLP_ENDPOINT` is set.
- **Dashboard:** full **management** dashboard is the goal, but the **cache API ships and is
  proven first**.

Non-goals for v1 (YAGNI — add when measured need appears): Redis, Temporal/queues,
multi-region, billing, GraphQL, and a standalone `turbo-cache` CLI (roadmap Phase 3). A plain
SQL cron covers cleanup until traffic says otherwise. **One metrics pipeline** (Prometheus) —
OTel is tracing only, so we don't run two.

---

## Architecture

```
 Turbo CLI (dev + CI)                 Humans (browser)
        │ Bearer TURBO_TOKEN                 │ Clerk login (dashboard ONLY)
        ▼                                     ▼
 ┌─────────────────────┐            ┌──────────────────────┐
 │  Go cache API (Fly) │◄───────────│ Next.js dashboard    │
 │  chi + net/http     │  read-only │ (Cloudflare Pages)   │
 │                     │  JSON API  └──────────────────────┘
 │  hot path:          │
 │  token→stream bytes │──────► S3 API (R2 default / MinIO self-host)
 │  meta (async)       │──────► Postgres (Neon / local) : orgs, tokens, artifact meta
 └─────────────────────┘
```

**Two separate auth worlds — do not mix them:**
- **CLI → cache API:** static hashed bearer token. No JWT, no OIDC, no vendor SDK.
- **Human → dashboard → Management API:** the browser holds an OIDC session (Clerk by
  default). The Go API validates the **JWT against a JWKS URL** (`OIDC_JWKS_URL` /
  `OIDC_ISSUER` env) — provider-agnostic (Clerk / Keycloak / ZITADEL / Auth.js). The JWT's
  org claim maps to `organizations.clerk_org_id` (rename → `idp_org_id`; the clean SaaS seam).
  **The Go backend imports no auth-vendor SDK.**

---

## Cache API (the whole product)

Turborepo v8 protocol. Base path `/v8/artifacts`. All scoped by `?teamId=` or `?slug=`.

| Method | Path | Behavior |
|---|---|---|
| `GET`  | `/v8/artifacts/status` | `{"status":"enabled"}` |
| `HEAD` | `/v8/artifacts/:hash` | 200 if exists, 404 if not (cheap existence check) |
| `PUT`  | `/v8/artifacts/:hash` | Stream request body → object storage. Store meta + `x-artifact-tag` if sent. `Content-Length` honored; body size capped per quota. |
| `GET`  | `/v8/artifacts/:hash` | Stream object storage → response. Echo `x-artifact-tag`. Async best-effort `last_accessed` bump (NOT on the blocking path). |
| `POST` | `/v8/artifacts` | Batch existence query → `{ hashes: {...} }` |
| `POST` | `/v8/artifacts/events` | Telemetry sink. No-op 200 (log-only). |

### Streaming & heavy-cache correctness (the user's explicit requirement)
- **Never buffer whole tarballs in memory.** Use `aws-sdk-go-v2` `manager.Uploader` (auto
  multipart, streaming) for PUT; `GetObject` body piped straight to the ResponseWriter for GET.
- **Content-addressed = idempotent.** Concurrent uploads of the same hash are identical bytes;
  last-write-wins is safe. No locking needed.
- **DB off the download hot path.** GET does: auth → `HeadObject`/stream. The `last_accessed`
  update is fire-and-forget (buffered channel + batched writer), so a slow DB never slows a
  cache hit. `// ponytail: fire-and-forget meta; if we ever need exact access counts, swap for a batched flush`
- **Bandwidth escape hatch (documented, not built in v1):** if the Go box's egress becomes
  the bottleneck, switch GET to a **presigned S3 URL redirect** so bytes bypass our server
  entirely. R2 has no egress fees, so proxy-streaming is fine to start.
  `// ponytail: proxy-stream now; presigned-redirect when egress on the API node is the ceiling`
- Server timeouts tuned for large bodies; `MaxBytesReader` enforces per-request cap.

### Auth (cache path)
- `Authorization: Bearer <token>` → SHA-256 the presented token → lookup `api_keys.token_hash`
  → resolve `org_id` → verify `teamId/slug` belongs to that org. Constant-time compare.
- Tokens shown **once** at creation (`turbo_` prefix + random); only the hash is stored.

---

## Data model (Postgres, `org_id` on everything)

```sql
organizations   (id, idp_org_id, slug, name, plan, storage_limit_bytes, created_at)  -- idp_org_id = OIDC org claim
projects        (id, org_id → organizations, slug, name, created_at)   -- unique(org_id, slug)
api_keys        (id, org_id, project_id NULL, name, token_hash UNIQUE, last_used_at, created_at, revoked_at)
cache_artifacts (id, org_id, project_id, hash, size_bytes, artifact_tag NULL,
                 created_at, last_accessed_at)   -- unique(org_id, hash)
usage_daily     (org_id, day, bytes_uploaded, bytes_downloaded, hits, misses)  -- rollup for dashboard
```
- Migrations: `goose` (plain SQL, dead simple, self-host friendly).
- Query layer: `pgx` + `sqlc` (typed queries from SQL, no ORM). Falls on the "already-solves-it"
  rung — no hand-rolled scanning.
- Quota columns (`storage_limit_bytes`) exist now, enforcement is a v-next flag. SaaS seam ready.

## Storage (pluggable interface, two backends)
```go
type Storage interface {
    Put(ctx, key string, r io.Reader, size int64) error   // streaming, no full buffer
    Get(ctx, key string) (io.ReadCloser, int64, error)
    Head(ctx, key string) (exists bool, size int64, err error)
    Delete(ctx, key string) error
}
```
- `internal/storage/filesystem` — writes under a root dir. **No MinIO, no cloud** — for local
  dev, tests, homelab, Raspberry Pi. `STORAGE_BACKEND=fs`, `STORAGE_PATH=/var/lib/turbo-cache`.
- `internal/storage/s3` — `aws-sdk-go-v2` + `manager.Uploader` (auto-multipart streaming).
  `STORAGE_BACKEND=s3` + endpoint/creds env → R2 / S3 / B2 all identical.
- Key layout (both backends): `{org_slug}/{project_slug}/{hash}` — isolation by prefix/dir.

---

## Repo structure (Turborepo monorepo)

```
turbo-cache/
  services/api/            Go: cmd/server, internal/{turbo,auth,storage,db,usage,obs,config}
                           storage/{filesystem,s3}   obs/{metrics,tracing,health}
  apps/dashboard/          Next.js 15 + TS + Tailwind + shadcn/ui + TanStack Query + Clerk
  packages/{types,api-client}/   shared TS types + typed dashboard SDK
  infra/docker/            docker-compose (api + postgres) — fs backend, zero cloud, no MinIO
  infra/migrations/        goose SQL
```

### Endpoint namespaces
- `/v8/artifacts/*` — **Turbo protocol, path dictated by the CLI.** `TURBO_API` is the base
  URL the client appends to. `// ponytail: verify whether a given Turbo version honors a
  base-path prefix before promising /turbo/v8 — until then, mount where the CLI calls.`
- `/api/v1/*` — Management API (orgs, projects, tokens, stats). Versioned independently so
  Turbo-protocol changes never break the dashboard contract.
- `/metrics` `/live` `/ready` `/health` — unversioned ops endpoints.

### Observability (v1)
- **Metrics — native Prometheus** (`prometheus/client_golang`), one pipeline:
  `cache_hits_total`, `cache_misses_total`, `upload_bytes_total`, `download_bytes_total`,
  `request_duration_seconds` (histogram), `storage_bytes` (gauge). Scrape `/metrics`.
- **Tracing — OpenTelemetry**, initialized as a **no-op** unless `OTEL_EXPORTER_OTLP_ENDPOINT`
  is set. `otelhttp` on the router; spans on storage + DB calls only. Grafana/Jaeger/SigNoz
  plug in with zero code change. `// ponytail: tracing seam only — no per-function spans in v1`
- **Health:** `/live` (process), `/ready` (DB + storage reachable), `/health` (aggregate).
- Sentry stays for panic/error capture (complements traces, not a metrics system).
- Go framework: **chi** (thin over net/http; Go 1.22 routing is close, chi adds middleware
  ergonomics without weight). No Fiber — stdlib-compatible handlers win for testability.
- Config: env vars only (`STORAGE_ENDPOINT`, `STORAGE_BUCKET`, `DATABASE_URL`, `CLERK_*`,
  `SENTRY_DSN`). `.env.example` documents every one.
- `docker compose up` → working cache against local Postgres + MinIO, zero cloud accounts.

---

## Build sequence (each phase independently useful)

**Phase 1 — Cache API MVP (the product).**
`status`/`HEAD`/`PUT`/`GET` + bearer-token auth + streaming to storage. Postgres for tokens +
artifact meta. `docker-compose` with MinIO. **Done when:** a real Turborepo repo caches against
it (`TURBO_API` / `TURBO_TOKEN` / `TURBO_TEAM`) and a second machine gets a cache hit.

**Phase 2 — Concurrency + heavy-cache hardening.**
Batch `POST /v8/artifacts`, `events` sink, async `last_accessed`, `MaxBytesReader` caps,
multipart streaming verified with large artifacts, Sentry on panics/storage/db errors,
concurrent-upload load test. **Done when:** N parallel CI jobs hammer it without memory blowup
or errors.

**Phase 3 — Management API (`/api/v1`) + cleanup.**
OIDC/JWT-protected: create/revoke tokens, create projects/orgs, `usage_daily` rollup, stats.
LRU/TTL cleanup as a `DELETE WHERE last_accessed < …` cron (in-process ticker, not a queue).
OIDC org-claim → `organizations.idp_org_id` linkage.

**Phase 4 — Dashboard.**
Next.js + Clerk login (browser only). Consumes `/api/v1`: Overview (storage / hit rate /
requests), Projects, Cache Statistics, Artifacts, API Keys (create/revoke — writes),
Team Members, Storage Usage, Settings. Billing page stubbed. Charts: plain numbers first, a
couple of ECharts panels only where a trend genuinely helps.

**Phase 5 — CLI (`turbo-cache`).** `login` (OIDC device flow), `token create`, `project
create`, `stats`, `doctor` (checks config + connectivity). Thin client over `/api/v1`.

**North-star roadmap (deferred, build only on measured need):** cache analytics → cache
policies (retention/quota enforcement) → distributed/multi-region cache → enterprise (SSO,
audit logs, billing). Redis hot-metadata/rate-limit and presigned-URL egress offload slot in
here when the single-node API becomes the ceiling.

---

## Verification

- **Protocol conformance:** point an actual Turborepo project at the server; confirm miss →
  upload → (second machine) hit, via `turbo run build --remote-only` and cache-status output.
- **Streaming/memory:** upload a multi-hundred-MB artifact; watch API RSS stays flat (proves no
  full buffering).
- **Concurrency:** parallel `PUT`/`GET` of the same and different hashes from multiple clients;
  no corruption, no errors, idempotent same-hash writes.
- **Auth isolation:** token for org A cannot read org B's `teamId`; revoked token → 401.
- **Self-host:** `docker compose up` on a clean box (fs backend, Postgres, **no MinIO**) yields
  a working cache with zero cloud creds.
- **Storage parity:** the same `Storage` interface tests run against both `filesystem` and `s3`
  (MinIO container) backends — identical behavior.
- **Auth:** table tests on token auth (hash match, constant-time, revoked, wrong-team) and JWT
  validation (good/expired/wrong-issuer/wrong-JWKS) for the Management API.
- **Ops endpoints:** `/metrics` exposes the counters; `/ready` flips to 503 when DB or storage
  is down; a trace exports only when `OTEL_EXPORTER_OTLP_ENDPOINT` is set.

## Locked decisions
- **Name / module:** `turbo-cache-forge` → `github.com/<org>/turbo-cache-forge`, Go module
  `github.com/<org>/turbo-cache-forge/services/api`. (User's call; the Turborepo-association /
  trademark risk was raised and consciously accepted — revisit before a public SaaS launch.)
- **Phase 1 DB:** **local Postgres in docker-compose** (no external accounts). Neon later is a
  `DATABASE_URL` swap, no code change.
- **Storage:** `filesystem` backend first, `s3` second (same interface).

## Still open (non-blocking, decide during build)
- Git remote / `<org>` for the module path.
- Which OIDC provider for the dashboard first (Clerk default) — only affects `OIDC_ISSUER`/`OIDC_JWKS_URL`.
- **Verify** whether your target Turbo CLI version honors a base-path prefix on `TURBO_API`
  (decides if the protocol can live under `/turbo/v8` or must stay at `/v8/artifacts`).
