---
title: What is Turbo Cache Forge?
description: The problem a Turborepo remote cache solves, and how Turbo Cache Forge implements it self-hosted.
---

Turbo Cache Forge is a **self-hosted remote cache server for [Turborepo](https://turborepo.com)**.
It implements the Turbo **API v8** protocol (`/v8/artifacts/*`), so the standard
`turbo` CLI can push and pull build artifacts to a server you run yourself — a
drop-in alternative to the hosted Vercel Remote Cache, with no cloud account.

## The problem it solves

Turborepo caches the output of each task (build, test, lint) keyed by a hash of its
inputs. On one machine that cache is local. A **remote cache** shares those outputs
across machines — so when CI, or a teammate, runs a task whose inputs haven't
changed, the result is downloaded instead of recomputed. Builds that took minutes
collapse to seconds.

The hosted remote cache requires a Vercel account and bills per seat. If you would
rather keep artifacts on your own infrastructure — for cost, data residency, or
air-gapped environments — you host the cache yourself. That is what this project is.

## What you get

Turbo Cache Forge is four surfaces around one Go server:

| Surface | What it is | Who talks to it |
|---|---|---|
| **Cache API** | The Turbo v8 protocol (`/v8/artifacts/*`), hashed-bearer auth | The `turbo` CLI |
| **Management API** | `/api/v1` — tokens, projects, stats, artifacts | The dashboard & CLI |
| **Dashboard** | Next.js console: hit rate, storage, trends, artifacts | Humans (browser) |
| **CLI** | `turbo-cache` — login, token/project create, stats, doctor | Operators (terminal) |

Metadata (organizations, tokens, projects, usage) lives in **SQLite by default —
zero setup, no external database** — or **Postgres** when you need multi-node
scale. Artifact blobs go to a **pluggable storage backend**: the local filesystem
by default, or any S3-compatible object store (AWS S3, Cloudflare R2, MinIO).

## What it is not

- **Not a Turborepo replacement.** You still use the normal `turbo` CLI; this is only
  the cache server it talks to.
- **Not multi-region or globally distributed** out of the box. It is a single
  self-hosted server (which is all most teams need). Distributed storage is a
  deferred roadmap item, not a shipped feature.

## Next steps

- [How it compares](/turbo-cache-forge/getting-started/comparison/) — vs. Vercel Remote Cache and ducktors, honestly.
- [Quickstart](/turbo-cache-forge/getting-started/quickstart/) — bring the whole stack up with Docker Compose.
- [Connect Turborepo](/turbo-cache-forge/guides/connect-turborepo/) — point your monorepo at it.
- [Architecture](/turbo-cache-forge/reference/architecture/) — how the pieces fit and the invariants that hold them apart.
