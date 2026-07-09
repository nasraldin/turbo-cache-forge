import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { formatPercent } from "@/lib/format";

// THE signature element (design brief): hit-rate as a large tabular-mono %
// beside a horizontal hit(teal)/miss(amber) bar. Large here on Overview (the
// hero); a slim variant echoes on stat tiles / the trend page in later tasks
// — kept as its own component now so that reuse doesn't require a rewrite.
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
    <Card>
      <CardContent className="p-6">
        {loading ? (
          <div className="space-y-4">
            <Skeleton data-testid="hit-meter-skeleton" className="h-14 w-40" />
            <Skeleton className="h-3 w-full rounded-full" />
          </div>
        ) : (
          <>
            <div className="flex items-baseline gap-3">
              <span className="font-data text-6xl font-semibold tracking-tight text-hit">
                {formatPercent(ratio)}
              </span>
              <span className="text-sm text-muted">hit rate</span>
            </div>

            <div
              className="mt-5 flex h-3 w-full overflow-hidden rounded-full bg-surface-2"
              role="img"
              aria-label={`${formatPercent(ratio)} hit rate: ${hits.toLocaleString()} hits, ${misses.toLocaleString()} misses`}
            >
              <div className="h-full bg-hit" style={{ width: `${hitPct}%` }} />
              <div className="h-full bg-miss" style={{ width: `${100 - hitPct}%` }} />
            </div>

            <div className="mt-2 flex justify-between font-data text-xs">
              <span className="text-hit">{hits.toLocaleString()} hits</span>
              <span className="text-miss">{misses.toLocaleString()} misses</span>
            </div>
          </>
        )}
      </CardContent>
    </Card>
  );
}
