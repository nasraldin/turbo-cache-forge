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
        elevated: "var(--elevated)",
        border: "var(--border)",
        "border-strong": "var(--border-strong)",
        text: "var(--text)",
        muted: "var(--muted)",
        faint: "var(--faint)",
        hit: "var(--hit)",
        miss: "var(--miss)",
        danger: "var(--danger)",
      },
      fontFamily: {
        sans: ["var(--font-inter)", "ui-sans-serif", "system-ui", "sans-serif"],
        mono: ["var(--font-jetbrains-mono)", "ui-monospace", "SFMono-Regular", "monospace"],
      },
      borderRadius: {
        DEFAULT: "8px",
        md: "10px",
        lg: "14px",
        xl: "18px",
        "2xl": "22px",
      },
      boxShadow: {
        sm: "var(--shadow-sm)",
        md: "var(--shadow-md)",
        lg: "var(--shadow-lg)",
      },
      maxWidth: {
        content: "1180px",
      },
    },
  },
  plugins: [require("tailwindcss-animate")],
};

export default config;
