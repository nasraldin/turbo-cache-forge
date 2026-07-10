"use client";
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Eye, Trash2 } from "lucide-react";
import { useState } from "react";
import { useApiClient } from "@/app/api";
import { ArtifactDetailDialog } from "@/components/artifact-detail-dialog";
import { ClearArtifactsDialog } from "@/components/clear-artifacts-dialog";
import { DataTable, type Column } from "@/components/data-table";
import { PageHeader } from "@/components/page-header";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import { formatBytes } from "@/lib/format";
import type { Artifact } from "@tcf/types";

const LIMIT = 50;

// ponytail: middle-truncate a hash inline (a1b2c3…9f3c).
const shortHash = (h: string) => (h.length > 18 ? `${h.slice(0, 10)}…${h.slice(-6)}` : h);

export default function ArtifactsPage() {
  const api = useApiClient();
  const qc = useQueryClient();
  const [offset, setOffset] = useState(0);
  const [detailHash, setDetailHash] = useState<string | null>(null);
  const [pendingDelete, setPendingDelete] = useState<string | null>(null);

  const { data, isLoading, isError, isFetching } = useQuery({
    queryKey: ["artifacts", offset],
    queryFn: () => api.listArtifacts({ limit: LIMIT, offset }),
    placeholderData: keepPreviousData,
  });
  const stats = useQuery({ queryKey: ["stats"], queryFn: () => api.getStats() });

  const refresh = () => {
    void qc.invalidateQueries({ queryKey: ["artifacts"] });
    void qc.invalidateQueries({ queryKey: ["stats"] });
  };
  const del = useMutation({
    mutationFn: (hash: string) => api.deleteArtifact(hash),
    onSuccess: () => { setPendingDelete(null); refresh(); },
  });

  const arts = data?.artifacts ?? [];
  const hasNext = arts.length === LIMIT;
  const hasPrev = offset > 0;

  const columns: Column<Artifact>[] = [
    { header: "Hash", cell: (a) => <code className="font-data text-sm" title={a.hash}>{shortHash(a.hash)}</code> },
    { header: "Size", cell: (a) => <span className="font-data">{formatBytes(a.size_bytes)}</span> },
    { header: "Tag", cell: (a) => (a.tag ? <Badge>{a.tag}</Badge> : <span className="text-muted">—</span>) },
    { header: "Created", cell: (a) => <span className="font-data text-muted">{new Date(a.created_at).toLocaleDateString()}</span> },
    { header: "Last accessed", cell: (a) => <span className="font-data text-muted">{new Date(a.last_accessed_at).toLocaleString()}</span> },
    {
      header: "",
      cell: (a) => (
        <div className="flex justify-end gap-1">
          <Button size="sm" variant="ghost" aria-label={`View ${a.hash}`} onClick={() => setDetailHash(a.hash)}>
            <Eye className="h-4 w-4" aria-hidden />
          </Button>
          <Button size="sm" variant="ghost" aria-label={`Delete ${a.hash}`} onClick={() => setPendingDelete(a.hash)}>
            <Trash2 className="h-4 w-4 text-danger" aria-hidden />
          </Button>
        </div>
      ),
    },
  ];

  return (
    <div>
      <PageHeader
        eyebrow="Monitor"
        title="Artifacts"
        description="Cached build outputs stored for this organization."
        actions={
          <ClearArtifactsDialog
            clearArtifacts={() => api.clearArtifacts()}
            onCleared={refresh}
            disabled={(stats.data?.artifact_count ?? 0) === 0}
          />
        }
      />

      {stats.data && (
        <div className="mb-5 flex flex-wrap gap-x-8 gap-y-1">
          <div>
            <span className="eyebrow">Artifacts</span>
            <p className="font-data text-lg text-text">{stats.data.artifact_count.toLocaleString()}</p>
          </div>
          <div>
            <span className="eyebrow">Total size</span>
            <p className="font-data text-lg text-text">{formatBytes(stats.data.storage_bytes)}</p>
          </div>
        </div>
      )}

      {isError ? (
        <p role="alert" className="rounded-md border border-danger/30 bg-danger/10 px-4 py-3 text-sm text-danger">
          Couldn&apos;t reach the cache API. Check that NEXT_PUBLIC_API_URL points at a running Turbo Cache Forge.
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
              <Button variant="outline" disabled={!hasPrev || isFetching} onClick={() => setOffset((o) => Math.max(0, o - LIMIT))}>Prev</Button>
              <Button variant="outline" disabled={!hasNext || isFetching} onClick={() => setOffset((o) => o + LIMIT)}>Next</Button>
            </div>
          )}
        </>
      )}

      <ArtifactDetailDialog hash={detailHash} onClose={() => setDetailHash(null)} />

      <Dialog open={!!pendingDelete} onOpenChange={(o) => !o && setPendingDelete(null)}>
        <DialogContent>
          <DialogHeader><DialogTitle>Delete artifact</DialogTitle></DialogHeader>
          <p className="text-sm text-muted">
            Remove this cached artifact? A later build will re-upload it on the next miss.
          </p>
          <div className="flex justify-end gap-2">
            <Button variant="outline" onClick={() => setPendingDelete(null)}>Cancel</Button>
            <Button variant="destructive" disabled={del.isPending} onClick={() => pendingDelete && del.mutate(pendingDelete)}>Delete</Button>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}
