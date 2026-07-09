import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./src/e2e",
  use: { baseURL: process.env.E2E_BASE_URL ?? "http://localhost:3000" },
  // Only auto-start a server when we're not pointed at an external one.
  webServer: process.env.E2E_BASE_URL
    ? undefined
    : {
        command: "pnpm start",
        url: "http://localhost:3000",
        reuseExistingServer: !process.env.CI,
      },
});
