import { Card } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { formatPercent } from "@/lib/format";

// THE signature element: hit-rate as a huge tabular-mono %, over a segmented
// hit(teal)/miss(amber) gauge that sweeps in on load — the one orchestrated
// moment. Reduced-motion neutralizes the sweep globally.
export function HitMeter({
  hits,
  misses,
  loading,
}: {
  hits: number;
  misses: number;
  loading?: boolean;
}) {
  const total = hits + misses;
  const ratio = total > 0 ? hits / total : 0;
  const hitPct = ratio * 100;

  return (
    <Card className="relative overflow-hidden p-6 sm:p-7">
      {/* faint hit-teal wash behind the hero number — atmosphere, not decoration */}
      <div
        className="pointer-events-none absolute -right-16 -top-20 h-56 w-56 rounded-full bg-hit/10 blur-3xl"
        aria-hidden
      />
      {loading ? (
        <div className="space-y-5">
          <Skeleton data-testid="hit-meter-skeleton" className="h-16 w-44" />
          <Skeleton className="h-3.5 w-full rounded-full" />
          <Skeleton className="h-4 w-56" />
        </div>
      ) : (
        <div className="relative">
          <span className="eyebrow">Hit rate</span>
          <div className="mt-3 flex items-baseline gap-3">
            <span
              className={`font-data text-6xl font-semibold leading-none tracking-tight sm:text-7xl ${
                total > 0 ? "text-hit" : "text-faint"
              }`}
            >
              {formatPercent(ratio)}
            </span>
            <span className="text-sm text-muted">
              {total > 0
                ? `of ${total.toLocaleString()} requests served from cache`
                : "awaiting the first request"}
            </span>
          </div>

          <div
            className="mt-6 flex h-3.5 w-full overflow-hidden rounded-full bg-surface-2 ring-1 ring-inset ring-border"
            role="img"
            aria-label={
              total > 0
                ? `${formatPercent(ratio)} hit rate: ${hits.toLocaleString()} hits, ${misses.toLocaleString()} misses`
                : "No cache activity yet"
            }
          >
            {total > 0 && (
              <div className="animate-sweep flex h-full w-full">
                <div className="h-full bg-hit" style={{ width: `${hitPct}%` }} />
                <div className="h-full bg-miss" style={{ width: `${100 - hitPct}%` }} />
              </div>
            )}
          </div>

          <div className="mt-3 flex items-center justify-between font-data text-xs">
            <span className="inline-flex items-center gap-1.5 text-hit">
              <span className="h-2 w-2 rounded-full bg-hit" aria-hidden />
              {hits.toLocaleString()} hits
            </span>
            <span className="inline-flex items-center gap-1.5 text-miss">
              {misses.toLocaleString()} misses
              <span className="h-2 w-2 rounded-full bg-miss" aria-hidden />
            </span>
          </div>
        </div>
      )}
    </Card>
  );
}
