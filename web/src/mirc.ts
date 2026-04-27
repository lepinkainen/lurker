const BOLD = "\x02";
const ITALIC = "\x1d";
const UNDERLINE = "\x1f";
const STRIKE = "\x1e";
const MONO = "\x11";
const RESET = "\x0f";
const COLOR = "\x03";

const MAX_COLOR = 15;

type State = {
  b: boolean;
  i: boolean;
  u: boolean;
  s: boolean;
  m: boolean;
  fg: number | null;
  bg: number | null;
};

function emptyState(): State {
  return { b: false, i: false, u: false, s: false, m: false, fg: null, bg: null };
}

function isActive(st: State): boolean {
  return st.b || st.i || st.u || st.s || st.m || st.fg !== null || st.bg !== null;
}

function openTag(st: State): string {
  const classes: string[] = [];
  if (st.b) classes.push("m-b");
  if (st.i) classes.push("m-i");
  if (st.u) classes.push("m-u");
  if (st.s) classes.push("m-s");
  if (st.m) classes.push("m-mono");
  const styles: string[] = [];
  if (st.fg !== null) styles.push(`color:var(--mirc-c-${st.fg})`);
  if (st.bg !== null) styles.push(`background:var(--mirc-c-${st.bg})`);
  const cls = classes.length ? ` class="${classes.join(" ")}"` : "";
  const sty = styles.length ? ` style="${styles.join(";")}"` : "";
  return `<span${cls}${sty}>`;
}

export function mircFormat(input: string): string {
  let out = "";
  const st = emptyState();
  let open = false;

  const closeIfOpen = () => {
    if (open) {
      out += "</span>";
      open = false;
    }
  };

  const writeChar = (c: string) => {
    if (!open && isActive(st)) {
      out += openTag(st);
      open = true;
    }
    out += c;
  };

  let i = 0;
  while (i < input.length) {
    const ch = input[i];
    switch (ch) {
      case BOLD:
        closeIfOpen();
        st.b = !st.b;
        i++;
        break;
      case ITALIC:
        closeIfOpen();
        st.i = !st.i;
        i++;
        break;
      case UNDERLINE:
        closeIfOpen();
        st.u = !st.u;
        i++;
        break;
      case STRIKE:
        closeIfOpen();
        st.s = !st.s;
        i++;
        break;
      case MONO:
        closeIfOpen();
        st.m = !st.m;
        i++;
        break;
      case RESET:
        closeIfOpen();
        st.b = st.i = st.u = st.s = st.m = false;
        st.fg = null;
        st.bg = null;
        i++;
        break;
      case COLOR: {
        closeIfOpen();
        i++;
        const fg = parseDigits(input, i);
        if (fg.value === null) {
          st.fg = null;
          st.bg = null;
        } else {
          i = fg.next;
          st.fg = fg.value > MAX_COLOR ? st.fg : fg.value;
          if (input[i] === ",") {
            const bg = parseDigits(input, i + 1);
            if (bg.value !== null) {
              i = bg.next;
              st.bg = bg.value > MAX_COLOR ? st.bg : bg.value;
            }
          }
        }
        break;
      }
      default:
        writeChar(ch);
        i++;
    }
  }

  closeIfOpen();
  return out;
}

function parseDigits(s: string, start: number): { value: number | null; next: number } {
  let n = 0;
  let consumed = 0;
  while (consumed < 2 && start + consumed < s.length) {
    const c = s.charCodeAt(start + consumed);
    if (c < 48 || c > 57) break;
    n = n * 10 + (c - 48);
    consumed++;
  }
  if (consumed === 0) return { value: null, next: start };
  return { value: n, next: start + consumed };
}
