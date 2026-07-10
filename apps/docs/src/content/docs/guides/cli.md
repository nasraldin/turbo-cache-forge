---
title: The CLI
description: turbo-cache — a standalone Go CLI to diagnose the server and manage tokens, projects, and stats from the terminal.
---

`turbo-cache` is a small, dependency-light Go CLI for operators. It is its own module
(it imports nothing from the server's storage or database) and talks to the management
API over plain HTTP. Use it to diagnose connectivity, log in, and manage resources
without opening the dashboard.

## Build it

```bash
cd services/cli
go build -o /tmp/turbo-cache ./cmd/turbo-cache
```

Prebuilt cross-compiled binaries (linux/darwin/windows × amd64/arm64) are produced by
GoReleaser from `.goreleaser.yaml` for tagged releases.

## Commands

### `doctor` — diagnose config, connectivity, and auth

The first thing to run when something isn't working. It checks that the API is
reachable, that health endpoints respond, and that your credentials are valid.

```bash
turbo-cache doctor --api http://localhost:8080
```

### `login` — authenticate via OIDC device flow

For OIDC deployments, `login` runs the device authorization flow: it prints a code and
URL, you approve in the browser, and the CLI stores the resulting session.

```bash
turbo-cache login --api https://cache.example.com
```

### `token create` — mint a cache token

Creates a bearer token for the Turborepo CLI. The plaintext is printed once.

```bash
turbo-cache token create --name ci-runner
```

### `project create` — create a cache namespace

```bash
turbo-cache project create --slug web-platform --name "Web Platform"
```

### `stats` — cache metrics from the terminal

```bash
turbo-cache stats
```

Prints hit rate, hits/misses, storage, and request counts — the same numbers the
dashboard Overview shows.

## When to use the CLI vs the dashboard

- **CLI** — CI scripting, quick diagnostics (`doctor`), headless servers, or anyone who
  lives in the terminal.
- **Dashboard** — visual monitoring, the artifact browser, and day-to-day management.

Both act on the same `/api/v1` management API, so they're interchangeable for creating
tokens and projects.
