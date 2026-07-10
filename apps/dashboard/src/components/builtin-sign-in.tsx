"use client";
import { Activity, Eye, EyeOff, Loader2 } from "lucide-react";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { useSession } from "@/app/session";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";

// Built-in username/password sign-in for self-hosted deployments with no
// OIDC provider configured (AUTH_MODE=builtin). Mirrors the shape of the
// Clerk <SignIn/> screen it replaces so the route can swap between them.
export function BuiltinSignIn() {
  const { login } = useSession();
  const router = useRouter();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [show, setShow] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    setSubmitting(true);
    try {
      await login?.(username, password);
      router.replace("/");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Sign-in failed. Try again.");
      setSubmitting(false);
    }
  }

  return (
    <div className="grid min-h-screen place-items-center bg-bg p-4">
      <Card className="w-full max-w-sm animate-in fade-in slide-in-from-bottom-1 overflow-hidden p-0 duration-300">
        {/* Status stripe: reuses --hit for its literal meaning ("system online"), not decoration. */}
        <div className="h-0.5 bg-hit" aria-hidden />
        <div className="p-8">
          <div className="mb-6 flex flex-col gap-3">
            <span className="font-data text-[11px] font-medium uppercase tracking-[0.14em] text-muted">
              Sign in
            </span>
            <div className="flex items-center gap-2">
              <span className="grid h-8 w-8 place-items-center rounded-md border border-border bg-surface-2">
                <Activity className="h-4 w-4 text-hit" aria-hidden />
              </span>
              <span className="text-lg font-semibold tracking-tight text-text">Turbo Cache Forge</span>
            </div>
          </div>

          <form onSubmit={onSubmit} className="flex flex-col gap-4">
            <div className="flex flex-col gap-1.5">
              <label
                htmlFor="username"
                className="text-[11px] font-medium uppercase tracking-wide text-muted"
              >
                Username
              </label>
              <Input
                id="username"
                autoComplete="username"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                required
                autoFocus
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <label
                htmlFor="password"
                className="text-[11px] font-medium uppercase tracking-wide text-muted"
              >
                Password
              </label>
              <div className="relative">
                <Input
                  id="password"
                  type={show ? "text" : "password"}
                  autoComplete="current-password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  required
                  className="pr-10"
                />
                <button
                  type="button"
                  onClick={() => setShow((s) => !s)}
                  className="absolute inset-y-0 right-0 grid w-10 place-items-center text-muted transition-colors hover:text-text"
                >
                  {/*
                   * Accessible name via visually-hidden text, not aria-label: an
                   * aria-label containing "password" is picked up by Testing
                   * Library's getByLabelText(/password/i) attribute matcher
                   * (it isn't restricted to labelled form controls), which
                   * collides with the password field's own label.
                   */}
                  <span className="sr-only">{show ? "Hide password" : "Show password"}</span>
                  {show ? <EyeOff className="h-4 w-4" aria-hidden /> : <Eye className="h-4 w-4" aria-hidden />}
                </button>
              </div>
            </div>

            {error && (
              <p role="alert" className="text-sm text-miss">
                {error}
              </p>
            )}

            <Button type="submit" variant="primary" disabled={submitting} className="w-full">
              {submitting && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />}
              Sign in
            </Button>
          </form>
        </div>
      </Card>
    </div>
  );
}
