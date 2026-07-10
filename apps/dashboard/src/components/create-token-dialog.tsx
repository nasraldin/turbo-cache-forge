"use client";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import type { CreatedToken } from "@tcf/types";

// Security-critical: the plaintext token only ever exists here, in
// dialog-local state (`secret`) — it is never re-fetched (the list endpoint
// returns metadata only) and is wiped by reset() on close.
export function CreateTokenDialog({ createToken, onCreated }:
  { createToken: (input: { name: string; read_only?: boolean }) => Promise<CreatedToken>; onCreated: () => void }) {
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [readOnly, setReadOnly] = useState(false);
  const [secret, setSecret] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  function reset() { setName(""); setReadOnly(false); setSecret(null); setBusy(false); }

  async function submit() {
    setBusy(true);
    try {
      const created = await createToken({ name, read_only: readOnly });
      setSecret(created.token); // held only while the dialog is open
      onCreated();
    } finally {
      setBusy(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={(o) => { setOpen(o); if (!o) reset(); }}>
      <DialogTrigger asChild><Button variant="primary">New token</Button></DialogTrigger>
      <DialogContent>
        <DialogHeader><DialogTitle>Create API token</DialogTitle></DialogHeader>
        {secret ? (
          <div className="space-y-3">
            <p className="text-sm text-muted">
              Copy this token now — you won&apos;t be able to see it again.
            </p>
            <div className="flex min-w-0 items-center gap-2">
              <code className="font-data min-w-0 flex-1 truncate rounded bg-surface-2 px-3 py-2 text-sm">{secret}</code>
              <Button variant="outline" className="shrink-0" onClick={() => navigator.clipboard?.writeText(secret)}>Copy</Button>
            </div>
            <Button onClick={() => setOpen(false)}>Done</Button>
          </div>
        ) : (
          <form className="space-y-3" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
            <label className="block text-sm font-medium" htmlFor="token-name">Name</label>
            <Input id="token-name" value={name} onChange={(e) => setName(e.target.value)} required />
            <label className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={readOnly}
                onChange={(e) => setReadOnly(e.target.checked)}
                className="h-4 w-4 rounded border-border"
              />
              <span>Read-only <span className="text-muted">— can pull from the cache but never push</span></span>
            </label>
            <Button type="submit" disabled={busy || !name}>Create</Button>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}
