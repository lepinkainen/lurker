import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { state } from "../src/app-state";
import {
  applyStateSnapshot,
  type ConnectionDeps,
  checkWebSocketHealth,
  createConnection,
  type StateSyncDeps,
} from "../src/connection";
import { resetAppState } from "../src/reset";

class FakeWebSocket extends EventTarget {
  static CONNECTING = 0;
  static OPEN = 1;
  static CLOSING = 2;
  static CLOSED = 3;

  readyState = FakeWebSocket.CONNECTING;
  send = vi.fn();
  close = vi.fn(() => {
    this.readyState = FakeWebSocket.CLOSED;
    this.dispatchEvent(new Event("close"));
  });

  constructor(public url: string) {
    super();
    FakeWebSocket.instances.push(this);
  }

  open() {
    this.readyState = FakeWebSocket.OPEN;
    this.dispatchEvent(new Event("open"));
  }

  receive(data: unknown) {
    this.dispatchEvent(new MessageEvent("message", { data: JSON.stringify(data) }));
  }

  sentCommands(): { type: string; since?: number }[] {
    return this.send.mock.calls.map((c) => JSON.parse(c[0] as string));
  }

  static instances: FakeWebSocket[] = [];
}

type FakeRenderer = {
  renderStatus: ReturnType<typeof vi.fn>;
  renderPromptNick: ReturnType<typeof vi.fn>;
  renderSidebar: ReturnType<typeof vi.fn>;
  renderHeader: ReturnType<typeof vi.fn>;
  renderActiveView: ReturnType<typeof vi.fn>;
  renderMembers: ReturnType<typeof vi.fn>;
  updateInputEnabled: ReturnType<typeof vi.fn>;
};
type FakeNavigation = {
  setActive: ReturnType<typeof vi.fn>;
  bufferFromHash: ReturnType<typeof vi.fn>;
};
type FakeTransport = {
  applySnapshot: ReturnType<typeof vi.fn>;
  handleMessage: ReturnType<typeof vi.fn>;
};
type Fakes = {
  domReady: () => boolean;
  renderer: FakeRenderer;
  navigation: FakeNavigation;
  transport: FakeTransport;
};

const deps = (): Fakes => {
  const renderer: FakeRenderer = {
    renderStatus: vi.fn(),
    renderPromptNick: vi.fn(),
    renderSidebar: vi.fn(),
    renderHeader: vi.fn(),
    renderActiveView: vi.fn(),
    renderMembers: vi.fn(),
    updateInputEnabled: vi.fn(),
  };
  const navigation: FakeNavigation = {
    setActive: vi.fn(),
    bufferFromHash: vi.fn(() => null),
  };
  const syncDeps = { renderer, navigation } satisfies StateSyncDeps;
  const transport: FakeTransport = {
    applySnapshot: vi.fn((s) => applyStateSnapshot(s, syncDeps)),
    handleMessage: vi.fn(),
  };
  return { domReady: () => true, renderer, navigation, transport };
};

function asConnectionDeps(f: Fakes): ConnectionDeps {
  return { domReady: f.domReady, renderer: f.renderer, transport: f.transport };
}

const SNAPSHOT = {
  type: "state",
  seq: 40,
  current_nick: "tester",
  networks: [{ id: "1", name: "Libera", status: "connected" }],
  buffers: [
    {
      id: "10",
      network_id: "1",
      name: "#go",
      kind: "channel",
      last_seen_id: "0",
      show_embeds: true,
      show_presence_events: true,
      collapse_presence_events: false,
      pinned: false,
    },
  ],
  initial_messages: { "10": [{ id: "99", buffer_id: "10", content: "missed while asleep" }] },
  members: {},
};

describe("connection recovery", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    resetAppState();
    FakeWebSocket.instances = [];
    vi.stubGlobal("WebSocket", FakeWebSocket);
    vi.stubGlobal(
      "fetch",
      vi.fn(() => Promise.resolve(new Response("{}", { status: 500 }))),
    );
  });

  afterEach(() => {
    resetAppState();
    localStorage.clear();
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  it("restores the last active buffer from localStorage over the first channel when the hash misses", () => {
    localStorage.setItem("lurker.lastActive", "20");
    const d = deps();
    d.navigation.bufferFromHash.mockReturnValue(null);

    applyStateSnapshot(
      {
        current_nick: "tester",
        networks: [{ id: "1", name: "Libera", status: "connected" }],
        buffers: [
          { id: "10", network_id: "1", name: "#go", kind: "channel", joined: true },
          { id: "20", network_id: "1", name: "#rust", kind: "channel", joined: true },
        ] as never,
        initial_messages: {},
        members: {},
      },
      { renderer: d.renderer, navigation: d.navigation },
    );

    expect(d.navigation.setActive).toHaveBeenCalledWith("20", { replaceHash: true });
  });

  it("prunes networks and buffers missing from the snapshot (deleted while offline)", () => {
    const d = deps();
    state.networks.set("0", { id: "0", name: "Gone" } as never);
    state.buffers.set("05", { id: "05", network_id: "0", name: "#gone", kind: "channel" } as never);
    state.messages.set("05", [{ id: "1", buffer_id: "05", content: "x" } as never]);
    state.members.set("05", []);
    state.activeId = "05";

    applyStateSnapshot(SNAPSHOT as never, { renderer: d.renderer, navigation: d.navigation });

    expect(state.networks.has("0")).toBe(false);
    expect(state.buffers.has("05")).toBe(false);
    expect(state.messages.has("05")).toBe(false);
    expect(state.members.has("05")).toBe(false);
    expect(state.buffers.has("10")).toBe(true);
  });

  it("sends hello with the cursor on open and installs the state frame", () => {
    const d = deps();
    createConnection(asConnectionDeps(d)).connect();
    const ws = FakeWebSocket.instances[0];
    ws.open();

    expect(ws.sentCommands()).toEqual([{ type: "hello", since: 0 }]);
    expect(fetch).not.toHaveBeenCalledWith("/api/state");

    ws.receive(SNAPSHOT);
    expect(state.lastSeq).toBe(40);
    expect(state.messages.get("10")).toEqual([{ id: "99", buffer_id: "10", content: "missed while asleep" }]);
    expect(d.renderer.renderSidebar).toHaveBeenCalled();
    expect(d.renderer.renderActiveView).toHaveBeenCalled();
    expect(d.transport.handleMessage).not.toHaveBeenCalled();
  });

  // The seq rule is the whole resync story (formal/ClientSync.tla): a frame
  // at or below the cursor is already reflected, anything newer applies.
  it("drops sequenced frames at or below the cursor and advances it on newer ones", () => {
    const d = deps();
    createConnection(asConnectionDeps(d)).connect();
    const ws = FakeWebSocket.instances[0];
    ws.open();
    ws.receive(SNAPSHOT);

    const stale = { type: "message", seq: 40, id: "98", buffer_id: "10" };
    const fresh = { type: "message", seq: 41, id: "100", buffer_id: "10" };
    const unsequenced = { type: "ping" };
    ws.receive(stale);
    ws.receive(fresh);
    ws.receive(unsequenced);
    ws.receive({ ...SNAPSHOT, seq: 39 });

    expect(d.transport.handleMessage.mock.calls.map((c) => c[0])).toEqual([fresh, unsequenced]);
    expect(d.transport.applySnapshot).toHaveBeenCalledTimes(1);
    expect(state.lastSeq).toBe(41);
  });

  it("resumes from the cursor on reconnect", () => {
    const d = deps();
    createConnection(asConnectionDeps(d)).connect();
    const ws = FakeWebSocket.instances[0];
    ws.open();
    ws.receive(SNAPSHOT);
    ws.receive({ type: "message", seq: 41, id: "100", buffer_id: "10" });

    state.loadingHistory.add("10"); // request in flight when the socket dies
    ws.close();
    expect(state.loadingHistory.has("10")).toBe(false);
    vi.advanceTimersByTime(RECONNECT_DELAY_MS);
    const next = FakeWebSocket.instances[1];
    next.open();

    expect(next.sentCommands()).toEqual([{ type: "hello", since: 41 }]);
    // Replayed events flow through the same gate.
    next.receive({ type: "message", seq: 42, id: "101", buffer_id: "10" });
    expect(d.transport.handleMessage).toHaveBeenLastCalledWith({
      type: "message",
      seq: 42,
      id: "101",
      buffer_id: "10",
    });
    expect(state.lastSeq).toBe(42);
  });

  it("closes and immediately reconnects a stale websocket that still appears open", () => {
    const d = deps();
    const connection = createConnection(asConnectionDeps(d));
    connection.connect();
    const ws = FakeWebSocket.instances[0];
    ws.open();
    state.lastWSActivityAt = Date.now() - 61_000;

    checkWebSocketHealth({
      domReady: d.domReady,
      renderer: d.renderer,
      scheduleReconnect: connection.scheduleReconnect,
    });

    expect(ws.close).toHaveBeenCalledOnce();
    expect(state.ws).toBeNull();
    expect(state.wsReady).toBe(false);
    expect(state.reconnectTimer).not.toBeNull();
    vi.advanceTimersByTime(0);
    expect(FakeWebSocket.instances).toHaveLength(2);
  });

  it("hydrate opens the websocket and syncs through hello, never through a fetch", () => {
    const d = deps();
    const connection = createConnection(asConnectionDeps(d));

    connection.hydrate();
    expect(state.reconnectAttempts).toBe(0);
    vi.advanceTimersByTime(0);
    expect(FakeWebSocket.instances).toHaveLength(1);
    const ws = FakeWebSocket.instances[0];
    ws.open();
    expect(ws.sentCommands()).toEqual([{ type: "hello", since: 0 }]);
    ws.receive(SNAPSHOT);
    expect(state.messages.get("10")).toEqual([{ id: "99", buffer_id: "10", content: "missed while asleep" }]);
    expect(fetch).not.toHaveBeenCalledWith("/api/state");
  });
});

// First reconnect after a close uses the base delay.
const RECONNECT_DELAY_MS = 1000;
