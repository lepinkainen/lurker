import { activeBuffer, type Network, state } from "./app-state";
import { renderChannelListPanel } from "./channel-list";
import type { DomRefs } from "./dom";
import { updateInputEnabled } from "./input";
import { renderMembers } from "./members";
import { renderActiveView, renderHeader } from "./messages";
import { nickAvatar } from "./nick";
import { renderSidebar } from "./sidebar";
import { renderSidebarStatus } from "./status";

export type AppViewDeps = {
  sendCmd: (cmd: Record<string, unknown>) => void;
  setActive: (id: string) => void;
};

export type AppView = ReturnType<typeof createAppView>;

export function createAppView(d: DomRefs, deps: AppViewDeps) {
  const messageArea = {
    messagesEl: d.messagesEl,
    statusViewEl: d.statusViewEl,
    bufferNameEl: d.bufferNameEl,
    bufferTopicEl: d.bufferTopicEl,
    inputEl: d.inputEl,
  };
  const view = {
    dom: d,
    renderStatus: () => renderSidebarStatus(d),
    renderPromptNick: () => {
      const nick = activePromptNick();
      d.inputNickEl.replaceChildren(nickAvatar(nick), nick);
    },
    renderHeader: () => renderHeader(messageArea, { renderPromptNick: view.renderPromptNick, iconEl }),
    renderActiveView: () => {
      if (state.channelList?.done) {
        d.statusViewEl.hidden = true;
        d.messagesEl.hidden = false;
        renderChannelListPanel(d.messagesEl, { sendCmd: deps.sendCmd, renderActiveView: view.renderActiveView });
        return;
      }
      renderActiveView(messageArea, { renderPromptNick: view.renderPromptNick, iconEl });
    },
    renderMembers: () =>
      renderMembers({
        memberPaneEl: d.memberPaneEl,
        toggleMembersEl: d.toggleMembersEl,
        memberCountEl: d.memberCountEl,
        memberCountInlineEl: d.memberCountInlineEl,
        bufferMemcountEl: d.bufferMemcountEl,
        memberListEl: d.memberListEl,
      }),
    updateInputEnabled: () => updateInputEnabled(d.inputEl),
    renderSidebar: () => renderSidebar({ sbScrollEl: d.sbScrollEl, setActive: deps.setActive, iconEl }),
  };
  return view;
}

const SVG_NS = "http://www.w3.org/2000/svg";

function iconEl(symbolId: string, size: number, opts: { className?: string; label?: string } = {}): SVGSVGElement {
  const svg = document.createElementNS(SVG_NS, "svg");
  svg.setAttribute("class", opts.className ? `icon ${opts.className}` : "icon");
  svg.setAttribute("width", String(size));
  svg.setAttribute("height", String(size));
  svg.setAttribute("focusable", "false");
  if (opts.label) {
    svg.setAttribute("role", "img");
    svg.setAttribute("aria-label", opts.label);
    const title = document.createElementNS(SVG_NS, "title");
    title.textContent = opts.label;
    svg.appendChild(title);
  } else {
    svg.setAttribute("aria-hidden", "true");
  }
  const useEl = document.createElementNS(SVG_NS, "use");
  useEl.setAttribute("href", `#${symbolId}`);
  svg.appendChild(useEl);
  return svg;
}

function activePromptNick(): string {
  const b = activeBuffer();
  if (b) {
    const n = state.networks.get(b.network_id) as (Network & { nick?: string }) | undefined;
    if (n?.nick) return n.nick;
  }
  return state.me.nick || "";
}
