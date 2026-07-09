import { clerkMiddleware, createRouteMatcher } from "@clerk/nextjs/server";
import { NextResponse } from "next/server";

const isPublic = createRouteMatcher(["/sign-in(.*)", "/sign-up(.*)"]);

export default clerkMiddleware(async (auth, req) => {
  if (isPublic(req)) return;
  const { userId } = await auth();
  // Redirect to our own /sign-in explicitly — auth.protect() only knows the
  // sign-in URL when NEXT_PUBLIC_CLERK_SIGN_IN_URL is baked into the build,
  // which the Docker image isn't; without it protect() 404s instead of redirecting.
  if (!userId) {
    return NextResponse.redirect(new URL("/sign-in", req.url));
  }
});

export const config = { matcher: ["/((?!_next|.*\\..*).*)", "/"] };
