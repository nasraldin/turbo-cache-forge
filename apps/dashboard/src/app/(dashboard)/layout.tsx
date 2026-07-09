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
  LogOut,
  Menu,
  Package,
  Settings,
  Users,
  X,
} from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useEffect, useState, type ComponentType, type ReactNode } from "react";
import { useSession } from "@/app/session";
import { ThemeToggle } from "@/components/theme-toggle";
import { cn } from "@/lib/utils";

// Mirrors the backend OIDC_ORG_ENABLED. Off by default: in personal mode Clerk
// has organizations disabled, and <OrganizationSwitcher/> hard-throws when rendered.
const orgEnabled = process.env.NEXT_PUBLIC_ORG_ENABLED === "true";

type NavItem = { href: string; label: string; icon: ComponentType<{ className?: string }> };

// Grouped so the 9 destinations read as a console, not a flat list. The mono
// group labels are the same instrument-label device used across every panel.
const navGroups: { label: string; items: NavItem[] }[] = [
  {
    label: "Monitor",
    items: [
      { href: "/", label: "Overview", icon: Gauge },
      { href: "/statistics", label: "Cache Statistics", icon: BarChart3 },
      { href: "/storage", label: "Storage Usage", icon: HardDrive },
      { href: "/artifacts", label: "Artifacts", icon: Package },
    ],
  },
  {
    label: "Manage",
    items: [
      { href: "/projects", label: "Projects", icon: FolderGit2 },
      { href: "/api-keys", label: "API Keys", icon: KeyRound },
      { href: "/team", label: "Team", icon: Users },
    ],
  },
  {
    label: "Account",
    items: [
      { href: "/settings", label: "Settings", icon: Settings },
      { href: "/billing", label: "Billing", icon: CreditCard },
    ],
  },
];

const allItems = navGroups.flatMap((g) => g.items);

function isActive(href: string, pathname: string | null): boolean {
  if (!pathname) return false;
  return href === "/" ? pathname === "/" : pathname.startsWith(href);
}

// A pulsing hit-teal dot + label: the whole console reads as a live instrument.
function LivePill() {
  return (
    <span className="inline-flex h-9 items-center gap-2 rounded-full border border-border bg-surface px-3">
      <span className="relative flex h-2 w-2" aria-hidden>
        <span className="absolute inline-flex h-full w-full animate-pulse-dot rounded-full bg-hit" />
        <span className="relative inline-flex h-2 w-2 rounded-full bg-hit" />
      </span>
      <span className="eyebrow !text-muted">Live</span>
    </span>
  );
}

const REPO_URL = "https://github.com/nasraldin/turbo-cache-forge";

// GitHub mark as inline SVG (this lucide build doesn't ship the brand icon).
function GithubMark() {
  return (
    <svg viewBox="0 0 24 24" className="h-4 w-4" fill="currentColor" aria-hidden>
      <path d="M12 .5C5.37.5 0 5.78 0 12.29c0 5.2 3.44 9.61 8.21 11.17.6.11.82-.26.82-.58 0-.29-.01-1.04-.02-2.05-3.34.72-4.04-1.61-4.04-1.61-.55-1.38-1.34-1.75-1.34-1.75-1.09-.74.08-.73.08-.73 1.2.08 1.84 1.22 1.84 1.22 1.07 1.83 2.81 1.3 3.5.99.11-.77.42-1.3.76-1.6-2.67-.3-5.47-1.32-5.47-5.87 0-1.3.47-2.36 1.23-3.19-.12-.3-.53-1.52.12-3.16 0 0 1-.32 3.3 1.22.96-.26 1.98-.39 3-.4 1.02.01 2.04.14 3 .4 2.28-1.54 3.29-1.22 3.29-1.22.65 1.64.24 2.86.12 3.16.77.83 1.23 1.89 1.23 3.19 0 4.56-2.81 5.56-5.49 5.86.43.37.81 1.1.81 2.22 0 1.6-.01 2.9-.01 3.29 0 .32.22.7.83.58C20.56 21.9 24 17.49 24 12.29 24 5.78 18.63.5 12 .5z" />
    </svg>
  );
}

// GitHub repo link — the "this is open source, here's the source" affordance
// every self-hosted tool should carry.
function RepoLink() {
  return (
    <a
      href={REPO_URL}
      target="_blank"
      rel="noreferrer"
      aria-label="View source on GitHub"
      title="View source on GitHub"
      className="grid h-9 w-9 shrink-0 place-items-center rounded-md border border-border bg-surface text-muted transition-colors hover:text-text"
    >
      <GithubMark />
    </a>
  );
}

function SidebarNav({ onNavigate }: { onNavigate?: () => void }) {
  const pathname = usePathname();
  const session = useSession();
  const isOidc = session.mode === "oidc";

  return (
    <div className="flex h-full flex-col gap-6 p-4">
      <div className="flex items-center justify-between px-2">
        <Link href="/" onClick={onNavigate} className="flex items-center gap-2">
          <span className="grid h-7 w-7 place-items-center rounded-md bg-hit/12 text-hit ring-1 ring-hit/25">
            <Activity className="h-4 w-4" aria-hidden />
          </span>
          <span className="text-[15px] font-semibold tracking-tight text-text">
            Turbo Cache Forge
          </span>
        </Link>
        {onNavigate && (
          <button
            type="button"
            onClick={onNavigate}
            aria-label="Close menu"
            className="grid h-8 w-8 place-items-center rounded-md text-muted hover:bg-surface-2 hover:text-text md:hidden"
          >
            <X className="h-4 w-4" aria-hidden />
          </button>
        )}
      </div>

      {isOidc && orgEnabled && (
        <div className="px-1">
          <OrganizationSwitcher hidePersonal afterSelectOrganizationUrl="/" />
        </div>
      )}

      <nav className="flex flex-1 flex-col gap-6 overflow-y-auto">
        {navGroups.map((group) => (
          <div key={group.label} className="flex flex-col gap-1">
            <span className="eyebrow px-3 pb-1">{group.label}</span>
            {group.items.map((item) => {
              const active = isActive(item.href, pathname);
              const Icon = item.icon;
              return (
                <Link
                  key={item.href}
                  href={item.href}
                  onClick={onNavigate}
                  aria-current={active ? "page" : undefined}
                  className={cn(
                    "group relative flex min-h-11 items-center gap-3 rounded-md px-3 text-sm transition-colors md:min-h-0 md:py-2",
                    active
                      ? "bg-surface-2 font-medium text-text"
                      : "text-muted hover:bg-surface-2 hover:text-text",
                  )}
                >
                  {active && (
                    <span
                      className="absolute left-0 top-1/2 h-5 w-0.5 -translate-y-1/2 rounded-full bg-hit"
                      aria-hidden
                    />
                  )}
                  <Icon
                    className={cn(
                      "h-4 w-4 shrink-0 transition-colors",
                      active ? "text-hit" : "text-faint group-hover:text-muted",
                    )}
                    aria-hidden
                  />
                  {item.label}
                </Link>
              );
            })}
          </div>
        ))}
      </nav>

      <div className="mt-auto border-t border-border pt-4">
        {isOidc ? (
          <div className="flex items-center gap-3 px-2">
            <UserButton />
            <span className="truncate text-sm text-muted">{session.userLabel ?? "Account"}</span>
          </div>
        ) : (
          <div className="flex items-center justify-between gap-2 px-1">
            <div className="flex min-w-0 items-center gap-2">
              <span className="grid h-8 w-8 shrink-0 place-items-center rounded-full bg-surface-2 font-data text-xs font-semibold uppercase text-muted">
                {(session.userLabel ?? "r").slice(0, 1)}
              </span>
              <span className="truncate text-sm text-text">{session.userLabel ?? "root"}</span>
            </div>
            <button
              type="button"
              onClick={session.signOut}
              aria-label="Sign out"
              className="grid h-8 w-8 shrink-0 place-items-center rounded-md text-muted transition-colors hover:bg-surface-2 hover:text-danger"
            >
              <LogOut className="h-4 w-4" aria-hidden />
            </button>
          </div>
        )}
      </div>
    </div>
  );
}

export default function DashboardLayout({ children }: { children: ReactNode }) {
  const pathname = usePathname();
  const [drawerOpen, setDrawerOpen] = useState(false);

  const active = allItems.find((i) => isActive(i.href, pathname));

  // Close the drawer on route change and on Escape; lock scroll while open.
  useEffect(() => {
    setDrawerOpen(false);
  }, [pathname]);

  useEffect(() => {
    if (!drawerOpen) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setDrawerOpen(false);
    document.addEventListener("keydown", onKey);
    document.body.style.overflow = "hidden";
    return () => {
      document.removeEventListener("keydown", onKey);
      document.body.style.overflow = "";
    };
  }, [drawerOpen]);

  return (
    <div className="min-h-screen bg-bg">
      {/* Desktop rail */}
      <aside className="fixed inset-y-0 left-0 z-30 hidden w-[248px] flex-col border-r border-border bg-surface md:flex">
        <SidebarNav />
      </aside>

      {/* Mobile drawer */}
      {drawerOpen && (
        <div className="fixed inset-0 z-50 md:hidden">
          <button
            type="button"
            aria-label="Close menu"
            onClick={() => setDrawerOpen(false)}
            className="absolute inset-0 bg-black/60 backdrop-blur-sm"
          />
          <aside className="animate-rise absolute inset-y-0 left-0 flex w-[86%] max-w-[320px] flex-col border-r border-border bg-surface shadow-lg">
            <SidebarNav onNavigate={() => setDrawerOpen(false)} />
          </aside>
        </div>
      )}

      {/* Content column */}
      <div className="flex min-h-screen min-w-0 flex-col overflow-x-clip md:pl-[248px]">
        <header className="sticky top-0 z-20 border-b border-border bg-bg/85 backdrop-blur">
          <div className="mx-auto flex h-14 w-full max-w-content items-center gap-3 px-4 md:h-16 md:px-8">
            <button
              type="button"
              onClick={() => setDrawerOpen(true)}
              aria-label="Open menu"
              aria-expanded={drawerOpen}
              className="grid h-9 w-9 shrink-0 place-items-center rounded-md text-muted hover:bg-surface-2 hover:text-text md:hidden"
            >
              <Menu className="h-5 w-5" aria-hidden />
            </button>

            {/* Mobile brand */}
            <Link href="/" className="flex min-w-0 items-center gap-2 md:hidden">
              <span className="grid h-7 w-7 shrink-0 place-items-center rounded-md bg-hit/12 text-hit ring-1 ring-hit/25">
                <Activity className="h-4 w-4" aria-hidden />
              </span>
              <span className="truncate text-sm font-semibold tracking-tight text-text">
                Turbo Cache Forge
              </span>
            </Link>

            {/* Desktop section title */}
            <div className="hidden min-w-0 flex-col md:flex">
              <span className="eyebrow">Console</span>
              <span className="mt-1 truncate text-sm font-semibold tracking-tight text-text">
                {active?.label ?? "Overview"}
              </span>
            </div>

            <div className="ml-auto flex shrink-0 items-center gap-2">
              <span className="hidden sm:inline-flex">
                <LivePill />
              </span>
              <RepoLink />
              <ThemeToggle />
            </div>
          </div>
        </header>

        <main className="mx-auto w-full max-w-content flex-1 p-4 md:p-8">{children}</main>
      </div>
    </div>
  );
}
