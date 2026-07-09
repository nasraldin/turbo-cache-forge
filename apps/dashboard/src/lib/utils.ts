import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

// Standard shadcn className combinator — used by src/components/ui/*.
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}
