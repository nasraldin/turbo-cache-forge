# turbo-cache-forge

A self-hosted Turborepo remote cache server (Turbo API v8) with Postgres metadata
and a pluggable storage backend (local filesystem or S3/R2). No cloud account
required to run it.

**Status:** Phase 1 (Cache API MVP) is complete. See [`docs/ROADMAP.md`](docs/ROADMAP.md)
for full status, the phased plan, and cross-phase invariants — read it first before
working on any phase.

## Quickstart (docker compose)

```bash
docker compose -f infra/docker/docker-compose.yml up -d --build
```

This starts Postgres, applies the schema migration (via a self-contained `migrate`
service built from this repo — no external goose image needed), and starts
`cache-api` on `http://localhost:8080` using the filesystem storage backend
(`STORAGE_BACKEND=fs`). Only `cache-api`'s port (`8080`) is published to the
host; Postgres is reachable only over the compose network, not from the host.
`cache-api` runs as a non-root user (uid `65532`) in a distroless image.

Seed an organization and a dev token (`turbo_dev`). The snippet below uses
`$POSTGRES_USER` / `$POSTGRES_DB` (falling back to the `tcf` defaults) so it
still works if you've overridden those creds (see "Overriding Postgres
creds" below):

```bash
docker compose -f infra/docker/docker-compose.yml exec postgres \
  psql -U "${POSTGRES_USER:-tcf}" -d "${POSTGRES_DB:-tcf}" -c \
  "INSERT INTO organizations (slug,name) VALUES ('my-team','My Team');"

TOKEN_HASH=$(printf 'turbo_dev' | shasum -a 256 | cut -d' ' -f1)
docker compose -f infra/docker/docker-compose.yml exec postgres \
  psql -U "${POSTGRES_USER:-tcf}" -d "${POSTGRES_DB:-tcf}" -c \
  "INSERT INTO api_keys (org_id,name,token_hash) SELECT id,'dev','$TOKEN_HASH' FROM organizations WHERE slug='my-team';"
```

### Overriding Postgres creds

Docker Compose auto-loads a `.env` file from the compose **file's directory**
(`infra/docker/`), not the repo root and not your current working directory.
Running `docker compose -f infra/docker/docker-compose.yml up` from the repo
root will **not** pick up a repo-root `.env` — the `POSTGRES_*` vars will
silently stay at the `tcf`/`tcf` defaults. To override them, do one of:

1. Copy [`.env.example`](.env.example) to `infra/docker/.env` (compose
   auto-loads it from there), or
2. Pass `--env-file path/to/your.env` explicitly to `docker compose`, or
3. Export `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB` in your shell
   before running `docker compose up` — compose inherits shell env vars too.

Point Turborepo at it:

```bash
export TURBO_API=http://localhost:8080
export TURBO_TOKEN=turbo_dev
export TURBO_TEAM=my-team
turbo run build --remote-only
```

Or exercise the protocol directly with curl:

```bash
curl -s -H "Authorization: Bearer turbo_dev" \
  "http://localhost:8080/v8/artifacts/status"                  # {"status":"enabled"}

echo "fake-artifact" | curl -s -X PUT --data-binary @- \
  -H "Authorization: Bearer turbo_dev" \
  "http://localhost:8080/v8/artifacts/abc123?teamId=my-team"   # 202

curl -s -H "Authorization: Bearer turbo_dev" \
  "http://localhost:8080/v8/artifacts/abc123?teamId=my-team"   # -> fake-artifact
```

Health/observability:

- `GET /live`, `GET /ready` (checks Postgres via `Repo.Ping`), `GET /metrics` (Prometheus).

### Optional: tracing + error reporting

Both are fully inert until you set their env var:

- `OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318` — exports spans (storage + DB calls) via OTLP/HTTP to any collector (Tempo, Jaeger, `otel-collector`). Metrics stay Prometheus-only; this is tracing only.
- `SENTRY_DSN=https://...` — reports panics and storage/DB errors that produce a 5xx. 4xx client errors are never reported.

### Concurrency / heavy-artifact load test

Excluded from the default `go test ./...` (build-tag gated) so CI stays fast:

```bash
go test -tags loadtest -race ./internal/turbo/... -v
```

Tear down:

```bash
docker compose -f infra/docker/docker-compose.yml down -v
```

### Run locally with built-in auth (no IdP)

Set a root user on the API and you can sign in to the dashboard with a
username + password — no Clerk/Keycloak needed:

```bash
AUTH_MODE=builtin \
AUTH_ROOT_USERNAME=root \
AUTH_ROOT_PASSWORD=change-me \
AUTH_SECRET=$(openssl rand -hex 32) \
  <your usual `docker compose` / `go run ./cmd/server` invocation>
```

The dashboard detects the mode from `GET /api/v1/auth/config` and shows the
built-in sign-in page automatically. Cache tokens for Turborepo are still
minted in the dashboard exactly as before.

## Configuration

See [`.env.example`](.env.example) for every environment variable (storage backend,
S3/R2 credentials, upload size limit, etc). `STORAGE_BACKEND=s3` switches the
filesystem backend for S3-compatible object storage (AWS S3, Cloudflare R2, MinIO).

## Repo layout

- `services/api` — Go cache server (chi router, storage abstraction, Postgres repo,
  bearer-token auth, Turbo v8 protocol handlers, Prometheus metrics).
- `infra/migrations` — goose SQL migrations.
- `infra/docker` — Dockerfile (multi-stage, distroless) and docker-compose stack.
