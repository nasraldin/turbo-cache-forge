import { Card } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";

// A quiet supporting readout: instrument-label eyebrow, one big tabular value,
// optional hint. The HIT/MISS meter is its own signature component
// (hit-meter.tsx); this is the calm tile everything else is built from.
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
    <Card className="p-5 transition-colors hover:border-border-strong">
      <span className="eyebrow block">{label}</span>
      {loading ? (
        <Skeleton data-testid="stat-tile-skeleton" className="mt-3 h-9 w-24" />
      ) : (
        <div className="mt-3 font-data text-[28px] font-semibold leading-none tracking-tight text-text">
          {value}
        </div>
      )}
      {hint && <p className="mt-2 font-data text-xs text-muted">{hint}</p>}
    </Card>
  );
}
