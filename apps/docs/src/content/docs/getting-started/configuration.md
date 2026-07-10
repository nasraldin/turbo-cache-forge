---
title: Configuration
description: Where config lives, the essential environment variables, and how to override Postgres credentials.
---

Turbo Cache Forge is configured entirely through **environment variables**. There is
no config file to edit — the [full reference](/turbo-cache-forge/reference/environment/)
lists every variable. This page covers the essentials and the one gotcha worth knowing.

## Where config lives

| File | Read by | Holds |
|---|---|---|
| `infra/docker/.env` | `docker compose` | Postgres creds, auth, `NEXT_PUBLIC_API_URL`, storage |
| `apps/dashboard/.env.local` | `pnpm --filter dashboard dev` | dashboard-only vars for local iteration |
| `.env.example` (repo root) | you (template) | every API variable with comments |

Both `.env` files are git-ignored — they may hold secrets, so never commit them.
Copy `.env.example` to `infra/docker/.env` and edit from there.

## The essential variables

```bash
# Storage
STORAGE_BACKEND=fs                 # fs | s3
STORAGE_PATH=/data                 # where fs blobs live (a mounted volume)

# Database
DATABASE_URL=postgres://tcf:tcf@postgres:5432/tcf?sslmode=disable

# Auth (see the Authentication guide)
AUTH_MODE=builtin                  # builtin | oidc
AUTH_ROOT_USERNAME=root
AUTH_ROOT_PASSWORD=change-me
AUTH_SECRET=<random 32+ bytes>     # HS256 session secret

# Upload guard
MAX_UPLOAD_BYTES=1073741824        # reject artifacts larger than this (1 GiB default)
```

For S3/R2/MinIO storage, see [Storage backends](/turbo-cache-forge/guides/storage-backends/).
For OIDC (Clerk, Keycloak), see [Authentication](/turbo-cache-forge/guides/authentication/).

## Overriding Postgres credentials

:::caution[The `.env` location gotcha]
Docker Compose auto-loads `.env` from the **compose file's directory**
(`infra/docker/`), **not** the repo root and not your current working directory.
Running `docker compose -f infra/docker/docker-compose.yml up` from the repo root
will **not** pick up a repo-root `.env` — `POSTGRES_*` will silently stay at the
`tcf`/`tcf` defaults.
:::

To override them, do one of:

1. Copy `.env.example` to `infra/docker/.env` (Compose auto-loads it there), **or**
2. Pass `--env-file path/to/your.env` explicitly, **or**
3. Export `POSTGRES_USER` / `POSTGRES_PASSWORD` / `POSTGRES_DB` in your shell before
   `docker compose up` (Compose inherits shell env too).

## Observability (optional)

Both are fully inert until their variable is set:

- `OTEL_EXPORTER_OTLP_ENDPOINT` — export spans (storage + DB calls) via OTLP/HTTP to
  any collector (Tempo, Jaeger). Tracing only; metrics stay Prometheus.
- `SENTRY_DSN` — report panics and storage/DB errors that produce a 5xx. Client 4xx
  errors are never reported.

Prometheus metrics are always available at `GET /metrics`.
