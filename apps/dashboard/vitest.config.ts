import react from "@vitejs/plugin-react";
import { configDefaults, defineConfig } from "vitest/config";

export default defineConfig({
  plugins: [react()],
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./vitest.setup.ts"],
    // e2e specs are Playwright's (src/e2e/*.spec.ts) — keep Vitest out of them.
    exclude: [...configDefaults.exclude, "src/e2e/**"],
  },
  resolve: { alias: { "@": new URL("./src", import.meta.url).pathname } },
});
