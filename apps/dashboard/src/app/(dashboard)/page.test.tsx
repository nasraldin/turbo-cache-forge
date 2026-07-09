import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import type { Stats } from "@tcf/types";
import { describe, expect, it, vi } from "vitest";
import { useApiClient } from "@/app/api";
import OverviewPage from "./page";

vi.mock("@/app/api", () => ({
  useApiClient: vi.fn(),
}));

function renderOverview() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <OverviewPage />
    </QueryClientProvider>,
  );
}

function mockStats(stats: Stats) {
  vi.mocked(useApiClient).mockReturnValue({
    getStats: vi.fn().mockResolvedValue(stats),
  } as unknown as ReturnType<typeof useApiClient>);
}

const baseStats: Stats = {
  storage_bytes: 5 * 1024 ** 3,
  artifact_count: 42,
  hits: 90,
  misses: 10,
  requests: 100,
  bytes_up: 1024 ** 3,
  bytes_down: 2 * 1024 ** 3,
};

describe("OverviewPage", () => {
  it("shows skeleton tiles while loading", () => {
    vi.mocked(useApiClient).mockReturnValue({
      getStats: vi.fn(() => new Promise(() => {})),
    } as unknown as ReturnType<typeof useApiClient>);

    renderOverview();
    expect(screen.getAllByTestId("stat-tile-skeleton").length).toBeGreaterThan(0);
  });

  it("renders the client-computed hit rate from getStats", async () => {
    mockStats(baseStats);
    renderOverview();
    await waitFor(() => expect(screen.getByText("90%")).toBeInTheDocument());
  });

  it("shows an actionable empty state when hits+misses==0", async () => {
    mockStats({ ...baseStats, hits: 0, misses: 0 });
    renderOverview();
    await waitFor(() =>
      expect(screen.getByText(/no cache activity yet/i)).toBeInTheDocument(),
    );
    expect(screen.getByText(/TURBO_TOKEN/)).toBeInTheDocument();
  });

  it("shows an error state when the query rejects", async () => {
    vi.mocked(useApiClient).mockReturnValue({
      getStats: vi.fn().mockRejectedValue(new Error("network down")),
    } as unknown as ReturnType<typeof useApiClient>);

    renderOverview();
    await waitFor(() =>
      expect(screen.getByText(/couldn't reach the cache api/i)).toBeInTheDocument(),
    );
    expect(screen.getByText(/NEXT_PUBLIC_API_URL/)).toBeInTheDocument();
  });
});
