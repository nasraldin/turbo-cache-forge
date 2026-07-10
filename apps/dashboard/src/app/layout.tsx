import { JetBrains_Mono, Inter } from "next/font/google";
import type { ReactNode } from "react";
import { Toaster } from "@/components/ui/sonner";
import { QueryProvider } from "./providers";
import { AuthRoot } from "./session";
import "./globals.css";

// Design brief: Inter for UI/body, JetBrains Mono (tabular-nums) for all
// machine values — rates, byte counts, hashes, tokens.
const inter = Inter({ subsets: ["latin"], variable: "--font-inter", display: "swap" });
const jetbrainsMono = JetBrains_Mono({
  subsets: ["latin"],
  variable: "--font-jetbrains-mono",
  display: "swap",
});

export const metadata = {
  title: "Turbo Cache Forge",
  description: "Self-hosted Turborepo remote cache — dashboard",
};

// Sets the theme before first paint (no flash): an explicit choice wins, else
// the OS preference. ThemeToggle keeps this attribute + localStorage in sync.
const themeScript = `(function(){try{var t=localStorage.getItem('tcf.theme');if(t!=='light'&&t!=='dark'){t=window.matchMedia('(prefers-color-scheme: dark)').matches?'dark':'light';}document.documentElement.setAttribute('data-theme',t);}catch(e){}})();`;

export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <html
      lang="en"
      className={`${inter.variable} ${jetbrainsMono.variable}`}
      suppressHydrationWarning
    >
      <body>
        <script dangerouslySetInnerHTML={{ __html: themeScript }} />
        <AuthRoot>
          <QueryProvider>
            {children}
            <Toaster />
          </QueryProvider>
        </AuthRoot>
      </body>
    </html>
  );
}
