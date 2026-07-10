// @ts-check
import { defineConfig } from "astro/config";
import starlight from "@astrojs/starlight";

// Project Pages live under /turbo-cache-forge/. `site` + `base` must match the
// GitHub Pages URL so generated links and asset paths resolve. Override the base
// with a local build via `astro build --base /` if you ever host at a root domain.
export default defineConfig({
  site: "https://nasraldin.github.io",
  base: "/turbo-cache-forge/",
  integrations: [
    starlight({
      title: "Turbo Cache Forge",
      description:
        "Self-hosted Turborepo remote cache (Turbo API v8) — Postgres metadata, pluggable filesystem/S3 storage, a management dashboard and CLI. No cloud account.",
      logo: { src: "./src/assets/logo.svg" },
      favicon: "/favicon.svg",
      social: [
        {
          icon: "github",
          label: "GitHub",
          href: "https://github.com/nasraldin/turbo-cache-forge",
        },
      ],
      editLink: {
        baseUrl:
          "https://github.com/nasraldin/turbo-cache-forge/edit/main/apps/docs/",
      },
      sidebar: [
        {
          label: "Getting Started",
          items: [
            { label: "What is Turbo Cache Forge?", slug: "getting-started/what-is-it" },
            { label: "Quickstart", slug: "getting-started/quickstart" },
            { label: "Configuration", slug: "getting-started/configuration" },
          ],
        },
        {
          label: "Guides",
          items: [
            { label: "Connect Turborepo", slug: "guides/connect-turborepo" },
            { label: "Authentication modes", slug: "guides/authentication" },
            { label: "Storage backends", slug: "guides/storage-backends" },
            { label: "The dashboard", slug: "guides/dashboard" },
            { label: "The CLI", slug: "guides/cli" },
          ],
        },
        {
          label: "Reference",
          items: [
            { label: "Cache API (Turbo v8)", slug: "reference/cache-api" },
            { label: "Management API", slug: "reference/management-api" },
            { label: "Environment variables", slug: "reference/environment" },
            { label: "Architecture", slug: "reference/architecture" },
          ],
        },
        {
          label: "Project",
          items: [
            { label: "Contributing", slug: "project/contributing" },
            { label: "Roadmap", slug: "project/roadmap" },
          ],
        },
      ],
    }),
  ],
});
