"use client";
import { createApiClient, type ApiClient } from "@tcf/api-client";
import { useMemo } from "react";
import { useSession } from "./session";

// The ONLY place the session token meets the SDK. Works for either auth mode —
// useSession().getToken() returns a Clerk JWT (oidc) or the built-in JWT.
export function useApiClient(): ApiClient {
  const { getToken } = useSession();
  return useMemo(
    () =>
      createApiClient({
        baseUrl: process.env.NEXT_PUBLIC_API_URL!,
        getToken: () => getToken(),
      }),
    [getToken],
  );
}
