# SQLite as the default zero-setup metadata store

**Date:** 2026-07-10
**Status:** Approved (design)
**Branch:** `feat/sqlite-default-store`

## Problem

Turbo Cache Forge requires an external **PostgreSQL** server for its metadata
(orgs, tokens, projects, artifacts, usage). That is a real setup cost and is the
one place a competitor — `ducktors/turborepo-remote-cache` — has a genuine
advantage: it needs **no database**. To match that low-setup story while keeping
our operator features, the default deployment must run with **zero external
database setup**.

## Goal

- **SQLite is the default** metadata store. `docker compose up -d` starts only
  `cache-api` + `dashboard` — no `postgres`, no `migrate` container, no external
  DB. The API creates and migrates its own SQLite file on first boot.
- **Postgres stays first-class**, selected via `DATABASE_URL` and an opt-in
  Compose overlay, for multi-node / HA / high-write deployments.
- One binary supports both; no logic is duplicated between them.

## Non-goals (YAGNI)

- No Postgres→SQLite (or reverse) **data-migration tool**. Switching engines
  starts a fresh store.
- No connecting to multiple databases at once, no ORM, no SQLite FTS/vector.
- No change to the two-auth-worlds model, the streaming hot path, or the
  DB-free download path.

## Decisions (locked)

1. **Self-migrate on boot.** The API embeds migrations and runs them at startup
   for whichever engine. No separate migrate step in either path.
2. **Postgres first-class opt-in.** Both drivers compiled into one binary;
   engine chosen by `DATABASE_URL` scheme.
3. **Unify on `database/sql`.** Rewrite the single DB file from `pgxpool` onto
   Go's standard `database/sql` with two registered drivers. One query set.

## Architecture

All database coupling lives in **one file**, `services/api/internal/db/repo.go`
(435 lines, 20 queries, the only pgx import). No transactions, no
`SELECT … FOR UPDATE`, no arrays/jsonb/CTEs/window functions. Consuming packages
(`turbo`, `mgmt`, `cleanup`, `usage`, `auth`, `oidcauth`, `localauth`) already
depend on **their own minimal interfaces**, which `*db.Repo` satisfies
structurally. So the change is contained to `repo.go`, `config.go`, and
`main.go` — nothing else moves.

### Drivers (one binary)

| Engine | `database/sql` driver | Notes |
|---|---|---|
| Postgres | `github.com/jackc/pgx/v5/stdlib` | keeps pgx's proven driver, registered as `pgx` |
| SQLite | `modernc.org/sqlite` | **pure Go, no cgo** — preserves the distroless static image |

`Repo` changes from `struct{ pool *pgxpool.Pool }` to
`struct{ db *sql.DB; dialect dialect }`. `Open` opens the right driver and
applies SQLite pragmas.

### Driver detection & defaults (`config.go`)

Pick the engine from the `DATABASE_URL` scheme:

- `postgres://` / `postgresql://` → Postgres
- `sqlite:` / `file:` / a bare filesystem path → SQLite
- **empty → default `sqlite:///data/tcf.db`** (inside the existing persisted
  `cache-data` volume). `DATABASE_URL` is **no longer required** — this is the
  zero-config win. `config.go` stops erroring on empty.

### Query layer (one query set)

Queries are authored with `?` placeholders. A small `rebind(driver, q)` rewrites
`?`→`$N` for Postgres (~12 lines, no new dependency). Only three expressions
diverge, handled by a tiny `dialect` struct and, for one method, a second query
string:

| Divergence | Postgres | SQLite | Sites |
|---|---|---|---|
| current time | `now()` | `CURRENT_TIMESTAMP` | 4 (`UpsertArtifact`, `TouchArtifact`, `RevokeToken`, upsert `last_accessed_at`) |
| `StatsSeries` day format + window | `to_char(day,'YYYY-MM-DD')`, `CURRENT_DATE - $2::int` | `strftime('%Y-%m-%d',day)`, `date('now', -? \|\| ' days')` | 1 method, two variants |
| `AddUsage` day cast | `$2::date` | — | bind `day` as a Go-formatted `YYYY-MM-DD` string for **both**; drop the cast |

Portable unchanged (verified against SQLite ≥ 3.35, which modernc bundles):
`ON CONFLICT (…) DO UPDATE SET … = excluded.…`, `RETURNING`, `EXISTS`,
`COALESCE`, `NULLIF`, `SUM/COUNT`, `LIMIT/OFFSET`, `ORDER BY … DESC`.

Under `database/sql`, `pgx.ErrNoRows` becomes `sql.ErrNoRows` (the 3 checks) and
pgx `CommandTag.RowsAffected()` becomes `sql.Result.RowsAffected()` (2 sites) —
both unify for free.

### Migrations — self-migrate on boot

- Migrations move **into the module** (Go `embed` requires in-tree files):
  `services/api/internal/db/migrations/{postgres,sqlite}/`.
  - `postgres/` = the current three files (`001_initial`, `002_usage_and_indexes`,
    `003_token_readonly`), unchanged.
  - `sqlite/` = a translated set: `INTEGER PRIMARY KEY AUTOINCREMENT` for ids,
    `CURRENT_TIMESTAMP` defaults, `0/1` for the read-only boolean,
    `PRAGMA foreign_keys=ON` (set per-connection at open, not in DDL), and
    **drop the `CHECK (slug ~ '^[a-z0-9-]+$')`** — SQLite has no regex operator;
    slug validation already happens in Go before insert.
- `main.go` embeds both dialect dirs via `embed.FS` and runs
  `github.com/pressly/goose/v3` in **library mode** (`goose.SetBaseFS`,
  `goose.SetDialect`, `goose.Up`) against the open `*sql.DB` on boot. Idempotent
  and safe to re-run. The standalone `goose` Docker target is removed from the
  default path (boot-migrate replaces it).

### Concurrency (honest ceiling)

SQLite opens with `journal_mode=WAL`, `busy_timeout=5000`, `foreign_keys=ON`,
and `db.SetMaxOpenConns(1)` (serialize writes → no `SQLITE_BUSY`). This is safe
because:

- the **download hot path is already DB-free** (streaming),
- writes are low-volume and single-statement: `UpsertArtifact` on PUT,
  async `TouchArtifact` on download, interval `AddUsage` rollups, batched
  cleanup deletes,
- concurrency correctness rides on `ON CONFLICT` upserts, not transactions —
  these map directly to SQLite.

`// ponytail:` documented ceiling — **SQLite = single node.** Multi-replica /
HA / heavy parallel-write deployments use the Postgres overlay (which also needs
shared session state, already noted in `main.go`). Postgres keeps normal
pooling.

## Docker Compose

- **Default `infra/docker/docker-compose.yml`:** remove `postgres` and
  `migrate`; `cache-api` runs SQLite at `/data/tcf.db` (in the existing
  `cache-data` volume), self-migrating on boot. `dashboard` unchanged.
- **New `infra/docker/docker-compose.postgres.yml` overlay:** adds the
  `postgres` service and sets `cache-api.DATABASE_URL=postgres://…` +
  `depends_on: postgres`. Boot-migrate handles the schema.

```bash
# default — zero external DB
docker compose -f infra/docker/docker-compose.yml up -d

# opt into Postgres
docker compose -f infra/docker/docker-compose.yml \
  -f infra/docker/docker-compose.postgres.yml up -d
```

`.env.example` documents the default and the Postgres opt-in.

## Testing & verification

- **Unit:** repo tests run against a **temp-file SQLite** by default (no service
  needed) — most of the suite gets faster and dependency-free. Add a `rebind`
  unit test (`?`→`$N`) and a `dialect` selection test.
- **CI matrix:** run the Go DB tests against **both** `sqlite` and `postgres`
  (Postgres via the existing service container) so neither dialect regresses.
- **End-to-end:** bring up the SQLite-default stack, run `apps/example`
  (cold build → MISS+upload, warm build → remote HIT), and confirm the dashboard
  shows hit rate / storage / artifacts — the same verification already used for
  the Postgres stack.

## Docs

- `getting-started/quickstart` + `getting-started/configuration`: SQLite is the
  default; document `DATABASE_URL` schemes and the Postgres overlay.
- **Comparison page + README:** update the "Metadata store" row from
  "Postgres (required)" to **"SQLite (default) · Postgres (optional)"**, closing
  ducktors' "no database required" edge. Add a one-line note in "honest caveats".

## Files touched (summary)

| File | Change |
|---|---|
| `services/api/internal/db/repo.go` | rewrite pgxpool → `database/sql`; add `rebind` + `dialect`; `sql.ErrNoRows` |
| `services/api/internal/db/migrations/{postgres,sqlite}/*.sql` | relocate PG set; add translated SQLite set |
| `services/api/internal/config/config.go` | scheme detection; default `sqlite:///data/tcf.db`; stop requiring `DATABASE_URL` |
| `services/api/cmd/server/main.go` | open by scheme; embed migrations; `goose.Up` on boot |
| `services/api/go.mod` | add `modernc.org/sqlite`, `pgx/v5/stdlib`, `pressly/goose/v3` |
| `infra/docker/docker-compose.yml` | drop postgres+migrate; SQLite default |
| `infra/docker/docker-compose.postgres.yml` | new opt-in overlay |
| `infra/docker/Dockerfile` | keep the `goose` target as an optional standalone tool; the `migrate` service is removed from **both** compose files (boot-migrate covers both engines) |
| `.env.example`, docs, comparison, README | reflect the new default |
| `.github/workflows/ci.yml` | DB test matrix: sqlite + postgres |

## Risks & mitigations

- **`modernc.org/sqlite` is a large pure-Go dependency** (transpiled SQLite),
  growing build time and binary size. Accepted — it's the standard cost of a
  cgo-free static image, and it removes an entire external service.
- **Dialect drift** (a query that works on one engine, not the other). Mitigated
  by the CI matrix running the full repo suite on both engines.
- **Migration divergence** between the two DDL sets. Mitigated by keeping the
  schemas trivially parallel (same tables/columns) and the boot-migrate +
  matrix tests exercising both from empty.
