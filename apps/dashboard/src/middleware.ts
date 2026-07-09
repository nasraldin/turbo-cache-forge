import { clerkMiddleware, createRouteMatcher } from "@clerk/nextjs/server";
import {
  NextResponse,
  type NextFetchEvent,
  type NextMiddleware,
  type NextRequest,
} from "next/server";

const isPublic = createRouteMatcher(["/sign-in(.*)", "/sign-up(.*)"]);

// Built lazily so builtin-mode deployments (no Clerk key) never construct it.
// Typed explicitly as NextMiddleware: ReturnType<typeof clerkMiddleware> resolves
// to the *last* overload of clerkMiddleware's overloaded type (a NextMiddleware
// invocation result, not the function itself), which mistypes this otherwise.
let guard: NextMiddleware | null = null;
function clerkGuard() {
  return (guard ??= clerkMiddleware(async (auth, req) => {
    if (isPublic(req)) return;
    const { userId } = await auth();
    if (!userId) return NextResponse.redirect(new URL("/sign-in", req.url));
  }));
}

export default function middleware(req: NextRequest, ev: NextFetchEvent) {
  // CLERK_SECRET_KEY presence is the oidc/builtin switch: built-in auth mode
  // ships NO Clerk secret, so skip Clerk here and let the client-side session
  // guard (BuiltinSessionProvider) handle redirects. If a builtin deployment
  // leaves a (dummy) CLERK_SECRET_KEY set, every route would redirect-loop.
  if (!process.env.CLERK_SECRET_KEY) return NextResponse.next();
  return clerkGuard()(req, ev);
}

export const config = { matcher: ["/((?!_next|.*\\..*).*)", "/"] };
