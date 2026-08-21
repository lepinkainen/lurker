import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { state } from "../src/app-state";
import { nickAvatar, nickEl, sysBodyDOM } from "../src/nick";
import {
  avatarUrlFor,
  hasAvatarFor,
  registerAvatar,
  registerBotNick,
  registerMemberNickColors,
  resetNickColors,
} from "../src/nick-colors";

const text = (nodes: Node[]) => nodes.map((n) => (n instanceof Element ? n.textContent : n.nodeValue)).join("");

describe("sysBodyDOM", () => {
  it("formats join", () => {
    const nodes = sysBodyDOM({ kind: "join", sender: "alice" });
    expect(text(nodes)).toContain("alice");
    expect(text(nodes)).toContain("joined");
  });

  it("includes reason on part", () => {
    const nodes = sysBodyDOM({ kind: "part", sender: "bob", content: "bye" });
    expect(text(nodes)).toContain("(bye)");
    expect(text(nodes)).toContain("left");
  });

  it("includes reason on quit", () => {
    const nodes = sysBodyDOM({ kind: "quit", sender: "bob", content: "timeout" });
    expect(text(nodes)).toContain("(timeout)");
    expect(text(nodes)).toContain("quit");
  });

  it("renders nick change with both nicks", () => {
    const nodes = sysBodyDOM({ kind: "nick", sender: "alice", target: "alice2" });
    expect(text(nodes)).toContain("is now known as");
    expect(text(nodes)).toContain("alice");
    expect(text(nodes)).toContain("alice2");
  });

  it("renders nick change fallback without target", () => {
    const nodes = sysBodyDOM({ kind: "nick", sender: "alice", content: "custom text" });
    expect(text(nodes)).toContain("custom text");
  });

  it("renders kick with target and kicker", () => {
    const nodes = sysBodyDOM({ kind: "kick", sender: "op", target: "bob", content: "spam" });
    expect(text(nodes)).toContain("was kicked by");
    expect(text(nodes)).toContain("bob");
    expect(text(nodes)).toContain("op");
    expect(text(nodes)).toContain("(spam)");
  });

  it("renders mode", () => {
    const nodes = sysBodyDOM({ kind: "mode", sender: "op", content: "+o bob" });
    expect(text(nodes)).toContain("set mode +o bob");
  });

  it("renders mode with target", () => {
    const nodes = sysBodyDOM({ kind: "mode", sender: "op", content: "+b", target: "#chan" });
    expect(text(nodes)).toContain("on #chan");
  });

  it("renders topic", () => {
    const nodes = sysBodyDOM({ kind: "topic", sender: "alice", content: "hello world" });
    expect(text(nodes)).toContain("set topic: hello world");
  });

  it("renders connected", () => {
    expect(text(sysBodyDOM({ kind: "connected" }))).toBe("connected");
  });

  it("renders disconnected with reason", () => {
    const nodes = sysBodyDOM({ kind: "disconnected", content: "ping timeout" });
    expect(text(nodes)).toContain("ping timeout");
  });

  it("nick spans use nickref class", () => {
    const nodes = sysBodyDOM({ kind: "join", sender: "alice" });
    const spans = nodes.filter((n) => n instanceof Element) as Element[];
    expect(spans.length).toBeGreaterThan(0);
    expect(spans[0].className).toBe("nickref");
  });

  it("each nick reference is its own element", () => {
    const nodes = sysBodyDOM({ kind: "nick", sender: "alice", target: "alice2" });
    const spans = nodes.filter((n) => n instanceof Element) as Element[];
    expect(spans).toHaveLength(2);
  });

  it("does not inject raw content strings as HTML", () => {
    const nodes = sysBodyDOM({ kind: "part", sender: "alice", content: "<script>xss</script>" });
    const el = document.createElement("div");
    el.replaceChildren(...nodes);
    expect(el.innerHTML).not.toContain("<script>");
    expect(el.textContent).toContain("<script>xss</script>");
  });
});

describe("nickEl", () => {
  it("has nickref class by default", () => {
    expect(nickEl("alice").className).toBe("nickref");
  });

  it("accepts custom class", () => {
    expect(nickEl("alice", "nick").className).toBe("nick");
  });

  it("accepts custom label", () => {
    const el = nickEl("alice", "nickref", "-alice-");
    expect(el.textContent).toContain("-alice-");
  });

  it("contains nick text by default", () => {
    expect(nickEl("bob").textContent).toContain("bob");
  });

  it("has a color style set", () => {
    const el = nickEl("alice");
    expect(el.style.color).toBeTruthy();
  });
});

describe("nickAvatar", () => {
  it("returns a canvas element", () => {
    expect(nickAvatar("alice")).toBeInstanceOf(HTMLCanvasElement);
  });

  it("has nick-avatar class", () => {
    expect(nickAvatar("alice").className).toBe("nick-avatar");
  });

  it("is deterministic — same nick same pixels", () => {
    const a = nickAvatar("alice");
    const b = nickAvatar("alice");
    const ctxA = a.getContext("2d");
    const ctxB = b.getContext("2d");
    expect(ctxA).not.toBeNull();
    expect(ctxB).not.toBeNull();
    const dataA = (ctxA as CanvasRenderingContext2D).getImageData(0, 0, a.width, a.height).data.join(",");
    const dataB = (ctxB as CanvasRenderingContext2D).getImageData(0, 0, b.width, b.height).data.join(",");
    expect(dataA).toBe(dataB);
  });

  it("differs for different nicks", () => {
    const a = nickAvatar("alice");
    const b = nickAvatar("bob");
    const ctxA = a.getContext("2d");
    const ctxB = b.getContext("2d");
    expect(ctxA).not.toBeNull();
    expect(ctxB).not.toBeNull();
    const dataA = (ctxA as CanvasRenderingContext2D).getImageData(0, 0, a.width, a.height).data.join(",");
    const dataB = (ctxB as CanvasRenderingContext2D).getImageData(0, 0, b.width, b.height).data.join(",");
    expect(dataA).not.toBe(dataB);
  });
});

describe("bot avatars", () => {
  // isBotNick scopes by the active buffer's network, so tests need one.
  function seedActiveBuffer(networkId = "n1") {
    state.buffers.set("b1", {
      id: "b1",
      network_id: networkId,
      name: "#a",
      kind: "channel",
      unread: 0,
      mentions: 0,
      show_embeds: true,
      show_presence_events: true,
      collapse_presence_events: false,
      pinned: false,
    });
    state.activeId = "b1";
  }

  beforeEach(() => {
    seedActiveBuffer();
  });

  afterEach(() => {
    resetNickColors();
    state.buffers.clear();
    state.activeId = null;
  });

  it("renders the robot glyph instead of an identicon", () => {
    registerBotNick("n1", "helperbot", true);
    const el = nickAvatar("helperbot");
    expect(el).not.toBeInstanceOf(HTMLCanvasElement);
    expect(el.textContent).toBe("🤖");
    expect(el.className).toBe("nick-avatar bot");
  });

  it("matches case-insensitively", () => {
    registerBotNick("n1", "helperbot", true);
    expect(nickAvatar("HelperBot").textContent).toBe("🤖");
  });

  it("leaves non-bots on the identicon", () => {
    registerBotNick("n1", "helperbot", true);
    expect(nickAvatar("alice")).toBeInstanceOf(HTMLCanvasElement);
  });

  it("scopes bot status to the network", () => {
    registerBotNick("n2", "helperbot", true);
    // Active buffer is on n1; the n2 bot must not leak across networks.
    expect(nickAvatar("helperbot")).toBeInstanceOf(HTMLCanvasElement);
  });

  it("clears bot status on an explicit bot=false", () => {
    registerBotNick("n1", "helperbot", true);
    expect(nickAvatar("helperbot").textContent).toBe("🤖");
    // A member-list snapshot without the flag (e.g. a human took the nick)
    // restores the identicon.
    registerMemberNickColors([{ nick: "helperbot", prefix: "", away: false, self: false }], "n1");
    expect(nickAvatar("helperbot")).toBeInstanceOf(HTMLCanvasElement);
  });

  it("picks up the bot flag from member lists", () => {
    registerMemberNickColors(
      [
        { nick: "helperbot", prefix: "", away: false, self: false, bot: true },
        { nick: "alice", prefix: "", away: false, self: false },
      ],
      "n1",
    );
    expect(nickAvatar("helperbot").textContent).toBe("🤖");
    expect(nickAvatar("alice")).toBeInstanceOf(HTMLCanvasElement);
  });

  it("nickEl still labels the bot with its nick", () => {
    registerBotNick("n1", "helperbot", true);
    const el = nickEl("helperbot");
    expect(el.textContent).toContain("helperbot");
    expect(el.textContent).toContain("🤖");
  });
});

describe("avatar registry", () => {
  afterEach(() => {
    resetNickColors();
    state.buffers.clear();
    state.activeId = null;
  });

  it("hasAvatarFor is false with no active buffer to scope against", () => {
    registerAvatar("n1", "alice", true);
    expect(hasAvatarFor("alice")).toBe(false);
  });

  it("avatarUrlFor is undefined for an unregistered nick", () => {
    state.buffers.set("b1", {
      id: "b1",
      network_id: "n1",
      name: "#a",
      kind: "channel",
      unread: 0,
      mentions: 0,
      show_embeds: true,
      show_presence_events: true,
      collapse_presence_events: false,
      pinned: false,
    });
    state.activeId = "b1";
    expect(avatarUrlFor("alice")).toBeUndefined();
  });

  it("avatarUrlFor builds the proxied /api/avatar URL, scoped by network", () => {
    state.buffers.set("b1", {
      id: "b1",
      network_id: "n1",
      name: "#a",
      kind: "channel",
      unread: 0,
      mentions: 0,
      show_embeds: true,
      show_presence_events: true,
      collapse_presence_events: false,
      pinned: false,
    });
    state.activeId = "b1";
    registerAvatar("n1", "alice", true);
    // biome-ignore lint/security/noSecrets: query string, not a secret
    expect(avatarUrlFor("alice")).toBe("/api/avatar?network=n1&nick=alice&size=64");
  });

  it("clears the avatar on an explicit has_avatar=false", () => {
    state.buffers.set("b1", {
      id: "b1",
      network_id: "n1",
      name: "#a",
      kind: "channel",
      unread: 0,
      mentions: 0,
      show_embeds: true,
      show_presence_events: true,
      collapse_presence_events: false,
      pinned: false,
    });
    state.activeId = "b1";
    registerAvatar("n1", "alice", true);
    expect(hasAvatarFor("alice")).toBe(true);
    registerAvatar("n1", "alice", false);
    expect(hasAvatarFor("alice")).toBe(false);
  });
});

describe("avatar images in nickAvatar", () => {
  // hasAvatarFor/avatarUrlFor scope by the active buffer's network, same as
  // isBotNick — tests need one active.
  function seedActiveBuffer(networkId = "n1") {
    state.buffers.set("b1", {
      id: "b1",
      network_id: networkId,
      name: "#a",
      kind: "channel",
      unread: 0,
      mentions: 0,
      show_embeds: true,
      show_presence_events: true,
      collapse_presence_events: false,
      pinned: false,
    });
    state.activeId = "b1";
  }

  beforeEach(() => {
    seedActiveBuffer();
  });

  afterEach(() => {
    resetNickColors();
    state.buffers.clear();
    state.activeId = null;
  });

  it("renders an img pointed at the proxied avatar URL when known", () => {
    registerAvatar("n1", "alice", true);
    const el = nickAvatar("alice");
    expect(el).toBeInstanceOf(HTMLImageElement);
    // biome-ignore lint/security/noSecrets: query string, not a secret
    expect((el as HTMLImageElement).getAttribute("src")).toBe("/api/avatar?network=n1&nick=alice&size=64");
  });

  it("falls back to the identicon when no avatar is known", () => {
    expect(nickAvatar("alice")).toBeInstanceOf(HTMLCanvasElement);
  });

  it("falls back to the identicon after an explicit has_avatar=false", () => {
    registerAvatar("n1", "alice", true);
    expect(nickAvatar("alice")).toBeInstanceOf(HTMLImageElement);
    registerAvatar("n1", "alice", false);
    expect(nickAvatar("alice")).toBeInstanceOf(HTMLCanvasElement);
  });

  it("scopes avatar presence to the network — no leak across networks", () => {
    registerAvatar("n2", "alice", true);
    expect(nickAvatar("alice")).toBeInstanceOf(HTMLCanvasElement);
  });

  it("bot mode takes precedence over a known avatar", () => {
    registerBotNick("n1", "alice", true);
    registerAvatar("n1", "alice", true);
    expect(nickAvatar("alice").textContent).toBe("🤖");
  });

  it("picks up has_avatar from member lists", () => {
    registerMemberNickColors([{ nick: "alice", prefix: "", away: false, self: false, has_avatar: true }], "n1");
    expect(nickAvatar("alice")).toBeInstanceOf(HTMLImageElement);
  });

  it("replaces itself with the identicon on image load error", () => {
    registerAvatar("n1", "alice", true);
    const el = nickAvatar("alice") as HTMLImageElement;
    const parent = document.createElement("div");
    parent.appendChild(el);
    el.dispatchEvent(new Event("error"));
    expect(parent.firstElementChild).toBeInstanceOf(HTMLCanvasElement);
  });
});
