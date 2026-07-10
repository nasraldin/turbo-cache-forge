"use client";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import type { Project } from "@tcf/types";

// Create-project modal — the same header-button-opens-a-Dialog pattern the
// whole console uses for "add" actions (see CreateTokenDialog). Keeping the
// shape identical is what makes the app feel consistent.
export function CreateProjectDialog({
  createProject,
  onCreated,
}: {
  createProject: (input: { name: string; slug: string }) => Promise<Project>;
  onCreated: () => void;
}) {
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  function reset() {
    setName("");
    setSlug("");
    setBusy(false);
    setError(null);
  }

  async function submit() {
    setBusy(true);
    setError(null);
    try {
      await createProject({ name, slug });
      onCreated();
      setOpen(false);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Couldn't create the project.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        setOpen(o);
        if (!o) reset();
      }}
    >
      <DialogTrigger asChild>
        <Button variant="primary">New project</Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New project</DialogTitle>
          <DialogDescription>A cache namespace for this organization.</DialogDescription>
        </DialogHeader>
        <form
          className="grid gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            void submit();
          }}
        >
          <div className="grid gap-1.5">
            <label htmlFor="project-name" className="text-sm text-muted">
              Name
            </label>
            <Input
              id="project-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Web app"
              required
              autoFocus
            />
          </div>
          <div className="grid gap-1.5">
            <label htmlFor="project-slug" className="text-sm text-muted">
              Slug
            </label>
            <Input
              id="project-slug"
              value={slug}
              onChange={(e) => setSlug(e.target.value)}
              placeholder="web-app"
              required
              className="font-data"
            />
          </div>
          {error && (
            <p role="alert" className="text-sm text-danger">
              {error}
            </p>
          )}
          <DialogFooter>
            <Button type="submit" variant="primary" disabled={busy || !name || !slug}>
              {busy ? "Creating…" : "Create project"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
