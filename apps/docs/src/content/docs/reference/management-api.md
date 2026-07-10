---
title: Management API
description: The /api/v1 routes the dashboard and CLI use — auth, tokens, projects, stats, and artifacts.
---

The management API lives under `/api/v1`. It is what the [dashboard](/turbo-cache-forge/guides/dashboard/)
and [CLI](/turbo-cache-forge/guides/cli/) talk to — **never** the cache path. It mounts
when the server has an auth configuration (built-in mode, or OIDC once `OIDC_ISSUER` is
set). An OpenAPI spec is served at `GET /api/v1/openapi.yaml` with Swagger UI at
`/api/v1/docs`.

## Public routes

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/v1/auth/config` | Discovery — returns `{ mode, org_enabled }` so the dashboard picks its sign-in UI |
| `POST` | `/api/v1/auth/login` | Built-in mode only — username/password → session JWT |
| `GET` | `/api/v1/openapi.yaml` | OpenAPI spec |
| `GET` | `/api/v1/docs/*` | Swagger UI |

## Authenticated routes

All require a session — a built-in JWT (from `/auth/login`) or an OIDC JWT — as
`Authorization: Bearer <jwt>`.

### Tokens (cache bearer tokens)

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/api/v1/tokens` | Create a cache token. Returns the plaintext **once** |
| `GET` | `/api/v1/tokens` | List tokens (never includes the hash) |
| `DELETE` | `/api/v1/tokens/{id}` | Revoke a token |

### Projects (cache namespaces)

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/api/v1/projects` | Create a project (`slug` must match `^[a-z0-9-]+$`) |
| `GET` | `/api/v1/projects` | List projects |

### Stats

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/v1/stats` | Aggregate: hit rate, hits, misses, requests, storage bytes |
| `GET` | `/api/v1/stats/timeseries?days=N` | Daily hits/misses/bytes for the last N days (1–365, default 30) |

### Artifacts

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/v1/artifacts` | List cached artifacts (hash, size, tag, timestamps) |
| `GET` | `/api/v1/artifacts/{hash}` | Artifact detail |
| `GET` | `/api/v1/artifacts/{hash}/download` | Download the blob |
| `DELETE` | `/api/v1/artifacts/{hash}` | Delete one artifact |
| `DELETE` | `/api/v1/artifacts` | Clear all artifacts for the org |

## Example: log in and create a token

```bash
API=http://localhost:8080

# 1. built-in login -> session JWT
JWT=$(curl -s -X POST "$API/api/v1/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"username":"root","password":"root"}' | jq -r .token)

# 2. create a cache token (plaintext returned once)
curl -s -X POST "$API/api/v1/tokens" \
  -H "Authorization: Bearer $JWT" \
  -H 'Content-Type: application/json' \
  -d '{"name":"ci-runner"}'
```
