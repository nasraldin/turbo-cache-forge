---
title: Environment variables
description: Every environment variable the cache API, migrations, and dashboard read.
---

Turbo Cache Forge is configured entirely by environment variables. This is the full
reference; see [Configuration](/turbo-cache-forge/getting-started/configuration/) for
where they live and the `.env` gotcha.

## Server / API

| Variable | Default | Description |
|---|---|---|
| `ADDR` | `:8080` | Listen address |
| `DATABASE_URL` | — | Postgres DSN (e.g. `postgres://tcf:tcf@postgres:5432/tcf?sslmode=disable`) |
| `MAX_UPLOAD_BYTES` | `1073741824` | Reject artifacts larger than this (1 GiB) |
| `REQUIRE_ARTIFACT_SIGNATURE` | `false` | Enforce Turbo artifact signatures on the cache path (see [Authentication → Signatures](/turbo-cache-forge/guides/authentication/)) |

## Storage

| Variable | Default | Description |
|---|---|---|
| `STORAGE_BACKEND` | `fs` | `fs` or `s3` |
| `STORAGE_PATH` | `/var/lib/turbo-cache-forge` | Blob directory (filesystem backend) |
| `STORAGE_S3_BUCKET` | — | Bucket name (s3 backend) |
| `STORAGE_S3_ENDPOINT` | — | Endpoint for R2/MinIO; empty for AWS S3 |
| `STORAGE_S3_REGION` | `auto` | Region |
| `STORAGE_S3_ACCESS_KEY` | — | Access key |
| `STORAGE_S3_SECRET_KEY` | — | Secret key |

## Postgres (Docker Compose)

Read by the `postgres` service in `infra/docker/docker-compose.yml`. Remember Compose
loads `.env` from `infra/docker/`, not the repo root.

| Variable | Default | Description |
|---|---|---|
| `POSTGRES_USER` | `tcf` | DB user |
| `POSTGRES_PASSWORD` | `tcf` | DB password |
| `POSTGRES_DB` | `tcf` | DB name |

## Auth — built-in

| Variable | Default | Description |
|---|---|---|
| `AUTH_MODE` | `oidc` | `builtin` or `oidc` (Compose sets `builtin`) |
| `AUTH_ROOT_USERNAME` | `root` | Built-in root username |
| `AUTH_ROOT_PASSWORD` | — | Built-in root password (or use `AUTH_ROOT_PASSWORD_HASH`, bcrypt) |
| `AUTH_SECRET` | random per boot | HS256 session secret — set a stable value or sessions reset on restart |
| `AUTH_TOKEN_TTL` | `12h` | Session lifetime |

## Auth — OIDC / management

The management API mounts only when `OIDC_ISSUER` is set (or in built-in mode).

| Variable | Default | Description |
|---|---|---|
| `OIDC_ISSUER` | — | IdP issuer URL; mounts `/api/v1` when set |
| `OIDC_JWKS_URL` | discovered | Explicit JWKS URL (else discovered from issuer) |
| `OIDC_AUDIENCE` | `turbo-cache-forge` | Required `aud` claim (org mode) |
| `OIDC_ORG_CLAIM` | `org_id` | JWT claim carrying the tenant/org id |
| `OIDC_ORG_ENABLED` | `true` | `true` = org mode (strict), `false` = personal mode (dedicated issuer only) |

## Background jobs

| Variable | Default | Description |
|---|---|---|
| `RETENTION_DAYS` | `30` | Artifact retention window |
| `USAGE_ROLLUP_INTERVAL_SEC` | `300` | How often in-memory hit/miss counters flush to `usage_daily` |
| `CLEANUP_INTERVAL_SEC` | `3600` | Cleanup job interval |

## CORS

| Variable | Default | Description |
|---|---|---|
| `CORS_ALLOWED_ORIGINS` | `http://localhost:3000` | Browser origins allowed to call `/api/v1`. Empty = CORS off |

## Observability (optional, inert until set)

| Variable | Default | Description |
|---|---|---|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | — | OTLP/HTTP endpoint for tracing (e.g. `http://localhost:4318`) |
| `SENTRY_DSN` | — | Sentry DSN for 5xx error reporting |

## Dashboard

| Variable | Default | Description |
|---|---|---|
| `NEXT_PUBLIC_API_URL` | `http://localhost:8080` | Where the browser reaches the API |
| `NEXT_PUBLIC_CLERK_PUBLISHABLE_KEY` | — | Clerk publishable key (OIDC mode) |
| `CLERK_SECRET_KEY` | — | **Must be unset in built-in mode** — its presence makes middleware enforce Clerk and redirect-loop |
| `NEXT_PUBLIC_ORG_ENABLED` | `false` | Show the org switcher (Clerk orgs) |
