"use client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useApiClient } from "@/app/api";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { CreateTokenDialog } from "@/components/create-token-dialog";
import { DataTable, type Column } from "@/components/data-table";
import { PageHeader } from "@/components/page-header";
import { Skeleton } from "@/components/ui/skeleton";
import type { Token } from "@tcf/types";

export default function ApiKeysPage() {
  const api = useApiClient();
  const qc = useQueryClient();
  const { data = [], isLoading, isError } = useQuery({ queryKey: ["tokens"], queryFn: () => api.listTokens() });
  const revoke = useMutation({
    mutationFn: (id: number) => api.revokeToken(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["tokens"] }),
  });
  const refresh = () => qc.invalidateQueries({ queryKey: ["tokens"] });

  // NOTE: fields are snake_case — revoked_at / last_used_at (NOT revokedAt / lastUsedAt).
  const columns: Column<Token>[] = [
    { header: "Name", cell: (t) => <span className="font-medium">{t.name}</span> },
    { header: "Status", cell: (t) => (t.revoked_at ? <Badge variant="danger">Revoked</Badge> : <Badge>Active</Badge>) },
    { header: "Last used", cell: (t) => <span className="font-data text-muted">{t.last_used_at ? new Date(t.last_used_at).toLocaleString() : "Never"}</span> },
    { header: "", cell: (t) => (t.revoked_at ? null
        : <Button size="sm" variant="ghost" disabled={revoke.isPending} onClick={() => revoke.mutate(t.id)}>Revoke</Button>) },
  ];

  return (
    <div>
      <div className="mb-6 flex items-center justify-between">
        <PageHeader title="API Keys" description="Bearer tokens used by the Turborepo CLI on the cache path." />
        <CreateTokenDialog createToken={(i) => api.createToken(i)} onCreated={refresh} />
      </div>
      {isError ? (
        <p role="alert" className="rounded-md border border-danger/30 bg-danger/10 px-4 py-3 text-sm text-danger">
          Couldn&apos;t reach the cache API. Check that NEXT_PUBLIC_API_URL points at a running turbo-cache-forge.
        </p>
      ) : isLoading ? (
        <div className="space-y-2">{Array.from({ length: 4 }).map((_, i) => <Skeleton key={i} className="h-10 w-full" />)}</div>
      ) : (
        <DataTable columns={columns} rows={data} empty="No API keys yet. Create one to let CI read and write the cache." />
      )}
    </div>
  );
}
