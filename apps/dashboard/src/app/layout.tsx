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

export const metadata = { title: "turbo-cache-forge", description: "Remote cache dashboard" };

export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <html lang="en" className={`${inter.variable} ${jetbrainsMono.variable}`}>
      <body>
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
