import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import StatisticsPage from "./page";

// ECharts can't render in jsdom — stub the chart and assert it received the
// fetched points instead of trying to render the real canvas/svg.
vi.mock("@/components/hit-rate-chart", () => ({
  HitRateChart: ({ points }: { points: unknown[] }) => (
    <div data-testid="hit-rate-chart">points:{points.length}</div>
  ),
}));
vi.mock("@/app/api", () => ({
  useApiClient: () => ({
    getStats: vi.fn().mockResolvedValue({
      storage_bytes: 1024,
      artifact_count: 3,
      hits: 90,
      misses: 10,
      requests: 100,
      bytes_up: 10,
      bytes_down: 20,
    }),
    getStatsTimeseries: vi.fn().mockResolvedValue([
      { day: "2026-07-01", hits: 40, misses: 5, bytes_up: 10, bytes_down: 20 },
      { day: "2026-07-02", hits: 50, misses: 5, bytes_up: 10, bytes_down: 20 },
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
    // formatPercent(90/100) => "90%" (0.9*100 is exactly 90 in JS, so the
    // "%d.0" branch never triggers) — verified against the real helper in
    // src/lib/format.ts rather than assuming "90.0%".
    expect(await screen.findByText("90%")).toBeInTheDocument();
    const chart = await screen.findByTestId("hit-rate-chart");
    expect(chart).toHaveTextContent("points:2");
  });
});
