// Built-in-auth session token store. The JWT is a bearer token the SPA must
// read to send as Authorization, so it lives in localStorage (accepted
// trade-off for a self-hosted single-user tool; short TTL limits exposure).
const KEY = "tcf.builtin.token";

export function decodeExp(token: string): number | null {
  const parts = token.split(".");
  if (parts.length !== 3) return null;
  try {
    const json = atob(parts[1].replace(/-/g, "+").replace(/_/g, "/"));
    const claims = JSON.parse(json) as { exp?: number };
    return typeof claims.exp === "number" ? claims.exp : null;
  } catch {
    return null;
  }
}

// decodeUsername reads the `username` claim so the shell can show who's signed
// in without a round-trip (the root user is single-tenant, set server-side).
export function decodeUsername(token: string): string | null {
  const parts = token.split(".");
  if (parts.length !== 3) return null;
  try {
    const json = atob(parts[1].replace(/-/g, "+").replace(/_/g, "/"));
    const claims = JSON.parse(json) as { username?: string };
    return typeof claims.username === "string" ? claims.username : null;
  } catch {
    return null;
  }
}

export function saveToken(token: string): void {
  if (typeof window !== "undefined") window.localStorage.setItem(KEY, token);
}

export function loadToken(): string | null {
  if (typeof window === "undefined") return null;
  const tok = window.localStorage.getItem(KEY);
  if (!tok) return null;
  const exp = decodeExp(tok);
  if (exp === null || Date.now() / 1000 >= exp) {
    window.localStorage.removeItem(KEY);
    return null;
  }
  return tok;
}

export function clearToken(): void {
  if (typeof window !== "undefined") window.localStorage.removeItem(KEY);
}

// login POSTs credentials, stores the returned token, and returns it. Throws
// with a user-facing message on failure.
export async function login(baseUrl: string, username: string, password: string): Promise<string> {
  const res = await fetch(`${baseUrl.replace(/\/$/, "")}/api/v1/auth/login`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ username, password }),
  });
  if (!res.ok) {
    throw new Error(res.status === 401 ? "Invalid username or password" : "Sign-in failed. Try again.");
  }
  const body = (await res.json()) as { token: string };
  saveToken(body.token);
  return body.token;
}
