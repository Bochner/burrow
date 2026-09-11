// Adapted from Hovel c461ba; see UPSTREAM.md. Apache-2.0.
import { defineConfig } from "astro/config";

export default defineConfig({
  site: "https://bochner.github.io",
  base: "/burrow",
  output: "static",
  trailingSlash: "ignore",
  compressHTML: false,
  build: { format: "preserve" },
  outDir: "./dist",
  cacheDir: "./dist/.astro-cache",
  vite: { build: { sourcemap: false } },
});
