import { nickColor, type SysMessage } from "./format";
import { avatarUrlFor, isBotNick, nickColorIndex } from "./nick-colors";
import { NICK_HUES } from "./nick-palette";

// Nicks flagged with IRCv3 bot mode get a robot glyph instead of the
// generated identicon — the identicon distinguishes humans from each other,
// which is not what matters about a bot. Otherwise, a known IRCv3 metadata
// avatar wins over the procedural identicon; the identicon is the fallback
// for everyone else, and for a broken/not-yet-cached avatar image (see
// avatarImg's error handler below).
export function nickAvatar(nick: string): HTMLElement {
  if (isBotNick(nick)) return botAvatar();
  const url = avatarUrlFor(nick);
  if (url) return avatarImg(nick, url);
  return identiconAvatar(nick);
}

function avatarImg(nick: string, url: string): HTMLImageElement {
  const img = document.createElement("img");
  img.className = "nick-avatar";
  img.src = url;
  img.alt = "";
  img.loading = "lazy";
  // 404 (no avatar after all), transient fetch failure, or a broken image —
  // fall back to the identicon rather than showing a broken-image glyph.
  img.addEventListener(
    "error",
    () => {
      img.replaceWith(identiconAvatar(nick));
    },
    { once: true },
  );
  return img;
}

function botAvatar(): HTMLElement {
  const span = document.createElement("span");
  span.className = "nick-avatar bot";
  span.textContent = "🤖";
  span.title = "bot";
  return span;
}

function identiconAvatar(nick: string): HTMLCanvasElement {
  const p = 2;
  const size = 5;
  const c = document.createElement("canvas");
  c.width = p * size;
  c.height = p * size;
  c.className = "nick-avatar";
  const ctx = c.getContext("2d");
  if (!ctx) return c;

  const idx = nickColorIndex(nick);
  const style = getComputedStyle(document.documentElement);
  const l = style.getPropertyValue("--nick-l").trim() || "72%";
  const cv = style.getPropertyValue("--nick-c").trim() || "0.12";
  // Unknown nick (no server color seen yet) draws gray, matching nickColor.
  const hue = idx === undefined ? null : (NICK_HUES[idx % NICK_HUES.length] ?? 0);
  ctx.fillStyle = hue === null ? `oklch(${l} 0 0deg)` : `oklch(${l} ${cv} ${hue}deg)`;

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
  span.style.color = nickColor(nickColorIndex(nick));
  span.append(nickAvatar(nick), label ?? nick);
  return span;
}

export function sysBodyDOM(m: SysMessage): Node[] {
  const t = (s: string) => document.createTextNode(s);
  const extra = m.content ? ` (${m.content})` : "";
  const uh = m.userhost ? ` (${m.userhost})` : "";
  switch (m.kind) {
    case "join":
      return [nickEl(m.sender || ""), t(` joined${uh}`)];
    case "part":
      return [nickEl(m.sender || ""), t(` left${uh}${extra}`)];
    case "quit":
      return [nickEl(m.sender || ""), t(` quit${uh}${extra}`)];
    case "nick":
      return m.target
        ? [nickEl(m.sender || ""), t(`${uh} is now known as `), nickEl(m.target)]
        : [t(m.content || "nick change")];
    case "kick":
      return [nickEl(m.target || ""), t(" was kicked by "), nickEl(m.sender || ""), t(`${uh}${extra}`)];
    case "mode":
      return [nickEl(m.sender || ""), t(`${uh} set mode ${m.content || ""}${m.target ? ` on ${m.target}` : ""}`)];
    case "topic":
      return [nickEl(m.sender || ""), t(`${uh} set topic: ${m.content || ""}`)];
    case "away":
      return [nickEl(m.target || m.sender || ""), t(` is away${extra}`)];
    case "back":
      return [nickEl(m.target || m.sender || ""), t(" is back")];
    case "account":
      return m.content
        ? [nickEl(m.target || m.sender || ""), t(` logged in as ${m.content}`)]
        : [nickEl(m.target || m.sender || ""), t(" logged out")];
    case "chghost":
      return [nickEl(m.target || m.sender || ""), t(` changed host to ${m.content || ""}`)];
    case "connected":
      return [t("connected")];
    case "disconnected":
      return [t(`disconnected${extra}`)];
    default:
      return [t(m.content || "")];
  }
}
