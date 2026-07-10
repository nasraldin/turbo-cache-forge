---
title: Contributing
description: How to build, test, and run each part of the repo, plus how CI and releases work.
---

Contributions are welcome. The repo is two toolchains (Go + pnpm/Turborepo) — you only
need the one for the part you're changing.

## Prerequisites

- **Go 1.25+** — for `services/api` and `services/cli`.
- **Node 20 + pnpm 9** — for the dashboard, docs, and shared packages.
- **Docker** — to run the full stack.

## Build & test

### Go (API + CLI)

```bash
cd services/api && go vet ./... && go test ./...
cd services/cli && go vet ./... && go test ./...
```

The heavy concurrency load test is build-tag gated so the default run stays fast:

```bash
cd services/api && go test -tags loadtest -race ./internal/turbo/... -v
```

### JavaScript (dashboard, docs, packages)

```bash
pnpm install
pnpm turbo run lint typecheck test    # everything
pnpm --filter dashboard dev           # dashboard on :3000
pnpm --filter docs dev                # these docs, locally
```

### Full stack

```bash
docker compose -f infra/docker/docker-compose.yml up -d --build
```

## CI

Every pull request runs [`.github/workflows/ci.yml`](https://github.com/nasraldin/turbo-cache-forge/blob/main/.github/workflows/ci.yml):

- **Go job** — a matrix over `services/api` and `services/cli` (`go vet` + `go test`),
  with a Postgres 16 service container for the API's DB-backed tests.
- **JS job** — `pnpm turbo run lint typecheck test`.

## Images & releases

On push to `main` and on `v*` tags — and only after the test jobs pass — the gated
`docker` job in [`.github/workflows/ci.yml`](https://github.com/nasraldin/turbo-cache-forge/blob/main/.github/workflows/ci.yml)
builds three images and pushes each to **both Docker Hub and GitHub Container Registry**:

- `nasraldin/turbo-cache-forge-{api,migrate,dashboard}` (Docker Hub)
- `ghcr.io/nasraldin/turbo-cache-forge-{api,migrate,dashboard}` (ghcr.io)

Tagged `latest` (main), `sha-<short>` (every commit), and semver on releases.
Docker Hub needs the `DOCKERHUB_USERNAME` / `DOCKERHUB_TOKEN` secrets; ghcr.io uses the
built-in `GITHUB_TOKEN`. Cross-compiled CLI binaries are produced by GoReleaser from
`.goreleaser.yaml`.

## Docs

This site is Astro Starlight in `apps/docs/`. Edit the Markdown under
`src/content/docs/`, run `pnpm --filter docs dev` to preview, and open a PR — merges to
`main` auto-publish to GitHub Pages via [`.github/workflows/docs.yml`](https://github.com/nasraldin/turbo-cache-forge/blob/main/.github/workflows/docs.yml).

## The invariants

Before changing the API, read the [Architecture](/turbo-cache-forge/reference/architecture/)
invariants. They're load-bearing — a change that violates one (mixing the two auth
worlds, buffering an artifact, storing a plaintext token) is a defect, not a feature.
