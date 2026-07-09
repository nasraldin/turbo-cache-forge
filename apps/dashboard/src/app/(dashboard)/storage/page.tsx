"use client";
import { useQuery } from "@tanstack/react-query";
import { useApiClient } from "@/app/api";
import { PageHeader } from "@/components/page-header";
import { StatTile } from "@/components/stat-tile";
import { formatBytes } from "@/lib/format";

// No per-project breakdown — the API has no size-by-project data. YAGNI.
export default function StoragePage() {
  const api = useApiClient();
  const stats = useQuery({ queryKey: ["stats"], queryFn: () => api.getStats() });
  const { isError } = stats;

  if (isError) {
    return (
      <div>
        <PageHeader title="Storage Usage" description="Object storage consumed by cached artifacts." />
        <p
          role="alert"
          className="rounded-md border border-danger/30 bg-danger/10 px-4 py-3 text-sm text-danger"
        >
          Couldn&apos;t reach the cache API. Check that NEXT_PUBLIC_API_URL points at a running
          turbo-cache-forge.
        </p>
      </div>
    );
  }

  return (
    <div>
      <PageHeader title="Storage Usage" description="Object storage consumed by cached artifacts." />
      <div className="grid gap-4 sm:grid-cols-2">
        <StatTile label="Total stored" loading={stats.isLoading}
          value={stats.data ? formatBytes(stats.data.storage_bytes) : ""} />
      </div>
    </div>
  );
}
