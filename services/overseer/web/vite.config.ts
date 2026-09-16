/// <reference types="vitest/config" />
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Overseer mounts under /overseer/ behind Caddy (which strips the prefix inbound),
// so assets and the app base must resolve there. The build writes into the Go
// service's embed directory (internal/webui/dist) so `go:embed` folds the SPA into
// the single binary.
export default defineConfig({
  base: "/overseer/",
  plugins: [react()],
  build: {
    outDir: "../internal/webui/dist",
    emptyOutDir: true,
  },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./src/test/setup.ts"],
  },
});
