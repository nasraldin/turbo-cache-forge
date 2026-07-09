import type { Config } from "tailwindcss";

// Design tokens from .superpowers/sdd/design-brief.md — CSS vars defined in
// src/app/globals.css (dark default, light override + prefers-color-scheme).
// Deliberately NOT the shadcn default HSL token set: the instrument-panel
// look uses its own hit/miss/danger vocabulary, not primary/secondary.
const config: Config = {
  // Theming is CSS-var based: dark is :root default, light is [data-theme="light"] override.
  // No dark: utilities used; selector set for future compatibility.
  darkMode: ["selector", ":root:not([data-theme=\"light\"])"],
  content: ["./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        bg: "var(--bg)",
        surface: "var(--surface)",
        "surface-2": "var(--surface-2)",
        border: "var(--border)",
        text: "var(--text)",
        muted: "var(--muted)",
        hit: "var(--hit)",
        miss: "var(--miss)",
        danger: "var(--danger)",
      },
      fontFamily: {
        sans: ["var(--font-inter)", "ui-sans-serif", "system-ui", "sans-serif"],
        mono: ["var(--font-jetbrains-mono)", "ui-monospace", "SFMono-Regular", "monospace"],
      },
      borderRadius: {
        DEFAULT: "6px",
        lg: "8px",
      },
    },
  },
  plugins: [require("tailwindcss-animate")],
};

export default config;
