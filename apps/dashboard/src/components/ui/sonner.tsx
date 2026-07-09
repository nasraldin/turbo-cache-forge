"use client";
import { Toaster as Sonner, type ToasterProps } from "sonner";

// Flat, hairline-bordered toasts matching the instrument-panel surface tokens.
function Toaster(props: ToasterProps) {
  return (
    <Sonner
      theme="system"
      className="toaster group"
      toastOptions={{
        classNames: {
          toast:
            "group toast bg-surface text-text border border-border rounded-md shadow-none",
          description: "text-muted",
          actionButton: "bg-hit text-bg",
          cancelButton: "bg-surface-2 text-muted",
        },
      }}
      {...props}
    />
  );
}

export { Toaster };
