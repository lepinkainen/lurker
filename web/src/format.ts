export type SysMessage = {
  sender?: string;
  target?: string;
  content?: string;
  kind?: string;
};

export function escapeHTML(s: unknown): string {
  return String(s ?? "").replace(
    /[&<>"']/g,
    (c) =>
      ({
        "&": "&amp;",
        "<": "&lt;",
        ">": "&gt;",
        '"': "&quot;",
        "'": "&#39;",
      })[c as "&" | "<" | ">" | '"' | "'"],
  );
}

export function escapeRegExp(s: unknown): string {
  return String(s ?? "").replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

export function linkify(html: string): string {
  return html.replace(/(https?:\/\/[^\s<]+)/g, '<a href="$1" target="_blank" rel="noreferrer">$1</a>');
}

export function inlineCode(html: string): string {
  return html.replace(/`([^`]+)`/g, "<code>$1</code>");
}

export function highlightMentions(html: string, nick: string): string {
  if (!nick) return html;
  const re = new RegExp(`\\b(${escapeRegExp(nick)})\\b`, "gi");
  return html.replace(re, '<span class="selfmention">$1</span>');
}

export function mentionsMe(m: { content?: string }, nick: string): boolean {
  if (!nick || !m.content) return false;
  return new RegExp(`\\b${escapeRegExp(nick)}\\b`, "i").test(m.content);
}

export function isSelf(m: { sender?: string }, nick: string): boolean {
  return (m.sender || "").toLowerCase() === (nick || "").toLowerCase();
}

export function nickClass(nick: unknown): string {
  let h = 5381;
  const s = String(nick ?? "").toLowerCase();
  for (let i = 0; i < s.length; i++) h = ((h << 5) + h + s.charCodeAt(i)) | 0;
  return `n${Math.abs(h) % 10}`;
}

export type MessageKind = "sys" | "notice" | "action" | "message";

export function classifyKind(kind: string | undefined): MessageKind {
  if (kind && ["join", "part", "quit", "nick", "kick", "mode", "topic", "connected", "disconnected"].includes(kind))
    return "sys";
  if (kind === "notice") return "notice";
  if (kind === "action") return "action";
  return "message";
}

export function sysBodyHTML(m: SysMessage): string {
  const sender = `<span class="nickref ${nickClass(m.sender || "")}">${escapeHTML(m.sender || "")}</span>`;
  const extra = m.content ? ` (${escapeHTML(m.content)})` : "";
  switch (m.kind) {
    case "join":
      return `${sender} joined`;
    case "part":
      return `${sender} left${extra}`;
    case "quit":
      return `${sender} quit${extra}`;
    case "nick":
      return m.target
        ? `${sender} is now known as <span class="nickref ${nickClass(m.target)}">${escapeHTML(m.target)}</span>`
        : escapeHTML(m.content || "nick change");
    case "kick":
      return `<span class="nickref ${nickClass(m.target || "")}">${escapeHTML(m.target || "")}</span> was kicked by ${sender}${extra}`;
    case "mode":
      return `${sender} set mode ${escapeHTML(m.content || "")}${m.target ? ` on ${escapeHTML(m.target)}` : ""}`;
    case "topic":
      return `${sender} set topic: ${escapeHTML(m.content || "")}`;
    case "connected":
      return "connected";
    case "disconnected":
      return `disconnected${extra}`;
    default:
      return escapeHTML(m.content || "");
  }
}

export function formatTime(iso: string | undefined | null): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const hh = String(d.getHours()).padStart(2, "0");
  const mm = String(d.getMinutes()).padStart(2, "0");
  return `${hh}:${mm}`;
}

export function dayKeyOf(ts: string | undefined | null): string | null {
  if (!ts) return null;
  const d = new Date(ts);
  if (Number.isNaN(d.getTime())) return null;
  return `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`;
}

export function dotClass(status: string | undefined | null): "on" | "warn" | "bad" {
  if (status === "connected") return "on";
  if (status === "connecting") return "warn";
  return "bad";
}
