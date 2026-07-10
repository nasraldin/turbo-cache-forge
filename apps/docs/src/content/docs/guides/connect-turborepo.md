---
title: Connect Turborepo
description: Point your monorepo at the cache and verify a MISS then HIT — plus the raw protocol with curl.
---

Once the [stack is running](/turbo-cache-forge/getting-started/quickstart/) and you
have a [cache token](/turbo-cache-forge/guides/dashboard/), connecting Turborepo is
three environment variables.

## Point `turbo` at your server

```bash
export TURBO_API=http://localhost:8080   # your cache server
export TURBO_TOKEN=<your-token>          # from Dashboard → API Keys
export TURBO_TEAM=root                   # your organization slug
```

Then run any cached task with remote caching enabled:

```bash
turbo run build --remote-only
```

- **First run** — nothing is cached, so tasks execute and their outputs are
  **uploaded** (cache MISS).
- **Second run** (same inputs) — outputs are **downloaded** instead of rebuilt
  (cache HIT). You'll see `>>> FULL TURBO` and near-instant tasks.

Watch the hit rate climb on the dashboard's [Cache Statistics](/turbo-cache-forge/guides/dashboard/) page.

## Persisting the config in `turbo.json`

Instead of env vars, you can commit the API URL to the repo and pass only the token
and team at runtime:

```json
// turbo.json
{
  "remoteCache": {
    "apiUrl": "http://localhost:8080"
  }
}
```

`TURBO_TOKEN` and `TURBO_TEAM` still come from the environment (keep the token out of
git). In CI, set them as secrets.

## Using it in CI

Set three secrets/vars in your CI provider and export them before `turbo run`:

```yaml
# GitHub Actions example
env:
  TURBO_API: https://cache.example.com
  TURBO_TOKEN: ${{ secrets.TURBO_CACHE_TOKEN }}
  TURBO_TEAM: my-team
```

Every CI runner now shares the same remote cache — the first job to build a given
input populates the cache; the rest download it.

## The raw protocol (curl)

The cache path is a small, well-defined HTTP surface. You can exercise it directly —
useful for debugging connectivity and auth:

```bash
# Is remote caching enabled for this token?
curl -s -H "Authorization: Bearer $TURBO_TOKEN" \
  "http://localhost:8080/v8/artifacts/status"          # {"status":"enabled"}

# Upload an artifact (PUT, streaming body) — returns 202
echo "fake-artifact" | curl -s -X PUT --data-binary @- \
  -H "Authorization: Bearer $TURBO_TOKEN" \
  "http://localhost:8080/v8/artifacts/abc123?teamId=root"

# Download it back
curl -s -H "Authorization: Bearer $TURBO_TOKEN" \
  "http://localhost:8080/v8/artifacts/abc123?teamId=root"   # -> fake-artifact
```

The full endpoint list is in the [Cache API reference](/turbo-cache-forge/reference/cache-api/).

## Troubleshooting

- **401 Unauthorized** — the token is wrong, revoked, or missing the `Bearer` prefix.
  Re-check it in Dashboard → API Keys, or run `turbo-cache doctor`.
- **Everything is a MISS** — inputs are changing between runs (timestamps, env vars in
  the hash), or `teamId`/`TURBO_TEAM` differs from the token's organization.
- **413 Payload Too Large** — an artifact exceeds `MAX_UPLOAD_BYTES`; raise it on the API.
