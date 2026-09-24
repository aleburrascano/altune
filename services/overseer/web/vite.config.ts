/// <reference types="vitest/config" />
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// Overseer mounts under /overseer/ behind Caddy (which strips the prefix inbound),
// so assets and the app base must resolve there. The build writes into the Go
// service's embed directory (internal/webui/dist) so `go:embed` folds the SPA into
// the single binary.
export default defineConfig({
  base: "/overseer/",
  plugins: [react(), tailwindcss()],
  build: {
    outDir: "../internal/webui/dist",
    // Do NOT empty the embed dir on build. It is outside the Vite root and holds a
    // committed .gitkeep sentinel that go:embed needs (so the Go-only CI jobs
    // build without Node). Emptying would delete the sentinel, and the generated
    // index.html/assets are gitignored, so a build leaves git clean — no stale
    // placeholder to check out. Docker/CI build into a fresh dir, so there is no
    // stale-asset accumulation there; only long-lived local dirs may retain old
    // hashed chunks, which are harmless and gitignored.
    emptyOutDir: false,
  },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./src/test/setup.ts"],
  },
});
