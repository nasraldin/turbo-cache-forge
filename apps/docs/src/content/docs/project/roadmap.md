---
title: Roadmap
description: What's shipped and what's deferred. All five planned phases are complete and merged.
---

Turbo Cache Forge was built in five phases, **all complete and merged to `main`**
(2026-07-09). The canonical status lives in [`docs/ROADMAP.md`](https://github.com/nasraldin/turbo-cache-forge/blob/main/docs/ROADMAP.md)
and [`docs/HANDOFF.md`](https://github.com/nasraldin/turbo-cache-forge/blob/main/docs/HANDOFF.md).

## Shipped

| Phase | Title | What landed |
|---|---|---|
| 1 | Cache API MVP | `/v8/artifacts` streaming protocol, storage interface (fs + S3), Postgres, hashed-bearer auth, Prometheus, distroless Docker |
| 2 | Concurrency & heavy-cache hardening | Flat-memory load test, batch existence endpoint, OTel + Sentry opt-in seams |
| 3 | Management API + OIDC/JWT | `/api/v1`, usage rollup, cleanup cron, OpenAPI/Swagger |
| 4 | Dashboard | Next.js 15 console, hit-meter, `/stats/timeseries` + ECharts trend, gated Playwright e2e |
| 5 | CLI (`turbo-cache`) | Own Go module: `login`, `token create`, `project create`, `stats`, `doctor` |

## Deferred (North star)

Built only on measured need, not speculatively:

- **Usage analytics** beyond the current hit/miss rollups.
- **Quota enforcement** — the schema already carries `org_id` and quota columns; they
  are present but unenforced until a phase turns them on.
- **Distributed storage** / multi-region.
- **Enterprise** concerns (SSO org management, richer RBAC).

## Cross-phase invariants

Several boundaries are load-bearing and must never regress — streaming-only cache hot
path, the DB off the download path, two separate auth worlds, tokens stored only as
SHA-256, and layered tenant isolation. They're documented in
[Architecture](/turbo-cache-forge/reference/architecture/) and enforced in review.
