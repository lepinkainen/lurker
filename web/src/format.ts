import { NICK_HUES } from "./nick-palette";

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

export function nickHue(nick: unknown): number {
  let h = 5381;
  const s = String(nick ?? "").toLowerCase();
  for (let i = 0; i < s.length; i++) h = ((h << 5) + h + s.charCodeAt(i)) | 0;
  return Math.abs(h) % NICK_HUES.length;
}

export function nickColor(nick: unknown): string {
  const hue = NICK_HUES[nickHue(nick)];
  return `oklch(var(--nick-l, 72%) var(--nick-c, 0.12) ${hue}deg)`;
}

export type MessageKind = "sys" | "notice" | "action" | "message";

export function classifyKind(kind: string | undefined): MessageKind {
  if (kind && ["join", "part", "quit", "nick", "kick", "mode", "topic", "connected", "disconnected"].includes(kind))
    return "sys";
  if (kind === "notice") return "notice";
  if (kind === "action") return "action";
  return "message";
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
