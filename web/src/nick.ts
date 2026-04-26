import { nickColor, nickHue, type SysMessage } from "./format";
import { NICK_HUES } from "./nick-palette";

export function nickAvatar(nick: string): HTMLCanvasElement {
  const p = 2;
  const size = 5;
  const c = document.createElement("canvas");
  c.width = p * size;
  c.height = p * size;
  c.className = "nick-avatar";
  const ctx = c.getContext("2d");
  if (!ctx) return c;

  const idx = nickHue(nick);
  const hue = NICK_HUES[idx];
  const style = getComputedStyle(document.documentElement);
  const l = style.getPropertyValue("--nick-l").trim() || "72%";
  const cv = style.getPropertyValue("--nick-c").trim() || "0.12";
  ctx.fillStyle = `oklch(${l} ${cv} ${hue}deg)`;

  // xorshift32 seeded from nick chars
  let r = 1;
  for (let i = 0; i < nick.length; i++) {
    r += nick.charCodeAt(i);
    r ^= r << 13;
    r ^= r >>> 17;
    r ^= r << 5;
  }

  const half = Math.floor(size / 2);
  for (let y = 0; y < size; y++) {
    for (let x = 0; x <= half; x++) {
      r ^= r << 13;
      r ^= r >>> 17;
      r ^= r << 5;
      if ((r >>> 0) % 2 === 1) {
        ctx.fillRect(p * x, p * y, p, p);
        ctx.fillRect(p * (size - 1 - x), p * y, p, p);
      }
    }
  }
  return c;
}

export function nickEl(nick: string, className = "nickref", label?: string): HTMLElement {
  const span = document.createElement("span");
  span.className = className;
  span.style.color = nickColor(nick);
  span.append(nickAvatar(nick), label ?? nick);
  return span;
}

export function sysBodyDOM(m: SysMessage): Node[] {
  const t = (s: string) => document.createTextNode(s);
  const extra = m.content ? ` (${m.content})` : "";
  switch (m.kind) {
    case "join":
      return [nickEl(m.sender || ""), t(" joined")];
    case "part":
      return [nickEl(m.sender || ""), t(` left${extra}`)];
    case "quit":
      return [nickEl(m.sender || ""), t(` quit${extra}`)];
    case "nick":
      return m.target
        ? [nickEl(m.sender || ""), t(" is now known as "), nickEl(m.target)]
        : [t(m.content || "nick change")];
    case "kick":
      return [nickEl(m.target || ""), t(" was kicked by "), nickEl(m.sender || ""), t(extra)];
    case "mode":
      return [nickEl(m.sender || ""), t(` set mode ${m.content || ""}${m.target ? ` on ${m.target}` : ""}`)];
    case "topic":
      return [nickEl(m.sender || ""), t(` set topic: ${m.content || ""}`)];
    case "connected":
      return [t("connected")];
    case "disconnected":
      return [t(`disconnected${extra}`)];
    default:
      return [t(m.content || "")];
  }
}
