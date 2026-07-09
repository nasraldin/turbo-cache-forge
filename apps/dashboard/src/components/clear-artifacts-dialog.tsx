"use client";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";

const PHRASE = "delete all";

// Destructive: wipes every artifact for the org. Gated on typing the phrase so
// it can't be triggered by a stray click.
export function ClearArtifactsDialog({
  clearArtifacts,
  onCleared,
  disabled,
}: {
  clearArtifacts: () => Promise<{ deleted: number }>;
  onCleared: () => void;
  disabled?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [phrase, setPhrase] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit() {
    setBusy(true);
    try {
      await clearArtifacts();
      onCleared();
      setOpen(false);
      setPhrase("");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={(o) => { setOpen(o); if (!o) setPhrase(""); }}>
      <DialogTrigger asChild>
        <Button variant="destructive" disabled={disabled}>Clear all</Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader><DialogTitle>Clear all artifacts</DialogTitle></DialogHeader>
        <form className="space-y-3" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
          <p className="text-sm text-muted">
            This permanently deletes every cached artifact for this organization. Type{" "}
            <code className="font-data text-text">{PHRASE}</code> to confirm.
          </p>
          <Input
            aria-label="Confirmation phrase"
            value={phrase}
            onChange={(e) => setPhrase(e.target.value)}
          />
          <Button type="submit" variant="destructive" disabled={busy || phrase !== PHRASE}>
            Delete everything
          </Button>
        </form>
      </DialogContent>
    </Dialog>
  );
}
