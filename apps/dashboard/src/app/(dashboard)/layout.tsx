"use client";
import { OrganizationSwitcher, UserButton } from "@clerk/nextjs";
import {
  Activity,
  BarChart3,
  CreditCard,
  FolderGit2,
  Gauge,
  HardDrive,
  KeyRound,
  Package,
  Settings,
  Users,
} from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import type { ComponentType, ReactNode } from "react";
import { cn } from "@/lib/utils";

// Mirrors the backend OIDC_ORG_ENABLED. Off by default: in personal mode Clerk
// has organizations disabled, and <OrganizationSwitcher/> hard-throws when rendered.
const orgEnabled = process.env.NEXT_PUBLIC_ORG_ENABLED === "true";

const nav: { href: string; label: string; icon: ComponentType<{ className?: string }> }[] = [
  { href: "/", label: "Overview", icon: Gauge },
  { href: "/projects", label: "Projects", icon: FolderGit2 },
  { href: "/statistics", label: "Cache Statistics", icon: BarChart3 },
  { href: "/artifacts", label: "Artifacts", icon: Package },
  { href: "/api-keys", label: "API Keys", icon: KeyRound },
  { href: "/team", label: "Team", icon: Users },
  { href: "/storage", label: "Storage Usage", icon: HardDrive },
  { href: "/settings", label: "Settings", icon: Settings },
  { href: "/billing", label: "Billing", icon: CreditCard },
];

export default function DashboardLayout({ children }: { children: ReactNode }) {
  const pathname = usePathname();

  return (
    <div className="grid min-h-screen grid-rows-[auto_1fr] md:grid-cols-[240px_1fr] md:grid-rows-1">
      <aside className="flex flex-col gap-6 border-b border-border bg-surface p-4 md:border-b-0 md:border-r">
        <Link href="/" className="flex items-center gap-2 px-2">
          <Activity className="h-5 w-5 text-hit" aria-hidden />
          <span className="text-lg font-semibold tracking-tight text-text">turbo-cache-forge</span>
        </Link>

        {orgEnabled && <OrganizationSwitcher hidePersonal afterSelectOrganizationUrl="/" />}

        <nav className="flex gap-1 overflow-x-auto md:flex-col md:overflow-visible">
          {nav.map((item) => {
            const active = item.href === "/" ? pathname === "/" : pathname?.startsWith(item.href);
            const Icon = item.icon;
            return (
              <Link
                key={item.href}
                href={item.href}
                aria-current={active ? "page" : undefined}
                className={cn(
                  "flex shrink-0 items-center gap-3 rounded-md px-3 py-2 text-sm transition-colors",
                  active
                    ? "bg-surface-2 text-text"
                    : "text-muted hover:bg-surface-2 hover:text-text",
                )}
              >
                <Icon className={cn("h-4 w-4", active && "text-hit")} aria-hidden />
                {item.label}
              </Link>
            );
          })}
        </nav>

        <div className="mt-auto flex items-center gap-2 px-2">
          <UserButton />
        </div>
      </aside>

      <main className="mx-auto w-full max-w-[1200px] p-8">{children}</main>
    </div>
  );
}
