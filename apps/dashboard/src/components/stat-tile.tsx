import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";

// Reused by Overview (Task 4) and Storage Usage (Task 5) — one plain numeric
// tile, no chart. The HIT/MISS meter is its own component (hit-meter.tsx);
// this is the quiet supporting-tile primitive per the design brief.
export function StatTile({
  label,
  value,
  hint,
  loading,
}: {
  label: string;
  value: string;
  hint?: string;
  loading?: boolean;
}) {
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-sm font-medium text-muted">{label}</CardTitle>
      </CardHeader>
      <CardContent>
        {loading ? (
          <Skeleton data-testid="stat-tile-skeleton" className="h-9 w-24" />
        ) : (
          <div className="font-data text-3xl font-semibold tracking-tight text-text">{value}</div>
        )}
        {hint && <p className="mt-1 font-data text-xs text-muted">{hint}</p>}
      </CardContent>
    </Card>
  );
}
