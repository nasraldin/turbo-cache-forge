# turbo-cache-forge

A self-hosted Turborepo remote cache server (Turbo API v8) with Postgres metadata
and a pluggable storage backend (local filesystem or S3/R2). No cloud account
required to run it.

## Quickstart (docker compose)

```bash
docker compose -f infra/docker/docker-compose.yml up -d --build
```

This starts Postgres, applies the schema migration, and starts `cache-api` on
`http://localhost:8080` using the filesystem storage backend (`STORAGE_BACKEND=fs`).

Seed an organization and a dev token (`turbo_dev`):

```bash
docker compose -f infra/docker/docker-compose.yml exec postgres \
  psql -U tcf -d tcf -c \
  "INSERT INTO organizations (slug,name) VALUES ('my-team','My Team');"

TOKEN_HASH=$(printf 'turbo_dev' | shasum -a 256 | cut -d' ' -f1)
docker compose -f infra/docker/docker-compose.yml exec postgres \
  psql -U tcf -d tcf -c \
  "INSERT INTO api_keys (org_id,name,token_hash) SELECT id,'dev','$TOKEN_HASH' FROM organizations WHERE slug='my-team';"
```

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

Tear down:

```bash
docker compose -f infra/docker/docker-compose.yml down -v
```

## Configuration

See [`.env.example`](.env.example) for every environment variable (storage backend,
S3/R2 credentials, upload size limit, etc). `STORAGE_BACKEND=s3` switches the
filesystem backend for S3-compatible object storage (AWS S3, Cloudflare R2, MinIO).

## Repo layout

- `services/api` — Go cache server (chi router, storage abstraction, Postgres repo,
  bearer-token auth, Turbo v8 protocol handlers, Prometheus metrics).
- `infra/migrations` — goose SQL migrations.
- `infra/docker` — Dockerfile (multi-stage, distroless) and docker-compose stack.
