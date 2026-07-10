---
title: The dashboard
description: A tour of the Next.js management console — hit rate, statistics, storage, artifacts, projects, and API keys.
---

The dashboard is a Next.js console that talks **only** to the management API
(`/api/v1`) — never to storage, the database, or the cache path directly. It reads
your live data and renders it. Sign in at **http://localhost:3000** (built-in mode:
`root` / `root` by default).

## Overview

The landing screen: cache **hit rate**, storage used, total requests, and the build
work saved (bytes that would have been recomputed without the cache).

![Dashboard overview showing a 90.9% hit rate, storage used, requests, and work saved](../../../assets/screenshots/overview.png)

## Cache Statistics

Hit rate and cache activity over time. The trend chart plots daily hits and misses so
you can watch the cache warm up as more of your builds get shared.

![Cache statistics with an 88% hit rate and a two-week hits/misses trend chart](../../../assets/screenshots/statistics.png)

## Artifacts

Browse every cached artifact — hash, size, tag, and when it was created and last
accessed. You can view an artifact's detail, download it, delete one, or clear them
all.

![Artifacts browser listing cached build outputs with hash, size, tag, and timestamps](../../../assets/screenshots/artifacts.png)

## Projects

Projects are **cache namespaces** within an organization. Create one per app or
package in your monorepo to keep artifacts organized.

![Projects list showing cache namespaces with name, slug, and created date](../../../assets/screenshots/projects.png)

## API Keys

Mint and revoke the **bearer tokens** the Turborepo CLI uses on the cache path. The
plaintext token is shown **once** at creation — copy it then; only its hash is stored.

![API Keys screen with an active cache token and a revoke action](../../../assets/screenshots/api-keys.png)

## Storage Usage

The total object storage consumed by cached artifacts, so you can keep an eye on
growth and set retention accordingly.

![Storage usage screen showing total stored bytes](../../../assets/screenshots/storage.png)

## A note on live data

In built-in mode the management API is mounted and the dashboard shows live data out of
the box. In OIDC mode, `/api/v1` mounts only when `OIDC_ISSUER` is set — until then the
data panels show their (safe) empty/error states by design. See
[Authentication](/turbo-cache-forge/guides/authentication/).
