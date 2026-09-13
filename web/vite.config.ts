/// <reference types="vitest/config" />
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// In development the Go API runs on :8080 (`ccd serve --dev`); this dev
// server proxies /v1 back to it so the SPA can run on Vite's port with fast
// refresh and still talk to a real backend. There is no mock layer on
// purpose — a UI developed against fabricated responses is a UI that has
// never met a 409.
export default defineConfig({
  plugins: [react()],
  build: { outDir: "dist", emptyOutDir: true },
  server: {
    port: 5173,
    proxy: { "/v1": { target: "http://127.0.0.1:8080", changeOrigin: true } },
  },
  test: {
    environment: "happy-dom",
    setupFiles: ["./src/setupTests.ts"],
  },
});
