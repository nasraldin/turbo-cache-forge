---
title: Authentication modes
description: The two auth worlds — hashed cache tokens and human sign-in (built-in password or OIDC) — and how to configure each.
---

Turbo Cache Forge keeps **two authentication worlds strictly separate**, and that
separation is a core invariant of the system:

1. **Cache path** (`/v8/artifacts/*`) — authenticated with a **hashed bearer token**.
   This is what the `turbo` CLI sends. Tokens are stored only as SHA-256 hashes; the
   plaintext is shown once at creation and never again.
2. **Management path** (`/api/v1/*` + the dashboard) — authenticated with a **human
   session**: either a built-in username/password or a JWT from an external OIDC
   provider.

Cache tokens can never access management routes, and session JWTs can never access the
cache path. They are different credentials for different jobs.

## Read-only cache tokens

A cache token can be minted **read-only**: it may **pull** from the cache (GET/HEAD, and
existence checks) but any **upload is rejected with `403`**. This is ideal for untrusted or
fan-out CI runners that should benefit from the cache without being able to poison it — only
your trusted builder needs a read-write token.

Create one from the dashboard (**API Keys → New token → Read-only**), or with the CLI:

```bash
turbo-cache token create --name ci-readers --read-only
```

The API Keys page shows each token's access (Read-only / Read-write). The flag is per-token,
so you can mix read-only and read-write tokens in the same organization. (This is finer-grained
than a global read-only switch — you choose per token.)

## Artifact signatures

Turborepo can **sign** cache artifacts: with `signature: true` in `turbo.json`'s `remoteCache`
and a `TURBO_REMOTE_CACHE_SIGNATURE_KEY` set, the `turbo` CLI computes an HMAC over each
artifact and sends it in the `x-artifact-tag` header; on download it re-verifies. **Signing and
verification are entirely client-side** — the server never holds the key.

The server's job is to round-trip the tag, which Turbo Cache Forge does. Set
`REQUIRE_ARTIFACT_SIGNATURE=true` to **enforce** it:

- Uploads without a signature are rejected (`400`).
- A stored artifact with no signature is treated as a **cache miss** on GET/HEAD.

This guarantees the cache only ever stores and serves **tagged** artifacts. The signature
itself is verified by the `turbo` client on download (the server holds no key and does no
crypto) — enforcement makes the presence of a signature mandatory, and the client rejects any
artifact whose bytes don't match its tag. It's a global server setting; leave it `false`
(default) to keep the download path database-free.

## Built-in auth (no external IdP)

The simplest mode, and the Compose default. A single root user is configured by
environment variable; the dashboard detects this via `GET /api/v1/auth/config` and
shows a password sign-in page.

```bash
AUTH_MODE=builtin
AUTH_ROOT_USERNAME=root
AUTH_ROOT_PASSWORD=change-me         # or AUTH_ROOT_PASSWORD_HASH (bcrypt)
AUTH_SECRET=$(openssl rand -hex 32)  # HS256 session secret
AUTH_TOKEN_TTL=12h                   # session lifetime
```

The session is an HS256 JWT signed with `AUTH_SECRET`. If `AUTH_SECRET` is empty, a
random secret is generated per boot — which means **sessions reset on every restart**.
Set a stable secret for anything beyond throwaway local use.

:::caution[Dashboard must run without `CLERK_SECRET_KEY`]
In built-in mode the dashboard must start with `CLERK_SECRET_KEY` **unset**. The
Next.js middleware runs Clerk auth whenever that variable is present, which would
redirect-loop every route to `/sign-in`. It is commented out in
`apps/dashboard/.env.example`.
:::

## OIDC auth (Clerk, Keycloak, …)

For multi-user or multi-tenant deployments, validate JWTs from an external identity
provider. The management API mounts only when `OIDC_ISSUER` is set.

```bash
AUTH_MODE=oidc
OIDC_ISSUER=https://your-idp.example.com
OIDC_AUDIENCE=turbo-cache-forge
OIDC_ORG_CLAIM=org_id                # JWT claim carrying the tenant/org id
```

The API performs OIDC discovery at boot. If it can't reach the issuer it exits
(`log.Fatal`) — only set `OIDC_ISSUER` once the issuer's
`…/.well-known/openid-configuration` is reachable and returns a matching `iss`.

### Org mode vs personal mode

OIDC has two sub-modes, chosen by `OIDC_ORG_ENABLED`:

- **Org mode (`true`, default, multi-tenant).** Strict: the token must carry a
  matching `aud` **and** an `org_id` claim. Use this when one server serves multiple
  teams. Requires an IdP org/JWT-template setup so the token includes `org_id`.
- **Personal mode (`false`, single-tenant).** No org needed — the tenant is derived
  from the user's `sub` claim and provisioned just-in-time. The audience check is
  skipped.

:::danger[Personal mode requires a dedicated issuer]
Skipping the `aud` check means **any validly-signed token from that issuer is
accepted**. This is safe only when `OIDC_ISSUER` is dedicated to this app. An IdP
realm shared with other applications would let their tokens in too. The server
restates this warning in its boot log.
:::

## Which should I use?

| You want… | Use |
|---|---|
| A quick local or single-operator self-host | **Built-in** |
| One server, one team, real user accounts | **OIDC, personal mode** (dedicated issuer) |
| One server, many teams, isolated tenants | **OIDC, org mode** |
