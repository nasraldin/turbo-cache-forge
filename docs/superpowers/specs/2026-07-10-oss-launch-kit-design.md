# OSS Launch Kit — Design

_Date: 2026-07-10. Goal: turn turbo-cache-forge into a polished, contributor-ready
open-source project: CI/CD via GitHub Actions, a published GitHub Pages docs site
(Astro Starlight) with real dashboard screenshots, and a rewritten professional README._

## Problem

The repo is feature-complete (all 5 phases merged) but has **no CI/CD**, **no docs
site**, and a **stale README** (still says "Phase 1 complete"). Nothing gates PRs,
no images are published, and a newcomer has only scattered `docs/*.md` to learn from.
This is the on-ramp for external users and contributors.

## Decisions (locked with owner)

| Decision | Choice |
|---|---|
| Container registry | **Docker Hub**, namespace `nasraldin/turbo-cache-forge-{api,migrate,dashboard}` |
| Docs framework | **Astro Starlight** (new pnpm workspace member `apps/docs`) |
| Screenshots | **Real** — boot the compose stack, seed live data, capture via Chrome DevTools MCP |
| CI scope | **Full** — Go tests + JS lint/typecheck/test on PRs; build+push images on `main`/tags |
| Pages deploy | Official GitHub Pages Actions flow (`actions/deploy-pages`), not a `gh-pages` branch |

## A. GitHub Actions (`.github/workflows/`)

### `ci.yml` — PRs + push to `main`
Two independent jobs (mirrors the repo's two build worlds):
- **go** — matrix over `services/api`, `services/cli`. `go vet` + `go test ./...`.
  The `api` leg gets a **Postgres 16 service container** (its repo tests need a DB);
  `DATABASE_URL` points at it. The load test (`-tags loadtest`) stays excluded.
- **js** — pnpm + Node 20, `pnpm install --frozen-lockfile`, then
  `pnpm turbo run lint typecheck test`. Playwright e2e stays out of CI (needs the full
  running stack) — noted as a follow-up.

### `docker.yml` — push to `main` + `v*` tags
- `docker/setup-buildx-action` + `docker/login-action` (Docker Hub secrets).
- `docker/metadata-action` for tags: `latest` (main), semver (tags), `sha-<short>` (always).
- Three `docker/build-push-action` steps, matching compose exactly:
  1. `nasraldin/turbo-cache-forge-api` — context `.`, `infra/docker/Dockerfile`, target `cache-api`
  2. `nasraldin/turbo-cache-forge-migrate` — same Dockerfile, target `goose`
  3. `nasraldin/turbo-cache-forge-dashboard` — `apps/dashboard/Dockerfile`, build-args as in compose
- GitHub Actions cache (`cache-from/to: type=gha`) for layer reuse.
- `needs: ci`-equivalent gating: the workflow runs only after tests pass (guarded via
  `workflow_run` or by including a fast test gate — simplest: a job dependency in a
  combined trigger; final choice is an implementation detail, but publishing must not
  outrun a red build on `main`).

### `docs.yml` — push to `main` touching `apps/docs/**` or docs sources, + manual dispatch
Standard Pages pipeline: build the Starlight site (`pnpm --filter docs build`),
`actions/upload-pages-artifact`, `actions/deploy-pages`. `permissions: pages: write,
id-token: write`. Astro `base: '/turbo-cache-forge/'`, `site:` set to the Pages URL.

**Required repo secrets:** `DOCKERHUB_USERNAME`, `DOCKERHUB_TOKEN`.
**Required repo setting:** Pages source = GitHub Actions.

## B. Docs site — Astro Starlight (`apps/docs/`)

New workspace member (add `apps/docs` — already covered by `apps/*` glob; `apps/example`
stays excluded). Structure:

- **Landing** (`index.mdx`, Starlight `splash` template): hero, one-line pitch, feature
  cards, quickstart CTA, hero screenshot.
- **Getting Started**: What is it · Quickstart (Docker Compose) · Configuration (env ref
  from `.env.example`).
- **Guides**: Connect Turborepo · Auth modes (built-in vs OIDC/personal vs org) · Storage
  backends (fs vs S3/R2/MinIO) · CLI (`turbo-cache`) · Dashboard tour (screenshots).
- **Reference**: Cache API (Turbo v8 protocol) · Management API (`/api/v1`) · Env var table
  · Architecture (two build worlds, cross-phase invariants).
- **Project**: Contributing · Roadmap.

Content is sourced from the real `README`/`HANDOFF`/`ROADMAP`/specs — no filler.
Screenshots stored in `apps/docs/src/assets/`.

## C. Screenshots (capture flow)

1. `docker compose -f infra/docker/docker-compose.yml --env-file infra/docker/.env up -d --build`
   (defaults: `AUTH_MODE=builtin`, root/root, `/api/v1` mounted, `CLERK_SECRET_KEY` unset).
2. Log into `http://localhost:3000` as root/root via Chrome DevTools MCP.
3. Seed live data: create a project + cache token in the dashboard; drive cache
   PUT/GET traffic with curl (`/v8/artifacts/*`) to populate hit/miss stats and the
   artifact browser. `USAGE_ROLLUP_INTERVAL_SEC=15` → stats appear within seconds.
4. Screenshot: overview, statistics, projects, api-keys, artifacts, storage.
5. `docker compose ... down -v` teardown.

Fallback if the stack won't boot cleanly: capture whatever screens do render, leave the
rest as noted gaps (never ship fake/placeholder images claiming to be real).

## D. README rewrite

Professional OSS front page: badges (CI, Docker Hub, license, release), one-line pitch,
hero screenshot, "Quickstart in 60s", feature highlights, architecture blurb, links into
the docs site, contributing/roadmap/license. Keep the real curl/protocol examples; move
deep detail into the docs site. Fix the stale "Phase 1 complete" status.

## Deliverables

- `.github/workflows/{ci,docker,docs}.yml`
- `apps/docs/` (Astro Starlight site) + captured screenshots in `src/assets/`
- rewritten `README.md`
- this spec

## Non-goals / follow-ups

- Playwright e2e in CI (needs full stack) — deferred, noted.
- GoReleaser CLI-release workflow — out of scope here (separate concern); `.goreleaser.yaml`
  already exists and can be wired later.
- Docs versioning — single "latest" for now.
