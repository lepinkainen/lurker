import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { __handleWSMessage, __initForTests, __resetForTests } from "../src/main";
import { fixtureIndexHTML } from "./fixture-index";

function installFixture() {
  const parsed = new DOMParser().parseFromString(fixtureIndexHTML, "text/html");
  document.body.innerHTML = parsed.body.innerHTML;
}

async function waitFor<T>(fn: () => T | null | undefined, timeoutMs = 3000): Promise<T> {
  const deadline = Date.now() + timeoutMs;
  let last: T | null | undefined;
  while (Date.now() < deadline) {
    last = fn();
    if (last) return last;
    await new Promise((r) => setTimeout(r, 25));
  }
  throw new Error(`waitFor timeout; last=${String(last)}`);
}

describe("main UI", () => {
  beforeEach(() => {
    installFixture();
  });

  afterEach(() => {
    __resetForTests();
  });

  it("renders networks and active channel from /api/state", async () => {
    await __initForTests();

    const names = await waitFor(() => {
      const nodes = document.querySelectorAll("#sb-scroll .netname");
      if (nodes.length < 2) return null;
      return [...nodes].map((n) => n.textContent);
    });
    expect(names).toEqual(expect.arrayContaining(["libera", "oftc"]));

    const bufName = document.getElementById("buffer-name")?.textContent || "";
    expect(bufName.length).toBeGreaterThan(0);
    expect(bufName).not.toBe("—");
  });

  it("increments unread/mention counts for non-active buffer", async () => {
    await __initForTests();

    // pick a non-active channel buffer
    const target = await waitFor(() => {
      const rows = document.querySelectorAll<HTMLButtonElement>(
        "#sb-scroll .sbrow.chan.channel:not(.active)",
      );
      return rows.length ? rows[0] : null;
    });
    const name = target.querySelector(".name")?.textContent || "";
    expect(name).toBeTruthy();

    // find buffer id from state via /api/state
    const stateRes = await (await fetch("/api/state")).json();
    const buf = stateRes.buffers.find((b: { name: string }) => b.name === name);
    expect(buf).toBeTruthy();

    const nick = stateRes.current_nick || stateRes.nick || "you";

    __handleWSMessage({
      type: "message",
      id: 10_000_001,
      buffer_id: buf.id,
      sender: "someone",
      content: `hey ${nick} look at this`,
      kind: "message",
      ts: new Date().toISOString(),
    });

    const row = await waitFor(() => {
      const rows = document.querySelectorAll<HTMLButtonElement>("#sb-scroll .sbrow.chan");
      for (const r of rows) {
        if (r.querySelector(".name")?.textContent === name && r.classList.contains("mention")) return r;
      }
      return null;
    });
    expect(row.classList.contains("unread")).toBe(true);
    expect(row.querySelector(".mentionbadge")?.textContent).toBe("1");
  });
});
