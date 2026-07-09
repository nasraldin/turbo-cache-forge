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

  it("renders an optional hint", () => {
    render(<StatTile label="Hit rate" value="90%" hint="90 hits / 10 misses" />);
    expect(screen.getByText("90 hits / 10 misses")).toBeInTheDocument();
  });
});
