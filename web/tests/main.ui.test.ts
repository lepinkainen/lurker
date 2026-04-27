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

    // pick a non-active buffer (query or channel) — queries are always visible without opening archive fold
    const target = await waitFor(() => {
      const rows = document.querySelectorAll<HTMLButtonElement>("#sb-scroll .sbrow.chan:not(.active):not(.archives)");
      return rows.length ? rows[0] : null;
    });
    const name = target.querySelector(".name")?.textContent || "";
    expect(name).toBeTruthy();

    // find buffer id from state via /api/state
    const stateRes = await (await fetch("/api/state")).json();
    const buf = stateRes.buffers.find((b: { name: string }) => b.name === name);
    expect(buf).toBeTruthy();

    const nick = stateRes.current_nick || stateRes.nick || stateRes.user?.nick || stateRes.networks?.[0]?.nick || "you";

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

  it("switches active buffer when sidebar row clicked", async () => {
    await __initForTests();

    const target = await waitFor(() => {
      const rows = document.querySelectorAll<HTMLButtonElement>("#sb-scroll .sbrow.chan:not(.active):not(.archives)");
      return rows.length ? rows[0] : null;
    });
    const name = target.querySelector(".name")?.textContent || "";
    expect(name).toBeTruthy();

    target.click();

    const active = await waitFor(() => {
      const row = document.querySelector<HTMLButtonElement>("#sb-scroll .sbrow.chan.active");
      return row && row.querySelector(".name")?.textContent === name ? row : null;
    });
    expect(active).toBeTruthy();
    const headerName = document.getElementById("buffer-name")?.textContent || "";
    expect(headerName.replace(/^#/, "")).toBe(name.replace(/^#/, ""));
  });

  it("shows slash-command popup on input and hides on clear", async () => {
    await __initForTests();
    await waitFor(() => document.querySelector("#sb-scroll .sbrow.chan"));

    const input = document.getElementById("input") as HTMLInputElement;
    const pop = document.getElementById("cmd-pop") as HTMLElement;
    expect(pop.hidden).toBe(true);

    input.value = "/j";
    input.dispatchEvent(new Event("input"));
    await waitFor(() => (!pop.hidden ? pop : null));
    expect(pop.querySelectorAll(".ci").length).toBeGreaterThan(0);
    expect(pop.querySelector(".ci.hl .c")?.textContent).toContain("/join");

    input.value = "";
    input.dispatchEvent(new Event("input"));
    await waitFor(() => (pop.hidden ? pop : null));
    expect(pop.hidden).toBe(true);
  });
});
