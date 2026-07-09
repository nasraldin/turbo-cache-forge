"use client";
import { useQuery } from "@tanstack/react-query";
import { FileText, Folder, Loader2 } from "lucide-react";
import { useApiClient } from "@/app/api";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { formatBytes } from "@/lib/format";

// Details + decoded contents of one artifact. Controlled by the parent (open
// when a hash is selected); the detail query runs only while open.
export function ArtifactDetailDialog({
  hash,
  onClose,
}: {
  hash: string | null;
  onClose: () => void;
}) {
  const api = useApiClient();
  const { data, isLoading, isError } = useQuery({
    queryKey: ["artifact", hash],
    queryFn: () => api.getArtifact(hash!),
    enabled: !!hash,
  });

  async function download() {
    if (!hash) return;
    const blob = await api.getArtifactBlob(hash);
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `${hash}.tar.zst`;
    a.click();
    URL.revokeObjectURL(url);
  }

  return (
    <Dialog open={!!hash} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>Artifact contents</DialogTitle>
        </DialogHeader>
        {isError ? (
          <p role="alert" className="text-sm text-danger">
            Couldn&apos;t load this artifact.
          </p>
        ) : isLoading || !data ? (
          <div className="flex items-center gap-2 text-sm text-muted">
            <Loader2 className="h-4 w-4 animate-spin" aria-hidden /> Loading…
          </div>
        ) : (
          <div className="space-y-4">
            <dl className="grid grid-cols-2 gap-x-4 gap-y-2 text-sm">
              <dt className="text-muted">Hash</dt>
              <dd className="font-data truncate" title={data.hash}>{data.hash}</dd>
              <dt className="text-muted">Size</dt>
              <dd className="font-data">{formatBytes(data.size_bytes)}</dd>
              <dt className="text-muted">Created</dt>
              <dd className="font-data">{new Date(data.created_at).toLocaleString()}</dd>
              <dt className="text-muted">Last accessed</dt>
              <dd className="font-data">{new Date(data.last_accessed_at).toLocaleString()}</dd>
            </dl>

            {data.content.format === "opaque" ? (
              <p className="rounded-md border border-border bg-surface-2 px-3 py-2 text-sm text-muted">
                Encrypted or non-Turbo artifact — download to inspect.
              </p>
            ) : (
              <div className="space-y-1">
                <p className="eyebrow">
                  Contents{data.content.truncated ? " (truncated)" : ""}
                </p>
                <ul className="max-h-72 space-y-1 overflow-y-auto rounded-md border border-border p-2">
                  {data.content.entries.map((e) => (
                    <li key={e.path} className="text-sm">
                      <div className="flex items-center justify-between gap-2">
                        <span className="flex min-w-0 items-center gap-2">
                          {e.is_dir ? (
                            <Folder className="h-4 w-4 shrink-0 text-faint" aria-hidden />
                          ) : (
                            <FileText className="h-4 w-4 shrink-0 text-faint" aria-hidden />
                          )}
                          <span className="font-data truncate" title={e.path}>{e.path}</span>
                        </span>
                        {!e.is_dir && (
                          <span className="font-data shrink-0 text-muted">{formatBytes(e.size)}</span>
                        )}
                      </div>
                      {e.previewable && e.preview && (
                        <pre className="font-data mt-1 max-h-40 overflow-auto rounded bg-surface-2 p-2 text-xs text-text">
                          {e.preview}
                        </pre>
                      )}
                    </li>
                  ))}
                </ul>
              </div>
            )}

            <div className="flex justify-end">
              <Button variant="outline" onClick={() => void download()}>
                Download
              </Button>
            </div>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
