# Read-only Tokens & Artifact Signature Enforcement — Design

_Date: 2026-07-10. Goal: close the two feature gaps the comparison page identified against
ducktors/turborepo-remote-cache — **read-only cache tokens** and **artifact signature
support** — implemented correctly against the real Turbo protocol._

## Background (verified facts)

- **Turbo artifact signing is 100% client-side.** With `signature: true` + a
  `TURBO_REMOTE_CACHE_SIGNATURE_KEY`, the `turbo` CLI computes
  `x-artifact-tag = base64(HMAC-SHA256(key, "artifact-signature:v2" ‖ hash ‖ team ‖ body))`
  (each field length-prefixed) on upload, and re-verifies it on download. **The server
  never holds the key and does no crypto** — it stores the tag on PUT and echoes it on GET.
  We already do this pass-through (`turbo/handlers.go` PUT ~L172, GET ~L225;
  `cache_artifacts.artifact_tag`). So client-side signing already works against our server.
- **ducktors' `TURBO_REMOTE_CACHE_SIGNATURE_KEY` is only a boolean toggle** meaning "require
  a tag to exist, else 404 the read." No server-side verification.
- **Turbo defines no token scopes.** Read/write is the server's concern. ducktors' default
  (static-token) mode is read-write only; read-only there is a *global* server flag.

## Decisions (locked with owner)

| Decision | Choice |
|---|---|
| Read-only granularity | **Per-token** `read_only` flag (better than ducktors' global static mode) |
| Signature enforcement | **Global env flag** `REQUIRE_ARTIFACT_SIGNATURE=true` |
| Scope | **Full stack**: Go API + migration + dashboard + CLI |

## Feature 1 — Read-only cache tokens

A cache token can be minted read-only. Read-only tokens may GET/HEAD/status/batch-exists
but **any PUT returns `403`**.

- **Migration** `003_token_scope.sql`: `ALTER TABLE api_keys ADD COLUMN read_only BOOLEAN NOT NULL DEFAULT false;`
- **Principal carries the flag**: add `ReadOnly bool` to `db.Org` (the per-request principal
  returned by `OrgByTokenHash`); `OrgByTokenHash` selects `k.read_only`. Middleware/context
  unchanged — the flag rides the existing `Org` that already flows to handlers.
- **Enforcement**: in `turbo/handlers.go` `put`, right after `OrgFromContext`, `if org.ReadOnly → 403`.
  Only the cache PUT is gated; `/api/v1` (the JWT/human world) is untouched (invariant: two
  auth worlds stay separate — a cache token never reaches mgmt routes).
- **Mint/list**: `POST /api/v1/tokens` accepts `{"name","read_only"}`; `CreateToken` repo
  signature gains `readOnly bool`; `APIKey`/`listTokens` expose `read_only`.
- **Types/client**: `@tcf/types` `Token.read_only`; api-client `createToken({name, read_only?})`.
- **Dashboard**: create-token dialog checkbox; API Keys page "Read-only" column.
- **CLI**: `turbo-cache token create --read-only`.

## Feature 2 — Artifact signature support (global flag)

New config `RequireArtifactSignature bool` from `REQUIRE_ARTIFACT_SIGNATURE`, plumbed into the
turbo `Handler`. A single switch (exactly like ducktors) that enables **both** the tag
round-trip and enforcement.

**Bug fixed en route:** GET currently returns `x-artifact-tag` from the *request* header
(`handlers.go:225`) — but a download client sends no such header, so the stored signature was
never returned and signed downloads silently failed. The correct behaviour is to return the
tag **stored at PUT** (`cache_artifacts.artifact_tag`). Add `ArtifactTag(ctx, orgID, hash)` to
the repo + `MetaRepo` interface for this.

Behaviour:
- **PUT** always stores the incoming `x-artifact-tag` (unchanged). When the flag is **on** and
  the tag is empty → `400` ("artifact signature required").
- **GET** when flag **on**: fetch the stored tag; empty → **cache miss (`404`)**; else set the
  `x-artifact-tag` response header and stream. When **off**: no tag handling at all.
- **HEAD** when flag **on**: tagless artifact → `404`.

**Invariant note:** the stored-tag read on GET happens **only when the flag is on**, so the
default download path stays DB-free (invariant "DB off the download hot path" preserved). When
enabled, the read is a single indexed point-lookup, far cheaper than the storage fetch already
on that path — a conscious, opt-in tradeoff for signature support. No key, no crypto, no body
buffering.

## Invariants respected (docs/ROADMAP.md)

- Two auth worlds never mixed — `read_only` lives on the cache token, enforced only in
  `internal/turbo`; `/api/v1` mgmt routes are not gated by it.
- Tokens stored only as SHA-256; `read_only` is metadata beside `token_hash`; hashing and
  once-only plaintext return unchanged.
- Streaming-only hot path — signature enforcement is a header presence check; the body still
  streams to `store.Put`, no buffering.
- `snake_case` end-to-end — `read_only` in every layer (Go json tags → `@tcf/types` → UI/CLI).
- Cache path stays SDK-free; tenant isolation via the existing `OrgByTokenHash` org join.

## Testing (TDD)

- **repo**: `CreateToken(readOnly=true)` persists and `OrgByTokenHash` returns `ReadOnly=true`;
  read-write default is false. (gated by `TEST_DATABASE_URL`, like existing repo tests.)
- **handler** (`turbo/handlers_test.go`): read-only principal → `PUT` returns `403`, `GET`
  still `200`. With `RequireArtifactSignature=true`: `PUT` without `x-artifact-tag` → `400`;
  `GET` of a tagless artifact → `404`; signed round-trip still works.

## Deliverables

- `infra/migrations/003_token_scope.sql`
- Go: `config`, `db.Org`+`OrgByTokenHash`+`CreateToken`+`APIKey`+`ListTokens`, `turbo/handlers.go` (+ `Handler` config), `mgmt/handlers.go` create/list, tests
- `@tcf/types`, `@tcf/api-client`, dashboard dialog + API Keys page, CLI `token create`
- Docs: env var reference + a short "read-only tokens / signatures" note; comparison-page rows
  flipped to ✅ (follow-up, once merged)
