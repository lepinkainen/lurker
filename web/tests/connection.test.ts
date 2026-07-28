import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { state } from "../src/app-state";
import {
  type ConnectionDeps,
  checkWebSocketHealth,
  createConnection,
  type StateSyncDeps,
  syncStateFromServer,
} from "../src/connection";
import { resetAppState } from "../src/reset";

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
  syncState: ReturnType<typeof vi.fn<() => Promise<void>>>;
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
  const transport: FakeTransport = {
    syncState: vi.fn<() => Promise<void>>(),
    handleMessage: vi.fn(),
  };
  const fakes: Fakes = { domReady: () => true, renderer, navigation, transport };
  transport.syncState.mockImplementation(() =>
    syncStateFromServer({
      renderer,
      navigation,
    } satisfies StateSyncDeps),
  );
  return fakes;
};

function asConnectionDeps(f: Fakes): ConnectionDeps {
  return {
    domReady: f.domReady,
    renderer: f.renderer,
    transport: f.transport,
  };
}

describe("connection recovery", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    resetAppState();
    FakeWebSocket.instances = [];
    vi.stubGlobal("WebSocket", FakeWebSocket);
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL) => {
        if (String(input) === "/api/update-status") {
          return Promise.resolve(new Response("{}", { status: 500 }));
        }
        return Promise.resolve(
          Response.json({
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
          }),
        );
      }),
    );
  });

  afterEach(() => {
    resetAppState();
    localStorage.clear();
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  it("restores the last active buffer from localStorage over the first channel when the hash misses", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL) => {
        if (String(input) === "/api/state") {
          return Promise.resolve(
            Response.json({
              current_nick: "tester",
              networks: [{ id: "1", name: "Libera", status: "connected" }],
              buffers: [
                { id: "10", network_id: "1", name: "#go", kind: "channel", joined: true },
                { id: "20", network_id: "1", name: "#rust", kind: "channel", joined: true },
              ],
              initial_messages: {},
              members: {},
            }),
          );
        }
        return Promise.resolve(new Response("{}", { status: 500 }));
      }),
    );
    localStorage.setItem("lurker.lastActive", "20");
    const d = deps();
    d.navigation.bufferFromHash.mockReturnValue(null);

    await syncStateFromServer({ renderer: d.renderer, navigation: d.navigation });

    expect(d.navigation.setActive).toHaveBeenCalledWith("20", { replaceHash: true });
  });

  it("closes and immediately reconnects a stale websocket that still appears open", () => {
    const d = deps();
    const connection = createConnection(asConnectionDeps(d));
    connection.connect();
    const ws = FakeWebSocket.instances[0];
    ws.open();
    state.lastWSActivityAt = Date.now() - 120_000;

    checkWebSocketHealth({
      domReady: d.domReady,
      renderer: d.renderer,
      scheduleReconnect: connection.scheduleReconnect,
    });

    expect(ws.close).toHaveBeenCalledOnce();
    expect(state.ws).toBeNull();
    expect(state.wsReady).toBe(false);
    expect(state.needsStateSyncOnConnect).toBe(true);
    expect(state.reconnectTimer).not.toBeNull();
    vi.advanceTimersByTime(0);
    expect(FakeWebSocket.instances).toHaveLength(2);
  });

  it("opens the websocket first on hydrate and syncs from the open handler", async () => {
    const d = deps();
    state.needsStateSyncOnConnect = false;
    const connection = createConnection(asConnectionDeps(d));

    await connection.hydrate();

    // hydrate must NOT pre-fetch the snapshot before the socket subscribes.
    expect(d.transport.syncState).not.toHaveBeenCalled();
    expect(state.needsStateSyncOnConnect).toBe(true);
    expect(state.reconnectAttempts).toBe(0);

    // scheduleReconnect(0) opens the socket; the open handler then syncs.
    vi.advanceTimersByTime(0);
    expect(FakeWebSocket.instances).toHaveLength(1);
    FakeWebSocket.instances[0].open();

    await vi.waitFor(() => {
      expect(state.messages.get("10")).toEqual([{ id: "99", buffer_id: "10", content: "missed while asleep" }]);
    });
    expect(d.transport.syncState).toHaveBeenCalledOnce();
    expect(state.needsStateSyncOnConnect).toBe(false);
  });

  it("skips state refresh on first websocket open when no sync is pending", () => {
    const d = deps();
    state.needsStateSyncOnConnect = false;

    createConnection(asConnectionDeps(d)).connect();
    FakeWebSocket.instances[0].open();

    expect(d.transport.syncState).not.toHaveBeenCalled();
  });

  it("refreshes state on reconnect so missed messages are backfilled", async () => {
    const d = deps();
    state.needsStateSyncOnConnect = true;

    createConnection(asConnectionDeps(d)).connect();
    FakeWebSocket.instances[0].open();
    await vi.waitFor(() => {
      expect(state.messages.get("10")).toEqual([{ id: "99", buffer_id: "10", content: "missed while asleep" }]);
    });

    expect(fetch).toHaveBeenCalledWith("/api/state");
    expect(d.renderer.renderSidebar).toHaveBeenCalled();
    expect(d.renderer.renderActiveView).toHaveBeenCalled();
    expect(state.needsStateSyncOnConnect).toBe(false);
  });

  it("keeps reconnect sync pending when state refresh fails", async () => {
    const d = deps();
    const error = new Error("state down");
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => undefined);
    d.transport.syncState.mockRejectedValueOnce(error);
    state.needsStateSyncOnConnect = true;

    createConnection(asConnectionDeps(d)).connect();
    FakeWebSocket.instances[0].open();

    await vi.waitFor(() => {
      expect(consoleError).toHaveBeenCalledWith("reconnect state sync failed", error);
    });
    expect(state.needsStateSyncOnConnect).toBe(true);

    consoleError.mockRestore();
  });
});
