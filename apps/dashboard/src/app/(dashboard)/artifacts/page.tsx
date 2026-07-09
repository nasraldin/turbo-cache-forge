"use client";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { useApiClient } from "@/app/api";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { DataTable, type Column } from "@/components/data-table";
import { PageHeader } from "@/components/page-header";
import { Skeleton } from "@/components/ui/skeleton";
import { formatBytes } from "@/lib/format";
import type { Artifact } from "@tcf/types";

const LIMIT = 50;

// ponytail: middle-truncate a hash inline (a1b2c3…9f3c); one column, no shared helper needed.
const shortHash = (h: string) => (h.length > 18 ? `${h.slice(0, 10)}…${h.slice(-6)}` : h);

const columns: Column<Artifact>[] = [
  { header: "Hash", cell: (a) => <code className="font-data text-sm" title={a.hash}>{shortHash(a.hash)}</code> },
  { header: "Size", cell: (a) => <span className="font-data">{formatBytes(a.size_bytes)}</span> },
  { header: "Tag", cell: (a) => (a.tag ? <Badge>{a.tag}</Badge> : <span className="text-muted">—</span>) },
  { header: "Created", cell: (a) => <span className="font-data text-muted">{new Date(a.created_at).toLocaleDateString()}</span> },
  { header: "Last accessed", cell: (a) => <span className="font-data text-muted">{new Date(a.last_accessed_at).toLocaleString()}</span> },
];

export default function ArtifactsPage() {
  const api = useApiClient();
  const [offset, setOffset] = useState(0);
  const { data, isLoading, isError, isFetching } = useQuery({
    queryKey: ["artifacts", offset],
    queryFn: () => api.listArtifacts({ limit: LIMIT, offset }),
    placeholderData: keepPreviousData,
  });

  const arts = data?.artifacts ?? [];
  const hasNext = arts.length === LIMIT; // full page → likely more (no total available)
  const hasPrev = offset > 0;

  return (
    <div>
      <PageHeader title="Artifacts" description="Cached build outputs stored for this organization." />
      {isError ? (
        <p role="alert" className="rounded-md border border-danger/30 bg-danger/10 px-4 py-3 text-sm text-danger">
          Couldn&apos;t reach the cache API. Check that NEXT_PUBLIC_API_URL points at a running turbo-cache-forge.
        </p>
      ) : isLoading ? (
        <div className="space-y-2">
          {Array.from({ length: 6 }).map((_, i) => <Skeleton key={i} className="h-10 w-full" />)}
        </div>
      ) : (
        <>
          <DataTable
            columns={columns}
            rows={arts}
            empty="No artifacts cached yet. Run a build with TURBO_TOKEN set and they'll show up here."
          />
          {(hasPrev || hasNext) && (
            <div className="mt-4 flex items-center gap-2">
              <Button variant="outline" disabled={!hasPrev || isFetching}
                onClick={() => setOffset((o) => Math.max(0, o - LIMIT))}>Prev</Button>
              <Button variant="outline" disabled={!hasNext || isFetching}
                onClick={() => setOffset((o) => o + LIMIT)}>Next</Button>
            </div>
          )}
        </>
      )}
    </div>
  );
}
