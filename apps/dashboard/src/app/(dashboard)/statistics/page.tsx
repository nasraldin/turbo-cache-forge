"use client";
import { useQuery } from "@tanstack/react-query";
import { useApiClient } from "@/app/api";
import { HitRateChart } from "@/components/hit-rate-chart";
import { PageHeader } from "@/components/page-header";
import { StatTile } from "@/components/stat-tile";
import { Skeleton } from "@/components/ui/skeleton";
import { formatPercent } from "@/lib/format";

// Real Stats (GET /api/v1/stats) is snake_case with NO hitRate field — the
// rate is computed here exactly like HitMeter does (hits / (hits + misses),
// divide-by-zero guarded), never read off a nonexistent field.
export default function StatisticsPage() {
  const api = useApiClient();
  const stats = useQuery({ queryKey: ["stats"], queryFn: () => api.getStats() });
  const series = useQuery({
    queryKey: ["stats-ts", 30],
    queryFn: () => api.getStatsTimeseries(30),
  });

  const s = stats.data;
  const total = s ? s.hits + s.misses : 0;
  const rate = s && total > 0 ? s.hits / total : 0;

  if (stats.isError || series.isError) {
    return (
      <div>
        <PageHeader
          title="Cache Statistics"
          description="Hit rate and cache activity over the last 30 days."
        />
        <p
          role="alert"
          className="rounded-md border border-danger/30 bg-danger/10 px-4 py-3 text-sm text-danger"
        >
          Couldn&apos;t reach the cache API. Check that NEXT_PUBLIC_API_URL points at a running
          Turbo Cache Forge.
        </p>
      </div>
    );
  }

  return (
    <div>
      <PageHeader
        title="Cache Statistics"
        description="Hit rate and cache activity over the last 30 days."
      />
      <div className="mb-6 grid gap-4 sm:grid-cols-3">
        <StatTile label="Hit rate" loading={stats.isLoading} value={s ? formatPercent(rate) : ""} />
        <StatTile label="Hits" loading={stats.isLoading} value={s ? s.hits.toLocaleString() : ""} />
        <StatTile
          label="Misses"
          loading={stats.isLoading}
          value={s ? s.misses.toLocaleString() : ""}
        />
      </div>
      <div className="rounded-lg border border-border p-4">
        {series.isLoading ? (
          <Skeleton className="h-[320px] w-full" />
        ) : (
          <HitRateChart points={series.data ?? []} />
        )}
      </div>
    </div>
  );
}
