#!/usr/bin/env bash
# Exercise turbo-cache-forge as a remote cache: one cold build (MISS, uploads),
# then one warm build with the LOCAL cache cleared (HIT, sourced only from the
# remote). Requires TURBO_API, TURBO_TOKEN, TURBO_TEAM in the environment.
set -euo pipefail
cd "$(dirname "$0")"

: "${TURBO_API:?set TURBO_API, e.g. http://localhost:8080}"
: "${TURBO_TOKEN:?set TURBO_TOKEN to a turbo_… cache token (dashboard → API Keys)}"
: "${TURBO_TEAM:?set TURBO_TEAM to any team/org slug}"
export TURBO_API TURBO_TOKEN TURBO_TEAM

[ -d node_modules ] || pnpm install

echo "### COLD build — expect cache MISS, uploads to $TURBO_API ###"
rm -rf .turbo packages/*/dist 2>/dev/null || true
pnpm exec turbo run build --cache=remote:rw

echo
echo "### WARM build — local cache cleared, expect remote HIT (FULL TURBO) ###"
rm -rf .turbo packages/*/dist 2>/dev/null || true
pnpm exec turbo run build --cache=remote:rw
