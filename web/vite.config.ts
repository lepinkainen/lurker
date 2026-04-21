import { defineConfig } from "vite";

export default defineConfig({
  server: {
    port: 5173,
    proxy: {
      "/api": {
        target: "http://127.0.0.1:8080",
        ws: true,
      },
      "/health": {
        target: "http://127.0.0.1:8080",
      },
      "/healthz": {
        target: "http://127.0.0.1:8080",
      },
      "/whoami": {
        target: "http://127.0.0.1:8080",
      },
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
});
