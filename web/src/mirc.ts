import type { MircSegment } from "./app-state";
import { escapeHTML } from "./format";

// mIRC control-code *parsing* lives server-side (Go mirc package); messages
// arrive with pre-parsed segments. This module only renders those segments
// as HTML. Colors map to the --mirc-c-0..15 CSS variables.

function openTag(seg: MircSegment): string {
  const classes: string[] = [];
  if (seg.bold) classes.push("m-b");
  if (seg.italic) classes.push("m-i");
  if (seg.underline) classes.push("m-u");
  if (seg.strike) classes.push("m-s");
  if (seg.mono) classes.push("m-mono");
  const styles: string[] = [];
  if (seg.fg !== undefined) styles.push(`color:var(--mirc-c-${seg.fg})`);
  if (seg.bg !== undefined) styles.push(`background:var(--mirc-c-${seg.bg})`);
  const cls = classes.length > 0 ? ` class="${classes.join(" ")}"` : "";
  const sty = styles.length > 0 ? ` style="${styles.join(";")}"` : "";
  return `<span${cls}${sty}>`;
}

function isStyled(seg: MircSegment): boolean {
  return (
    seg.bold === true ||
    seg.italic === true ||
    seg.underline === true ||
    seg.strike === true ||
    seg.mono === true ||
    seg.fg !== undefined ||
    seg.bg !== undefined
  );
}

export function renderSegmentsHTML(segments: MircSegment[]): string {
  let out = "";
  for (const seg of segments) {
    const text = escapeHTML(seg.text);
    out += isStyled(seg) ? `${openTag(seg)}${text}</span>` : text;
  }
  return out;
}
