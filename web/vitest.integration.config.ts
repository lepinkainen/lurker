import { playwright } from "@vitest/browser-playwright";
import { defineConfig } from "vitest/config";

const BACKEND_PORT = process.env.LURKER_TEST_PORT ?? "8099";
const BACKEND_URL = `http://127.0.0.1:${BACKEND_PORT}`;

// Integration-tier config. Starts the backend (via globalSetup) and runs tests
// matching `tests/**/*.integration.test.ts`. Requires the seedtest fixture; see
// individual test file headers for seed dependencies.
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
    include: ["tests/**/*.integration.test.ts"],
  },
});
