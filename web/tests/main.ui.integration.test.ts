// Integration / e2e-tier test: boots the full app (main.ts entrypoint), mounts
// the real index fixture DOM, and drives behavior end-to-end via stubbed
// fetch/WebSocket. Heavier than the unit suite. Each `it` targets one
// observable behavior; shared setup is in beforeEach.
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { state } from "../src/app-state";
import { __handleWSMessage, __initForTests, __resetForTests } from "../src/main";
import { fixtureIndexHTML } from "./fixture-index";

const LEADING_HASH_RE = /^#/u;

// ---------------------------------------------------------------------------
// Fake WebSocket (same pattern as connection.test.ts)
// ---------------------------------------------------------------------------
class FakeWebSocket extends EventTarget {
  static CONNECTING = 0;
  static OPEN = 1;
  static CLOSING = 2;
  static CLOSED = 3;

  readyState = FakeWebSocket.CONNECTING;
  close = vi.fn(() => {
    this.readyState = FakeWebSocket.CLOSED;
    this.dispatchEvent(new Event("close"));
  });
  send = vi.fn();

  constructor(public url: string) {
    super();
    FakeWebSocket.instances.push(this);
    // Simulate open asynchronously so connection setup finishes first
    queueMicrotask(() => {
      this.readyState = FakeWebSocket.OPEN;
      this.dispatchEvent(new Event("open"));
    });
  }

  static instances: FakeWebSocket[] = [];
  static reset() {
    FakeWebSocket.instances = [];
  }
}

// ---------------------------------------------------------------------------
// Fixture data — 2 networks, 4 channel buffers spanning both
// ---------------------------------------------------------------------------
const FIXTURE = {
  current_nick: "tester",
  networks: [
    { id: "net1", name: "libera", status: "connected" },
    { id: "net2", name: "oftc", status: "connected" },
  ],
  buffers: [
    {
      id: "buf1",
      network_id: "net1",
      name: "#lurker",
      kind: "channel",
      joined: true,
      last_seen_id: "0",
      unread: 0,
      mentions: 0,
      show_embeds: true,
      show_presence_events: true,
      collapse_presence_events: false,
      pinned: false,
    },
    {
      id: "buf2",
      network_id: "net1",
      name: "#go",
      kind: "channel",
      joined: true,
      last_seen_id: "0",
      unread: 0,
      mentions: 0,
      show_embeds: true,
      show_presence_events: true,
      collapse_presence_events: false,
      pinned: false,
    },
    {
      id: "buf3",
      network_id: "net2",
      name: "#debian",
      kind: "channel",
      joined: true,
      last_seen_id: "0",
      unread: 0,
      mentions: 0,
      show_embeds: true,
      show_presence_events: true,
      collapse_presence_events: false,
      pinned: false,
    },
    {
      id: "buf4",
      network_id: "net2",
      name: "#ubuntu",
      kind: "channel",
      joined: true,
      last_seen_id: "0",
      unread: 0,
      mentions: 0,
      show_embeds: true,
      show_presence_events: true,
      collapse_presence_events: false,
      pinned: false,
    },
  ],
  initial_messages: {} as Record<string, unknown[]>,
  members: {} as Record<string, unknown[]>,
};

function makeFetchMock() {
  return vi.fn((input: RequestInfo | URL) => {
    const url = String(input);
    if (url === "/api/state") {
      return Promise.resolve(Response.json(FIXTURE));
    }
    // update-status and tailscale-status — return 500 to keep them null
    return Promise.resolve(new Response("{}", { status: 500 }));
  });
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------
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

// ---------------------------------------------------------------------------
// Test suite
// ---------------------------------------------------------------------------
describe("main UI", () => {
  beforeEach(() => {
    installFixture();
    FakeWebSocket.reset();
    vi.stubGlobal("fetch", makeFetchMock());
    vi.stubGlobal("WebSocket", FakeWebSocket);
  });

  afterEach(() => {
    __resetForTests();
    vi.unstubAllGlobals();
  });

  it("renders network names in the sidebar from /api/state", async () => {
    await __initForTests();

    const names = await waitFor(() => {
      const nodes = document.querySelectorAll("#sb-scroll .netname");
      if (nodes.length < 2) return null;
      return [...nodes].map((n) => n.textContent);
    });
    expect(names).toEqual(expect.arrayContaining(["libera", "oftc"]));
  });

  it("activates an initial buffer header on first hydrate", async () => {
    await __initForTests();
    await waitFor(() => document.querySelector("#sb-scroll .netname"));

    const bufName = document.getElementById("buffer-name")?.textContent ?? "";
    expect(bufName.length).toBeGreaterThan(0);
    expect(bufName).not.toBe("—");
  });

  it("increments unread/mention counts for non-active buffer", async () => {
    await __initForTests();

    // pick a non-active buffer
    const target = await waitFor(() => {
      const rows = document.querySelectorAll<HTMLButtonElement>("#sb-scroll .sbrow.chan:not(.active):not(.archives)");
      return rows.length > 0 ? rows[0] : null;
    });
    const name = target.querySelector(".name")?.textContent;
    if (!name) throw new Error("missing buffer name");

    // look up buffer id directly from state instead of re-fetching
    const buf = [...state.buffers.values()].find((b) => b.name === name);
    if (!buf) throw new Error(`missing buffer for name=${name}`);

    const nick = state.me.nick;

    __handleWSMessage({
      type: "message",
      id: "10000001",
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
      return rows.length > 0 ? rows[0] : null;
    });
    const name = target.querySelector(".name")?.textContent;
    if (!name) throw new Error("missing buffer name");

    target.click();

    const active = await waitFor(() => {
      const row = document.querySelector<HTMLButtonElement>("#sb-scroll .sbrow.chan.active");
      return row && row.querySelector(".name")?.textContent === name ? row : null;
    });
    expect(active).toBeTruthy();
    const headerName = document.getElementById("buffer-name")?.textContent ?? "";
    expect(headerName.replace(LEADING_HASH_RE, "")).toBe(name.replace(LEADING_HASH_RE, ""));
  });

  it("shows slash-command popup with matching items when slash typed", async () => {
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
  });

  it("hides slash-command popup when input is cleared", async () => {
    await __initForTests();
    await waitFor(() => document.querySelector("#sb-scroll .sbrow.chan"));

    const input = document.getElementById("input") as HTMLInputElement;
    const pop = document.getElementById("cmd-pop") as HTMLElement;

    input.value = "/j";
    input.dispatchEvent(new Event("input"));
    await waitFor(() => (!pop.hidden ? pop : null));

    input.value = "";
    input.dispatchEvent(new Event("input"));
    await waitFor(() => (pop.hidden ? pop : null));
    expect(pop.hidden).toBe(true);
  });

  it("opens shortcuts help with ?", async () => {
    await __initForTests();

    document.dispatchEvent(new KeyboardEvent("keydown", { key: "?", bubbles: true }));

    const dialog = await waitFor(() =>
      document.querySelector<HTMLDialogElement>("dialog[data-overlay='keyboard-help'][open]"),
    );
    expect(dialog.textContent).toContain("Keyboard shortcuts");
    expect(dialog.textContent).toContain("Open channel switcher");
  });

  it("opens shortcuts help from header button", async () => {
    await __initForTests();

    const btn = document.getElementById("shortcuts-help-btn") as HTMLButtonElement | null;
    expect(btn).not.toBeNull();
    if (!btn) throw new Error("missing shortcuts help button");
    btn.click();

    const dialog = await waitFor(() =>
      document.querySelector<HTMLDialogElement>("dialog[data-overlay='keyboard-help'][open]"),
    );
    expect(dialog.textContent).toContain("Keyboard shortcuts");
  });

  it("opens channel switcher and jumps to selected buffer", async () => {
    await __initForTests();

    const target = await waitFor(() => {
      const rows = document.querySelectorAll<HTMLButtonElement>("#sb-scroll .sbrow.chan:not(.active):not(.archives)");
      return rows.length > 0 ? rows[0] : null;
    });
    const targetName = target.querySelector(".name")?.textContent;
    if (!targetName) throw new Error("missing target name");

    document.dispatchEvent(new KeyboardEvent("keydown", { key: "k", ctrlKey: true, bubbles: true }));
    const channelSwitcherSelector = `dialog[data-overlay='${"channel-switcher"}'][open]`;
    const dialog = await waitFor(() => document.querySelector<HTMLDialogElement>(channelSwitcherSelector));
    const switcherInput = dialog.querySelector<HTMLInputElement>(".ks-input");
    if (!switcherInput) throw new Error("missing switcher input");

    switcherInput.value = targetName;
    switcherInput.dispatchEvent(new Event("input", { bubbles: true }));
    switcherInput.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true }));

    const active = await waitFor(() => {
      const row = document.querySelector<HTMLButtonElement>("#sb-scroll .sbrow.chan.active");
      return row?.querySelector(".name")?.textContent === targetName ? row : null;
    });
    expect(active).toBeTruthy();
    expect(document.querySelector(channelSwitcherSelector)).toBeNull();
  });

  it("navigates visible buffers with alt+arrowdown", async () => {
    await __initForTests();

    const before = await waitFor(() =>
      document.querySelector<HTMLButtonElement>("#sb-scroll .sbrow.chan.active, #sb-scroll .net-hdr.active"),
    );
    const beforeLabel = before.querySelector(".name, .netname")?.textContent ?? "";

    document.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowDown", altKey: true, bubbles: true }));

    const after = await waitFor(() => {
      const node = document.querySelector<HTMLButtonElement>(
        "#sb-scroll .sbrow.chan.active, #sb-scroll .net-hdr.active",
      );
      const label = node?.querySelector(".name, .netname")?.textContent ?? "";
      return label && label !== beforeLabel ? node : null;
    });
    expect(after).toBeTruthy();
  });

  it("groups consecutive messages from the same author in the open channel", async () => {
    await __initForTests();

    // open a channel buffer so the message pane is active
    const row = await waitFor(() => {
      const rows = document.querySelectorAll<HTMLButtonElement>("#sb-scroll .sbrow.chan:not(.archives)");
      return rows.length > 0 ? rows[0] : null;
    });
    row.click();
    const name = await waitFor(() => {
      const active = document.querySelector<HTMLButtonElement>("#sb-scroll .sbrow.chan.active");
      return active?.querySelector(".name")?.textContent || null;
    });

    // Look up buffer directly from state
    const buf = [...state.buffers.values()].find((b) => b.name === name && b.kind === "channel");
    if (!buf) throw new Error(`missing channel buffer for name=${name}`);

    const base = Date.now();
    const send = (id: string, sender: string, offsetMs: number) =>
      __handleWSMessage({
        type: "message",
        id,
        buffer_id: buf.id,
        sender,
        content: `grouping probe ${id}`,
        kind: "message",
        ts: new Date(base + offsetMs).toISOString(),
      });

    // three from one author within the window, then one from another author
    send("90000001", "groupie", 0);
    send("90000002", "groupie", 1000);
    send("90000003", "groupie", 2000);
    send("90000004", "loner", 3000);

    const last = await waitFor(() => document.querySelector<HTMLElement>('#messages [data-id="90000004"]'));
    const cls = (id: string) => document.querySelector<HTMLElement>(`#messages [data-id="${id}"]`)?.classList;
    expect(cls("90000001")?.contains("cont")).toBe(false); // first of the run keeps its nick
    expect(cls("90000002")?.contains("cont")).toBe(true);
    expect(cls("90000003")?.contains("cont")).toBe(true);
    expect(last.classList.contains("cont")).toBe(false); // different author starts a new group

    // the continuation row's nick is blanked by CSS but remains in the DOM
    const contNick = document.querySelector<HTMLElement>('#messages [data-id="90000002"] .nick');
    expect(contNick).not.toBeNull();
    expect(getComputedStyle(contNick as HTMLElement).visibility).toBe("hidden");
  });

  it("jumps to the first unread buffer with alt+a", async () => {
    await __initForTests();

    const activeName = document.getElementById("buffer-name")?.textContent ?? "";
    const rows = document.querySelectorAll<HTMLButtonElement>("#sb-scroll .sbrow.chan:not(.archives)");
    const target = [...rows].find((row) => {
      const name = row.querySelector(".name")?.textContent ?? "";
      return name && name !== activeName;
    });
    expect(target).toBeTruthy();
    if (!target) throw new Error("missing unread target row");

    const targetName = target.querySelector(".name")?.textContent ?? "";
    // Look up buffer from state directly
    const buf = [...state.buffers.values()].find((b) => b.name === targetName);
    expect(buf).toBeTruthy();
    if (!buf) throw new Error("missing unread target buffer");

    __handleWSMessage({
      type: "message",
      id: "10000002",
      buffer_id: buf.id,
      sender: "someone",
      content: "plain unread message",
      kind: "message",
      ts: new Date().toISOString(),
    });

    document.dispatchEvent(new KeyboardEvent("keydown", { key: "a", altKey: true, bubbles: true }));

    const active = await waitFor(() => {
      const row = document.querySelector<HTMLButtonElement>("#sb-scroll .sbrow.chan.active");
      return row?.querySelector(".name")?.textContent === targetName ? row : null;
    });
    expect(active).toBeTruthy();
  });
});
