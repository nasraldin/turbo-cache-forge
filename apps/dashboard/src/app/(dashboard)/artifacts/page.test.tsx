import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import ArtifactsPage from "./page";

const listArtifacts = vi.fn();
vi.mock("@/app/api", () => ({ useApiClient: () => ({ listArtifacts }) }));

const art = (hash: string, size: number, tag: string | null) => ({
  hash, size_bytes: size, tag,
  created_at: "2026-07-01T00:00:00Z", last_accessed_at: "2026-07-02T00:00:00Z",
});

function wrap(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

describe("ArtifactsPage", () => {
  it("shows a page of artifacts and pages forward with offset", async () => {
    // page 1: a FULL page of 50 (→ there may be more) with a recognizable first row
    const full = [art("aaaaaaaa11111111", 2048, null),
      ...Array.from({ length: 49 }, (_, i) => art(`h${i}`, 1024, null))];
    listArtifacts
      .mockResolvedValueOnce({ limit: 50, offset: 0, artifacts: full })
      // page 2: SHORT page (1 row) → no more after this
      .mockResolvedValueOnce({ limit: 50, offset: 50, artifacts: [art("bbbbbbbb22222222", 4096, "build")] });

    wrap(<ArtifactsPage />);
    expect(await screen.findByText("2 KiB")).toBeInTheDocument();   // size_bytes 2048 -> "2 KiB"
    const next = screen.getByRole("button", { name: /next/i });
    expect(next).toBeEnabled();
    expect(screen.getByRole("button", { name: /prev/i })).toBeDisabled();

    await userEvent.click(next);
    expect(await screen.findByText("build")).toBeInTheDocument();   // tag badge on page 2
    expect(listArtifacts).toHaveBeenLastCalledWith({ limit: 50, offset: 50 });
    await waitFor(() => expect(screen.getByRole("button", { name: /next/i })).toBeDisabled());
    expect(screen.getByRole("button", { name: /prev/i })).toBeEnabled();
  });

  it("shows the empty state when there are no artifacts", async () => {
    listArtifacts.mockResolvedValueOnce({ limit: 50, offset: 0, artifacts: [] });
    wrap(<ArtifactsPage />);
    await waitFor(() => expect(screen.getByText(/no artifacts cached yet/i)).toBeInTheDocument());
  });
});
