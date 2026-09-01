/// <reference types="vitest/config" />
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// During development the Go API runs on :8080 (start it with
// `ccd serve --dev`); this dev server proxies /v1 back to it so the SPA can
// be served on Vite's own port (5173) with fast refresh.
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
  server: {
    port: 5173,
    proxy: {
      "/v1": {
        target: "http://127.0.0.1:8080",
        changeOrigin: true,
      },
    },
  },
  test: {
    setupFiles: ["./src/setupTests.ts"],
  },
});
