# turbo-cache-forge — Phase 4 (Dashboard) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **Design skills (read before building UI, don't inline them):** consult `frontend-design` for aesthetic direction — the dashboard should read as an intentional product, not a shadcn template dump (deliberate type scale, spacing rhythm, one restrained accent, empty/loading/error states that feel designed). Consult `dataviz` before writing any chart code in Task 6 — chart color formula, mark specs, light/dark parity, stat-tile layout.

**Goal:** A focused management + observability dashboard for turbo-cache-forge that a human logs into with Clerk and uses to watch cache health and manage tokens/projects — talking to **exactly one backend, the Phase-3 `/api/v1`**, and nothing else. Provable by: log in via Clerk → see live stats from `/api/v1` → create a token in the UI (plaintext shown once) → use it on the cache path → revoke it → confirm the cache path now 401s.

**Architecture:** pnpm + Turborepo monorepo. `apps/dashboard` is a Next.js 15 App Router client that renders server components for the shell and client components (TanStack Query) for the live data. Clerk holds the browser session; every `/api/v1` call carries the Clerk JWT in `Authorization: Bearer <jwt>` and the Phase-3 OIDC middleware validates it. The dashboard is backend-agnostic and self-hostable: `NEXT_PUBLIC_API_URL` is the only wiring to the Go API. The dashboard **never** holds a cache (`turbo_`) token, never touches Postgres or object storage, and never calls `/v8/artifacts`. Shared TS types live in `packages/types`; the typed SDK over `/api/v1` lives in `packages/api-client`.

```
 Human (browser)
   │ Clerk session (JWT)
   ▼
 apps/dashboard (Next.js 15)  ──uses──►  packages/api-client ──fetch(JWT)──►  /api/v1 (Phase 3 Go API)
   │                                        │                                     │ validates JWT vs JWKS
   └─ shadcn/ui + Tailwind + TanStack Query └─ packages/types (shared shapes)     └─ orgs/projects/tokens/stats/artifacts
```

**Tech Stack:** pnpm 9 (workspaces) + Turborepo 2. Next.js 15 (App Router, React 19) + TypeScript 5 + Tailwind CSS 4 + shadcn/ui (Radix primitives) + TanStack Query v5 + `@clerk/nextjs` v6. Apache ECharts (`echarts` + `echarts-for-react`) for the one trend panel that earns it. Vitest 2 + React Testing Library + `@testing-library/jest-dom` + `jsdom` for component tests; Playwright for the one critical end-to-end flow. Node 20 base image, multi-stage Docker with Next.js `output: "standalone"`.

## Global Constraints

- **One backend, `/api/v1` only.** The dashboard talks to `NEXT_PUBLIC_API_URL` + `/api/v1/*`. No direct DB, no object storage, no `/v8/artifacts`. All network access routes through `packages/api-client`; no bare `fetch()` to the API anywhere in `apps/dashboard`.
- **Two auth worlds stay separate (cross-phase invariant).** The dashboard is the *human* world: Clerk JWT only. It must never generate, store, display-in-storage, or transmit a cache bearer token except the **one-time plaintext** returned by `POST /api/v1/tokens` (rendered once, never re-fetched, never persisted client-side).
- **API keys shown once then hashed server-side (cross-phase invariant).** The create-token UI shows the plaintext exactly once from the create response; there is no "reveal" action, because the server only stores the SHA-256 hash. Every later render shows metadata only.
- **`NEXT_PUBLIC_API_URL` is the only backend coupling.** No hard-coded hosts. Self-hosters change one env var. Clerk keys (`NEXT_PUBLIC_CLERK_PUBLISHABLE_KEY`, `CLERK_SECRET_KEY`) are the only other required env.
- **The SDK is the seam.** `packages/api-client` is the *only* module that knows the URL shape and attaches the JWT. Pages import typed functions, never construct URLs. `packages/types` is the single source of the response shapes, imported by both the SDK and the pages.
- **Every task ends green** (`pnpm test` for the touched package, `pnpm -w typecheck`, `pnpm -w lint`) and is committed. Frontend TDD: write the failing Vitest/RTL component test first, then the component. Playwright covers only the critical token-lifecycle flow (Task 10).
- **YAGNI on charts.** Tiles and tables are plain numbers/HTML first. Exactly one ECharts panel (the hit/miss trend, Task 6) — add a second only when a real trend genuinely needs it. Billing is a static stub.
- Package versions are pinned in each `package.json` (no `^` drift for the framework-critical deps); Turborepo caches `build`/`lint`/`typecheck`/`test` via `turbo.json`.

---

## File structure (monorepo additions — `services/api` and `infra/migrations` already exist from Phases 1–3)

```
turbo-cache-forge/
  package.json                      root: workspaces + turbo scripts (dev/build/lint/test/typecheck)
  turbo.json                        pipeline: build depends on ^build, test/lint/typecheck cached
  pnpm-workspace.yaml               packages: apps/*, packages/*
  .npmrc                            strict-peer-dependencies=false (Next 15 / React 19)

  packages/types/
    package.json
    tsconfig.json
    src/index.ts                    Stats, Project, Artifact, Token, Paginated<T> — the /api/v1 shapes

  packages/api-client/
    package.json
    tsconfig.json
    vitest.config.ts
    src/client.ts                   createApiClient({ baseUrl, getToken }) → typed methods
    src/client.test.ts              mocked-fetch tests (URL, JWT header, error mapping)
    src/index.ts

  apps/dashboard/
    package.json
    tsconfig.json
    next.config.ts                  output: "standalone"
    tailwind.config.ts / postcss.config.mjs
    vitest.config.ts / vitest.setup.ts
    playwright.config.ts
    middleware.ts                   Clerk route protection
    .env.example
    Dockerfile
    src/
      app/
        layout.tsx                  <ClerkProvider> + <QueryProvider> + app shell
        globals.css                 Tailwind + design tokens
        providers.tsx               TanStack Query client (client component)
        api.ts                      useApiClient() — wires SDK to Clerk getToken + NEXT_PUBLIC_API_URL
        (dashboard)/
          layout.tsx                sidebar nav + <UserButton/> + <OrganizationSwitcher/>
          page.tsx                  Overview
          projects/page.tsx
          statistics/page.tsx
          artifacts/page.tsx
          api-keys/page.tsx
          team/page.tsx
          storage/page.tsx
          settings/page.tsx
          billing/page.tsx          stub
        sign-in/[[...sign-in]]/page.tsx
        sign-up/[[...sign-up]]/page.tsx
      components/
        stat-tile.tsx
        stat-tile.test.tsx
        data-table.tsx
        page-header.tsx
        hit-rate-chart.tsx          the one ECharts panel
        create-token-dialog.tsx
        create-token-dialog.test.tsx
        ui/                         shadcn primitives (button, card, dialog, table, input, badge, skeleton)
      lib/
        format.ts                   bytes/percent/date formatters (+ test)
        format.test.ts
      e2e/
        token-lifecycle.spec.ts     Playwright critical flow

  infra/docker/
    docker-compose.yml              (extend) add `dashboard` service driven by NEXT_PUBLIC_API_URL
```

---

## Task 1: Monorepo wiring (pnpm + Turborepo) + `packages/types`

**Files:**
- Create: `package.json`, `turbo.json`, `pnpm-workspace.yaml`, `.npmrc`, `packages/types/package.json`, `packages/types/tsconfig.json`, `packages/types/src/index.ts`
- Test: (types package is compile-only; `pnpm -w typecheck` is its check)

**Interfaces:**
- Produces the `@tcf/types` package exporting the `/api/v1` response shapes every downstream module imports; a `pnpm dev` / `pnpm build` / `pnpm test` root that Turborepo drives.

- [ ] **Step 1: Workspace + root scripts**

`pnpm-workspace.yaml`:
```yaml
packages:
  - "apps/*"
  - "packages/*"
```

`.npmrc`:
```
strict-peer-dependencies=false
auto-install-peers=true
```

`package.json` (root):
```json
{
  "name": "turbo-cache-forge",
  "private": true,
  "packageManager": "pnpm@9.12.0",
  "scripts": {
    "dev": "turbo run dev",
    "build": "turbo run build",
    "lint": "turbo run lint",
    "test": "turbo run test",
    "typecheck": "turbo run typecheck"
  },
  "devDependencies": {
    "turbo": "2.1.3",
    "typescript": "5.6.3"
  }
}
```

`turbo.json`:
```json
{
  "$schema": "https://turbo.build/schema.json",
  "tasks": {
    "build": { "dependsOn": ["^build"], "outputs": [".next/**", "!.next/cache/**", "dist/**"] },
    "dev": { "cache": false, "persistent": true },
    "lint": { "dependsOn": ["^build"] },
    "typecheck": { "dependsOn": ["^build"] },
    "test": { "dependsOn": ["^build"] }
  }
}
```
`// ponytail: Go's services/api has no package.json, so Turborepo simply never sees it — the Go build stays on its own toolchain. No cross-language wiring needed.`

- [ ] **Step 2: `@tcf/types` — the one source of the API shapes**

`packages/types/package.json`:
```json
{
  "name": "@tcf/types",
  "version": "0.0.0",
  "private": true,
  "main": "./src/index.ts",
  "types": "./src/index.ts",
  "scripts": {
    "lint": "eslint src --max-warnings 0 || true",
    "typecheck": "tsc --noEmit"
  },
  "devDependencies": { "typescript": "5.6.3" }
}
```

`packages/types/tsconfig.json`:
```json
{
  "compilerOptions": {
    "target": "ES2022",
    "module": "ESNext",
    "moduleResolution": "Bundler",
    "strict": true,
    "declaration": true,
    "noEmit": true,
    "skipLibCheck": true
  },
  "include": ["src"]
}
```

`packages/types/src/index.ts` — these mirror the Phase-3 `/api/v1` responses (storage used, hit/miss, requests, projects, tokens, artifacts):
```ts
export interface Stats {
  storageBytes: number;
  hits: number;
  misses: number;
  requests: number;
  hitRate: number; // 0..1, server-computed
}

export interface StatsPoint {
  day: string; // ISO date
  hits: number;
  misses: number;
  bytesUploaded: number;
  bytesDownloaded: number;
}

export interface Project {
  id: number;
  slug: string;
  name: string;
  createdAt: string;
}

export interface Artifact {
  hash: string;
  sizeBytes: number;
  tag: string | null;
  projectSlug: string | null;
  createdAt: string;
  lastAccessedAt: string;
}

export interface Token {
  id: number;
  name: string;
  lastUsedAt: string | null;
  createdAt: string;
  revokedAt: string | null;
}

// POST /api/v1/tokens — plaintext present exactly once, on create.
export interface CreatedToken extends Token {
  token: string;
}

export interface Paginated<T> {
  items: T[];
  nextCursor: string | null;
}
```

- [ ] **Step 3: Install + verify + commit**

```bash
pnpm install
pnpm -w typecheck   # types package compiles clean
git add package.json turbo.json pnpm-workspace.yaml .npmrc packages/types pnpm-lock.yaml
git commit -m "feat(dashboard): pnpm+turborepo workspace and shared @tcf/types"
```

**Self-review:** one lockfile at root; `@tcf/types` has zero runtime deps; the Go service is untouched and invisible to Turborepo. Shapes match what Phase 3 returns — if Phase 3's JSON differs, this file is the single place to reconcile.

---

## Task 2: `packages/api-client` — typed SDK over `/api/v1` (with mocked-fetch tests)

**Files:**
- Create: `packages/api-client/package.json`, `tsconfig.json`, `vitest.config.ts`, `src/client.ts`, `src/index.ts`
- Test: `packages/api-client/src/client.test.ts`

**Interfaces:**
- Produces:
```ts
createApiClient(opts: { baseUrl: string; getToken: () => Promise<string | null> }): ApiClient
interface ApiClient {
  getStats(): Promise<Stats>
  getStatsTimeseries(days: number): Promise<StatsPoint[]>
  listProjects(): Promise<Project[]>
  createProject(input: { slug: string; name: string }): Promise<Project>
  listArtifacts(params?: { cursor?: string; limit?: number }): Promise<Paginated<Artifact>>
  listTokens(): Promise<Token[]>
  createToken(input: { name: string }): Promise<CreatedToken>
  revokeToken(id: number): Promise<void>
}
class ApiError extends Error { status: number }
```

- [ ] **Step 1: Package + vitest config**

`packages/api-client/package.json`:
```json
{
  "name": "@tcf/api-client",
  "version": "0.0.0",
  "private": true,
  "main": "./src/index.ts",
  "types": "./src/index.ts",
  "scripts": {
    "test": "vitest run",
    "lint": "eslint src --max-warnings 0 || true",
    "typecheck": "tsc --noEmit"
  },
  "dependencies": { "@tcf/types": "workspace:*" },
  "devDependencies": {
    "typescript": "5.6.3",
    "vitest": "2.1.2"
  }
}
```

`packages/api-client/vitest.config.ts`:
```ts
import { defineConfig } from "vitest/config";
export default defineConfig({ test: { environment: "node" } });
```

`packages/api-client/tsconfig.json`: same as `@tcf/types` but add `"types": ["vitest/globals"]` is unnecessary — the test imports from `vitest` explicitly.

- [ ] **Step 2: Write the failing test first**

`packages/api-client/src/client.test.ts`:
```ts
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError, createApiClient } from "./client";

function mockFetch(body: unknown, init: Partial<Response> = {}) {
  return vi.fn().mockResolvedValue({
    ok: init.status ? init.status < 400 : true,
    status: init.status ?? 200,
    json: async () => body,
    text: async () => JSON.stringify(body),
  } as Response);
}

const base = "https://api.example.com";

afterEach(() => vi.restoreAllMocks());

describe("api-client", () => {
  it("GETs /api/v1/stats with the JWT attached", async () => {
    const fetchMock = mockFetch({ storageBytes: 10, hits: 8, misses: 2, requests: 10, hitRate: 0.8 });
    vi.stubGlobal("fetch", fetchMock);
    const client = createApiClient({ baseUrl: base, getToken: async () => "jwt-123" });

    const stats = await client.getStats();

    expect(stats.hitRate).toBe(0.8);
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe(`${base}/api/v1/stats`);
    expect((init.headers as Record<string, string>).Authorization).toBe("Bearer jwt-123");
  });

  it("POSTs a token and returns the one-time plaintext", async () => {
    const fetchMock = mockFetch(
      { id: 1, name: "ci", token: "turbo_PLAINTEXT_ONCE", lastUsedAt: null, createdAt: "t", revokedAt: null },
      { status: 201 },
    );
    vi.stubGlobal("fetch", fetchMock);
    const client = createApiClient({ baseUrl: base, getToken: async () => "jwt" });

    const created = await client.createToken({ name: "ci" });

    expect(created.token).toBe("turbo_PLAINTEXT_ONCE");
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe(`${base}/api/v1/tokens`);
    expect(init.method).toBe("POST");
    expect(JSON.parse(init.body as string)).toEqual({ name: "ci" });
  });

  it("throws ApiError with the status on a non-2xx", async () => {
    const fetchMock = mockFetch({ error: "unauthorized" }, { status: 401 });
    vi.stubGlobal("fetch", fetchMock);
    const client = createApiClient({ baseUrl: base, getToken: async () => null });

    await expect(client.getStats()).rejects.toMatchObject({ name: "ApiError", status: 401 });
  });

  it("passes pagination params through", async () => {
    const fetchMock = mockFetch({ items: [], nextCursor: null });
    vi.stubGlobal("fetch", fetchMock);
    const client = createApiClient({ baseUrl: base, getToken: async () => "jwt" });

    await client.listArtifacts({ cursor: "c1", limit: 50 });

    expect(fetchMock.mock.calls[0][0]).toBe(`${base}/api/v1/artifacts?limit=50&cursor=c1`);
  });
});
```

- [ ] **Step 3: Run → FAIL, then implement**

```bash
pnpm --filter @tcf/api-client test   # FAIL: cannot resolve ./client
```

`packages/api-client/src/client.ts`:
```ts
import type {
  Artifact, CreatedToken, Paginated, Project, Stats, StatsPoint, Token,
} from "@tcf/types";

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

export interface ApiClientOptions {
  baseUrl: string;
  getToken: () => Promise<string | null>;
}

export function createApiClient(opts: ApiClientOptions) {
  const root = `${opts.baseUrl.replace(/\/$/, "")}/api/v1`;

  async function request<T>(path: string, init?: RequestInit): Promise<T> {
    const token = await opts.getToken();
    const res = await fetch(`${root}${path}`, {
      ...init,
      headers: {
        "Content-Type": "application/json",
        ...(token ? { Authorization: `Bearer ${token}` } : {}),
        ...(init?.headers ?? {}),
      },
    });
    if (!res.ok) {
      const text = await res.text().catch(() => res.statusText);
      throw new ApiError(res.status, text || `request failed: ${res.status}`);
    }
    if (res.status === 204) return undefined as T;
    return (await res.json()) as T;
  }

  return {
    getStats: () => request<Stats>("/stats"),
    getStatsTimeseries: (days: number) =>
      request<StatsPoint[]>(`/stats/timeseries?days=${days}`),
    listProjects: () => request<Project[]>("/projects"),
    createProject: (input: { slug: string; name: string }) =>
      request<Project>("/projects", { method: "POST", body: JSON.stringify(input) }),
    listArtifacts: (params?: { cursor?: string; limit?: number }) => {
      const q = new URLSearchParams();
      if (params?.limit) q.set("limit", String(params.limit));
      if (params?.cursor) q.set("cursor", params.cursor);
      const suffix = q.toString() ? `?${q}` : "";
      return request<Paginated<Artifact>>(`/artifacts${suffix}`);
    },
    listTokens: () => request<Token[]>("/tokens"),
    createToken: (input: { name: string }) =>
      request<CreatedToken>("/tokens", { method: "POST", body: JSON.stringify(input) }),
    revokeToken: (id: number) =>
      request<void>(`/tokens/${id}`, { method: "DELETE" }),
  };
}

export type ApiClient = ReturnType<typeof createApiClient>;
```
`URLSearchParams` orders `limit` before `cursor` because they are set in that order — the test asserts that exact string. `src/index.ts`: `export * from "./client";`.

- [ ] **Step 4: Run + commit**

```bash
pnpm --filter @tcf/api-client test   # PASS (4 tests)
pnpm -w typecheck
git add packages/api-client pnpm-lock.yaml
git commit -m "feat(dashboard): typed @tcf/api-client SDK over /api/v1 with JWT auth"
```

**Self-review:** the SDK is the *only* place a URL is built or a JWT is attached (Global Constraint). `getToken` is injected, so the package has zero Clerk dependency and is unit-testable with a plain function. `ApiError` carries `status` so pages can branch on 401 vs 500.

---

## Task 3: Dashboard scaffold — Next.js 15 + Clerk auth + TanStack Query + app shell

**Files:**
- Create: `apps/dashboard/package.json`, `next.config.ts`, `tsconfig.json`, `tailwind.config.ts`, `postcss.config.mjs`, `vitest.config.ts`, `vitest.setup.ts`, `middleware.ts`, `.env.example`, `src/app/layout.tsx`, `src/app/globals.css`, `src/app/providers.tsx`, `src/app/api.ts`, `src/app/(dashboard)/layout.tsx`, `src/app/sign-in/[[...sign-in]]/page.tsx`, `src/app/sign-up/[[...sign-up]]/page.tsx`, `src/lib/format.ts`, plus shadcn `src/components/ui/*`
- Test: `apps/dashboard/src/lib/format.test.ts`

**Interfaces:**
- Produces: the running Next.js app protected by Clerk; `useApiClient()` hook that binds `@tcf/api-client` to Clerk's `getToken()` and `NEXT_PUBLIC_API_URL`; the shared layout/sidebar every page slots into.

- [ ] **Step 1: Scaffold + deps**

```bash
cd apps
pnpm create next-app@15 dashboard --ts --tailwind --app --src-dir --import-alias "@/*" --no-eslint --use-pnpm
cd dashboard
pnpm add @clerk/nextjs@6 @tanstack/react-query@5 @tcf/types@workspace:* @tcf/api-client@workspace:*
pnpm add echarts@5 echarts-for-react@3
pnpm add -D vitest@2 @vitejs/plugin-react jsdom @testing-library/react @testing-library/user-event @testing-library/jest-dom @playwright/test
# shadcn primitives used across pages:
pnpm dlx shadcn@latest init -d
pnpm dlx shadcn@latest add button card dialog input table badge skeleton sonner
```

Set `next.config.ts` → `export default { output: "standalone" };` (required by the Task 11 Docker image).

- [ ] **Step 2: Clerk provider + Query provider + shell**

`src/app/providers.tsx` (client):
```tsx
"use client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState, type ReactNode } from "react";

export function QueryProvider({ children }: { children: ReactNode }) {
  const [client] = useState(() => new QueryClient({
    defaultOptions: { queries: { staleTime: 30_000, retry: 1 } },
  }));
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}
```

`src/app/layout.tsx`:
```tsx
import { ClerkProvider } from "@clerk/nextjs";
import type { ReactNode } from "react";
import { QueryProvider } from "./providers";
import "./globals.css";

export const metadata = { title: "turbo-cache-forge", description: "Remote cache dashboard" };

export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <ClerkProvider>
      <html lang="en">
        <body>
          <QueryProvider>{children}</QueryProvider>
        </body>
      </html>
    </ClerkProvider>
  );
}
```

`src/app/api.ts` — the bridge from Clerk's session token to the SDK (this is the ONLY place the JWT source meets the SDK):
```tsx
"use client";
import { useAuth } from "@clerk/nextjs";
import { createApiClient, type ApiClient } from "@tcf/api-client";
import { useMemo } from "react";

export function useApiClient(): ApiClient {
  const { getToken } = useAuth();
  return useMemo(
    () => createApiClient({
      baseUrl: process.env.NEXT_PUBLIC_API_URL!,
      getToken: () => getToken(), // Clerk session JWT; Phase-3 validates it against JWKS
    }),
    [getToken],
  );
}
```

`middleware.ts` (Clerk protects everything except the sign-in/up routes):
```ts
import { clerkMiddleware, createRouteMatcher } from "@clerk/nextjs/server";

const isPublic = createRouteMatcher(["/sign-in(.*)", "/sign-up(.*)"]);

export default clerkMiddleware(async (auth, req) => {
  if (!isPublic(req)) await auth.protect();
});

export const config = { matcher: ["/((?!_next|.*\\..*).*)", "/"] };
```

`src/app/(dashboard)/layout.tsx` — the shell (sidebar nav + Clerk org switcher + user button). Follow `frontend-design`: a real left rail, active-route highlighting, generous spacing — not a bare `<ul>`:
```tsx
import { OrganizationSwitcher, UserButton } from "@clerk/nextjs";
import Link from "next/link";
import type { ReactNode } from "react";

const nav = [
  { href: "/", label: "Overview" },
  { href: "/projects", label: "Projects" },
  { href: "/statistics", label: "Cache Statistics" },
  { href: "/artifacts", label: "Artifacts" },
  { href: "/api-keys", label: "API Keys" },
  { href: "/team", label: "Team" },
  { href: "/storage", label: "Storage Usage" },
  { href: "/settings", label: "Settings" },
  { href: "/billing", label: "Billing" },
];

export default function DashboardLayout({ children }: { children: ReactNode }) {
  return (
    <div className="grid min-h-screen grid-cols-[240px_1fr]">
      <aside className="flex flex-col gap-6 border-r bg-muted/30 p-4">
        <span className="px-2 text-lg font-semibold tracking-tight">turbo-cache-forge</span>
        <OrganizationSwitcher hidePersonal afterSelectOrganizationUrl="/" />
        <nav className="flex flex-col gap-1">
          {nav.map((n) => (
            <Link key={n.href} href={n.href}
              className="rounded-md px-3 py-2 text-sm text-muted-foreground hover:bg-accent hover:text-foreground">
              {n.label}
            </Link>
          ))}
        </nav>
        <div className="mt-auto px-2"><UserButton /></div>
      </aside>
      <main className="p-8">{children}</main>
    </div>
  );
}
```
`// ponytail: active-link state via usePathname is a nice-to-have; sidebar ships static, add the highlight when the nav is real.` (Keep it small — one `usePathname` compare is fine to add here if the implementer prefers.)

Sign-in/up pages are Clerk drop-ins:
```tsx
// src/app/sign-in/[[...sign-in]]/page.tsx
import { SignIn } from "@clerk/nextjs";
export default function Page() {
  return <div className="grid min-h-screen place-items-center"><SignIn /></div>;
}
```
(`sign-up/[[...sign-up]]/page.tsx` is the same with `<SignUp />`.)

- [ ] **Step 3: Vitest config + a real first test (formatters)**

`vitest.config.ts`:
```ts
import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";
export default defineConfig({
  plugins: [react()],
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./vitest.setup.ts"],
  },
  resolve: { alias: { "@": new URL("./src", import.meta.url).pathname } },
});
```
`vitest.setup.ts`: `import "@testing-library/jest-dom";`

`src/lib/format.test.ts` (written before `format.ts`):
```ts
import { describe, expect, it } from "vitest";
import { formatBytes, formatPercent } from "./format";

describe("formatters", () => {
  it("scales bytes to human units", () => {
    expect(formatBytes(0)).toBe("0 B");
    expect(formatBytes(1536)).toBe("1.5 KB");
    expect(formatBytes(5 * 1024 ** 3)).toBe("5 GB");
  });
  it("renders a 0..1 ratio as a percent", () => {
    expect(formatPercent(0.8342)).toBe("83.4%");
    expect(formatPercent(1)).toBe("100%");
  });
});
```

`src/lib/format.ts`:
```ts
export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  const units = ["KB", "MB", "GB", "TB", "PB"];
  let v = n / 1024;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
  const s = v % 1 === 0 ? v.toFixed(0) : v.toFixed(1);
  return `${s} ${units[i]}`;
}

export function formatPercent(ratio: number): string {
  const pct = ratio * 100;
  return `${pct % 1 === 0 ? pct.toFixed(0) : pct.toFixed(1)}%`;
}
```

- [ ] **Step 4: `.env.example` + run + commit**

`apps/dashboard/.env.example`:
```env
# The ONLY backend coupling — points at the Phase-3 Go API.
NEXT_PUBLIC_API_URL=http://localhost:8080
# Clerk (browser session → JWT sent to /api/v1).
NEXT_PUBLIC_CLERK_PUBLISHABLE_KEY=pk_test_xxx
CLERK_SECRET_KEY=sk_test_xxx
NEXT_PUBLIC_CLERK_SIGN_IN_URL=/sign-in
NEXT_PUBLIC_CLERK_SIGN_UP_URL=/sign-up
```

```bash
pnpm --filter dashboard test        # format tests PASS
pnpm --filter dashboard build       # standalone build succeeds
git add apps/dashboard pnpm-lock.yaml
git commit -m "feat(dashboard): Next.js 15 scaffold, Clerk auth, TanStack Query, app shell"
```

**Self-review:** `useApiClient()` is the single JWT→SDK bridge; no page will ever call `getToken` itself. Middleware protects all app routes; only Clerk auth pages are public. Cross-phase invariant holds — nothing here reaches for a cache token or `/v8/artifacts`.

---

## Task 4: Overview page — storage / hit-rate / requests tiles

**Files:**
- Create: `src/components/stat-tile.tsx`, `src/components/page-header.tsx`, `src/app/(dashboard)/page.tsx`
- Test: `src/components/stat-tile.test.tsx`

**Interfaces:**
- Consumes `useApiClient().getStats()`. Produces the reusable `<StatTile>` used by Overview and Storage Usage.

- [ ] **Step 1: Write the failing component test**

`src/components/stat-tile.test.tsx`:
```tsx
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { StatTile } from "./stat-tile";

describe("StatTile", () => {
  it("renders label and value", () => {
    render(<StatTile label="Hit rate" value="83.4%" />);
    expect(screen.getByText("Hit rate")).toBeInTheDocument();
    expect(screen.getByText("83.4%")).toBeInTheDocument();
  });

  it("shows a skeleton while loading", () => {
    render(<StatTile label="Storage" value="" loading />);
    expect(screen.getByTestId("stat-tile-skeleton")).toBeInTheDocument();
    expect(screen.queryByText("Storage")).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run → FAIL, then implement**

```bash
pnpm --filter dashboard test -- stat-tile   # FAIL: no ./stat-tile
```

`src/components/stat-tile.tsx`:
```tsx
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";

export function StatTile({ label, value, hint, loading }:
  { label: string; value: string; hint?: string; loading?: boolean }) {
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-sm font-medium text-muted-foreground">{label}</CardTitle>
      </CardHeader>
      <CardContent>
        {loading
          ? <Skeleton data-testid="stat-tile-skeleton" className="h-9 w-24" />
          : <div className="text-3xl font-semibold tabular-nums tracking-tight">{value}</div>}
        {hint && <p className="mt-1 text-xs text-muted-foreground">{hint}</p>}
      </CardContent>
    </Card>
  );
}
```

`src/components/page-header.tsx`:
```tsx
export function PageHeader({ title, description }: { title: string; description?: string }) {
  return (
    <header className="mb-6">
      <h1 className="text-2xl font-semibold tracking-tight">{title}</h1>
      {description && <p className="mt-1 text-sm text-muted-foreground">{description}</p>}
    </header>
  );
}
```

- [ ] **Step 3: The Overview page (live data)**

`src/app/(dashboard)/page.tsx`:
```tsx
"use client";
import { useQuery } from "@tanstack/react-query";
import { useApiClient } from "@/app/api";
import { PageHeader } from "@/components/page-header";
import { StatTile } from "@/components/stat-tile";
import { formatBytes, formatPercent } from "@/lib/format";

export default function OverviewPage() {
  const api = useApiClient();
  const { data, isLoading, isError } = useQuery({ queryKey: ["stats"], queryFn: () => api.getStats() });

  return (
    <div>
      <PageHeader title="Overview" description="Live cache health from /api/v1." />
      {isError && <p className="text-sm text-destructive">Could not load stats. Check your connection to the API.</p>}
      <div className="grid gap-4 sm:grid-cols-3">
        <StatTile label="Storage used" loading={isLoading} value={data ? formatBytes(data.storageBytes) : ""} />
        <StatTile label="Hit rate" loading={isLoading} value={data ? formatPercent(data.hitRate) : ""}
          hint={data ? `${data.hits} hits / ${data.misses} misses` : undefined} />
        <StatTile label="Requests" loading={isLoading} value={data ? data.requests.toLocaleString() : ""} />
      </div>
    </div>
  );
}
```

- [ ] **Step 4: Run + commit**

```bash
pnpm --filter dashboard test   # stat-tile + format PASS
git add apps/dashboard/src
git commit -m "feat(dashboard): Overview page with storage/hit-rate/requests tiles"
```

**Self-review:** three plain numeric tiles, no chart (YAGNI). Loading = skeleton, error = a real message, not a blank screen. All data via `useApiClient` → SDK → `/api/v1/stats`.

---

## Task 5: Projects page

**Files:**
- Create: `src/components/data-table.tsx`, `src/app/(dashboard)/projects/page.tsx`
- Test: `src/app/(dashboard)/projects/page.test.tsx`

**Interfaces:**
- Consumes `listProjects()` + `createProject()`. Produces the generic `<DataTable>` reused by Artifacts (Task 7).

- [ ] **Step 1: Failing test (mock the SDK hook)**

`src/app/(dashboard)/projects/page.test.tsx`:
```tsx
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import ProjectsPage from "./page";

const listProjects = vi.fn();
vi.mock("@/app/api", () => ({ useApiClient: () => ({ listProjects, createProject: vi.fn() }) }));

function renderWithQuery(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

describe("ProjectsPage", () => {
  beforeEach(() => vi.clearAllMocks());

  it("lists projects from the API", async () => {
    listProjects.mockResolvedValue([
      { id: 1, slug: "web", name: "Web App", createdAt: "2026-01-01T00:00:00Z" },
      { id: 2, slug: "api", name: "API", createdAt: "2026-01-02T00:00:00Z" },
    ]);
    renderWithQuery(<ProjectsPage />);
    expect(await screen.findByText("Web App")).toBeInTheDocument();
    expect(screen.getByText("api")).toBeInTheDocument();
  });

  it("shows an empty state when there are no projects", async () => {
    listProjects.mockResolvedValue([]);
    renderWithQuery(<ProjectsPage />);
    await waitFor(() => expect(screen.getByText(/no projects yet/i)).toBeInTheDocument());
  });
});
```

- [ ] **Step 2: Run → FAIL, then implement `<DataTable>` + page**

`src/components/data-table.tsx`:
```tsx
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import type { ReactNode } from "react";

export interface Column<T> { header: string; cell: (row: T) => ReactNode; }

export function DataTable<T>({ columns, rows, empty }:
  { columns: Column<T>[]; rows: T[]; empty: string }) {
  if (rows.length === 0) {
    return <p className="rounded-md border border-dashed p-8 text-center text-sm text-muted-foreground">{empty}</p>;
  }
  return (
    <Table>
      <TableHeader>
        <TableRow>{columns.map((c) => <TableHead key={c.header}>{c.header}</TableHead>)}</TableRow>
      </TableHeader>
      <TableBody>
        {rows.map((row, i) => (
          <TableRow key={i}>{columns.map((c) => <TableCell key={c.header}>{c.cell(row)}</TableCell>)}</TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
```

`src/app/(dashboard)/projects/page.tsx`:
```tsx
"use client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useApiClient } from "@/app/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { DataTable, type Column } from "@/components/data-table";
import { PageHeader } from "@/components/page-header";
import type { Project } from "@tcf/types";

const columns: Column<Project>[] = [
  { header: "Name", cell: (p) => <span className="font-medium">{p.name}</span> },
  { header: "Slug", cell: (p) => <code className="text-sm">{p.slug}</code> },
  { header: "Created", cell: (p) => new Date(p.createdAt).toLocaleDateString() },
];

export default function ProjectsPage() {
  const api = useApiClient();
  const qc = useQueryClient();
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const { data = [], isLoading } = useQuery({ queryKey: ["projects"], queryFn: () => api.listProjects() });
  const create = useMutation({
    mutationFn: () => api.createProject({ name, slug }),
    onSuccess: () => { setName(""); setSlug(""); qc.invalidateQueries({ queryKey: ["projects"] }); },
  });

  return (
    <div>
      <PageHeader title="Projects" description="Cache namespaces in this organization." />
      <form className="mb-6 flex gap-2" onSubmit={(e) => { e.preventDefault(); create.mutate(); }}>
        <Input placeholder="Name" value={name} onChange={(e) => setName(e.target.value)} required />
        <Input placeholder="slug" value={slug} onChange={(e) => setSlug(e.target.value)} required />
        <Button type="submit" disabled={create.isPending}>Add project</Button>
      </form>
      {isLoading ? <p className="text-sm text-muted-foreground">Loading…</p>
        : <DataTable columns={columns} rows={data} empty="No projects yet — add one above." />}
    </div>
  );
}
```

- [ ] **Step 3: Run + commit**

```bash
pnpm --filter dashboard test -- projects   # PASS
git add apps/dashboard/src
git commit -m "feat(dashboard): Projects page (list + create) and reusable DataTable"
```

**Self-review:** `<DataTable>` is generic and now the shared table primitive. Create uses a mutation + `invalidateQueries` so the list refreshes without a manual refetch. Empty state is a real message.

---

## Task 6: Cache Statistics page — the one ECharts trend panel

**Files:**
- Create: `src/components/hit-rate-chart.tsx`, `src/app/(dashboard)/statistics/page.tsx`
- Test: `src/app/(dashboard)/statistics/page.test.tsx`

**Interfaces:**
- Consumes `getStats()` + `getStatsTimeseries(days)`. Produces the hit/miss-over-time chart — the single place ECharts earns its weight.

**Before writing the chart:** read `dataviz` — use its categorical color pairing for hits vs misses, ensure the panel is legible in light and dark, label axes, no chartjunk. Two series (hits, misses) stacked or lined over days.

- [ ] **Step 1: Failing test**

`src/app/(dashboard)/statistics/page.test.tsx`:
```tsx
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import StatisticsPage from "./page";

// ECharts needs canvas/DOM APIs jsdom lacks — stub the chart component to a marker.
vi.mock("@/components/hit-rate-chart", () => ({
  HitRateChart: ({ points }: { points: unknown[] }) =>
    <div data-testid="hit-rate-chart">points:{points.length}</div>,
}));
vi.mock("@/app/api", () => ({
  useApiClient: () => ({
    getStats: vi.fn().mockResolvedValue({ storageBytes: 1024, hits: 90, misses: 10, requests: 100, hitRate: 0.9 }),
    getStatsTimeseries: vi.fn().mockResolvedValue([
      { day: "2026-07-01", hits: 40, misses: 5, bytesUploaded: 10, bytesDownloaded: 20 },
      { day: "2026-07-02", hits: 50, misses: 5, bytesUploaded: 10, bytesDownloaded: 20 },
    ]),
  }),
}));

function wrap(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

describe("StatisticsPage", () => {
  it("renders headline stats and the trend chart with the fetched points", async () => {
    wrap(<StatisticsPage />);
    expect(await screen.findByText("90.0%")).toBeInTheDocument();     // hit rate tile
    const chart = await screen.findByTestId("hit-rate-chart");
    expect(chart).toHaveTextContent("points:2");
  });
});
```

- [ ] **Step 2: Run → FAIL, then implement**

`src/components/hit-rate-chart.tsx` (client-only; ECharts is not SSR-safe):
```tsx
"use client";
import ReactECharts from "echarts-for-react";
import type { StatsPoint } from "@tcf/types";

export function HitRateChart({ points }: { points: StatsPoint[] }) {
  // dataviz: two-series categorical pair; swap these hexes for the project palette.
  const option = {
    tooltip: { trigger: "axis" },
    legend: { data: ["Hits", "Misses"], bottom: 0 },
    grid: { left: 40, right: 16, top: 24, bottom: 40 },
    xAxis: { type: "category", data: points.map((p) => p.day) },
    yAxis: { type: "value" },
    series: [
      { name: "Hits", type: "line", smooth: true, areaStyle: {}, data: points.map((p) => p.hits), color: "#2563eb" },
      { name: "Misses", type: "line", smooth: true, data: points.map((p) => p.misses), color: "#f59e0b" },
    ],
  };
  return <ReactECharts option={option} style={{ height: 320 }} notMerge lazyUpdate />;
}
```

`src/app/(dashboard)/statistics/page.tsx`:
```tsx
"use client";
import { useQuery } from "@tanstack/react-query";
import { useApiClient } from "@/app/api";
import { HitRateChart } from "@/components/hit-rate-chart";
import { PageHeader } from "@/components/page-header";
import { StatTile } from "@/components/stat-tile";
import { formatPercent } from "@/lib/format";

export default function StatisticsPage() {
  const api = useApiClient();
  const stats = useQuery({ queryKey: ["stats"], queryFn: () => api.getStats() });
  const series = useQuery({ queryKey: ["stats-ts", 30], queryFn: () => api.getStatsTimeseries(30) });

  return (
    <div>
      <PageHeader title="Cache Statistics" description="Hit rate and cache activity over the last 30 days." />
      <div className="mb-6 grid gap-4 sm:grid-cols-3">
        <StatTile label="Hit rate" loading={stats.isLoading} value={stats.data ? formatPercent(stats.data.hitRate) : ""} />
        <StatTile label="Hits" loading={stats.isLoading} value={stats.data ? stats.data.hits.toLocaleString() : ""} />
        <StatTile label="Misses" loading={stats.isLoading} value={stats.data ? stats.data.misses.toLocaleString() : ""} />
      </div>
      <div className="rounded-lg border p-4">
        {series.isLoading ? <p className="text-sm text-muted-foreground">Loading trend…</p>
          : <HitRateChart points={series.data ?? []} />}
      </div>
    </div>
  );
}
```

- [ ] **Step 3: Run + commit**

```bash
pnpm --filter dashboard test -- statistics   # PASS (chart stubbed in jsdom)
git add apps/dashboard/src
git commit -m "feat(dashboard): Cache Statistics page with hit/miss ECharts trend"
```

**Self-review:** exactly one chart, and the test stubs it (ECharts can't render in jsdom) while still asserting the page passes the fetched points through — so the data wiring is covered without a brittle canvas test. The real chart is eyeballed in the Task 10 / final verification. Colors are placeholders flagged for the `dataviz` palette swap.

---

## Task 7: Artifacts page — paginated table

**Files:**
- Create: `src/app/(dashboard)/artifacts/page.tsx`
- Test: `src/app/(dashboard)/artifacts/page.test.tsx`

**Interfaces:**
- Consumes `listArtifacts({ cursor, limit })` returning `Paginated<Artifact>`; cursor-based "Load more".

- [ ] **Step 1: Failing test (pagination)**

`src/app/(dashboard)/artifacts/page.test.tsx`:
```tsx
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import ArtifactsPage from "./page";

const listArtifacts = vi.fn();
vi.mock("@/app/api", () => ({ useApiClient: () => ({ listArtifacts }) }));

function wrap(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

describe("ArtifactsPage", () => {
  it("renders a page of artifacts and loads the next page on demand", async () => {
    listArtifacts
      .mockResolvedValueOnce({ items: [{ hash: "aaa", sizeBytes: 2048, tag: null, projectSlug: "web", createdAt: "2026-07-01T00:00:00Z", lastAccessedAt: "2026-07-02T00:00:00Z" }], nextCursor: "c2" })
      .mockResolvedValueOnce({ items: [{ hash: "bbb", sizeBytes: 4096, tag: "build", projectSlug: "api", createdAt: "2026-07-01T00:00:00Z", lastAccessedAt: "2026-07-02T00:00:00Z" }], nextCursor: null });

    wrap(<ArtifactsPage />);
    expect(await screen.findByText("aaa")).toBeInTheDocument();
    expect(screen.getByText("2 KB")).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: /load more/i }));
    expect(await screen.findByText("bbb")).toBeInTheDocument();
    // last page → button gone
    expect(screen.queryByRole("button", { name: /load more/i })).not.toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run → FAIL, then implement with `useInfiniteQuery`**

`src/app/(dashboard)/artifacts/page.tsx`:
```tsx
"use client";
import { useInfiniteQuery } from "@tanstack/react-query";
import { useApiClient } from "@/app/api";
import { Button } from "@/components/ui/button";
import { DataTable, type Column } from "@/components/data-table";
import { PageHeader } from "@/components/page-header";
import { Badge } from "@/components/ui/badge";
import { formatBytes } from "@/lib/format";
import type { Artifact } from "@tcf/types";

const columns: Column<Artifact>[] = [
  { header: "Hash", cell: (a) => <code className="text-sm">{a.hash}</code> },
  { header: "Project", cell: (a) => a.projectSlug ?? "—" },
  { header: "Size", cell: (a) => formatBytes(a.sizeBytes) },
  { header: "Tag", cell: (a) => a.tag ? <Badge variant="secondary">{a.tag}</Badge> : "—" },
  { header: "Last accessed", cell: (a) => new Date(a.lastAccessedAt).toLocaleString() },
];

export default function ArtifactsPage() {
  const api = useApiClient();
  const q = useInfiniteQuery({
    queryKey: ["artifacts"],
    queryFn: ({ pageParam }) => api.listArtifacts({ cursor: pageParam ?? undefined, limit: 50 }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.nextCursor ?? undefined,
  });

  const rows = q.data?.pages.flatMap((p) => p.items) ?? [];

  return (
    <div>
      <PageHeader title="Artifacts" description="Cached build outputs stored for this organization." />
      {q.isLoading ? <p className="text-sm text-muted-foreground">Loading…</p>
        : <DataTable columns={columns} rows={rows} empty="No artifacts cached yet." />}
      {q.hasNextPage && (
        <Button className="mt-4" variant="outline" disabled={q.isFetchingNextPage}
          onClick={() => q.fetchNextPage()}>Load more</Button>
      )}
    </div>
  );
}
```

- [ ] **Step 3: Run + commit**

```bash
pnpm --filter dashboard test -- artifacts   # PASS
git add apps/dashboard/src
git commit -m "feat(dashboard): Artifacts page with cursor-paginated table"
```

**Self-review:** cursor pagination via `useInfiniteQuery`; the button disappears when `nextCursor` is null (test asserts it). Reuses `<DataTable>` and `formatBytes`. Read-only — the dashboard never mutates artifacts.

---

## Task 8: API Keys page — create shows plaintext ONCE + revoke

**Files:**
- Create: `src/components/create-token-dialog.tsx`, `src/app/(dashboard)/api-keys/page.tsx`
- Test: `src/components/create-token-dialog.test.tsx`

**Interfaces:**
- Consumes `listTokens()`, `createToken({ name })` → `CreatedToken` (plaintext once), `revokeToken(id)`.

**This is the security-critical page (cross-phase invariant).** The plaintext token is displayed exactly once, straight from the create response, with a copy button and a "you won't see this again" warning. It is never stored in state beyond the open dialog, never re-fetched, and cleared when the dialog closes.

- [ ] **Step 1: Failing test — one-time reveal**

`src/components/create-token-dialog.test.tsx`:
```tsx
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { CreateTokenDialog } from "./create-token-dialog";

describe("CreateTokenDialog", () => {
  it("creates a token and reveals the plaintext exactly once", async () => {
    const createToken = vi.fn().mockResolvedValue({
      id: 7, name: "ci", token: "turbo_ONE_TIME_SECRET",
      lastUsedAt: null, createdAt: "2026-07-08T00:00:00Z", revokedAt: null,
    });
    const onCreated = vi.fn();
    render(<CreateTokenDialog createToken={createToken} onCreated={onCreated} />);

    await userEvent.click(screen.getByRole("button", { name: /new token/i }));
    await userEvent.type(screen.getByLabelText(/name/i), "ci");
    await userEvent.click(screen.getByRole("button", { name: /^create$/i }));

    // plaintext shown + warning present
    expect(await screen.findByText("turbo_ONE_TIME_SECRET")).toBeInTheDocument();
    expect(screen.getByText(/won.t be able to see it again/i)).toBeInTheDocument();
    expect(createToken).toHaveBeenCalledWith({ name: "ci" });
    expect(onCreated).toHaveBeenCalled();

    // closing the dialog forgets the secret
    await userEvent.click(screen.getByRole("button", { name: /done/i }));
    expect(screen.queryByText("turbo_ONE_TIME_SECRET")).not.toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run → FAIL, then implement**

`src/components/create-token-dialog.tsx`:
```tsx
"use client";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import type { CreatedToken } from "@tcf/types";

export function CreateTokenDialog({ createToken, onCreated }:
  { createToken: (input: { name: string }) => Promise<CreatedToken>; onCreated: () => void }) {
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [secret, setSecret] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  function reset() { setName(""); setSecret(null); setBusy(false); }

  async function submit() {
    setBusy(true);
    const created = await createToken({ name });
    setSecret(created.token); // held only while the dialog is open
    setBusy(false);
    onCreated();
  }

  return (
    <Dialog open={open} onOpenChange={(o) => { setOpen(o); if (!o) reset(); }}>
      <DialogTrigger asChild><Button>New token</Button></DialogTrigger>
      <DialogContent>
        <DialogHeader><DialogTitle>Create API token</DialogTitle></DialogHeader>
        {secret ? (
          <div className="space-y-3">
            <p className="text-sm text-muted-foreground">
              Copy this token now — you won&apos;t be able to see it again.
            </p>
            <div className="flex items-center gap-2">
              <code className="flex-1 truncate rounded bg-muted px-3 py-2 text-sm">{secret}</code>
              <Button variant="outline" onClick={() => navigator.clipboard?.writeText(secret)}>Copy</Button>
            </div>
            <Button onClick={() => setOpen(false)}>Done</Button>
          </div>
        ) : (
          <form className="space-y-3" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
            <label className="block text-sm font-medium" htmlFor="token-name">Name</label>
            <Input id="token-name" value={name} onChange={(e) => setName(e.target.value)} required />
            <Button type="submit" disabled={busy || !name}>Create</Button>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}
```

- [ ] **Step 3: The API Keys page (list + revoke)**

`src/app/(dashboard)/api-keys/page.tsx`:
```tsx
"use client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useApiClient } from "@/app/api";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { CreateTokenDialog } from "@/components/create-token-dialog";
import { DataTable, type Column } from "@/components/data-table";
import { PageHeader } from "@/components/page-header";
import type { Token } from "@tcf/types";

export default function ApiKeysPage() {
  const api = useApiClient();
  const qc = useQueryClient();
  const { data = [], isLoading } = useQuery({ queryKey: ["tokens"], queryFn: () => api.listTokens() });
  const revoke = useMutation({
    mutationFn: (id: number) => api.revokeToken(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["tokens"] }),
  });
  const refresh = () => qc.invalidateQueries({ queryKey: ["tokens"] });

  const columns: Column<Token>[] = [
    { header: "Name", cell: (t) => <span className="font-medium">{t.name}</span> },
    { header: "Status", cell: (t) => t.revokedAt ? <Badge variant="destructive">Revoked</Badge> : <Badge>Active</Badge> },
    { header: "Last used", cell: (t) => t.lastUsedAt ? new Date(t.lastUsedAt).toLocaleString() : "Never" },
    { header: "", cell: (t) => t.revokedAt ? null
        : <Button size="sm" variant="ghost" onClick={() => revoke.mutate(t.id)}>Revoke</Button> },
  ];

  return (
    <div>
      <div className="mb-6 flex items-center justify-between">
        <PageHeader title="API Keys" description="Bearer tokens used by the Turborepo CLI on the cache path." />
        <CreateTokenDialog createToken={(i) => api.createToken(i)} onCreated={refresh} />
      </div>
      {isLoading ? <p className="text-sm text-muted-foreground">Loading…</p>
        : <DataTable columns={columns} rows={data} empty="No tokens yet — create one to start caching." />}
    </div>
  );
}
```

- [ ] **Step 4: Run + commit**

```bash
pnpm --filter dashboard test -- create-token-dialog   # PASS
git add apps/dashboard/src
git commit -m "feat(dashboard): API Keys page — one-time token reveal + revoke"
```

**Self-review:** the plaintext lives only in dialog-local `secret` state and is wiped on close (`reset()`); no query ever re-fetches it (the list endpoint returns metadata only). Revoke invalidates the list so status flips to "Revoked". This is the UI half of the acceptance test — the cache-path effect is proven in Task 10 / final Verification.

---

## Task 9: Secondary pages — Team, Storage Usage, Settings, Billing stub

**Files:**
- Create: `src/app/(dashboard)/team/page.tsx`, `storage/page.tsx`, `settings/page.tsx`, `billing/page.tsx`
- Test: `src/app/(dashboard)/storage/page.test.tsx` (Storage reuses tiles + a real fetch; the others are thin enough to cover via the Playwright smoke in Task 10)

**Interfaces:**
- Team = Clerk `<OrganizationProfile />` (members managed by Clerk, not our API — cross-phase: org membership is the IdP's job). Storage = `getStats().storageBytes` tile + per-project breakdown from `listProjects()`. Settings = org info + the `NEXT_PUBLIC_API_URL` the dashboard is pointed at. Billing = static stub.

- [ ] **Step 1: Team (Clerk org management — no custom API)**

`src/app/(dashboard)/team/page.tsx`:
```tsx
import { OrganizationProfile } from "@clerk/nextjs";
import { PageHeader } from "@/components/page-header";

export default function TeamPage() {
  return (
    <div>
      <PageHeader title="Team Members" description="Managed through your identity provider." />
      <OrganizationProfile routing="hash" />
    </div>
  );
}
```
`// ponytail: Clerk already renders invite/role/remove UI — reimplementing it over our API would duplicate the IdP. Org membership stays in Clerk; our API only ever sees the org claim in the JWT.`

- [ ] **Step 2: Storage Usage (failing test first)**

`src/app/(dashboard)/storage/page.test.tsx`:
```tsx
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import StoragePage from "./page";

vi.mock("@/app/api", () => ({
  useApiClient: () => ({
    getStats: vi.fn().mockResolvedValue({ storageBytes: 3 * 1024 ** 3, hits: 0, misses: 0, requests: 0, hitRate: 0 }),
    listProjects: vi.fn().mockResolvedValue([]),
  }),
}));

function wrap(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

describe("StoragePage", () => {
  it("shows total storage used", async () => {
    wrap(<StoragePage />);
    expect(await screen.findByText("3 GB")).toBeInTheDocument();
  });
});
```

`src/app/(dashboard)/storage/page.tsx`:
```tsx
"use client";
import { useQuery } from "@tanstack/react-query";
import { useApiClient } from "@/app/api";
import { PageHeader } from "@/components/page-header";
import { StatTile } from "@/components/stat-tile";
import { formatBytes } from "@/lib/format";

export default function StoragePage() {
  const api = useApiClient();
  const stats = useQuery({ queryKey: ["stats"], queryFn: () => api.getStats() });
  return (
    <div>
      <PageHeader title="Storage Usage" description="Object storage consumed by cached artifacts." />
      <div className="grid gap-4 sm:grid-cols-2">
        <StatTile label="Total stored" loading={stats.isLoading}
          value={stats.data ? formatBytes(stats.data.storageBytes) : ""} />
      </div>
    </div>
  );
}
```

- [ ] **Step 3: Settings + Billing stub**

`src/app/(dashboard)/settings/page.tsx`:
```tsx
"use client";
import { PageHeader } from "@/components/page-header";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

export default function SettingsPage() {
  return (
    <div>
      <PageHeader title="Settings" description="Dashboard and connection configuration." />
      <Card>
        <CardHeader><CardTitle className="text-sm">Backend API</CardTitle></CardHeader>
        <CardContent>
          <code className="text-sm">{process.env.NEXT_PUBLIC_API_URL}</code>
          <p className="mt-2 text-xs text-muted-foreground">Set via NEXT_PUBLIC_API_URL. This is the only backend the dashboard talks to.</p>
        </CardContent>
      </Card>
    </div>
  );
}
```

`src/app/(dashboard)/billing/page.tsx`:
```tsx
import { PageHeader } from "@/components/page-header";

export default function BillingPage() {
  return (
    <div>
      <PageHeader title="Billing" description="Not available in the self-hosted edition." />
      <p className="rounded-md border border-dashed p-8 text-center text-sm text-muted-foreground">
        Billing is a stub. Plans and metering land with the SaaS phase.
      </p>
    </div>
  );
}
```

- [ ] **Step 4: Run + commit**

```bash
pnpm --filter dashboard test -- storage   # PASS
pnpm --filter dashboard build             # all routes compile
git add apps/dashboard/src
git commit -m "feat(dashboard): Team (Clerk), Storage Usage, Settings, Billing stub"
```

**Self-review:** Team defers entirely to Clerk (no duplicate membership API). Billing is honestly a stub with a clear message, not a fake screen. Settings surfaces the one env coupling. Storage reuses the tile + formatter.

---

## Task 10: Playwright — the critical token-lifecycle flow end to end

**Files:**
- Create: `apps/dashboard/playwright.config.ts`, `src/e2e/token-lifecycle.spec.ts`
- Modify: `apps/dashboard/package.json` (add `test:e2e` script)

**Interfaces:**
- Drives a real browser against the running dashboard + a running `/api/v1`, using a Clerk test user, to prove the DoD path: log in → see stats → create a token (plaintext once) → revoke it.

**Decision:** Playwright covers only this one flow — it is the acceptance test. Everything else is component-tested. Auth uses Clerk's testing token (`@clerk/testing`) so the run is non-interactive; the cache-path 401-after-revoke assertion (curl with the plaintext) is scripted in the final Verification section rather than in-browser, because the browser never holds a cache token by design.

- [ ] **Step 1: Config + Clerk test setup**

```bash
pnpm --filter dashboard add -D @clerk/testing
```

`playwright.config.ts`:
```ts
import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./src/e2e",
  use: { baseURL: process.env.E2E_BASE_URL ?? "http://localhost:3000" },
  webServer: process.env.E2E_BASE_URL ? undefined : {
    command: "pnpm start",
    url: "http://localhost:3000",
    reuseExistingServer: !process.env.CI,
  },
});
```

`package.json` scripts: add `"test:e2e": "playwright test"`.

- [ ] **Step 2: The spec**

`src/e2e/token-lifecycle.spec.ts`:
```ts
import { clerk, clerkSetup } from "@clerk/testing/playwright";
import { expect, test } from "@playwright/test";

test.beforeAll(async () => { await clerkSetup(); });

test("log in, see live stats, create and revoke a token", async ({ page }) => {
  await page.goto("/");
  await clerk.signIn({
    page,
    signInParams: {
      strategy: "password",
      identifier: process.env.E2E_CLERK_USER!,
      password: process.env.E2E_CLERK_PASSWORD!,
    },
  });

  // Overview shows live numbers from /api/v1 (tiles render values, not skeletons)
  await page.goto("/");
  await expect(page.getByText("Hit rate")).toBeVisible();
  await expect(page.getByText("Storage used")).toBeVisible();

  // Create a token — plaintext appears exactly once
  await page.goto("/api-keys");
  await page.getByRole("button", { name: /new token/i }).click();
  await page.getByLabel(/name/i).fill("e2e-token");
  await page.getByRole("button", { name: /^create$/i }).click();
  const secret = await page.getByText(/^turbo_/).innerText();
  expect(secret).toMatch(/^turbo_/);
  await page.getByRole("button", { name: /done/i }).click();

  // It is now listed as Active, then revoke it
  await expect(page.getByText("e2e-token")).toBeVisible();
  await page.getByRole("button", { name: /revoke/i }).first().click();
  await expect(page.getByText("Revoked")).toBeVisible();
});
```

- [ ] **Step 3: Run against a live stack + commit**

```bash
# with the Phase-3 API on :8080 and the dashboard built+started on :3000:
pnpm --filter dashboard build && pnpm --filter dashboard start &
E2E_CLERK_USER=... E2E_CLERK_PASSWORD=... pnpm --filter dashboard test:e2e
git add apps/dashboard/playwright.config.ts apps/dashboard/src/e2e apps/dashboard/package.json
git commit -m "test(dashboard): Playwright token-lifecycle e2e (login → stats → create → revoke)"
```

**Self-review:** one Playwright flow, and it is the DoD path minus the cache-path effect (asserted by curl in Verification, since the browser deliberately never holds the `turbo_` token). Clerk testing token keeps it headless/CI-safe.

---

## Task 11: Docker — dashboard image + compose service driven by `NEXT_PUBLIC_API_URL`

**Files:**
- Create: `apps/dashboard/Dockerfile`, `apps/dashboard/.dockerignore`
- Modify: `infra/docker/docker-compose.yml` (add the `dashboard` service)

- [ ] **Step 1: Multi-stage Dockerfile (pnpm + Next standalone)**

`apps/dashboard/Dockerfile` (build context is the repo root so pnpm workspace + local packages resolve):
```dockerfile
FROM node:20-slim AS base
RUN corepack enable
WORKDIR /repo

FROM base AS deps
COPY pnpm-lock.yaml pnpm-workspace.yaml package.json .npmrc ./
COPY packages ./packages
COPY apps/dashboard/package.json ./apps/dashboard/package.json
RUN pnpm install --frozen-lockfile

FROM base AS build
COPY --from=deps /repo /repo
COPY packages ./packages
COPY apps/dashboard ./apps/dashboard
# NEXT_PUBLIC_* is inlined at build time — pass the API URL as a build arg.
ARG NEXT_PUBLIC_API_URL=http://localhost:8080
ENV NEXT_PUBLIC_API_URL=$NEXT_PUBLIC_API_URL
RUN pnpm --filter dashboard build

FROM node:20-slim AS run
WORKDIR /app
ENV NODE_ENV=production
COPY --from=build /repo/apps/dashboard/.next/standalone ./
COPY --from=build /repo/apps/dashboard/.next/static ./apps/dashboard/.next/static
COPY --from=build /repo/apps/dashboard/public ./apps/dashboard/public
EXPOSE 3000
CMD ["node", "apps/dashboard/server.js"]
```
`.dockerignore`: `node_modules`, `.next`, `.turbo`.

`// ponytail: NEXT_PUBLIC_API_URL is baked at build time by Next — for a self-hoster who wants to swap the API URL without rebuilding, that's the "runtime env" upgrade path (a small entrypoint that rewrites the built placeholder). Not built until someone actually needs it.`

- [ ] **Step 2: Compose service**

Add to `infra/docker/docker-compose.yml`:
```yaml
  dashboard:
    build:
      context: ../..
      dockerfile: apps/dashboard/Dockerfile
      args:
        NEXT_PUBLIC_API_URL: ${NEXT_PUBLIC_API_URL:-http://localhost:8080}
    depends_on:
      cache-api: { condition: service_started }
    environment:
      NEXT_PUBLIC_API_URL: ${NEXT_PUBLIC_API_URL:-http://localhost:8080}
      NEXT_PUBLIC_CLERK_PUBLISHABLE_KEY: ${NEXT_PUBLIC_CLERK_PUBLISHABLE_KEY}
      CLERK_SECRET_KEY: ${CLERK_SECRET_KEY}
    ports: ["3000:3000"]
```

- [ ] **Step 3: Build + smoke + commit**

```bash
NEXT_PUBLIC_API_URL=http://localhost:8080 \
  docker compose -f infra/docker/docker-compose.yml up -d --build dashboard
curl -sI http://localhost:3000/sign-in | head -1   # 200 — app serves
git add apps/dashboard/Dockerfile apps/dashboard/.dockerignore infra/docker/docker-compose.yml
git commit -m "feat(dashboard): standalone Docker image + compose service via NEXT_PUBLIC_API_URL"
```

**Self-review:** one image, Next standalone output (small runtime), build context at repo root so the workspace packages resolve. The only backend knob is `NEXT_PUBLIC_API_URL`; Clerk keys are the only other required env. Self-hoster runs `docker compose up` and gets the dashboard next to the cache API.

---

## Definition of Done (from ROADMAP Phase 4 — verbatim acceptance)

> **Log in via Clerk, see live stats from `/api/v1`, create + revoke a token from the UI and confirm it works/stops working on the cache path.**

Checklist:
- [ ] `pnpm dev` runs the whole monorepo; `pnpm build`, `pnpm test`, `pnpm typecheck`, `pnpm lint` all green (Turborepo-cached).
- [ ] A human logs in through Clerk and lands on the Overview.
- [ ] Overview / Statistics / Storage show **live** numbers fetched from `/api/v1` (not mock data).
- [ ] Projects, Artifacts, API Keys, Team, Settings render; Billing is a labelled stub.
- [ ] Creating a token shows the plaintext **once**; revoking flips it to Revoked.
- [ ] The dashboard talks **only** to `/api/v1` (no storage, no DB, no `/v8/artifacts`) and holds no cache token beyond the one-time reveal.
- [ ] `docker compose up` brings the dashboard up beside the cache API, wired solely by `NEXT_PUBLIC_API_URL`.

---

## Verification (run before calling Phase 4 done)

Prereq: the Phase-3 `/api/v1` on `http://localhost:8080` with a Clerk org whose JWT it accepts, and the dashboard on `http://localhost:3000`.

1. **Login + live stats.** Open `http://localhost:3000`, sign in via Clerk. Overview tiles show real storage / hit-rate / request numbers from `/api/v1/stats` (stop the API → tiles show the error state, proving the data is live, not mocked).
2. **Create a token (plaintext once).** API Keys → New token → name it `verify` → Create. The plaintext `turbo_…` is shown once with the "won't see it again" warning. Copy it; close the dialog; confirm it is gone from the page and re-opening the list shows metadata only.
3. **It works on the cache path.** Using the copied plaintext against the *cache* API (not the dashboard):
   ```bash
   curl -s -H "Authorization: Bearer turbo_<copied>" \
     "http://localhost:8080/v8/artifacts/status"        # {"status":"enabled"}
   ```
4. **Revoke from the UI.** API Keys → Revoke the `verify` token → status becomes "Revoked".
5. **It stops working on the cache path.**
   ```bash
   curl -s -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer turbo_<copied>" \
     "http://localhost:8080/v8/artifacts/status"        # 401
   ```
6. **Boundary check.** Confirm the dashboard network tab shows requests only to `${NEXT_PUBLIC_API_URL}/api/v1/*` (plus Clerk) — never to `/v8/artifacts`, storage, or Postgres.
7. **Self-host.** On a clean box, `NEXT_PUBLIC_API_URL=… docker compose up` yields a working dashboard beside the cache API with only that env var + Clerk keys set.

## Deferred to later / not built here (YAGNI)
- Runtime (non-build-time) `NEXT_PUBLIC_API_URL` swapping — add an entrypoint rewrite only if a self-hoster needs it without rebuilding (Task 11 note).
- Active-nav highlighting, per-project storage breakdown charts, a second ECharts panel — add when the data justifies them.
- Real Billing, usage export, audit-log UI → North-star / SaaS phase.
- Any dashboard→storage or dashboard→DB path — forbidden by the cross-phase invariant, never build it.
