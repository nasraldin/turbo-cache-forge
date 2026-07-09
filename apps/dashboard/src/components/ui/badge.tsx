import { cva, type VariantProps } from "class-variance-authority";
import type { HTMLAttributes } from "react";
import { cn } from "@/lib/utils";

// HIT/MISS is the through-line — badge variants map straight onto the
// product's own vocabulary, not generic primary/secondary.
const badgeVariants = cva(
  "inline-flex items-center rounded-md border px-2 py-0.5 text-xs font-semibold font-data",
  {
    variants: {
      variant: {
        hit: "border-hit/30 bg-hit/10 text-hit",
        miss: "border-miss/30 bg-miss/10 text-miss",
        danger: "border-danger/30 bg-danger/10 text-danger",
        neutral: "border-border bg-surface-2 text-muted",
      },
    },
    defaultVariants: { variant: "neutral" },
  },
);

export interface BadgeProps extends HTMLAttributes<HTMLSpanElement>, VariantProps<typeof badgeVariants> {}

function Badge({ className, variant, ...props }: BadgeProps) {
  return <span className={cn(badgeVariants({ variant }), className)} {...props} />;
}

export { Badge, badgeVariants };
