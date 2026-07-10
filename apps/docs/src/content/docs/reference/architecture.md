---
title: Architecture
description: The two build worlds, the request paths, the data stores, and the invariants that keep them separate.
---

Turbo Cache Forge is deliberately split into **two decoupled build worlds** — Go and
TypeScript — that communicate only over HTTP.

## The big picture

```
                    ┌─────────────────────────────────────────────┐
   turbo CLI ──────▶│  Cache API   /v8/artifacts/*                 │
  (hashed bearer)   │  (streaming, hashed-bearer auth)             │
                    │                                              │
   Dashboard ──────▶│  Management API   /api/v1/*                  │──▶ Postgres
   (session JWT)    │  (OIDC/JWT or built-in session)             │    (metadata)
                    │                                              │
   turbo-cache CLI ▶│                              services/api    │──▶ Storage
   (session/OIDC)   └─────────────────────────────────────────────┘    (fs | S3)
```

- **`services/api`** (Go, chi router) — one server, two route trees. The cache path
  streams artifact blobs; the management path serves the dashboard and CLI.
- **`apps/dashboard`** (Next.js 15, React 19) — talks **only** to `/api/v1`.
- **`services/cli`** (Go, its own module) — talks to `/api/v1`; imports nothing from
  the API's storage or DB.
- **Postgres** — metadata only (orgs, tokens, projects, usage rollups).
- **Storage** — artifact blobs on the [filesystem or S3](/turbo-cache-forge/guides/storage-backends/).

## Repository layout

| Path | Contents |
|---|---|
| `services/api/` | Go cache + management API (own module) |
| `services/cli/` | `turbo-cache` CLI (own module, stdlib HTTP only) |
| `apps/dashboard/` | Next.js dashboard |
| `apps/docs/` | This documentation site (Astro Starlight) |
| `packages/types`, `packages/api-client` | Shared TS types + `/api/v1` client |
| `infra/docker/` | `Dockerfile` (multi-target) + `docker-compose.yml` |
| `infra/migrations/` | goose SQL migrations |

## Two toolchains, on purpose

The repo is a **pnpm + Turborepo** workspace (dashboard, docs, packages) alongside
**independent Go modules** (`services/api`, `services/cli`). They share no build. This
is why CI runs two separate jobs — a Go matrix and a JS job — rather than one.

## Invariants (never regress these)

These are the load-bearing boundaries of the system:

1. **The dashboard talks only to `/api/v1`** — never to storage, the database, or the
   cache path (`/v8`).
2. **The CLI is its own module** and imports no `services/api` storage or DB code.
3. **Two auth worlds stay separate** — the cache path uses a hashed bearer token; the
   management path uses an OIDC/JWT or built-in session. Neither credential works on the
   other path.
4. **Tokens are stored only hashed** — the plaintext is shown once at creation.
5. **The cache hot path is streaming-only** — artifact bodies are never buffered whole.

Because each unit has one job and a well-defined interface, you can change the internals
of one without breaking the others — and reason about each in isolation.

## Images

The multi-stage `infra/docker/Dockerfile` produces several targets from one build:

- **`cache-api`** — the Go server, distroless, non-root (uid `65532`), port `8080`.
- **`goose`** — a self-contained migrator that applies `infra/migrations/*.sql`.

The dashboard has its own `apps/dashboard/Dockerfile` (Node 20, Next.js standalone
output, port `3000`). CI publishes all three to Docker Hub — see
[Contributing](/turbo-cache-forge/project/contributing/).
