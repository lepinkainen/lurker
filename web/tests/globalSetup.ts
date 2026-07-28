import { type ChildProcess, spawn, spawnSync } from "node:child_process";
import { mkdirSync } from "node:fs";
import { resolve } from "node:path";
import process from "node:process";

const REPO_ROOT = resolve(import.meta.dirname, "..", "..");
const BACKEND_PORT = process.env.LURKER_TEST_PORT ?? "8099";
const DATA_DIR = resolve(REPO_ROOT, "data-test");
const BINARY = resolve(REPO_ROOT, "build", "lurker-test");
const HEALTH_URL = `http://127.0.0.1:${BACKEND_PORT}/healthz`;

let backend: ChildProcess | undefined;

export async function setup() {
  if (await isHealthy()) {
    console.log(`[globalSetup] backend already running at ${HEALTH_URL}, reusing it`);
    return;
  }
  seed();
  build();
  backend = startBackend();
  await waitForHealth();
}

export async function teardown() {
  if (!backend || backend.killed) return;
  const proc = backend;
  backend = undefined;
  await new Promise<void>((done) => {
    proc.once("exit", () => done());
    proc.kill("SIGTERM");
    setTimeout(() => {
      if (!proc.killed) proc.kill("SIGKILL");
    }, 5000);
  });
}

function seed() {
  const r = spawnSync("go", ["run", "./cmd/seedtest", "--data-dir", DATA_DIR, "--reset"], {
    cwd: REPO_ROOT,
    stdio: "inherit",
  });
  if (r.status !== 0) throw new Error(`seed failed: exit ${r.status}`);
}

function build() {
  mkdirSync(resolve(REPO_ROOT, "build"), { recursive: true });
  const r = spawnSync("go", ["build", "-o", BINARY, "."], {
    cwd: REPO_ROOT,
    stdio: "inherit",
  });
  if (r.status !== 0) throw new Error(`build failed: exit ${r.status}`);
}

function startBackend() {
  const proc = spawn(BINARY, [], {
    cwd: REPO_ROOT,
    env: {
      ...process.env,
      DATA_DIR,
      CONFIG_PATH: resolve(DATA_DIR, "config.yaml"),
      ADDR: `:${BACKEND_PORT}`,
      LURKER_TEST_FIXTURE_RUNTIME: "1",
    },
    stdio: ["ignore", "inherit", "inherit"],
  });
  proc.once("exit", (code, signal) => {
    if (backend && code !== 0 && signal !== "SIGTERM") {
      console.error(`backend exited early: code=${code} signal=${signal}`);
    }
  });
  return proc;
}

async function isHealthy(): Promise<boolean> {
  try {
    const r = await fetch(HEALTH_URL);
    return r.ok;
  } catch {
    return false;
  }
}

async function waitForHealth() {
  const deadline = Date.now() + 15_000;
  while (Date.now() < deadline) {
    try {
      const r = await fetch(HEALTH_URL);
      if (r.ok) return;
    } catch {
      // not ready yet
    }
    await new Promise((r) => setTimeout(r, 200));
  }
  throw new Error(`backend at ${HEALTH_URL} did not become healthy`);
}
