# Frontend architecture

Frontend lives in `web/` and is intentionally simple.

Current characteristics:

- Vite + TypeScript
- mostly single-file app logic in `web/src/main.ts`
- local UI state in memory plus some localStorage-backed layout preferences
- server is source of truth for networks, buffers, messages, and read state

## Source-of-truth rules

Server-backed state:

- networks (including `disabled` flag)
- network ordering via `sort_order`
- buffers
- buffer settings (`show_embeds`, `show_presence_events`, `collapse_presence_events`, `pinned`)
- messages
- read state
- member lists

Client-only persisted layout state:

- collapsed sections
- sidebar visibility
- members drawer state

Important invariants:

- network ordering is no longer a frontend-only preference; it is shared persistent state stored in the control DB
- active channels honor the backend's manual `(sort_order, name)` ordering; `buffer_reorder` broadcasts update that order live, while queries and archived buffers remain alphabetical
- `pinned` and `pin_order` are stored server-side in `buffer_settings` so they follow the user across browsers/devices; the Pinned section sorts `(pin_order, name)` and pinned channels stay listed under their network group as well
- sidebar drag-and-drop (`src/sidebar-dnd.ts`) covers two independent drags, tracked in separate `state.drag` (networks) / `state.pinDrag` (pinned rows) slots so neither highlights the other's targets. Both share one implementation (`attachReorderDragHandlers` / `endDropZone`), draw a 2px accent insertion bar on the hovered target, and use strict insert-before semantics plus an always-present end-of-list strip (`.sb-net-end` / `.sb-pin-end`) so the last slot is reachable — the bar is therefore always where the dragged item lands. Networks POST the ordered id list to `/api/networks/reorder` and apply `sort_order` optimistically; pinned drops POST the full ordered id list to `/api/buffers/pinned/reorder`, applies `pin_order` optimistically, and rolls back if the request fails (e.g. 404 against a backend predating the endpoint). The server's `pinned_reorder` broadcast then confirms or corrects the order
- other buffer settings (`show_embeds`, `show_presence_events`, `collapse_presence_events`) are also server-persisted

## Hydration model

On load:

1. fetch `/api/state`
2. populate maps for networks, buffers (including settings fields), messages, and members
3. take server-derived read state per buffer verbatim (`unread`, `mentions`, `marker_id`, `marker_ts` — see `behaviors/new-messages-marker.md`)
4. restore sidebar visibility from localStorage
5. connect WebSocket stream
6. apply incoming events incrementally (including `buffer_settings` for live settings sync)

Buffer settings are mutated via `PATCH /api/buffers/{id}/settings` and the server publishes a `buffer_settings` hub event so all open clients stay in sync.

See [rest-api.md](rest-api.md) for `/api/state` and [websocket-protocol.md](websocket-protocol.md) for incremental events.

## Rendering model

WebSocket events route through one named view helper per event in `app-core.ts` `handleWSMessage`. The view helper lives on the object returned by `createAppView` (`web/src/app-view.ts`) and owns DOM dispatch and scroll preservation. `mark_read` is sent only on explicit user ack (Esc / unread-bar click via `ackBufferRead` in `read-tracker.ts`), never as an event follow-up. State mutators stay in `messages.ts` / `channel-list.ts` and are called only from inside the view helper.

| Event | View helper | DOM strategy |
| --- | --- | --- |
| `message` | `view.appendMessage(msg)` | full rerender of active buffer (drives day separators, presence collapse, unread bar) |
| `history_result` | `view.prependHistory(msg)` | full rerender of active buffer with scroll-offset preservation |
| `preview` | `view.patchPreview(msg)` | targeted DOM patch on the matching row; no rerender |
| `buffer_update` / `buffer_settings` | `view.updateBuffer(msg, { rerenderActive })` | header + sidebar refresh; full rerender on settings change and on updates to the active buffer (remote ack must clear its divider + unread bar live) |
| `member_list` | `view.setMembers(bufferId, members)` | header + member pane rerender when buffer is active |
| `channel_list` | `view.renderChannelList()` | full rerender; `renderActiveView` dispatches to the channel-list panel branch when `state.channelList?.done` is set |

`renderActiveView` is the single entry point for full rerenders. It picks one of three branches by state: status pane, channel-list panel, or message list. The channel-list panel is not a special case in `app-core.ts` — it lives inside `renderActiveView` like any other active-buffer view.

The only DOM patch path is `patchPreview`. New strategies must either add a named view helper or fall through `renderActiveView`; do not call `messagesEl` mutations from `app-core.ts`.

## Theming

UI themes are YAML files loaded by the backend and applied client-side via CSS
custom properties. See [theming.md](theming.md) for the model and how to add one.

## Interaction specs

- [keyboard-shortcuts.md](keyboard-shortcuts.md) defines the v1 keyboard shortcut set and channel switcher behavior.
