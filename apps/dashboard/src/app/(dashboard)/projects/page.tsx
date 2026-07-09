"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useApiClient } from "@/app/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { DataTable, type Column } from "@/components/data-table";
import { PageHeader } from "@/components/page-header";
import type { Project } from "@tcf/types";

const columns: Column<Project>[] = [
  { header: "Name", cell: (p) => <span className="font-medium text-text">{p.name}</span> },
  {
    header: "Slug",
    cell: (p) => <code className="font-data text-sm text-text">{p.slug}</code>,
  },
  {
    header: "Created",
    cell: (p) => (
      <span className="font-data text-sm text-muted">
        {new Date(p.created_at).toLocaleDateString()}
      </span>
    ),
  },
];

// Cache namespaces (projects) for the org — list + create, reusing the same
// data layer and house style as Overview. DataTable is the shared list
// primitive; Artifacts (Task 7) reuses it as-is.
export default function ProjectsPage() {
  const api = useApiClient();
  const qc = useQueryClient();
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const {
    data = [],
    isLoading,
    isError,
  } = useQuery({ queryKey: ["projects"], queryFn: () => api.listProjects() });
  const create = useMutation({
    mutationFn: () => api.createProject({ name, slug }),
    onSuccess: () => {
      setName("");
      setSlug("");
      qc.invalidateQueries({ queryKey: ["projects"] });
    },
  });

  return (
    <div>
      <PageHeader title="Projects" description="Cache namespaces in this organization." />
      <form
        className="mb-6 flex gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          create.mutate();
        }}
      >
        <Input placeholder="Name" value={name} onChange={(e) => setName(e.target.value)} required />
        <Input placeholder="slug" value={slug} onChange={(e) => setSlug(e.target.value)} required />
        <Button type="submit" variant="primary" disabled={create.isPending}>
          {create.isPending ? "Adding…" : "Add project"}
        </Button>
      </form>

      {isError ? (
        <p
          role="alert"
          className="rounded-md border border-danger/30 bg-danger/10 px-4 py-3 text-sm text-danger"
        >
          Couldn&apos;t reach the cache API. Check that NEXT_PUBLIC_API_URL points at a running
          turbo-cache-forge.
        </p>
      ) : isLoading ? (
        <div className="space-y-3 rounded-md border border-border p-4">
          <Skeleton data-testid="projects-skeleton" className="h-5 w-full" />
          <Skeleton className="h-5 w-full" />
          <Skeleton className="h-5 w-full" />
        </div>
      ) : (
        <DataTable
          columns={columns}
          rows={data}
          empty="No projects yet — add one above."
        />
      )}
    </div>
  );
}
