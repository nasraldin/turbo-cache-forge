---
title: Quickstart
description: Bring up Postgres, the cache API, and the dashboard with a single Docker Compose command.
---

The fastest way to run the full stack — Postgres, migrations, the cache API, and the
dashboard — is Docker Compose.

## Prerequisites

- **Docker** with Compose v2 (`docker compose version`).
- Ports **8080** (API) and **3000** (dashboard) free on the host.

## 1. Start the stack

```bash
git clone https://github.com/nasraldin/turbo-cache-forge.git
cd turbo-cache-forge
docker compose -f infra/docker/docker-compose.yml up -d --build
```

This starts four services:

| Service | Port | Role |
|---|---|---|
| `postgres` | (internal only) | metadata store |
| `migrate` | — | applies the schema, then exits |
| `cache-api` | **8080** | Turbo v8 cache + `/api/v1` management API |
| `dashboard` | **3000** | Next.js web console |

Postgres is **not** published to the host — only `cache-api` and `migrate` reach it
over the Compose network. The API runs as a non-root user (uid `65532`) in a
distroless image and uses the filesystem storage backend (`STORAGE_BACKEND=fs`) by
default.

:::note[Docker Compose and `.env`]
Compose auto-loads `.env` from the **compose file's directory** (`infra/docker/`),
not the repo root. To change Postgres credentials or auth, put them in
`infra/docker/.env` (copy [`.env.example`](/turbo-cache-forge/getting-started/configuration/)),
or pass `--env-file`.
:::

## 2. Sign in to the dashboard

The default compose config uses **built-in auth** (`AUTH_MODE=builtin`) with a single
root user (`root` / `root`). Open **http://localhost:3000** and sign in.

You'll land on the Overview with a live hit rate, storage usage, and request counts.
See the [dashboard tour](/turbo-cache-forge/guides/dashboard/) for every screen.

:::caution
`root` / `root` is a **local-dev default**. Before exposing the server anywhere,
set `AUTH_ROOT_USERNAME`, `AUTH_ROOT_PASSWORD`, and a random `AUTH_SECRET` — see
[Authentication](/turbo-cache-forge/guides/authentication/).
:::

## 3. Mint a cache token

In the dashboard, go to **API Keys → New token**. The plaintext token is shown
**once** — copy it. That token is what Turborepo will send on the cache path.

## 4. Point Turborepo at it

```bash
export TURBO_API=http://localhost:8080
export TURBO_TOKEN=<your-token>
export TURBO_TEAM=root          # your organization slug
turbo run build --remote-only
```

Run it twice: the first run uploads (MISS), the second downloads (HIT). Full
walkthrough in [Connect Turborepo](/turbo-cache-forge/guides/connect-turborepo/).

## 5. Tear down

```bash
docker compose -f infra/docker/docker-compose.yml down -v   # -v also removes cached artifacts
```

## Prefer prebuilt images?

Every push to `main` publishes images to **both Docker Hub and GitHub Container
Registry (ghcr.io)** — pull from whichever you prefer:

| Image | Docker Hub | GitHub Container Registry |
|---|---|---|
| API | `nasraldin/turbo-cache-forge-api` | `ghcr.io/nasraldin/turbo-cache-forge-api` |
| Migrator | `nasraldin/turbo-cache-forge-migrate` | `ghcr.io/nasraldin/turbo-cache-forge-migrate` |
| Dashboard | `nasraldin/turbo-cache-forge-dashboard` | `ghcr.io/nasraldin/turbo-cache-forge-dashboard` |

Both registries carry identical images and tags: `latest` (main), `sha-<short>`
(every commit), and semver on releases.
