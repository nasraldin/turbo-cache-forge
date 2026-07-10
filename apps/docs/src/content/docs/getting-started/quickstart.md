---
title: Quickstart
description: Bring up the cache API and the dashboard with a single Docker Compose command — no external database required.
---

The fastest way to run the full stack — the cache API and the dashboard — is Docker
Compose. No external database is required: the API self-migrates an embedded
**SQLite** file on boot.

## Prerequisites

- **Docker** with Compose v2 (`docker compose version`).
- Ports **8080** (API) and **3000** (dashboard) free on the host.

## 1. Start the stack

```bash
git clone https://github.com/nasraldin/turbo-cache-forge.git
cd turbo-cache-forge
docker compose -f infra/docker/docker-compose.yml up -d --build
```

This starts two services:

| Service | Port | Role |
|---|---|---|
| `cache-api` | **8080** | Turbo v8 cache + `/api/v1` management API |
| `dashboard` | **3000** | Next.js web console |

`cache-api` self-migrates its metadata store — an embedded **SQLite** file at
`/data/tcf.db`, inside the persisted `cache-data` volume — on boot, so there's no
separate migration step and no database service to run. The API runs as a non-root
user (uid `65532`) in a distroless image and uses the filesystem storage backend
(`STORAGE_BACKEND=fs`) by default.

### Prefer Postgres?

For multi-node deployments, add the Postgres overlay to run a `postgres` service and
point `cache-api` at it instead (still self-migrating, no separate `migrate` step):

```bash
docker compose -f infra/docker/docker-compose.yml \
  -f infra/docker/docker-compose.postgres.yml up -d --build
```

See [Configuration](/turbo-cache-forge/getting-started/configuration/) for the
`DATABASE_URL` schemes (SQLite vs. Postgres).

:::note[Docker Compose and `.env`]
Compose auto-loads `.env` from the **compose file's directory** (`infra/docker/`),
not the repo root. To change Postgres credentials or auth, put them in
`infra/docker/.env` (copy [`.env.example`](/turbo-cache-forge/getting-started/configuration/)),
or pass `--env-file`. The Postgres credentials part only applies if you're running
the Postgres overlay described above.
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
