import { activeBuffer, type Member, type Message, type NetsplitInfo, state } from "./app-state";
import type { AppView } from "./app-view";
import { applyChannelListUpdate, type ChannelListUpdate } from "./channel-list";
import { registerAvatar, registerMemberNickColors, registerMessageNickColors } from "./nick-colors";

type WSMessage =
  | ({ type: "message" } & Message)
  | { type: "buffer_created"; id: string; network_id: string; name: string; kind: string; sort_order?: number }
  | {
      type: "buffer_update";
      id: string;
      network_id?: string;
      topic?: string;
      topic_set_by?: string;
      topic_set_at?: string;
      joined?: boolean;
      archived?: boolean;
      last_seen_id?: string;
      // mark_read variant: marker_id always present (null = clear); the
      // topic/joined variant omits it entirely (= unchanged).
      marker_id?: string | null;
      marker_ts?: string;
      unread?: number;
      mentions?: number;
    }
  | {
      type: "buffer_settings";
      id: string;
      show_embeds: boolean;
      show_presence_events: boolean;
      collapse_presence_events: boolean;
      pinned: boolean;
      archived: boolean;
      pin_order?: number;
    }
  | { type: "pinned_reorder"; buffers?: { id: string; pin_order: number }[] }
  | { type: "buffer_reorder"; network_id: string; buffers?: { id: string; sort_order: number }[] }
  | { type: "buffer_deleted"; id: string; network_id: string }
  | { type: "network_state"; network_id: string; state: string }
  | { type: "history_result"; buffer_id: string; messages?: Message[] }
  | { type: "history_backfill"; network_id: string; buffer_id: string; count: number }
  | { type: "preview"; buffer_id: string; message_id: string; previews?: Message["previews"] }
  | { type: "member_list"; network_id: string; buffer_id: string; members?: Member[] }
  | { type: "avatar"; network_id: string; nick: string; has_avatar?: boolean }
  | { type: "netsplit"; buffer_id: string; netsplit: NetsplitInfo; message_ids?: string[] }
  | ({ type: "channel_list" } & ChannelListUpdate)
  | {
      type: "ignorelist_result";
      req_id: string;
      network_id: string;
      entries: { mask: string; level: "hide" | "mute" }[];
    }
  | { type: "highlights"; patterns?: string[] };

export function createWSRouter(view: AppView, sendCmd: (cmd: Record<string, unknown>) => void): (msg: unknown) => void {
  return (msg: unknown) => {
    const m = msg as WSMessage;
    switch (m.type) {
      case "message":
        registerMessageNickColors(m);
        view.appendMessage(m);
        break;
      case "buffer_created": {
        const net = state.networks.get(m.network_id);
        const isDatasource = net?.kind !== undefined && net.kind !== "irc";
        state.buffers.set(m.id, {
          id: m.id,
          network_id: m.network_id,
          name: m.name,
          kind: m.kind,
          joined: m.kind === "channel" && isDatasource,
          topic: "",
          unread: 0,
          mentions: 0,
          last_seen_id: "",
          show_embeds: m.kind !== "status",
          show_presence_events: true,
          collapse_presence_events: false,
          pinned: false,
          ...(m.sort_order === undefined ? {} : { sort_order: m.sort_order }),
        });
        view.renderSidebar();
        break;
      }
      case "buffer_deleted":
        view.removeBuffer(m.id);
        break;
      case "buffer_update":
      case "buffer_settings":
        // Updates targeting the active buffer must rerender the message view:
        // the divider and unread bar are server state (marker_id) and a remote
        // ack has to clear them here too.
        view.updateBuffer(m, { rerenderActive: m.type === "buffer_settings" || m.id === state.activeId });
        break;
      case "pinned_reorder":
        for (const entry of m.buffers || []) {
          const buffer = state.buffers.get(entry.id);
          if (buffer) buffer.pin_order = entry.pin_order;
        }
        view.renderSidebar();
        break;
      case "buffer_reorder":
        for (const entry of m.buffers || []) {
          const buffer = state.buffers.get(entry.id);
          if (buffer) buffer.sort_order = entry.sort_order;
        }
        view.renderSidebar();
        break;
      case "network_state": {
        const n = state.networks.get(m.network_id);
        if (n && n.status !== m.state) {
          n.status = m.state;
          view.renderSidebar();
          view.renderHeader();
        }
        break;
      }
      case "history_result":
        for (const msg of m.messages || []) registerMessageNickColors(msg);
        view.prependHistory(m);
        break;
      case "history_backfill":
        // A CHATHISTORY replay inserted older messages server-side without
        // live message events. Refetch the recent window; onHistoryResult
        // merges by id, so the gap rows slot into place. Buffers we haven't
        // loaded yet just see the rows on their normal first load.
        if (state.messages.has(m.buffer_id)) {
          sendCmd({ type: "history", buffer_id: m.buffer_id, limit: Math.min(500, m.count + 100) });
        }
        break;
      case "preview":
        view.patchPreview(m);
        break;
      case "member_list":
        registerMemberNickColors(m.members || [], m.network_id);
        view.setMembers(m.buffer_id, m.members || []);
        break;
      case "avatar":
        registerAvatar(m.network_id, m.nick, m.has_avatar);
        // Only the active buffer's network is ever on screen (member list,
        // message rows, input nick all render the active buffer). Reuse the
        // same refresh calls member_list already does for the active buffer.
        if (activeBuffer()?.network_id === m.network_id) {
          view.renderMembers();
          view.renderActiveView();
          view.renderPromptNick();
        }
        break;
      case "netsplit": {
        // Retroactive annotation: earlier quits were published before the
        // cluster qualified as a netsplit. Patch them so rendering collapses
        // the whole group.
        const msgs = state.messages.get(m.buffer_id);
        if (!msgs) break;
        const ids = new Set(m.message_ids || []);
        let patched = false;
        for (const msg of msgs) {
          if (ids.has(msg.id) && !msg.netsplit) {
            msg.netsplit = m.netsplit;
            patched = true;
          }
        }
        if (patched && state.activeId === m.buffer_id) view.renderActiveView();
        break;
      }
      case "channel_list":
        if (applyChannelListUpdate(m)) view.renderActiveView();
        break;
      case "ignorelist_result":
        console.log(
          "ignore list:",
          m.entries.map((e) => `${e.mask} (${e.level})`),
        );
        break;
      case "highlights":
        // Highlight matching is server-side; new flags arrive on future
        // messages. Notify any open settings dialog so its list refreshes.
        document.dispatchEvent(new CustomEvent("lurker:highlights", { detail: m.patterns || [] }));
        break;
    }
  };
}
