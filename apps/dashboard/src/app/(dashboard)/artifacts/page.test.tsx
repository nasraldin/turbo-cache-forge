import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import ArtifactsPage from "./page";

const listArtifacts = vi.fn();
const getStats = vi.fn();
const getArtifact = vi.fn();
const deleteArtifact = vi.fn();
const clearArtifacts = vi.fn();
const getArtifactBlob = vi.fn();
vi.mock("@/app/api", () => ({
  useApiClient: () => ({ listArtifacts, getStats, getArtifact, deleteArtifact, clearArtifacts, getArtifactBlob }),
}));

const art = (hash: string, size: number, tag: string | null) => ({
  hash, size_bytes: size, tag,
  created_at: "2026-07-01T00:00:00Z", last_accessed_at: "2026-07-02T00:00:00Z",
});
const STATS = { storage_bytes: 2048, artifact_count: 2, hits: 0, misses: 0, requests: 0, bytes_up: 0, bytes_down: 0 };

function wrap(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

beforeEach(() => {
  vi.clearAllMocks();
  getStats.mockResolvedValue(STATS);
});

describe("ArtifactsPage", () => {
  it("shows a page of artifacts and pages forward with offset", async () => {
    const full = [art("aaaaaaaa11111111", 4096, null),
      ...Array.from({ length: 49 }, (_, i) => art(`h${i}`, 1024, null))];
    listArtifacts
      .mockResolvedValueOnce({ limit: 50, offset: 0, artifacts: full })
      .mockResolvedValueOnce({ limit: 50, offset: 50, artifacts: [art("bbbbbbbb22222222", 8192, "build")] });

    wrap(<ArtifactsPage />);
    expect(await screen.findByText("4 KiB")).toBeInTheDocument();
    const next = screen.getByRole("button", { name: /next/i });
    expect(next).toBeEnabled();
    expect(screen.getByRole("button", { name: /prev/i })).toBeDisabled();

    await userEvent.click(next);
    expect(await screen.findByText("build")).toBeInTheDocument();
    expect(listArtifacts).toHaveBeenLastCalledWith({ limit: 50, offset: 50 });
    await waitFor(() => expect(screen.getByRole("button", { name: /next/i })).toBeDisabled());
    expect(screen.getByRole("button", { name: /prev/i })).toBeEnabled();
  });

  it("shows the empty state when there are no artifacts", async () => {
    listArtifacts.mockResolvedValueOnce({ limit: 50, offset: 0, artifacts: [] });
    wrap(<ArtifactsPage />);
    await waitFor(() => expect(screen.getByText(/no artifacts cached yet/i)).toBeInTheDocument());
  });

  it("opens the details dialog and shows the decoded manifest", async () => {
    listArtifacts.mockResolvedValueOnce({ limit: 50, offset: 0, artifacts: [art("abcd1234efgh5678", 100, null)] });
    getArtifact.mockResolvedValueOnce({
      ...art("abcd1234efgh5678", 100, null),
      content: { format: "zstd-tar", total_entries: 1, truncated: false,
        entries: [{ path: "out/log.txt", size: 5, is_dir: false, preview: "hello", previewable: true }] },
    });
    wrap(<ArtifactsPage />);
    await userEvent.click(await screen.findByRole("button", { name: /view abcd1234efgh5678/i }));
    expect(await screen.findByText("out/log.txt")).toBeInTheDocument();
    expect(screen.getByText("hello")).toBeInTheDocument();
  });

  it("gates clear-all on the typed confirmation phrase", async () => {
    listArtifacts.mockResolvedValueOnce({ limit: 50, offset: 0, artifacts: [art("h", 1, null)] });
    listArtifacts.mockResolvedValue({ limit: 50, offset: 0, artifacts: [] });
    clearArtifacts.mockResolvedValueOnce({ deleted: 2 });
    wrap(<ArtifactsPage />);
    await userEvent.click(await screen.findByRole("button", { name: /clear all/i }));
    const confirm = await screen.findByRole("button", { name: /delete everything/i });
    expect(confirm).toBeDisabled();
    await userEvent.type(screen.getByLabelText(/confirmation phrase/i), "delete all");
    expect(confirm).toBeEnabled();
    await userEvent.click(confirm);
    await waitFor(() => expect(clearArtifacts).toHaveBeenCalled());
    await waitFor(() => expect(screen.getByText(/no artifacts cached yet/i)).toBeInTheDocument());
  });
});
