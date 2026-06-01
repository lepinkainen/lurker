import { activeBuffer, type Member, state } from "./app-state";
import { nickColor } from "./format";
import { nickAvatar } from "./nick";

export type SendCmd = (cmd: Record<string, unknown>) => void;

type PopupCtx = {
  member: Member;
  anchor: HTMLElement;
  sendCmd: SendCmd;
};

type ActiveForm = "invite" | "ignore" | null;

type PopupHandle = {
  el: HTMLElement;
  anchor: HTMLElement;
  tail: HTMLElement;
  member: Member;
  networkId: string;
  channel: string;
  sendCmd: SendCmd;
  showDefaultTail: () => void;
  reposition: () => void;
  activeForm: ActiveForm;
};

let current: PopupHandle | null = null;

export function openUserPopup(ctx: PopupCtx) {
  if (current && current.anchor === ctx.anchor) {
    closeUserPopup();
    return;
  }
  closeUserPopup();

  const buffer = activeBuffer();
  if (!buffer) return;
  const network = state.networks.get(buffer.network_id);
  const networkId = buffer.network_id;
  const channel = buffer.kind === "channel" ? buffer.name : "";
  const myNick = (network && (network as { nick?: string }).nick) || state.me.nick || "";

  const el = document.createElement("div");
  el.className = "user-popup";
  el.setAttribute("role", "dialog");
  el.setAttribute("aria-label", `User info: ${ctx.member.nick}`);

  el.append(buildHeader(ctx.member), buildMeta(ctx.member, myNick));

  const actions = document.createElement("div");
  actions.className = "up-actions";
  el.appendChild(actions);

  const tail = document.createElement("div");
  el.appendChild(tail);

  const handle: PopupHandle = {
    el,
    anchor: ctx.anchor,
    tail,
    member: ctx.member,
    networkId,
    channel,
    sendCmd: ctx.sendCmd,
    showDefaultTail: () => showDefaultTail(handle),
    reposition: () => positionPopup(el, ctx.anchor),
    activeForm: null,
  };

  renderActions(handle, actions);
  showDefaultTail(handle);

  document.body.appendChild(el);
  ctx.anchor.classList.add("popup-open");
  handle.reposition();

  current = handle;
}

export function closeUserPopup() {
  if (!current) return;
  current.el.remove();
  current.anchor.classList.remove("popup-open");
  current = null;
}

// Close the popup if its anchor is no longer connected to the DOM.
// Members re-render replaces the anchor button with a fresh node, leaving
// the popup pointing at a detached element and operating in stale context.
export function dismissUserPopupIfDetached() {
  if (current && !current.anchor.isConnected) closeUserPopup();
}

function renderActions(h: PopupHandle, actions: HTMLElement) {
  actions.replaceChildren();
  actions.appendChild(
    actionBtn("?", "Whois", () => {
      h.sendCmd({ type: "whois", network_id: h.networkId, target: h.member.nick });
      closeUserPopup();
    }),
  );
  if (!h.member.self) {
    actions.appendChild(
      actionBtn("»", "Open query", () => {
        h.sendCmd({ type: "query", network_id: h.networkId, target: h.member.nick });
        closeUserPopup();
      }),
    );
  }
  actions.appendChild(
    actionBtn("+", "Invite to channel…", () => {
      if (h.activeForm === "invite") {
        h.tail.querySelector<HTMLInputElement>(".up-inline-form input")?.focus();
        return;
      }
      showInviteForm(h);
    }),
  );
  if (!h.member.self) {
    actions.appendChild(
      actionBtn(
        "×",
        "Ignore…",
        () => {
          if (h.activeForm === "ignore") {
            h.tail.querySelector<HTMLInputElement>(".up-inline-form input")?.focus();
            return;
          }
          showIgnoreForm(h);
        },
        true,
      ),
    );
  }
}

function showDefaultTail(h: PopupHandle) {
  h.activeForm = null;
  h.tail.replaceChildren();
  if (!h.member.self) {
    h.tail.appendChild(buildComposer(h));
  }
  h.reposition();
}

function buildHeader(member: Member): HTMLElement {
  const hdr = document.createElement("div");
  hdr.className = "up-hdr";
  hdr.appendChild(nickAvatar(member.nick));

  const who = document.createElement("div");
  who.className = "up-who";

  const nick = document.createElement("div");
  nick.className = "up-nick";
  if (member.prefix) {
    const pre = document.createElement("span");
    pre.className = `up-pre ${prefixClass(member.prefix)}`;
    pre.textContent = member.prefix;
    nick.appendChild(pre);
  }
  const nameSpan = document.createElement("span");
  nameSpan.textContent = member.nick;
  nameSpan.style.color = nickColor(member.nick);
  nick.appendChild(nameSpan);
  who.appendChild(nick);

  const sub = document.createElement("div");
  sub.className = "up-sub";
  sub.textContent = subLine(member);
  who.appendChild(sub);

  hdr.appendChild(who);
  return hdr;
}

function buildMeta(member: Member, myNick: string): HTMLElement {
  const meta = document.createElement("div");
  meta.className = "up-meta";

  const rows: Array<[string, string]> = [];
  if (member.self || (myNick && member.nick === myNick)) rows.push(["you", "this is you"]);
  rows.push(["nick", member.nick]);
  const realname = cleanRealname(member.realname);
  if (realname && realname !== member.nick) rows.push(["name", realname]);
  rows.push(["mode", modeLabel(member.prefix)]);
  rows.push(["status", member.away ? "away" : "online"]);

  for (const [k, v] of rows) {
    const row = document.createElement("div");
    row.className = "up-row";
    const kEl = document.createElement("span");
    kEl.className = "up-k";
    kEl.textContent = k;
    const vEl = document.createElement("span");
    vEl.className = "up-v";
    vEl.textContent = v;
    row.append(kEl, vEl);
    meta.appendChild(row);
  }
  return meta;
}

function buildComposer(h: PopupHandle): HTMLElement {
  const wrap = document.createElement("div");
  wrap.className = "up-send";

  const label = document.createElement("label");
  label.className = "up-send-label";
  label.textContent = "Send a message:";
  wrap.appendChild(label);

  const input = document.createElement("input");
  input.type = "text";
  input.placeholder = `msg ${h.member.nick}…`;
  input.autocomplete = "off";
  input.addEventListener("keydown", (e) => {
    if (e.key === "Enter") {
      const text = input.value.trim();
      if (!text) return;
      h.sendCmd({ type: "msg", network_id: h.networkId, target: h.member.nick, content: text });
      closeUserPopup();
    } else if (e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation();
      closeUserPopup();
    }
  });
  wrap.appendChild(input);
  return wrap;
}

function showInviteForm(h: PopupHandle) {
  h.activeForm = "invite";
  h.tail.replaceChildren();
  const form = document.createElement("div");
  form.className = "up-inline-form";

  const label = document.createElement("label");
  label.textContent = `Invite ${h.member.nick} to channel:`;
  form.appendChild(label);

  const input = document.createElement("input");
  input.type = "text";
  input.placeholder = h.channel || "#channel";
  input.value = "";
  input.autocomplete = "off";
  form.appendChild(input);

  const send = () => {
    const ch = input.value.trim();
    if (!ch) return;
    h.sendCmd({ type: "invite", network_id: h.networkId, target: h.member.nick, channel: ch });
    closeUserPopup();
  };

  const actions = document.createElement("div");
  actions.className = "up-form-actions";
  const cancel = document.createElement("button");
  cancel.type = "button";
  cancel.textContent = "Back";
  cancel.addEventListener("click", () => h.showDefaultTail());
  const submit = document.createElement("button");
  submit.type = "button";
  submit.className = "primary";
  submit.textContent = "Invite";
  submit.addEventListener("click", send);
  input.addEventListener("keydown", (e) => {
    if (e.key === "Enter") send();
    else if (e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation();
      h.showDefaultTail();
    }
  });
  actions.append(cancel, submit);
  form.appendChild(actions);

  h.tail.appendChild(form);
  h.reposition();
  input.focus();
}

function showIgnoreForm(h: PopupHandle) {
  h.activeForm = "ignore";
  h.tail.replaceChildren();
  const form = document.createElement("div");
  form.className = "up-inline-form";

  const label = document.createElement("label");
  label.textContent = "Ignore mask:";
  form.appendChild(label);

  const input = document.createElement("input");
  input.type = "text";
  input.placeholder = `${h.member.nick}!*@*`;
  input.value = `${h.member.nick}!*@*`;
  input.autocomplete = "off";
  form.appendChild(input);

  const send = () => {
    const mask = input.value.trim();
    if (!mask) return;
    h.sendCmd({ type: "ignore", network_id: h.networkId, target: mask });
    closeUserPopup();
  };

  const actions = document.createElement("div");
  actions.className = "up-form-actions";
  const cancel = document.createElement("button");
  cancel.type = "button";
  cancel.textContent = "Back";
  cancel.addEventListener("click", () => h.showDefaultTail());
  const submit = document.createElement("button");
  submit.type = "button";
  submit.className = "primary";
  submit.textContent = "Ignore";
  submit.addEventListener("click", send);
  input.addEventListener("keydown", (e) => {
    if (e.key === "Enter") send();
    else if (e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation();
      h.showDefaultTail();
    }
  });
  actions.append(cancel, submit);
  form.appendChild(actions);

  h.tail.appendChild(form);
  h.reposition();
  input.focus();
  input.select();
}

function actionBtn(icon: string, label: string, onClick: () => void, danger = false): HTMLButtonElement {
  const b = document.createElement("button");
  b.type = "button";
  if (danger) b.className = "danger";
  const ic = document.createElement("span");
  ic.className = "up-ic";
  ic.textContent = icon;
  const lab = document.createElement("span");
  lab.textContent = label;
  b.append(ic, lab);
  b.addEventListener("click", onClick);
  return b;
}

function prefixClass(prefix: string): string {
  if (prefix === "@") return "op";
  if (prefix === "%") return "hop";
  if (prefix === "+") return "voice";
  return "";
}

// Strip mIRC formatting codes (color, bold, italic, etc.) and trim.
// Realname is rendered via textContent so control chars leak visually
// without this.
export function cleanRealname(s: string | undefined): string {
  if (!s) return "";
  let out = "";
  let i = 0;
  while (i < s.length) {
    const code = s.charCodeAt(i);
    if (code === 0x03) {
      // color: \x03[fg[,bg]] with 1-2 digit numbers
      i++;
      let n = 0;
      while (n < 2 && i < s.length && s.charCodeAt(i) >= 0x30 && s.charCodeAt(i) <= 0x39) {
        i++;
        n++;
      }
      if (n > 0 && s.charCodeAt(i) === 0x2c) {
        const save = i;
        i++;
        let m = 0;
        while (m < 2 && i < s.length && s.charCodeAt(i) >= 0x30 && s.charCodeAt(i) <= 0x39) {
          i++;
          m++;
        }
        if (m === 0) i = save;
      }
      continue;
    }
    if (code === 0x04) {
      // hex color: \x04[RRGGBB[,RRGGBB]]
      i++;
      let n = 0;
      while (n < 6 && i < s.length && isHex(s.charCodeAt(i))) {
        i++;
        n++;
      }
      if (n > 0 && s.charCodeAt(i) === 0x2c) {
        const save = i;
        i++;
        let m = 0;
        while (m < 6 && i < s.length && isHex(s.charCodeAt(i))) {
          i++;
          m++;
        }
        if (m === 0) i = save;
      }
      continue;
    }
    if (
      code === 0x02 ||
      code === 0x0f ||
      code === 0x11 ||
      code === 0x16 ||
      code === 0x1d ||
      code === 0x1e ||
      code === 0x1f
    ) {
      i++;
      continue;
    }
    out += s[i];
    i++;
  }
  return out.trim();
}

function isHex(c: number): boolean {
  return (c >= 0x30 && c <= 0x39) || (c >= 0x41 && c <= 0x46) || (c >= 0x61 && c <= 0x66);
}

function modeLabel(prefix: string): string {
  if (prefix === "@") return "operator";
  if (prefix === "%") return "half-op";
  if (prefix === "+") return "voiced";
  return "regular";
}

function subLine(member: Member): string {
  const bits: string[] = [];
  bits.push(modeLabel(member.prefix));
  if (member.away) bits.push("away");
  if (member.self) bits.push("you");
  return bits.join(" · ");
}

function positionPopup(el: HTMLElement, anchor: HTMLElement) {
  const margin = 6;
  const r = anchor.getBoundingClientRect();
  const w = el.offsetWidth || 264;
  const h = el.offsetHeight || 220;

  let left = r.left - w - margin;
  if (left < 8) left = Math.min(r.right + margin, window.innerWidth - w - 8);
  left = Math.max(8, left);

  let top = r.top;
  if (top + h > window.innerHeight - 8) top = Math.max(8, window.innerHeight - h - 8);
  top = Math.max(8, top);

  el.style.left = `${left}px`;
  el.style.top = `${top}px`;
}

// ---- global dismiss wiring ----
document.addEventListener(
  "click",
  (e) => {
    if (!current) return;
    const t = e.target as Node | null;
    if (!t) return;
    if (current.el.contains(t)) return;
    if (current.anchor.contains(t)) return;
    closeUserPopup();
  },
  true,
);

// Use capture phase + stopPropagation so the popup swallows Escape before
// keyboard-routing.ts collapses the members drawer underneath it.
document.addEventListener(
  "keydown",
  (e) => {
    if (e.key !== "Escape" || !current) return;
    e.preventDefault();
    e.stopPropagation();
    closeUserPopup();
  },
  true,
);

window.addEventListener("resize", () => current?.reposition());
window.addEventListener("scroll", () => current?.reposition(), true);
