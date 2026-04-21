import { defineConfig } from "vitest/config";
import { playwright } from "@vitest/browser-playwright";

const BACKEND_PORT = process.env.LURKER_TEST_PORT ?? "8099";
const BACKEND_URL = `http://127.0.0.1:${BACKEND_PORT}`;

export default defineConfig({
  server: {
    proxy: {
      "/api": { target: BACKEND_URL, ws: true, changeOrigin: true },
      "/healthz": { target: BACKEND_URL, changeOrigin: true },
      "/whoami": { target: BACKEND_URL, changeOrigin: true },
    },
  },
  test: {
    globalSetup: ["./tests/globalSetup.ts"],
    browser: {
      enabled: true,
      provider: playwright(),
      headless: true,
      instances: [{ browser: "chromium" }],
    },
    include: ["tests/**/*.test.ts"],
  },
});
