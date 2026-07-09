import type { HTMLAttributes } from "react";
import { cn } from "@/lib/utils";

// Loading state per design brief: skeleton rows/tiles in --surface-2, no
// generic spinners for page content.
function Skeleton({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn("animate-pulse rounded-md bg-surface-2", className)}
      role="status"
      aria-label="Loading"
      {...props}
    />
  );
}

export { Skeleton };
