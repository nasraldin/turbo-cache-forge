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
      { id: 1, slug: "web", name: "Web App", created_at: "2026-01-01T00:00:00Z" },
      { id: 2, slug: "api", name: "API", created_at: "2026-01-02T00:00:00Z" },
    ]);
    renderWithQuery(<ProjectsPage />);
    expect(await screen.findByText("Web App")).toBeInTheDocument();
    expect(screen.getByText("api")).toBeInTheDocument();
    // created_at (snake_case, per @tcf/types) must actually render, not just exist on the mock.
    expect(
      screen.getByText(new Date("2026-01-01T00:00:00Z").toLocaleDateString()),
    ).toBeInTheDocument();
  });

  it("shows an empty state when there are no projects", async () => {
    listProjects.mockResolvedValue([]);
    renderWithQuery(<ProjectsPage />);
    await waitFor(() => expect(screen.getByText(/no projects yet/i)).toBeInTheDocument());
  });
});
