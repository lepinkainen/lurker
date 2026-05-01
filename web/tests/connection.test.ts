import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { state } from "../src/app-state";
import { checkWebSocketHealth, connectWS } from "../src/connection";
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

const deps = () => ({
  domReady: () => true,
  renderSidebarStatus: vi.fn(),
  renderPromptNick: vi.fn(),
  renderSidebar: vi.fn(),
  renderHeader: vi.fn(),
  renderActiveView: vi.fn(),
  renderMembers: vi.fn(),
  updateInputEnabled: vi.fn(),
  maybeMarkActiveRead: vi.fn(),
  inferUnreadCounts: vi.fn(),
  scheduleReconnect: vi.fn(),
  nextReconnectDelay: vi.fn(() => 1000),
  setActive: vi.fn(),
  bufferFromHash: vi.fn(() => null),
  handleWSMessage: vi.fn(),
});

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
            networks: [{ id: 1, name: "Libera", status: "connected" }],
            buffers: [
              {
                id: 10,
                network_id: 1,
                name: "#go",
                kind: "channel",
                last_seen_id: 0,
                show_embeds: true,
                show_presence_events: true,
                collapse_presence_events: false,
                pinned: false,
              },
            ],
            initial_messages: { "10": [{ id: 99, buffer_id: 10, content: "missed while asleep" }] },
            members: {},
          }),
        );
      }),
    );
  });

  afterEach(() => {
    resetAppState();
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  it("closes and immediately reconnects a stale websocket that still appears open", () => {
    const d = deps();
    connectWS(d);
    const ws = FakeWebSocket.instances[0];
    ws.open();
    state.lastWSActivityAt = Date.now() - 120_000;

    checkWebSocketHealth({
      domReady: d.domReady,
      renderSidebarStatus: d.renderSidebarStatus,
      updateInputEnabled: d.updateInputEnabled,
      renderSidebar: d.renderSidebar,
      scheduleReconnect: d.scheduleReconnect,
    });

    expect(ws.close).toHaveBeenCalledOnce();
    expect(state.ws).toBeNull();
    expect(state.wsReady).toBe(false);
    expect(state.needsStateSyncOnConnect).toBe(true);
    expect(d.scheduleReconnect).toHaveBeenCalledWith(0);
  });

  it("refreshes state on reconnect so missed messages are backfilled", async () => {
    const d = deps();
    state.needsStateSyncOnConnect = true;

    connectWS(d);
    FakeWebSocket.instances[0].open();
    await vi.waitFor(() => {
      expect(state.messages.get(10)).toEqual([{ id: 99, buffer_id: 10, content: "missed while asleep" }]);
    });

    expect(fetch).toHaveBeenCalledWith("/api/state");
    expect(d.inferUnreadCounts).toHaveBeenCalled();
    expect(d.renderSidebar).toHaveBeenCalled();
    expect(d.renderActiveView).toHaveBeenCalled();
    expect(state.needsStateSyncOnConnect).toBe(false);
  });
});
