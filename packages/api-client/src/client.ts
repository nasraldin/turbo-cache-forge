import type {
  ArtifactDetail,
  ArtifactsPage,
  ClearArtifactsResult,
  CreatedToken,
  Project,
  Stats,
  StatsPoint,
  Token,
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
    listArtifacts: (params?: { limit?: number; offset?: number }) => {
      const q = new URLSearchParams();
      if (params?.limit) q.set("limit", String(params.limit));
      if (params?.offset) q.set("offset", String(params.offset));
      const suffix = q.toString() ? `?${q}` : "";
      return request<ArtifactsPage>(`/artifacts${suffix}`);
    },
    getArtifact: (hash: string) => request<ArtifactDetail>(`/artifacts/${encodeURIComponent(hash)}`),
    deleteArtifact: (hash: string) =>
      request<void>(`/artifacts/${encodeURIComponent(hash)}`, { method: "DELETE" }),
    clearArtifacts: () => request<ClearArtifactsResult>("/artifacts", { method: "DELETE" }),
    // Raw download needs the Bearer header, so it can't be a bare <a href>. The
    // caller turns the Blob into an object-URL download.
    getArtifactBlob: async (hash: string): Promise<Blob> => {
      const token = await opts.getToken();
      const res = await fetch(`${root}/artifacts/${encodeURIComponent(hash)}/download`, {
        headers: token ? { Authorization: `Bearer ${token}` } : {},
      });
      if (!res.ok) {
        const text = await res.text().catch(() => res.statusText);
        throw new ApiError(res.status, text || `request failed: ${res.status}`);
      }
      return res.blob();
    },
    listTokens: () => request<Token[]>("/tokens"),
    createToken: (input: { name: string }) =>
      request<CreatedToken>("/tokens", { method: "POST", body: JSON.stringify(input) }),
    revokeToken: (id: number) => request<void>(`/tokens/${id}`, { method: "DELETE" }),
  };
}

export type ApiClient = ReturnType<typeof createApiClient>;
