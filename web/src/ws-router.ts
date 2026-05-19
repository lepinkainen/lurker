import { type Member, type Message, state } from "./app-state";
import type { AppView } from "./app-view";
import { applyChannelListUpdate, type ChannelListUpdate } from "./channel-list";

type WSMessage =
  | ({ type: "message" } & Message)
  | { type: "buffer_created"; id: string; network_id: string; name: string; kind: string }
  | { type: "buffer_update"; id: string; topic?: string; joined?: boolean; last_seen_id?: string }
  | {
      type: "buffer_settings";
      id: string;
      show_embeds: boolean;
      show_presence_events: boolean;
      collapse_presence_events: boolean;
      pinned: boolean;
    }
  | { type: "network_state"; network_id: string; state: string }
  | { type: "history_result"; buffer_id: string; messages?: Message[] }
  | { type: "preview"; buffer_id: string; message_id: string; previews?: Message["previews"] }
  | { type: "member_list"; buffer_id: string; members?: Member[] }
  | ({ type: "channel_list" } & ChannelListUpdate)
  | { type: "ignorelist_result"; req_id: string; network_id: string; masks: string[] };

export function createWSRouter(view: AppView): (msg: unknown) => void {
  return (msg: unknown) => {
    const m = msg as WSMessage;
    switch (m.type) {
      case "message":
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
          show_embeds: true,
          show_presence_events: true,
          collapse_presence_events: false,
          pinned: false,
        });
        view.renderSidebar();
        break;
      }
      case "buffer_update":
      case "buffer_settings":
        view.updateBuffer(m, { rerenderActive: m.type === "buffer_settings" });
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
        view.prependHistory(m);
        break;
      case "preview":
        view.patchPreview(m);
        break;
      case "member_list":
        view.setMembers(m.buffer_id, m.members || []);
        break;
      case "channel_list":
        if (applyChannelListUpdate(m)) view.renderActiveView();
        break;
      case "ignorelist_result":
        console.log("ignore list:", m.masks);
        break;
    }
  };
}
