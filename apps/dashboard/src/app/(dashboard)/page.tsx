"use client";

import { useQuery } from "@tanstack/react-query";
import { useApiClient } from "@/app/api";
import { HitMeter } from "@/components/hit-meter";
import { PageHeader } from "@/components/page-header";
import { StatTile } from "@/components/stat-tile";
import { formatBytes } from "@/lib/format";

// The Overview is the dashboard's hero — the HIT/MISS meter (HitMeter) is the
// one signature element (design brief). Everything else here is a quiet
// supporting tile; no chart, no extra color (YAGNI — trend charting is
// Task 6's job on a separate page).
export default function OverviewPage() {
  const api = useApiClient();
  const { data, isLoading, isError } = useQuery({
    queryKey: ["stats"],
    queryFn: () => api.getStats(),
  });

  const isEmpty = !isLoading && !isError && !!data && data.hits + data.misses === 0;

  return (
    <div>
      <PageHeader title="Overview" description="Live cache health from /api/v1." />

      {isError ? (
        <p
          role="alert"
          className="rounded-md border border-danger/30 bg-danger/10 px-4 py-3 text-sm text-danger"
        >
          Couldn&apos;t reach the cache API. Check that NEXT_PUBLIC_API_URL points at a running
          turbo-cache-forge.
        </p>
      ) : (
        <div className="grid gap-4">
          {isEmpty && (
            <p className="rounded-md border border-border bg-surface px-4 py-3 text-sm text-muted">
              No cache activity yet. Run a build with{" "}
              <code className="font-data text-text">TURBO_TOKEN</code> set and hits will show up
              here.
            </p>
          )}

          <HitMeter hits={data?.hits ?? 0} misses={data?.misses ?? 0} loading={isLoading} />

          <div className="grid gap-4 sm:grid-cols-3">
            <StatTile
              label="Storage used"
              loading={isLoading}
              value={data ? formatBytes(data.storage_bytes) : ""}
              hint={data ? `${data.artifact_count.toLocaleString()} artifacts` : undefined}
            />
            <StatTile
              label="Requests"
              loading={isLoading}
              value={data ? data.requests.toLocaleString() : ""}
            />
            <StatTile
              label="Work saved"
              loading={isLoading}
              value={data ? formatBytes(data.bytes_down) : ""}
              hint={data ? `${formatBytes(data.bytes_up)} uploaded` : undefined}
            />
          </div>
        </div>
      )}
    </div>
  );
}
