---
title: How it compares
description: An honest comparison of Turbo Cache Forge with Vercel Remote Cache and the ducktors/turborepo-remote-cache project.
---

There's more than one way to give Turborepo a remote cache. This page compares Turbo
Cache Forge with the two most relevant options, honestly — including where they beat us.

All three speak the **same open Turbo v8 protocol**, so switching between them is just a
matter of changing `TURBO_API` and your token. You're never locked in.

## The three options at a glance

- **[Vercel Remote Cache](https://turborepo.dev/docs/core-concepts/remote-caching)** — the
  official **hosted** cache. Zero setup, free under fair use, fully managed. You don't run
  anything, and you don't control where artifacts live.
- **[ducktors/turborepo-remote-cache](https://github.com/ducktors/turborepo-remote-cache)** —
  the mature, popular **self-hosted** OSS server (TypeScript/Fastify). Battle-tested,
  API-only, broad cloud-storage support.
- **Turbo Cache Forge** (this project) — a **self-hosted** OSS server (Go) that adds a
  **web dashboard, observability, and a management API + CLI** on top of the cache.

## Feature comparison

| | **Turbo Cache Forge** | **Vercel Remote Cache** | **ducktors** |
|---|:--:|:--:|:--:|
| Hosting model | Self-hosted | Hosted SaaS | Self-hosted |
| Turbo v8 protocol | ✅ | ✅ (official) | ✅ |
| Cost | Free (OSS) + your infra | Free (fair use) | Free (OSS) + your infra |
| Data ownership | Full — your infra | Vercel-managed¹ | Full — your infra |
| Setup effort | `docker compose up` | None (zero-config) | Deploy the server |
| Storage backends | Filesystem, S3-compatible (S3/R2/MinIO) | Managed | Filesystem, **S3, GCS, Azure**, S3-compatible |
| Web dashboard | ✅ hit rate, trends, artifact browser | Account/token only | ❌ API-only |
| Observability | ✅ Prometheus + OpenTelemetry + Sentry | Managed | Logging only |
| Human auth | Built-in password **or** OIDC/JWT | Vercel account | — |
| Cache-token auth | Hashed bearer tokens | Vercel tokens | Static tokens / JWT / none |
| Artifact signing | ❌ | Managed | ✅ |
| Read-only tokens | ❌ | — | ✅ |
| Management API + CLI | ✅ `/api/v1` + `turbo-cache` CLI | Vercel dashboard/API | ❌ |
| Metadata store | Postgres (projects, usage, orgs) | Managed | None required |
| Language | Go (distroless image) | — (SaaS) | TypeScript / Fastify |
| License | MIT | Proprietary service | MIT |
| Maturity | New (2026) | Official, mature | Mature (~1.5k★, since 2021) |

<small>¹ Vercel's docs don't specify the storage region/location of cached artifacts.</small>

## When to choose which

Pick the tool that matches what you actually need — not the one with the most checkmarks.

### Choose **Vercel Remote Cache** if…
- You don't need to self-host and want **zero operational overhead**.
- You're fine with a managed service holding your artifacts.
- It's **free under fair use** and requires no infrastructure — genuinely the easiest path
  if data residency isn't a concern.

### Choose **ducktors/turborepo-remote-cache** if…
- You want a **mature, battle-tested** self-hosted server (years in production, large
  community, frequent releases).
- You need **native Google Cloud Storage or Azure Blob** backends, **artifact signing**, or
  **read-only tokens** — all of which it has and Turbo Cache Forge does not.
- You want the **simplest possible** self-hosted server with **no database** to run.

### Choose **Turbo Cache Forge** if…
- You want to **self-host** *and* get a **web dashboard** — live hit rate, a daily trend
  chart, and an artifact browser. Neither other option has this.
- You care about **observability**: Prometheus metrics, OpenTelemetry tracing, and Sentry
  error reporting are built in (ducktors offers logging only).
- You want a **management API and CLI** to script tokens, projects, and stats
  (`/api/v1` + `turbo-cache`).
- You value a **Postgres-backed model** with projects, per-day usage rollups, and a
  multi-tenant organization structure.

## Honest caveats

Turbo Cache Forge is the **newest** of the three. ducktors has years of production use and a
larger community; if maturity is your top priority, that matters. We also **don't** yet have
artifact signing, read-only tokens, or native GCS/Azure backends — if any of those are
must-haves, ducktors is the better fit today. And if you don't need to self-host at all,
Vercel's free managed cache is hard to beat on convenience.

Where Turbo Cache Forge is distinctly ahead is the **operator experience** — the dashboard,
the metrics, and the management API/CLI. If you self-host and want visibility into your
cache instead of a black box, that's the gap it fills.

---

<small>Comparison compiled 2026-07-10 from each project's official documentation and
repository. Facts about other tools can change — if something here is out of date, please
[open an issue or PR](https://github.com/nasraldin/turbo-cache-forge/issues).</small>
