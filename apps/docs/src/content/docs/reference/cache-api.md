---
title: Cache API (Turbo v8)
description: The Turbo v8 protocol endpoints the Turborepo CLI uses — status, upload, download, batch existence, and events.
---

The cache path implements the Turborepo **API v8** protocol under `/v8/artifacts`. All
endpoints require a hashed **bearer token** (`Authorization: Bearer <token>`) and a
team/organization via the `teamId` query parameter. The hot path is **streaming** —
artifact bodies are never buffered whole in memory.

## Endpoints

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/v8/artifacts/status` | Whether remote caching is enabled for this token |
| `PUT` | `/v8/artifacts/{hash}` | Upload an artifact (streaming body). Returns `202` |
| `GET` | `/v8/artifacts/{hash}` | Download an artifact by hash |
| `POST` | `/v8/artifacts` | Batch existence check for a set of hashes |
| `POST` | `/v8/artifacts/events` | Telemetry sink (the CLI posts cache events here) |

## Headers

- `Authorization: Bearer <token>` — **required** on every call.
- `x-artifact-tag` — optional integrity/metadata tag the CLI sends on `PUT`; stored and
  returned on `GET`. Shown as the artifact's **tag** in the dashboard.

## Query parameters

- `teamId` — the organization slug the artifact belongs to. Must match the token's org.

## Examples

```bash
TOKEN=<your-token>
API=http://localhost:8080

# status
curl -s -H "Authorization: Bearer $TOKEN" \
  "$API/v8/artifacts/status"                       # {"status":"enabled"}

# upload (streaming) -> 202
echo "fake-artifact" | curl -s -X PUT --data-binary @- \
  -H "Authorization: Bearer $TOKEN" \
  -H "x-artifact-tag: my-build" \
  "$API/v8/artifacts/abc123?teamId=root"

# download
curl -s -H "Authorization: Bearer $TOKEN" \
  "$API/v8/artifacts/abc123?teamId=root"           # -> fake-artifact
```

## Status codes

| Code | Meaning |
|---|---|
| `200` | Download succeeded |
| `202` | Upload accepted |
| `401` | Missing/invalid/revoked token |
| `404` | Artifact not found (cache MISS) |
| `413` | Artifact exceeds `MAX_UPLOAD_BYTES` |

You normally never call these yourself — the `turbo` CLI does. See
[Connect Turborepo](/turbo-cache-forge/guides/connect-turborepo/).

## Health & metrics (outside `/v8`)

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/live` | Liveness — process is up |
| `GET` | `/ready` | Readiness — checks Postgres connectivity |
| `GET` | `/metrics` | Prometheus metrics |
