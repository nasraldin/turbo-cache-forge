import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError, createApiClient } from "./client";

function mockFetch(body: unknown, init: { status?: number } = {}) {
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
    const fetchMock = mockFetch({
      storage_bytes: 10,
      artifact_count: 3,
      hits: 8,
      misses: 2,
      requests: 10,
      bytes_up: 100,
      bytes_down: 200,
    });
    vi.stubGlobal("fetch", fetchMock);
    const client = createApiClient({ baseUrl: base, getToken: async () => "jwt-123" });

    const stats = await client.getStats();

    expect(stats.hits).toBe(8);
    expect(stats.storage_bytes).toBe(10);
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe(`${base}/api/v1/stats`);
    expect((init.headers as Record<string, string>).Authorization).toBe("Bearer jwt-123");
    expect(init.method ?? "GET").toBe("GET");
  });

  it("GETs /api/v1/projects", async () => {
    const fetchMock = mockFetch([{ id: 1, slug: "web", name: "Web", created_at: "t" }]);
    vi.stubGlobal("fetch", fetchMock);
    const client = createApiClient({ baseUrl: base, getToken: async () => "jwt" });

    const projects = await client.listProjects();

    expect(projects).toEqual([{ id: 1, slug: "web", name: "Web", created_at: "t" }]);
    expect(fetchMock.mock.calls[0][0]).toBe(`${base}/api/v1/projects`);
  });

  it("POSTs a project and returns the created record", async () => {
    const fetchMock = mockFetch({ id: 2, slug: "api", name: "API", created_at: "t" }, { status: 201 });
    vi.stubGlobal("fetch", fetchMock);
    const client = createApiClient({ baseUrl: base, getToken: async () => "jwt" });

    const created = await client.createProject({ slug: "api", name: "API" });

    expect(created.id).toBe(2);
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe(`${base}/api/v1/projects`);
    expect(init.method).toBe("POST");
    expect(JSON.parse(init.body as string)).toEqual({ slug: "api", name: "API" });
  });

  it("GETs /api/v1/artifacts with limit/offset params", async () => {
    const fetchMock = mockFetch({ limit: 50, offset: 10, artifacts: [] });
    vi.stubGlobal("fetch", fetchMock);
    const client = createApiClient({ baseUrl: base, getToken: async () => "jwt" });

    const page = await client.listArtifacts({ limit: 50, offset: 10 });

    expect(page.limit).toBe(50);
    expect(fetchMock.mock.calls[0][0]).toBe(`${base}/api/v1/artifacts?limit=50&offset=10`);
  });

  it("GETs /api/v1/artifacts with no params when none given", async () => {
    const fetchMock = mockFetch({ limit: 50, offset: 0, artifacts: [] });
    vi.stubGlobal("fetch", fetchMock);
    const client = createApiClient({ baseUrl: base, getToken: async () => "jwt" });

    await client.listArtifacts();

    expect(fetchMock.mock.calls[0][0]).toBe(`${base}/api/v1/artifacts`);
  });

  it("GETs /api/v1/tokens", async () => {
    const fetchMock = mockFetch([
      { id: 1, name: "ci", created_at: "t", last_used_at: null, revoked_at: null },
    ]);
    vi.stubGlobal("fetch", fetchMock);
    const client = createApiClient({ baseUrl: base, getToken: async () => "jwt" });

    const tokens = await client.listTokens();

    expect(tokens[0].name).toBe("ci");
    expect(fetchMock.mock.calls[0][0]).toBe(`${base}/api/v1/tokens`);
  });

  it("POSTs a token and returns the one-time plaintext", async () => {
    const fetchMock = mockFetch({ id: 1, name: "ci", token: "turbo_PLAINTEXT_ONCE" }, { status: 201 });
    vi.stubGlobal("fetch", fetchMock);
    const client = createApiClient({ baseUrl: base, getToken: async () => "jwt" });

    const created = await client.createToken({ name: "ci" });

    expect(created.token).toBe("turbo_PLAINTEXT_ONCE");
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe(`${base}/api/v1/tokens`);
    expect(init.method).toBe("POST");
    expect(JSON.parse(init.body as string)).toEqual({ name: "ci" });
  });

  it("DELETEs /api/v1/tokens/{id} and resolves with no value on 204", async () => {
    const fetchMock = mockFetch(undefined, { status: 204 });
    vi.stubGlobal("fetch", fetchMock);
    const client = createApiClient({ baseUrl: base, getToken: async () => "jwt" });

    const result = await client.revokeToken(7);

    expect(result).toBeUndefined();
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe(`${base}/api/v1/tokens/7`);
    expect(init.method).toBe("DELETE");
  });

  it("throws ApiError with the status on a non-2xx", async () => {
    const fetchMock = mockFetch({ error: "unauthorized" }, { status: 401 });
    vi.stubGlobal("fetch", fetchMock);
    const client = createApiClient({ baseUrl: base, getToken: async () => null });

    await expect(client.getStats()).rejects.toMatchObject({ name: "ApiError", status: 401 });
  });

  it("does not attach an Authorization header when getToken resolves null", async () => {
    const fetchMock = mockFetch({ storage_bytes: 0, artifact_count: 0, hits: 0, misses: 0, requests: 0, bytes_up: 0, bytes_down: 0 });
    vi.stubGlobal("fetch", fetchMock);
    const client = createApiClient({ baseUrl: base, getToken: async () => null });

    await client.getStats();

    const [, init] = fetchMock.mock.calls[0];
    expect((init.headers as Record<string, string>).Authorization).toBeUndefined();
  });

  it("GETs /api/v1/artifacts/{hash} detail", async () => {
    const detail = {
      hash: "abc", size_bytes: 10, tag: null,
      created_at: "2026-07-01T00:00:00Z", last_accessed_at: "2026-07-01T00:00:00Z",
      content: { format: "zstd-tar", total_entries: 0, truncated: false, entries: [] },
    };
    const fetchMock = mockFetch(detail);
    vi.stubGlobal("fetch", fetchMock);
    const client = createApiClient({ baseUrl: base, getToken: async () => "jwt" });

    const got = await client.getArtifact("abc");

    expect(got.content.format).toBe("zstd-tar");
    expect(fetchMock.mock.calls[0][0]).toBe(`${base}/api/v1/artifacts/abc`);
  });

  it("DELETEs a single artifact (204)", async () => {
    const fetchMock = mockFetch(undefined, { status: 204 });
    vi.stubGlobal("fetch", fetchMock);
    const client = createApiClient({ baseUrl: base, getToken: async () => "jwt" });

    await client.deleteArtifact("abc");

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe(`${base}/api/v1/artifacts/abc`);
    expect(init.method).toBe("DELETE");
  });

  it("DELETEs all artifacts and returns the count", async () => {
    const fetchMock = mockFetch({ deleted: 3 });
    vi.stubGlobal("fetch", fetchMock);
    const client = createApiClient({ baseUrl: base, getToken: async () => "jwt" });

    const res = await client.clearArtifacts();

    expect(res.deleted).toBe(3);
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe(`${base}/api/v1/artifacts`);
    expect(init.method).toBe("DELETE");
  });

  it("downloads an artifact blob with the JWT attached", async () => {
    const blob = new Blob(["RAW"]);
    // mockResolvedValue (like mockFetch) keeps the call-args type as any[]; an
    // arg-less `vi.fn(async () => …)` narrows mock.calls to an empty tuple, which
    // then can't be destructured as [url, init].
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 200, blob: async () => blob } as Response);
    vi.stubGlobal("fetch", fetchMock);
    const client = createApiClient({ baseUrl: base, getToken: async () => "jwt" });

    const got = await client.getArtifactBlob("abc");

    expect(got).toBe(blob);
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe(`${base}/api/v1/artifacts/abc/download`);
    expect((init.headers as Record<string, string>).Authorization).toBe("Bearer jwt");
  });
});
