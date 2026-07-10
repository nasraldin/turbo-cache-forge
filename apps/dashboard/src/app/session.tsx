"use client";
import { ClerkProvider, useAuth, useClerk, useUser } from "@clerk/nextjs";
import { usePathname, useRouter } from "next/navigation";
import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import {
  clearToken,
  decodeUsername,
  loadToken,
  login as builtinLogin,
} from "@/lib/builtin-auth";

export interface Session {
  mode: "oidc" | "builtin";
  isLoaded: boolean;
  isSignedIn: boolean;
  getToken: () => Promise<string | null>;
  signOut: () => void;
  userLabel: string | null;
  login?: (username: string, password: string) => Promise<void>;
}

const SessionContext = createContext<Session | null>(null);

export function useSession(): Session {
  const ctx = useContext(SessionContext);
  if (!ctx) throw new Error("useSession must be used within <AuthRoot>");
  return ctx;
}

const API_URL = process.env.NEXT_PUBLIC_API_URL ?? "";

// ClerkSessionBridge is mounted ONLY inside <ClerkProvider> (oidc mode), so its
// Clerk hooks are always active — no conditional-hook violation.
function ClerkSessionBridge({ children }: { children: ReactNode }) {
  const { getToken, isLoaded, isSignedIn } = useAuth();
  const { user } = useUser();
  const clerk = useClerk();
  const value = useMemo<Session>(
    () => ({
      mode: "oidc",
      isLoaded,
      isSignedIn: Boolean(isSignedIn),
      getToken: () => getToken(),
      signOut: () => void clerk.signOut(),
      userLabel: user?.primaryEmailAddress?.emailAddress ?? user?.username ?? null,
    }),
    [getToken, isLoaded, isSignedIn, clerk, user],
  );
  return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>;
}

function BuiltinSessionProvider({ children }: { children: ReactNode }) {
  const router = useRouter();
  const pathname = usePathname();
  const [token, setToken] = useState<string | null>(null);
  const [isLoaded, setLoaded] = useState(false);

  useEffect(() => {
    setToken(loadToken());
    setLoaded(true);
  }, []);

  // Client-side route guard: builtin mode ships no Clerk middleware.
  useEffect(() => {
    if (isLoaded && !token && pathname !== "/sign-in") router.replace("/sign-in");
  }, [isLoaded, token, pathname, router]);

  const value = useMemo<Session>(
    () => ({
      mode: "builtin",
      isLoaded,
      isSignedIn: Boolean(token),
      getToken: async () => loadToken(),
      signOut: () => {
        clearToken();
        setToken(null);
        router.replace("/sign-in");
      },
      userLabel: token ? decodeUsername(token) : null,
      login: async (username, password) => {
        const t = await builtinLogin(API_URL, username, password);
        setToken(t);
      },
    }),
    [isLoaded, token, router],
  );
  return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>;
}

// AuthRoot fetches the server's auth mode once, then mounts the matching
// provider. ClerkProvider is only ever mounted in oidc mode, so builtin
// deployments need no Clerk publishable key.
export function AuthRoot({ children }: { children: ReactNode }) {
  const [mode, setMode] = useState<"oidc" | "builtin" | null>(null);

  useEffect(() => {
    let alive = true;
    fetch(`${API_URL.replace(/\/$/, "")}/api/v1/auth/config`)
      .then((r) => (r.ok ? r.json() : Promise.reject(new Error(String(r.status)))))
      .then((cfg: { mode: "oidc" | "builtin" }) => {
        if (alive) setMode(cfg.mode === "builtin" ? "builtin" : "oidc");
      })
      .catch(() => {
        // Fallback: infer from a baked Clerk key, else assume builtin.
        if (alive) setMode(process.env.NEXT_PUBLIC_CLERK_PUBLISHABLE_KEY ? "oidc" : "builtin");
      });
    return () => {
      alive = false;
    };
  }, []);

  if (mode === null) {
    return (
      <div className="grid min-h-screen place-items-center bg-bg text-muted" aria-busy="true">
        Loading…
      </div>
    );
  }
  if (mode === "builtin") {
    return <BuiltinSessionProvider>{children}</BuiltinSessionProvider>;
  }
  return (
    <ClerkProvider>
      <ClerkSessionBridge>{children}</ClerkSessionBridge>
    </ClerkProvider>
  );
}
