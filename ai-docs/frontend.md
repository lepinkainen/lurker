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
- per-buffer options (pin, embeds, presence, archive, delete) are edited via the topicbar gear button (`#buffer-options-btn`, hidden for status buffers), which opens a small `nf-dialog` for the active buffer (`sidebar.ts` `openBufferOptions`); sidebar rows carry no inline controls

## Settings view

`settings-dialog.ts` `openSettingsView()` renders an IRCCloud-style in-pane
settings surface: an opaque overlay covering `#main` (sidebar stays
interactive; covered `#main` children get `inert`; the member pane is hidden
via inline `style.display` so unlayered `mobile.css` can't override it). Left
nav + content pane; categories come from a `SETTINGS_CATEGORIES` registry
(`{id, label, build}`) — currently General (highlight words, server info),
Appearance (theme picker), Media library (inline media browser from
`media-browser.ts` `buildMediaBrowser`), Config file sync (auto-fetched
side-by-side diff + save). Panels are built once per open and cached
(`replaceChildren` swap) so tab switches don't refetch or duplicate listeners;
teardown callbacks registered via `SettingsViewHandle.onClose` run at close.
Toggled by the sidebar footer gear; closes via Close button or capture-phase
`Esc` (which backs off while a real `<dialog>` is stacked on top). On ≤640px
the nav becomes a horizontal tab strip (`mobile.css`).

## Image attachment

- `src/input-upload.ts` handles composer image attach: a paperclip button (`uploadButtonEl` → hidden `<input type=file>`), drag&drop onto the input form, and paste. `uploadFile` POSTs `multipart/form-data` (field `file`) to `/api/upload`; the returned `url` is inserted at the caret via `insertTextAtCursor`, ready to send as a normal message.
- Paste is bound on `document`, not on the composer input: iOS Safari does not reliably deliver an image paste to a plain `<input type=text>`, and the photo is often pasted while focus sits elsewhere. Composer pastes bubble to the same listener, so there is exactly one handler and no double upload. `clipboardImage` takes the first `image/*` from `clipboardData.files`, falling back to `clipboardData.items` (Safari can leave `files` empty). Pastes into any other text field, pastes while the composer is disabled, and pastes that also carry non-empty `text/plain` (rich text drags images along; copying an image off a web page attaches its source URL) are left alone as ordinary text pastes.
- `uploadFilename` synthesizes `pasted-<ts>.<ext>` when the clipboard file has no name (Safari) — the server rejects an empty multipart filename.
- The file input's `accept` lists the stored image types explicitly rather than `image/*`, which makes iOS Safari transcode HEIC photos to JPEG on pick; the backend has no HEIC decoder.
- The button is disabled and the form gets a `.uploading` class during the request; drag hover toggles `.upload-dragover`. Progress and failures also show in a `.upload-note` above the composer (errors clear after 8s) — an upload has no other visible result until the URL lands, and on a phone the console is out of reach. Server-side optimization/validation and the URL shape are documented in [rest-api.md](rest-api.md#post-apiupload).

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

Nick avatars (the small identicon square next to a nick) are a deterministic,
client-agnostic algorithm — see [nick-identicon.md](nick-identicon.md) for the
spec every conforming client must reproduce exactly.

## Interaction specs

- [keyboard-shortcuts.md](keyboard-shortcuts.md) defines the v1 keyboard shortcut set and channel switcher behavior.
