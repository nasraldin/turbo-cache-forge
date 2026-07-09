# Artifacts Admin Enhancements — Design

**Date:** 2026-07-10
**Status:** Approved (design forks confirmed by user)

## Goal

Make the dashboard **Artifacts** page a genuinely useful, professional admin surface for a
self-hosted Turborepo remote cache: let an operator inspect *what a cached artifact actually
contains*, download it, delete individual artifacts, and clear the whole cache — all org-scoped
and consistent with the redesigned control-room UI.

## Subject grounding (verified facts)

- Turbo 2.x uploads each artifact as a **zstd-compressed tar** (magic `28b5 2ffd`). The tar holds
  the task's output files plus its `.turbo/turbo-build.log`. Files are typically small and text.
- The server stores the artifact **verbatim** (`application/octet-stream`) — no server-side
  compression, encryption, or transformation. A client *may* encrypt artifacts (Turbo signature
  key); those decode to non-tar bytes and must degrade gracefully to "opaque".
- Storage key = `"{orgSlug}/{hash}"` (`turbo.StorageKey`). Storage interface already has
  `Get`, `Head`, `Delete` (FS + S3). No `List`.
- DB `cache_artifacts (org_id, hash, size_bytes, artifact_tag, created_at, last_accessed_at)`,
  `UNIQUE (org_id, hash)`. `repo.DeleteArtifact(orgID, hash)` deletes the **row only**.
- Canonical single delete (blob then row) is already implemented in `cleanup.RunOnce`:
  `store.Delete(StorageKey(orgSlug, hash))` → `repo.DeleteArtifact(orgID, hash)`.
- Every mgmt handler resolves its tenant from `auth.OrgFromContext`; builtin mode = one implicit
  root org. New endpoints inherit this scoping for free.
- `github.com/klauspost/compress` (zstd) is already in the module graph (indirect via aws-sdk);
  promote to a direct dependency. Decode uses stdlib `archive/tar`.

## Design decisions (locked)

1. **Content viewer = manifest + inline text preview.** The detail response decodes the tarball to
   a file manifest and inlines the text of small text files, with a binary/oversized fallback.
2. **Destructive actions = per-row delete + clear-all**, both confirmed.
3. **Clear-all uses typed confirmation** ("delete all"); single delete uses a one-click confirm.

## Backend (Go) — `services/api`

### New / changed dependencies
- Promote `github.com/klauspost/compress` to a direct require (`go mod tidy`).

### New package: `internal/artifactview`
Pure, unit-testable decoder. No I/O beyond the reader it's given.

```go
type Entry struct {
    Path        string `json:"path"`
    Size        int64  `json:"size"`
    IsDir       bool   `json:"is_dir"`
    Preview     string `json:"preview,omitempty"`     // UTF-8 text, capped; empty if not previewable
    Previewable bool   `json:"previewable"`           // true iff a text preview is present
}

type Manifest struct {
    Format       string  `json:"format"`         // "zstd-tar" | "opaque"
    TotalEntries int     `json:"total_entries"`
    Truncated    bool    `json:"truncated"`      // entry cap or byte cap hit
    Entries      []Entry `json:"entries"`
}

// Caps (package constants):
//   maxEntries        = 1000      // stop listing beyond this (Truncated=true)
//   maxDecompressed   = 32 << 20  // 32 MiB total decompressed read budget (zip-bomb guard)
//   maxPreviewBytes   = 64 << 10  // per-file preview cap (64 KiB)
//   maxTotalPreview   = 512 << 10 // total inlined preview budget across all files
//
// Decode(r io.Reader) (Manifest, error):
//   - zstd-decompress via a LimitReader(maxDecompressed); if the first bytes aren't a valid
//     zstd stream OR tar parsing fails on the first header → return Manifest{Format:"opaque"}
//     (never an error the handler surfaces as 500 for a well-stored-but-unrecognized blob).
//   - Walk tar headers. For each regular file with Size <= maxPreviewBytes, read it; if it is
//     valid UTF-8 with no NUL byte and the total preview budget isn't exhausted, set Preview +
//     Previewable=true. Otherwise Previewable=false, no Preview.
//   - Directories: IsDir=true, Previewable=false.
```

Binary detection: a file is "text" iff it contains no NUL byte and is valid UTF-8 (`utf8.Valid`).

### New repo methods (`internal/db/repo.go`)
- `GetArtifact(ctx, orgID int64, hash string) (Artifact, error)` — single row by `(org_id, hash)`;
  returns `pgx.ErrNoRows`-mapped sentinel (reuse existing not-found pattern) when absent.
- `ListArtifactHashes(ctx, orgID int64) ([]string, error)` — all hashes for the org (for clear-all
  blob deletion). Ordered; no limit (org artifact counts are operator-scale, but read in one query).
- `DeleteAllArtifacts(ctx, orgID int64) (int64, error)` — `DELETE FROM cache_artifacts WHERE
  org_id=$1`, returns rows affected.
- Reuse existing `DeleteArtifact(ctx, orgID, hash)`.

### mgmt handler changes (`internal/mgmt/handlers.go`)
- Extend `Repo` interface with `GetArtifact`, `ListArtifactHashes`, `DeleteAllArtifacts`.
- `Handler` gains a `store storage.Storage`; `NewHandler(repo Repo, store storage.Storage)`.
  Router call becomes `mgmt.NewHandler(d.Repo, d.Store)`.
- Blob key via `turbo.StorageKey(org.Slug, hash)` (mgmt→turbo import; no cycle).

New routes (mounted in `Handler.Mount`, all under the existing `/api/v1` auth group):

| Method | Path                          | Behavior |
|--------|-------------------------------|----------|
| GET    | `/artifacts/{hash}`           | Metadata (`GetArtifact`) + decoded `content` manifest (fetch blob via `store.Get`, `artifactview.Decode`). 404 if the row is absent. If the blob is missing, return metadata with `content.format="opaque"` + `content.entries=[]`. |
| GET    | `/artifacts/{hash}/download`  | `store.Get` → stream raw bytes; `Content-Type: application/octet-stream`, `Content-Disposition: attachment; filename="{hash}.tar.zst"`, `Content-Length` from `Head`. 404 if blob absent. |
| DELETE | `/artifacts/{hash}`           | `store.Delete(key)` then `repo.DeleteArtifact` (blob-then-row, mirroring `cleanup.RunOnce`). `204`. Idempotent: deleting an already-gone artifact still `204`. |
| DELETE | `/artifacts`                  | Clear-all: `ListArtifactHashes` → `store.Delete` each (continue on per-blob error), then `DeleteAllArtifacts`. Respond `200 {"deleted": <rowCount>}`. |

`{hash}` is validated with the same `turbo.validHash` rules (exported or duplicated) to reject
path-escape before building a storage key.

Detail response shape:
```json
{
  "hash": "271ca18f04d13daa",
  "size_bytes": 290,
  "tag": null,
  "created_at": "2026-07-09T20:09:20Z",
  "last_accessed_at": "2026-07-09T20:09:24Z",
  "content": { "format": "zstd-tar", "total_entries": 3, "truncated": false, "entries": [ ... ] }
}
```

### OpenAPI
Add the four routes to `internal/openapi` spec (docs only; response schemas are already sparse in
this project — keep parity with existing style, no invented schemas beyond what handlers return).

## Frontend — `apps/dashboard`

### Types (`packages/types`)
`ArtifactEntry`, `ArtifactContent`, `ArtifactDetail` (extends `Artifact` with `content`),
`ClearArtifactsResult { deleted: number }`.

### API client (`packages/api-client`)
- `getArtifact(hash): Promise<ArtifactDetail>` → `GET /artifacts/{hash}`.
- `deleteArtifact(hash): Promise<void>` → `DELETE /artifacts/{hash}` (204).
- `clearArtifacts(): Promise<ClearArtifactsResult>` → `DELETE /artifacts`.
- `getArtifactBlob(hash): Promise<Blob>` → `GET /artifacts/{hash}/download` (auth header attached;
  page turns the Blob into an object-URL download so the JWT never rides in a bare anchor href).

### Artifacts page (`app/(dashboard)/artifacts/page.tsx`)
- **Summary strip**: artifact count + total size (from `/stats`), styled as `StatTile`-adjacent
  eyebrow readouts — reinforces "this is the whole cache footprint".
- **Header action**: `Clear all` (danger `Button`), disabled when there are zero artifacts.
- **Row actions column**: an **Eye** icon (open details) and a **Trash** icon (delete). ≥44px hit
  targets on mobile.
- Keep offset pagination + the existing empty/error states and the `TURBO_TOKEN` empty copy.

### New components
- `ArtifactDetailDialog` (reuses the shared `Dialog`, so it inherits the top-center animation):
  metadata grid (hash w/ copy, size, tag, created, last accessed) + a **file manifest** list
  (path + size, dir/file icon). Text-previewable entries expand to a `font-data` code block
  (preview string already inlined). `format: "opaque"` → a neutral "Encrypted or non-Turbo
  artifact — download to inspect" note. A **Download** button (uses `getArtifactBlob`).
- `ClearArtifactsDialog`: typed-confirmation modal — the destructive button stays disabled until
  the user types `delete all`. On success, toast/inline "Removed N artifacts" and invalidate
  `["artifacts"]` + `["stats"]`.
- Single delete: a lightweight confirm (reuse `Dialog` with a compact body; not typed).

### Data flow
TanStack Query: `["artifacts", offset]` (list, existing), `["artifact", hash]` (detail, enabled
only when the drawer is open). Mutations invalidate `["artifacts"]` and `["stats"]` so the summary
strip and Overview stay live.

## Testing

- **Go**: `artifactview.Decode` table tests — a real zstd-tar fixture (text + a synthetic binary
  file + a directory), an oversized-preview case, a NUL-byte binary case, an entry-cap truncation
  case, and an "opaque" (non-zstd / random bytes) case. mgmt handler tests for GET detail (200 +
  manifest, 404 missing row), DELETE single (204, blob+row gone), DELETE all (`{"deleted":n}`),
  and hash-validation rejects. Reuse the storage conformance/in-memory patterns already in the
  repo's handler tests.
- **Dashboard (vitest)**: api-client tests for the 4 new methods (correct verbs/paths, blob
  handling); Artifacts page tests — renders rows with actions, opens detail dialog and shows
  manifest + preview, delete flow invalidates, clear-all button is gated on the typed phrase.
  Preserve the existing `/no artifacts cached yet/i` + `TURBO_TOKEN` assertions.

## Constraints (must keep green)

- The **two-auth-worlds invariant** stays intact — new routes live only in the mgmt (`/api/v1`)
  group, never the `/v8` cache path.
- Existing artifact-list contract, empty/error copy, and testids unchanged.
- Zip-bomb / DoS safety: every decode path is bounded by the caps above; download streams without
  buffering the whole blob.
- Redesign tokens/components reused (Dialog, Button, Badge, eyebrow, font-data); light + dark both
  work.

## Out of scope (YAGNI)

- Per-entry lazy fetch endpoints (previews are inlined; artifacts are small).
- Editing/re-tagging artifacts. Project-level filtering (write path never sets `project_id`).
- Restore/undo of deletes. Multi-select bulk delete (per-row + clear-all cover the need).
