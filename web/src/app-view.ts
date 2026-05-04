import { activeBuffer, type Member, type Message, type Network, state } from "./app-state";
import { tryRenderActiveChannelList } from "./channel-list";
import type { DomRefs } from "./dom";
import { updateInputEnabled } from "./input";
import { renderMembers } from "./members";
import {
  inferUnreadCounts,
  onBufferUpdate,
  onHistoryResult,
  onMessage,
  onPreview,
  renderHeader,
  renderActiveView as renderMessagesView,
} from "./messages";
import { nickAvatar } from "./nick";
import { renderSidebar } from "./sidebar";
import { renderSidebarStatus } from "./status";

export type AppViewDeps = {
  sendCmd: (cmd: Record<string, unknown>) => void;
  setActive: (id: string) => void;
  maybeMarkActiveRead?: () => void;
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
  const markRead = () => deps.maybeMarkActiveRead?.();
  const view = {
    dom: d,
    renderStatus: () => renderSidebarStatus(d),
    renderPromptNick: () => {
      const nick = activePromptNick();
      d.inputNickEl.replaceChildren(nickAvatar(nick), nick);
    },
    renderHeader: () => renderHeader(messageArea, { renderPromptNick: view.renderPromptNick, iconEl }),
    renderActiveView: () => {
      if (
        tryRenderActiveChannelList(d.messagesEl, d.statusViewEl, {
          sendCmd: deps.sendCmd,
          renderActiveView: view.renderActiveView,
        })
      )
        return;
      renderMessagesView(messageArea, { renderPromptNick: view.renderPromptNick, iconEl });
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
    appendMessage: (msg: Message) => {
      onMessage(msg, {
        renderActiveView: view.renderActiveView,
        maybeMarkActiveRead: markRead,
        renderSidebar: view.renderSidebar,
      });
    },
    prependHistory: (msg: { buffer_id: string; messages?: Message[] }) => {
      onHistoryResult(msg, { renderActiveView: view.renderActiveView }, d.messagesEl);
    },
    patchPreview: (msg: { buffer_id: string; message_id: string; previews?: Message["previews"] }) => {
      onPreview(msg, d.messagesEl);
    },
    updateBuffer: (msg: Parameters<typeof onBufferUpdate>[0], opts: { rerenderActive?: boolean } = {}) => {
      onBufferUpdate(msg, {
        inferUnreadCounts,
        renderHeader: view.renderHeader,
        renderSidebar: view.renderSidebar,
      });
      if (opts.rerenderActive) view.renderActiveView();
    },
    setMembers: (bufferId: string, members: Member[]) => {
      state.members.set(bufferId, members);
      if (bufferId !== state.activeId) return;
      view.renderHeader();
      view.renderMembers();
    },
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
