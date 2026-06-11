import { playwright } from "@vitest/browser-playwright";
import { defineConfig } from "vitest/config";

const BACKEND_PORT = process.env.LURKER_TEST_PORT ?? "8099";
const BACKEND_URL = `http://127.0.0.1:${BACKEND_PORT}`;

// Unit-tier config. Backend is NOT started; tests that need a real backend live
// in `tests/**/*.integration.test.ts` and run via vitest.integration.config.ts.
export default defineConfig({
  server: {
    proxy: {
      "/api": { target: BACKEND_URL, ws: true, changeOrigin: true },
      "/healthz": { target: BACKEND_URL, changeOrigin: true },
      "/whoami": { target: BACKEND_URL, changeOrigin: true },
    },
  },
  test: {
    browser: {
      enabled: true,
      provider: playwright(),
      headless: true,
      instances: [{ browser: "chromium" }],
    },
    setupFiles: ["tests/setup.ts"],
    include: ["tests/**/*.test.ts"],
    exclude: ["tests/**/*.integration.test.ts", "node_modules/**", "dist/**"],
    coverage: {
      provider: "v8",
      reporter: ["text", "json-summary", "html"],
      include: ["src/**/*.ts"],
      exclude: ["src/**/*.d.ts"],
    },
  },
});
