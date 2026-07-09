import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import StoragePage from "./page";

vi.mock("@/app/api", () => ({
  useApiClient: () => ({
    // real Stats is snake_case; storage_bytes = 3 GiB
    getStats: vi.fn().mockResolvedValue({
      storage_bytes: 3 * 1024 ** 3, artifact_count: 0,
      hits: 0, misses: 0, requests: 0, bytes_up: 0, bytes_down: 0,
    }),
  }),
}));

function wrap(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

describe("StoragePage", () => {
  it("shows total storage used", async () => {
    wrap(<StoragePage />);
    expect(await screen.findByText("3 GiB")).toBeInTheDocument(); // formatBytes(3*1024^3)
  });
});
