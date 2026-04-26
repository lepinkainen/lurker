import { describe, expect, it } from "vitest";
import { nickAvatar, nickEl, sysBodyDOM } from "../src/nick";

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
    if (ctxA && ctxB) {
      const dataA = ctxA.getImageData(0, 0, a.width, a.height).data.join(",");
      const dataB = ctxB.getImageData(0, 0, b.width, b.height).data.join(",");
      expect(dataA).toBe(dataB);
    }
  });

  it("differs for different nicks", () => {
    const a = nickAvatar("alice");
    const b = nickAvatar("bob");
    const ctxA = a.getContext("2d");
    const ctxB = b.getContext("2d");
    if (ctxA && ctxB) {
      const dataA = ctxA.getImageData(0, 0, a.width, a.height).data.join(",");
      const dataB = ctxB.getImageData(0, 0, b.width, b.height).data.join(",");
      expect(dataA).not.toBe(dataB);
    }
  });
});
