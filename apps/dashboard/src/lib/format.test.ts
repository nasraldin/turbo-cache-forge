import { describe, expect, it } from "vitest";
import { formatBytes, formatPercent } from "./format";

describe("formatters", () => {
  it("scales bytes to human units", () => {
    expect(formatBytes(0)).toBe("0 B");
    expect(formatBytes(1536)).toBe("1.5 KiB");
    expect(formatBytes(5 * 1024 ** 3)).toBe("5 GiB");
  });
  it("renders a 0..1 ratio as a percent", () => {
    expect(formatPercent(0.8342)).toBe("83.4%");
    expect(formatPercent(1)).toBe("100%");
  });
});
