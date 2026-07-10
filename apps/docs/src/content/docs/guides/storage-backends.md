---
title: Storage backends
description: Store artifact blobs on the local filesystem or any S3-compatible object store (AWS S3, Cloudflare R2, MinIO).
---

Artifact **blobs** are stored through a pluggable backend, selected by
`STORAGE_BACKEND`. Metadata (which artifacts exist, sizes, tags, usage) always lives
in Postgres regardless of backend.

## Filesystem (default)

```bash
STORAGE_BACKEND=fs
STORAGE_PATH=/data          # a directory the API can write to
```

In Compose this is a named volume (`cache-data:/data`), so artifacts survive restarts
but are removed by `docker compose down -v`. Simple, fast, and all most single-server
deployments need. Back it with a persistent disk in production.

## S3-compatible (AWS S3, Cloudflare R2, MinIO)

Switch the backend to `s3` and supply credentials for any S3-compatible store:

```bash
STORAGE_BACKEND=s3
STORAGE_S3_BUCKET=my-turbo-cache
STORAGE_S3_REGION=auto
STORAGE_S3_ACCESS_KEY=...
STORAGE_S3_SECRET_KEY=...
# Endpoint is required for non-AWS providers:
STORAGE_S3_ENDPOINT=https://<account>.r2.cloudflarestorage.com   # Cloudflare R2
# STORAGE_S3_ENDPOINT=http://localhost:9000                      # MinIO
```

Leave `STORAGE_S3_ENDPOINT` empty for AWS S3 (the SDK derives it from the region).
Set it explicitly for R2, MinIO, and other S3-compatible providers.

### Provider notes

- **AWS S3** — set `STORAGE_S3_REGION` to your bucket's region; leave the endpoint empty.
- **Cloudflare R2** — use `STORAGE_S3_REGION=auto` and the account-scoped endpoint above.
- **MinIO** — point the endpoint at your MinIO server; works great for on-prem object
  storage without a cloud dependency.

## Choosing a backend

| | Filesystem | S3-compatible |
|---|---|---|
| Setup | Zero | A bucket + credentials |
| Durability | As durable as the disk | Provider-managed, highly durable |
| Multiple API replicas | Needs shared/network disk | Works naturally (shared bucket) |
| Best for | Single server, simplicity | Scale, durability, cloud-native |

## Upload size limit

Both backends respect `MAX_UPLOAD_BYTES` (default `1073741824`, i.e. 1 GiB). Artifacts
larger than this are rejected with `413 Payload Too Large`. Raise it if your build
outputs are big; keep it sane to avoid a single artifact filling your store.
