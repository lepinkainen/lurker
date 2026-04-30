import { type Member, state } from "./app-state";
import { nickEl } from "./nick";

export type MembersDom = {
  memberPaneEl: HTMLElement;
  toggleMembersEl: HTMLButtonElement;
  memberCountEl: HTMLElement;
  memberCountInlineEl: HTMLElement;
  bufferMemcountEl: HTMLElement;
  memberListEl: HTMLElement;
};

export function membersForActive(): Member[] {
  return state.activeId !== null ? state.members.get(state.activeId) || [] : [];
}

export function populateMembersForActive() {
  const buffer = state.buffers.get(state.activeId);
  if (!buffer || buffer.kind !== "channel") return;
  if (state.members.has(buffer.id)) return;
  const names = new Set<string>();
  for (const message of state.messages.get(buffer.id) || []) {
    if (message.sender) names.add(message.sender);
    if (message.target && ["kick", "nick"].includes(message.kind || "")) names.add(message.target);
  }
  if (state.me.nick) names.add(state.me.nick);
  const sorted = [...names].sort((a, b) => a.localeCompare(b));
  state.members.set(
    buffer.id,
    sorted.map((nick) => ({
      nick,
      prefix: nick === state.me.nick ? "+" : "",
      away: false,
      self: nick === state.me.nick,
    })),
  );
}

function memberRow(member: Member) {
  const btn = document.createElement("button");
  btn.type = "button";
  btn.className = `member ${member.away ? "away" : ""} ${member.self ? "me" : ""}`.trim();
  const cls = member.prefix === "@" ? "op" : member.prefix === "%" ? "hop" : member.prefix === "+" ? "voice" : "none";
  const pfx = document.createElement("span");
  pfx.className = `pfx ${cls}`;
  pfx.textContent = member.prefix || "\u00a0";
  const mn = nickEl(member.nick, "mn");
  btn.append(pfx, mn);
  return btn;
}

export function renderMembers(dom: MembersDom) {
  const active = state.buffers.get(state.activeId);
  const isChannel = active && active.kind === "channel";
  const showPane = state.showMemberList && isChannel;
  dom.memberPaneEl.dataset.hidden = showPane ? "false" : "true";
  dom.toggleMembersEl.classList.toggle("toggled", state.showMemberList);

  const members = isChannel ? membersForActive() : [];
  dom.memberCountEl.textContent = String(members.length);
  dom.memberCountInlineEl.textContent = String(members.length);
  dom.bufferMemcountEl.hidden = !isChannel;

  dom.memberListEl.innerHTML = "";
  if (!showPane) return;

  const groups: [string, Member[]][] = [
    ["ops", members.filter((member) => member.prefix === "@")],
    ["half-ops", members.filter((member) => member.prefix === "%")],
    ["voice", members.filter((member) => member.prefix === "+")],
    ["regulars", members.filter((member) => !member.prefix)],
  ];
  for (const [name, items] of groups) {
    if (items.length === 0) continue;
    const heading = document.createElement("div");
    heading.className = "mgroup";
    heading.textContent = `${name} · ${items.length}`;
    dom.memberListEl.appendChild(heading);
    for (const member of items) dom.memberListEl.appendChild(memberRow(member));
  }
}
