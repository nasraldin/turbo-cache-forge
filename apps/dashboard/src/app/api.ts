"use client";
import { useAuth } from "@clerk/nextjs";
import { createApiClient, type ApiClient } from "@tcf/api-client";
import { useMemo } from "react";

// The ONLY place the Clerk JWT source meets the SDK. No page should ever
// call Clerk's getToken() itself or fetch() the API directly — everything
// routes through here so the backend coupling stays exactly NEXT_PUBLIC_API_URL
// + @tcf/api-client.
export function useApiClient(): ApiClient {
  const { getToken } = useAuth();
  return useMemo(
    () =>
      createApiClient({
        baseUrl: process.env.NEXT_PUBLIC_API_URL!,
        getToken: () => getToken(), // Clerk session JWT; Phase-3 validates it against JWKS
      }),
    [getToken],
  );
}
