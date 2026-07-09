# turbo-cache-forge example

A tiny, self-contained Turborepo used to exercise **turbo-cache-forge** as a real
remote cache. Two packages with a dependency edge (`@example/app` → `@example/math`),
each with a deliberately slow `build` (sleeps a few seconds) so a cache **HIT** is
obviously faster than a cold build.

This is its own workspace — it is intentionally excluded from the root monorepo
(`!apps/example` in the root `pnpm-workspace.yaml`). Run it on its own.

## 1. Get a cache token

In the dashboard → **API Keys** → create one. The plaintext `turbo_…` token is shown
once; copy it. (It is stored only hashed; the same token authenticates the `/v8` cache
path.)

## 2. Point turbo at your local cache

```bash
cd apps/example
pnpm install

export TURBO_API=http://localhost:8080
export TURBO_TOKEN=turbo_xxxxxxxx        # the token from step 1
export TURBO_TEAM=your-org-slug          # any value; the backend keys off the token
```

## 3. Run it

```bash
./run-demo.sh          # cold build (MISS, uploads) then warm build (HIT from remote)
```

Or by hand:

```bash
# cold — uploads to the remote cache
rm -rf .turbo packages/*/dist
pnpm exec turbo run build --cache=remote:rw

# warm — clear the LOCAL cache so a hit can only come from turbo-cache-forge
rm -rf .turbo packages/*/dist
pnpm exec turbo run build --cache=remote:rw     # >>> FULL TURBO
```

The warm run prints `cache hit, replaying logs` and finishes in milliseconds.

## Forcing a fresh miss

The `build` task declares `CACHE_BUST` as an input (see `turbo.json`), so changing it
gives every task a new hash — a clean way to generate fresh misses on demand:

```bash
CACHE_BUST=v2 pnpm exec turbo run build --cache=remote:rw
```

## Seeing it in the dashboard

Cache activity shows up under **Overview**, **Cache Statistics**, and **Artifacts**.
Storage and artifact counts update immediately; hit/miss counts flush on the rollup
interval (`USAGE_ROLLUP_INTERVAL_SEC`, 15s in the local compose, 300s by default).
