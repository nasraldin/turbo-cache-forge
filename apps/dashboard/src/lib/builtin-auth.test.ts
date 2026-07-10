import { afterEach, describe, expect, it, vi } from "vitest";
import { clearToken, decodeExp, loadToken, saveToken } from "./builtin-auth";

// Build an unsigned JWT-shaped string with a given exp (seconds).
function fakeJwt(expSec: number): string {
  const b64 = (o: unknown) =>
    btoa(JSON.stringify(o)).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
  return `${b64({ alg: "HS256" })}.${b64({ exp: expSec })}.sig`;
}

afterEach(() => {
  clearToken();
  vi.useRealTimers();
});

describe("builtin-auth token store", () => {
  it("saves and loads a non-expired token", () => {
    const tok = fakeJwt(Math.floor(Date.now() / 1000) + 3600);
    saveToken(tok);
    expect(loadToken()).toBe(tok);
  });

  it("returns null for an expired token", () => {
    const tok = fakeJwt(Math.floor(Date.now() / 1000) - 1);
    saveToken(tok);
    expect(loadToken()).toBeNull();
  });

  it("clearToken removes it", () => {
    saveToken(fakeJwt(Math.floor(Date.now() / 1000) + 3600));
    clearToken();
    expect(loadToken()).toBeNull();
  });

  it("decodeExp reads the exp claim", () => {
    expect(decodeExp(fakeJwt(1234))).toBe(1234);
    expect(decodeExp("garbage")).toBeNull();
  });
});
