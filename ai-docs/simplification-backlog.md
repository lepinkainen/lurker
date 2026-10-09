# Simplification backlog

Repo-wide over-engineering audit, 2026-08-06; re-audited 2026-09-21. Grouped by implementation scope. Backend sections are subsystems of the same Go service, not separate deployed services. Correctness/security/perf are outside this audit's scope.

Tags: `delete` dead code / speculative feature · `stdlib` hand-rolled stdlib · `native` platform already does it · `yagni` abstraction with one implementation · `shrink` same logic, fewer lines.

Items marked *2026-09-21* came from the second audit; unmarked items came from the first. Estimates and findings are preserved from those audits and have not been revalidated as part of this regrouping. Shared cleanup bundles retain their combined estimates.

**Not on this list (deliberate design decisions, see ARCHITECTURE.md):** the TUI as a third client, and the S3 media backend. Do not re-propose.

| Scope | Locations and work |
| --- | --- |
| [Backend only](#backend-only) | API, IRC, Bluesky, database, media, previews, updates, configuration, and theme loading |
| [Web client](#web-client) | TypeScript, CSS, browser UI, and web test fixtures |
| [Native Apple client](#native-apple-client) | SwiftUI, models, transport, and Apple tests |
| [Terminal client](#terminal-client) | TUI configuration and rendering |
| [Web and Tauri desktop shell](#web-and-tauri-desktop-shell) | Upload precheck in both TypeScript and Rust |
| [Shared backend and frontend work](#shared-backend-and-frontend-work) | Netsplits, shared Go helpers, and cleanup bundles that span clients and server |
| [Build, dependencies, and development tools](#build-dependencies-and-development-tools) | Generators, dependencies, Taskfile, CI, and seed fixtures |

## Backend only

These proposals target server code and its tests. Theme loading belongs here even though clients consume the resulting themes.

### API and WebSocket commands

- [ ] `yagni` api: 7 interfaces, 1 implementation (`*irc.Manager`) — `manager` = `wsManager`(=`messageSender`+`channelOps`+`presenceOps`+`modeOps`) + `stateManager` + `networkManager`; sub-interfaces exist only for per-slice test mocks. Collapse to one interface, one mock struct. `api/server.go:23-27`, `api/ws.go:82-126`, `api/state.go:80-85`, `api/networks.go:36-40`. ~245 lines incl tests.
- [ ] `shrink` api `cmdIgnore`≡`cmdMute` (level differs), `cmdUnignore`≡`cmdUnmute` (byte-identical bar error string). `cmdSetIgnore(ctx,c,cmd,level)` / `cmdRemoveIgnore`. `api/ws.go:833-889`. ~26 *2026-09-21*
- [ ] `shrink` api 8× `if s.Hub != nil { s.Hub.Publish(x) }` while nil-safe `s.publish` exists (used 4×). `api/buffer_delete.go:50,76`, `buffer_reorder.go:55`, `buffer_settings.go:70,105`, `highlights.go:59`, `ws.go:474`. ~16 *2026-09-21*
- [ ] `yagni` api `clientCmd.internal` flag + second kind-gate in `cmdInput`, exists to save one `LookupBuffer` on re-dispatch. Delete flag and `ws.go:372-382`; let `handleCmd` gate. `api/ws.go:33-37,257`. ~15 *2026-09-21*

### IRC connections and handlers

- [ ] `yagni` irc 17 copy-pasted `Manager.<Cmd>` methods (Send/Join/Part/ChangeNick/Me/Topic/Whois/Invite/Kick/Mode/Away/Back/Quit/Rejoin/Notice/CTCP/ListChannels/Raw), each lock→`m.conn[id]`→`IsConnected`→`ErrNotConnected`. One `withConn(id, func(*girc.Client) error) error`; each command 1–3 lines. `irc/manager.go:222-496`. ~100 *2026-09-21*
- [ ] `yagni` `userChannels` dual index (`byChannel` reverse map, `purgeStale`, `applyChannelMembership`, `foldNickSet`) — "O(K) not O(N)" for a single-user bouncer. Keep `byNick`; `dropChannel` iterates it; `channelsFor` = `slices.Sorted(maps.Keys(s))`. `irc/user_channels.go`. ~90 *2026-09-21*
- [ ] `yagni` irc handler: 9 closure seams (`connectedHook nickFn memberListHook clearMemberListHook setJoinedHook drainJoinedHook hasCap sendRaw historyLimit`) all wired from one call site, nil-guarded at ~14 sites. Store `mgr *Manager` + `client *girc.Client`, call methods. `irc/handler.go:24-29`, `irc/manager.go:809-850`. ~60 lines.
- [ ] `yagni` `tls_max_version` YAML knob — never set; automatic TLS-1.2 retry at `irc/manager.go:680` covers legacy ircds. Drop field, `parseTLSMaxVersion`, `tlsMaxVersionLabel`; keep internal `ServerConfig.TLSMaxVersion` for the fallback. `config.go:419-430`. ~40 lines.
- [ ] `delete` `isExplicitlyHandledEvent` — hand-copied second list of `register()`'s 30 handler keys; build a map inside `register()`. `irc/handler_register.go:88-124`. ~35
- [ ] `stdlib` FNV member-list change hashing → store previous `[]ChannelUser`, `slices.Equal`. `irc/handler_presence.go:168-208`. ~32
- [ ] `native` `caseFoldNick` reimplements `girc.ToRFC1459` byte-for-byte. Replace all call sites. `irc/casemap.go`. ~30 *2026-09-21*

### Bluesky ingestion

- [ ] `yagni` `datasource.Source` interface + `datasource.Manager` — one implementation (bluesky); Manager is Register/Start/Wait/Names over a one-element slice; `Post.Target` always `""`. Construct `bluesky.Source` directly in `main.go:171-200`. ~130 lines.
- [ ] `delete` bluesky `uriLRU` + `parentCache` — DB unique index `(buffer_id, msgid)` already dedupes on insert (`inserted=false`); parentCache only needs to survive one poll → plain map in `pollOnce`. `datasource/bluesky/source.go:527-627`. ~100 lines, drops `container/list`.
- [ ] `delete` bluesky reserved channel kinds (`ChannelSearch/List/Feed/Notifications`) parse only to error "reserved"; `ChannelConfig.Query`/`.URI` never read; `defaultInterval` ignores its param. `datasource/bluesky/channel.go:11-58`, `config.go:399-404`. ~50 lines.

### Database and storage

- [ ] `yagni` db three Network row mappers over three near-identical queries (`GetNetwork`, `ListNetworks`, `ListNetworksWithSASL`). Same columns, one `networkFromRow`, drop `ListNetworksWithSASL` (filter `!Disabled` in main); API DTO already strips SASL. `db/control.go:26`, `db/network_store.go:56,135`, `db/control_queries/networks.sql:37`. ~45 *2026-09-21*
- [ ] `delete` unused sqlc queries `InsertMessagePreviewLink` (hand-written raw insert used instead) and `DeleteURLPreviewsBefore` (no callers). Delete `db/log_queries/previews.sql`, the query in `db/preview_queries/url_previews.sql:25`, `task generate`. ~45 incl generated *2026-09-21*
- [ ] `shrink` db `ReorderNetworks` reimplements `assignDenseOrder` validation by hand; `ReorderNetworkBuffers`/`ReorderPinnedBuffers` re-SELECT after writing just to build the result. Reuse `assignDenseOrder` (+ `len(ids)==len(all)` check), collect entries inside the `setOrder` callback. `db/control.go:169`, `db/buffer_registry.go:71`, `db/buffer_settings.go:225`. ~35 *2026-09-21*
- [ ] `shrink` db duplicate helpers: `nullStr`==`nullableString`, `parseFetchedAt`==`parseMediaTime`, `boolInt` re-inlined at `control.go:63,120`. ~23
- [ ] `shrink` `recentRowsToMessages`/`beforeRowsToMessages` byte-identical over two sqlc row types → one generic or unified query columns. `db/logstore.go:177-214`. ~22
- [ ] `yagni` db `OpenControl`/`OpenLog`/`OpenPreviews`/`OpenMedia` one-line wrappers, one prod caller each; `OpenLogStore` wraps `OpenLog`. Call `openAndMigrate(path, fs, dir)` from store constructors. `db/db.go:31-52`, `db/logstore.go:26`. ~22 *2026-09-21*
- [ ] `shrink` db `ensureBufferLogRow`/`insertBufferLogRow` share a 12-line "parse id, compare, copy topic/lastseen/createdAt" block; `ensureBufferRegistryRow` duplicates its found-branch in the insert-race path. `adoptLogRow(row, buf)` / `adoptRegistryRow(row, buf)`. `db/multistore.go:327-440`. ~22 *2026-09-21*
- [ ] `shrink` db `applyBufferSettings` (EnsureBuffer) and `networkBuffers` (ListAllBuffers) hand-apply settings→Buffer with the same default/status-override rules → one `applySettings(buf, s, ok)`; `scanLogMessageRows` callback helper has one caller → inline. `db/multistore.go:442-460,519-532`, `db/shared_store.go:57-68`. ~20 *2026-09-21*
- [ ] `stdlib` `MultiStore.Close` firstErr ladder → `errors.Join(…)` (Close methods already nil-safe). `db/multistore.go:70-95`. ~15 *2026-09-21*
- [ ] `yagni` `PreviewStore.Now` injectable clock exists for one test; test TTL by `Put` with an old `FetchedAt`. `db/preview_store.go:45-56`, `db/preview_store_test.go:44`. ~10 *2026-09-21*
- [ ] `native` `MediaStore.List` runs COUNT then SELECT with the same WHERE → one query with `COUNT(*) OVER() AS total`. `db/media_store.go:150-167`. ~8 *2026-09-21*

### Media uploads and object storage

- [ ] `yagni` `Media*FileConfig` mirror structs + manual copy loop → yaml tags on the real structs (as `PreviewConfig` does). `config.go:63-124,274-320`. ~40 lines.
- [ ] `delete` media `classifyMedia`/`mediaKind*` + 512-byte sniff + video branch ("reserved scaffold"; comment admits `optimizeImage` is the real gate). Let decode reject → 415. `media/transcode.go:119-151`, `media/upload.go:52-65`. ~40 *2026-09-21*
- [ ] `yagni` `Blobs.Exists` — `Put` already contracts `os.ErrExist`/never-overwrite (O_EXCL on disk, objstore) and `allocateAndStore` already `continue`s on it. Drop from interface, `DiskBlobs`, `objstore.Client`, `fakeBlobs`. `media/blobs.go:19-20,75-90`, `media/upload.go:248-253`, `internal/objstore/objstore.go:141`. ~30 *2026-09-21*
- [ ] `yagni` `objstore.New` re-runs the identical required-field table + `"://"` scheme check that `buildMediaConfig` just did; `Client.Bucket()` has no callers. Validate once. `internal/objstore/objstore.go:58-76,102`, `config.go:491-559`. ~25 (on top of the Media mirror-struct item above) *2026-09-21*
- [ ] `yagni` media `Variant` slice "currently always a single main variant", read via `Variants[0]` everywhere. Flatten `Key` into `Media`; drop JSON `variants` column. Schema change. `media/store.go:14-23`, `media/upload.go`, `db/media_store.go`. ~25 in media, more in db *2026-09-21*
- [ ] `stdlib` `newMediaID` base62 + rejection sampling → `crypto/rand.Text()`. `media/upload.go:290-312`. ~24
- [ ] fix stale comment: `media/store.go:40-41` claims Q/Kind unimplemented; `db/media_store.go:144` implements both.

### URL previews

- [ ] `shrink` preview `CheckURL` resolves DNS then discards result — `pinningDialContext` re-resolves and enforces on every dial anyway. Reduce to scheme/port/IP-literal checks. `preview/ssrf.go:44-58`. ~20 + 1 DNS lookup per URL
- [ ] `stdlib` preview `collapseWS` → `strings.Join(strings.Fields(line), " ")` per line; `truncateRunes` → `r := []rune(s); if len(r) > n { … }`. `preview/fediverse.go:126-162`. ~25 *2026-09-21*
- [ ] `yagni` preview `classifyHead` HEAD probe — extra round trip; the GET path classifies by Content-Type and `LimitReader(MaxBytes)` bounds the body anyway. `preview/fetcher.go:96-97,150-164`. ~20 *2026-09-21*

### Update checker

- [ ] `delete` updates/ dead flexibility: `Config.Repo`/`Config.Logger` never set outside package → const + `slog`; `BuildInfo{Commit}` one-field struct → `string`; `CheckNow` test-only → tests call `check`; `clampUpdateInterval` in `config.go:843-851` duplicates the clamp in `updates.New`. `updates/updates.go:14-95`, `main.go:91`. ~40 *2026-09-21*
- [ ] `delete` update-check log dedupe (`lastLoggedUpdate`/`lastErrorLoggedAt`/`lastErrorMessage` + `logTransition`/`logError`): interval floor is 1h so the "<1h same error" suppression never fires from the ticker. Log directly in `check`. `updates/updates.go:46-49,179-202`. ~30 *2026-09-21*

### Configuration, themes, and backend helpers

- [ ] `native` theme/ dir loading + `THEMES_DIR` env + runtime re-read for 6 static YAMLs → `//go:embed themes/*.yaml`. `theme/theme.go`, `main.go:85`. ~60 lines.
- [ ] `yagni` Go pure-delegation wrappers: `stateString`, `markNonYAMLNetworksDisabled`, `handler.publishNetworkState`/`publishBufferCreated`, `closeFunc`, `botTracker.set`, `parsedConfig`, `internal/closeutil` (5-line func, own package, 3 callers). Inline. ~50
- [ ] `stdlib` `cmp.Or`: `firstNonEmpty(...)`, `envOr(k,d)` → `cmp.Or(os.Getenv(k), d)`, four `if pv.X > 0 { cfg.Previews.X = pv.X }` → `cmp.Or(pv.X, cfg.Previews.X)`; `absInt` + nearest-size loop → `max(n-sz, sz-n)`. `config.go:431-445,686-693,812-817`, `api/avatar.go:143-162`. ~30 *2026-09-21*
- [ ] `stdlib` irc/bluesky misc: `drainJoined` → `slices.Collect(maps.Keys(channels))`; `StateSnapshot` → `maps.Clone(m.state)`; `sleepCtx` timer/Stop dance → `select { case <-ctx.Done(): …; case <-time.After(d): … }` (no leak since Go 1.23). `irc/manager.go:516-522,611-626`, `datasource/bluesky/source.go:~455-468`. ~15 *2026-09-21*

## Frontends

### Web client

- [ ] `shrink` web autocomplete: `emoji-autocomplete.ts` and `nick-autocomplete.ts` are the same controller (state/tokenAtCursor/updatePop/replaceToken/handleKey/highlightRow/initPop/blur-timeout) with different match/render. One `createPopupController({match, render, replace, rowClass})` + two ~20-line configs. `web/src/emoji-autocomplete.ts`, `web/src/nick-autocomplete.ts`. ~110 *2026-09-21*
- [ ] `yagni` web `connection.ts` DI type tower — 9 type aliases (`Renderer`, `Navigation`, `Transport`, `*Deps`) describing one wiring built once in `app-core.ts:88-107`; `createConnection` re-shreds its own deps into `Pick<>` subsets. Pass `AppView` + `sendCmd` directly. `web/src/connection.ts:19-79,161-193`. ~75 lines.
- [ ] `shrink` web three button families `.nf-btn*`/`.sd-btn*`/`.mb-btn*` (18 rule blocks, same colors/padding/hover/disabled; `.mb-btn-ghost` unused). One `.btn` + `-primary/-secondary/-ghost/-danger`. `web/src/styles/dialogs.css:134-172,369-410,644-668`. ~60 *2026-09-21*
- [ ] `native` web hand-rolled popup positioning + dismiss → Popover API (`popover="auto"` gives light dismiss + Esc + top layer) + CSS anchor positioning. `web/src/user-popup.ts:380-422`. ~55 lines. Conservative variant: popover only, keep JS positioning (~30).
- [ ] `yagni` web `sidebar-model.ts` — view-model layer with one consumer (`renderSidebar`); 3 types describing one immediately-destructured object. Fold into `sidebar.ts`. ~55 lines.
- [ ] `shrink` web `showInviteForm`/`showIgnoreForm` same 50-line inline form, different strings. One `inlineForm(h, {label, value, placeholder, submitLabel, onSubmit})`. `web/src/user-popup.ts:246-348`. ~45 *2026-09-21*
- [ ] `yagni` web `create*` factory/thunk layer — `createSetActive({getDom: () => dom})` closures over module-level state in the same file, read back through getter thunks, one caller each. Plain module functions. `web/src/app-core.ts:30-38`, `active-buffer.ts`, `read-tracker.ts`, `scroll-stick.ts`. ~40 lines.
- [ ] `shrink` web `THEME_VARS` 31-entry reset array → `root.style.cssText = ""` (`theme.ts` is sole writer of `documentElement.style`). `web/src/theme.ts:18-53`. ~35
- [ ] `delete` web `dialog.ts` + `keyboard-dialogs.ts` — two parallel `<dialog>` helpers with identical backdrop-close listener → one function, two optional args. ~25
- [ ] `shrink` web five `msgCountsAsUnread/msgMuted/msgMentionsMe/msgHighlight/msgIsSelf` wrappers are `x.field === true`; only `msgDisplayKind` does work. Inline. `web/src/messages.ts:19-40`. ~20 *2026-09-21*
- [ ] `yagni` web media browser `kind` `<select>` with exactly one non-"All" option (`image`) + `itemCount()` wrapping a local var. Pass `kind=image`. `web/src/media-browser.ts:100-111,138-140`. ~18 *2026-09-21*
- [ ] `stdlib` web `formatTime` → `toLocaleTimeString`, `dayKeyOf` → `toDateString()` (already used in `daySeparator`), today/yesterday → `Intl.RelativeTimeFormat` (pattern in `settings-dialog.ts:16`). `web/src/format.ts:59-73`, `messages.ts:718-742`. ~17
- [ ] `delete` web dead: `closeAllDrawers` (`ui-shell.ts:27`), `applyThemeDefaults` re-setting hardcoded density, `data-density="balanced|comfortable"` CSS never set, `.flash` class, `ignorelist_result` console.log-only case + union member + `/ignorelist` command (surface or cut), 20 needless `export` keywords. ~33
- [ ] `delete` web hand-copied `index.html` test fixture, already drifted (missing SVG sprite, `data-accent`). `import html from "../index.html?raw"`. `web/tests/fixture-index.ts`. ~80 *2026-09-21*
- [ ] `delete` stale screenshot fixtures: no `toMatchScreenshot` callers, folder named after a test file that no longer exists (`main.ui.test.ts` → `main.ui.integration.test.ts`). `web/tests/__screenshots__/main.ui.test.ts/*.png`. 3 files, 140 KB *2026-09-21*

### Native Apple client

- [ ] `native` Apple sidebar drag-and-drop stack — `SidebarDropDelegate`, drag-cancel detection polling `NSEvent.pressedMouseButtons` every 250ms, hand-rolled `SidebarOrdering.moving`, six near-identical drag predicates, three near-identical `commitXDrop`. Replace with `List` + `.onMove`, or `.draggable`/`.dropDestination` + `Transferable`; `Array.move(fromOffsets:toOffset:)` is stdlib. `apple/Lurker/SidebarView.swift:6-548`. ~400 lines incl 16 test cases.
- [ ] `shrink` Apple misc: `command→send`, `previewImageURL`/`inlineImageURL`→`normalizedImageURL`, `isPresence`, `SidebarBufferGroups.all`, `EmojiCatalog` struct for one dict, `CachedAsyncImage` third generic (one user), `AnyShapeStyle` triple-wrap → `HierarchicalShapeStyle`, `nickHues` 48-element table → `i%48*7.5`, dead `ISO8601DateFormatter` fallback in `parseTimestamp`, `FixtureTransport.buildFullMessages` 10 templates → 3, `NetworkHeaderRow` duplicate action params + TODO menu, write-only `ClientCommand.before/.limit/.reqID` correlation channel, `toggleSidebar()` (unreferenced; `SidebarCommands()` covers it), `ChannelSwitcher.results` subsumed filter clauses, `ComposerPopupHeightKey` PreferenceKey → `.onGeometryChange`. ~180
- [ ] `shrink` Apple `Network`/`Buffer` hand-written `CodingKeys` + memberwise inits — byte-identical to synthesized; move `init(from:)` to an extension and synthesis returns. `apple/Lurker/Models.swift:39-69,119-165`. ~80 lines.
- [ ] `delete` Apple `AppModel.previewSidebar()` — 70-line fixture feeding one `#Preview`; `FixtureTransport.snapshot()` already covers it. `apple/Lurker/AppModel.swift:1037-1106`. ~75 lines.
- [ ] `native` Apple `LurkerCodingKey` + `WireKeyTransform` — reimplements snake_case; Foundation ships `.convertFromSnakeCase`/`.convertToSnakeCase`; rename 10 props `bufferID`→`bufferId`. `apple/Lurker/Models.swift:483-556`. ~55 lines.
- [ ] `delete` Apple `SidebarBufferOccurrence` wrapper — carries no data, only a composite ForEach id; `ForEach(buffers, id: \.id)`. `apple/Lurker/SidebarView.swift:40-57`. ~45
- [ ] `yagni` Apple `HistoryStubTransport` — 9 of 11 `LurkerTransport` methods `throw Unsupported()`; add cursor recording to `FixtureTransport` instead. `apple/LurkerTests/AppModelTests.swift:1141-1178`. ~38 lines.
- [ ] `shrink` Apple `LurkerAPI` 4× repeated POST-JSON-decode → one generic `post<B,T>`; collapse `get`→`request`→`requestJSON` chain. `apple/Lurker/LurkerAPI.swift:120-151,276-296`. ~30

### Terminal client

- [ ] `yagni` TUI YAML config file with one key + 3-location candidate search, for a value already settable via `-url`. `flag.String("url", cmp.Or(os.Getenv("LURKER_URL"), "http://localhost:8080"), …)`; drop `Config` + yaml import. `cmd/tui/config.go`, `cmd/tui/main.go:12`. ~45 *2026-09-21*
- [ ] `shrink` TUI `formatTS(m.TS)` re-parses RFC3339 per render though `messageDTO.TSParsed` is cached on every ingest path → `formatTSTime(m.TSParsed)`; `markerLine`/`renderUnreadBar` share centered-divider code → `divider(label, width)`. `cmd/tui/model_render.go:96-105,142-147,205-216,380-386`. ~18 *2026-09-21*
- [ ] `shrink` TUI `pinnedSidebarItems` re-implements `firstPinnedChannel` (enabled map + filter + PinOrder/name sort) and `networkLookup`. `sortedPinned(buffers, networks)`. `cmd/tui/sidebar.go:13-32`, `cmd/tui/state.go:120-137`. ~15 *2026-09-21*

### Web and Tauri desktop shell

This is client-side work in two clients; the existing server-side deduplication remains in place.

- [ ] `yagni` client-side upload dedup precheck (SHA-256 + `GET /api/media/exists`) in web and Tauri; server already dedups by source hash (`media/upload.go:68-76`). Only saves bandwidth for >1 MiB files on a private tailnet. Drop `checkExisting`/`sha256Hex`, Rust `existing_url`/`sha256_hex`/`PRECHECK_MIN_BYTES`, `sha2` crate. `web/src/input-upload.ts:27-53`, `desktop/src/main.rs:270-332`. ~55, −1 crate *2026-09-21*

## Shared backend and frontend work

### Backend and terminal client

These items touch both server and TUI code, or shared packages consumed by both. The miscellaneous Go item also includes backend-only substitutions.

- [ ] `delete` Netsplit clustering exists twice: batch `GroupPresence`/`nsCluster`/`PresenceGroup`/`NetsplitGroup` vs live incremental `netsplitTracker`, hand-kept-in-sync and pinned by a contract test. Keep the live tracker (already stamps `MessageCore.Netsplit`), persist/serve that annotation, drop the batch path. `irc/netsplit.go:63-250`, `api/state.go:314-348`, `cmd/tui/model_render.go` (`groupAndFormatMessages`). ~450 lines incl tests.
- [ ] `stdlib` `sort.Slice` in ~10 files → `slices.SortFunc` + `cmp.Or`; tui hand-rolled insertion sort → `slices.SortStableFunc`. `cmd/tui/switcher.go:50-62`, `theme/theme.go:60`, `cmd/tui/state.go:121,152,158`, `irc/*`, `db/db.go`. ~50
- [ ] `shrink` duplicate URL regex byte-identical in `preview/extract.go:9` and `cmd/tui/model_input.go` (`urlRe`) — export one.
- [ ] `delete` `internal/httpjson` unused surface (package itself stays — see verified-clean): `Request.Header`, `Client.MaxBytes`, `Response.Header`, `Error.Header`, exported `Do`. All three users call `DoJSON` with `URL/Method/Body/Authorization` only. Unexport `Do` → `[]byte`, drop `Response`. `internal/httpjson/httpjson.go:34-49,84-99`. ~25 + ~40 test *2026-09-21*
- [ ] `stdlib` misc Go: `fmt.Sscanf`→`strconv.Atoi` (`irc/handler_list.go:16`), `envVarRE`→`strings.CutPrefix`/`CutSuffix` (`config.go:345-357`), migration listing → `fs.Glob` (`db/db.go:93-108`), `splitFields`→`strings.Fields`, `splitFirstSpace`→`strings.Cut` (`api/input.go:145-171`), `nickcolor` hexByte→`fmt.Sprintf("#%02x%02x%02x")`, `sha256HexRe`→`hex.DecodeString` len check (`media/browse.go:16`), `trimTrailing`→`strings.TrimRight` (`preview/extract.go:38-51`), `ensureParentDir` `"."` guard. ~55
- [ ] `stdlib` `placeholders` byte-building ×3 → `strings.TrimSuffix(strings.Repeat("?,", n), ",")`; TUI `removeBuffer` index loop → `slices.IndexFunc` + `slices.Delete`; `overlay` dead `bgLine`/`_ = bgLine`, unused `pw`/`ph` clamps. `db/network_store.go:103-109`, `db/logstore.go:403-407`, `db/preview_store.go:157-165`, `cmd/tui/model_events.go:305-316`, `cmd/tui/switcher.go:110-141`. ~26 *2026-09-21*

### Backend, web, and terminal cleanup bundles

The original audit combined unrelated edits in these bullets. In particular, the “web small files” item also includes an API dispatcher change. Scope each part by its listed path.

- [ ] `yagni` web small files: `main.ts`/`bootstrap.ts` re-export chain (3 files, 2 ≤6 lines), `navigation.ts` 9-line wrapper, `alias()` one caller (`slash-commands.ts:23-37`), `handleBufferLifecycleCmd` reachable only from identical switch's default (`api/ws.go:294-311`). ~45
- [ ] `delete` write-only / never-read fields: `LogStore.NetworkID`; `LogMessageInput.Raw` (written to `messages.raw` on every insert, never SELECTed — stop populating, column can stay NULL); `GetLogBufferTopicLastSeen` selects `topic` that `LogBufferLastSeen` discards; `previewLinkRef.position`; `Config.ControlDBPath` (feeds one log line; `OpenMultiStore` derives its own); bluesky `Client.did`; `GetTimeline` `cursor` param only ever `""` outside tests; TUI DTO fields `networkDTO.Status`, `channelMember.Realname`/`Self`, `wsEvent.Channel`/`ReqID`/`CreatedAt`/`ShowEmbeds`; web `LayoutSettings.pinned` (pinning is server-side), `SettingsViewHandle.root` + `_handle` params, `SetActive` type re-declared in `navigation.ts` vs `active-buffer.ts`; `--themes-dir` flag (env var used everywhere, flag passed nowhere). `db/logstore.go:20,59,263`, `api/state.go:424,463`, `config.go:22,179`, `datasource/bluesky/client.go:39,81,107,114`, `cmd/tui/types.go:16,66-67,128-144`, `web/src/app-state.ts:5,199`, `web/src/settings-dialog.ts:164,315,409`, `main.go:30,81-84`. ~50 *2026-09-21*
- [ ] `shrink` small dups: `onWhoisKeyValue`/`onKeyValue` both call `applyKeyValueNumeric` → give it the handler signature, register for 760/761 directly (`irc/handler_metadata.go:56-74`); TUI `connectWSCmd`≡`reconnectWSCmd` minus `time.Sleep` (`cmd/tui/model.go:176-202`); web `buffer-settings-api.ts` hand-rolls fetch/headers/error while `http.ts` `sendJSON` exists → `sendJSON(\`/api/buffers/${id}/settings\`, "PATCH", patch)`; four `sidebar-dnd.ts` one-line exports just bind `state.drag`/`state.pinDrag` → call `attachReorderDragHandlers` directly (`web/src/sidebar-dnd.ts:10,86-100`); `isBotNick`/`hasAvatarFor`/`avatarUrlFor` each re-derive `activeBuffer()?.network_id` (`web/src/nick-colors.ts:59-101`); `formatUnhandledEventContent` tail → three early returns (`irc/handler_unhandled.go:72-81`); `avatarCache` nil-receiver guards exist only because `avatar_test.go` skips `Handler()` → set `avatarCache: newAvatarCache()` in the fixture (`api/avatar.go:53-55,66-68`); `sidebar-model.pinnedBuffers()` / `sidebar.rerender()` wrap one call each. ~60 *2026-09-21*

## Build, dependencies, and development tools

Verified against the repository on 2026-09-21. Five proposals were confirmed; the dependency proposal was only partly correct.

- [x] `delete` Removed `web/scripts/generate-icons.mjs`. It had no callers; `scripts/gen-icons.sh` already generates the committed web icons.
- [x] `delete` Removed the direct `@vitest/browser` dependency. Both Vitest configs import `@vitest/browser-playwright`, which depends on `@vitest/browser`. **Correction:** retained `stylelint` and `stylelint-config-standard`; both lint scripts run Stylelint, and its CSS conventions are not configured in Biome. Coverage removal is covered below.
- [x] `stdlib` Replaced the generated hue table with `nickHue()` in `web/src/format.ts`, shared by nick labels and identicons. Removed `scripts/gen-nick-palette.mjs`, `web/src/nick-palette.ts`, and `task gen-palette`. The 48 values remain `i * 7.5`.
- [x] `yagni` Removed icon/palette dependencies from `web-build` and the CI build job's renderer installation. Web builds now use committed icons, matching Docker. Run `task icons` after changing the SVG and commit the images. **Correction:** `icons-web` was fingerprinted, so it did not regenerate on every build. Desktop and Apple icon task dependencies remain.
- [x] `delete` Removed unreferenced Taskfile wrappers `test-web-all` and `build-backend`, plus the unused `test:coverage` script, Vitest coverage configuration, and direct `@vitest/coverage-v8` dependency. CI's Go coverage stays. Also dropped the `test:all` npm script; `task test-web` and `task test-web-integration` cover both halves.
- [x] `shrink` Simplified `cmd/seedtest`: removed `resolveLocalBuffer` and the unused `Members` field, create each status buffer through `seedStatus` only, and share one `fixture()` result between seeding and config generation.

## Completed

- [x] **updates/** — simplified, not deleted (2026-08-06): OCI registry dance (token challenge, manifest index, platform select, config blob; 4 HTTP calls) replaced with one GitHub Actions API call comparing the latest successful `release.yml` run's `head_sha` against `main.gitHash`. 456 → ~200 prod lines, tests rewritten, dropped `UPDATE_CHECK_IMAGE/TAG` + `GHCR_USERNAME/TOKEN`. Also moots the `updates.Platform.Variant` dead-symbol finding below.
- [x] `delete` `scripts/migrate_int_ids_to_uuidv7.py` — done 2026-08-06. 576 lines.
- [x] `delete` Go dead symbols — done 2026-08-06: `db/store.go`, `PreviewStore.PurgeExpired`, `LogStore.String`, `LookupLogBuffer` (+sqlc query), `MediaStore.Now`+`now()`, `preview.DefaultConfig`, `preview.Config.UserAgent`/`.QueueCapacity`, bluesky `Client.DID()`, `var _ = json.Marshal` at `api/ws.go`, `MessageSemantics` json tags. **Corrections found on verify:** `peekLogBufferID` USED (`db/multistore.go:299`); `LogBufferRow`/`LogMessageRow` USED cross-package (exported return types, `irc/ergo_integration_test.go`) — unexporting is a rename job, not a delete; `updates.Platform.Variant` already gone with updates/ rewrite; `media.Service.Handler()` not free (~19 test call sites in `media/*_test.go` need rewiring to a local mux) — moved to judgment calls below.
- [x] `delete` Apple dead wire fields — done 2026-08-06, all 14 removed (incl. `BufferCreatedEvent.createdAt` which only fed `Buffer.createdAt`, and the orphaned `remote_ip` entry in `WireKeyTransform.decodedAcronyms`).

## Backend refinements and review notes

- bluesky `uriLRU`/`parentCache`: if the full delete above is not taken, the fallback is one `fifoCache[V any]` (both are the same bounded FIFO over `container/list`, written twice). ~45
- theme/: if `//go:embed` is not taken, at minimum `theme.Loader{Dir}` one-field struct + one method → `func Load(dir string) ([]Theme, error)`. ~12
- `media.Service.Handler()`: only `media/upload_test.go` calls it; ~19 test call sites → move to a `_test.go` helper.
- `RenameNetworkLogDB` has an api caller despite the "no casual rename flows" invariant — correctness/scope question, not complexity; route to a normal review.


## Noted, low yield / judgment calls

### Backend

- `preview.Resolver` interface + `FetcherConfig.SSRFCheck` are two overlapping test seams for the same need (`customCheck` flag exists to reconcile them) — one would do. `preview/fetcher.go:23-27`.
- `media.Store` interface justified only by a "separate process later" comment — one impl + test fake.
- `media.Service.Handler()` — production-dead but ~19 test call sites in `media/upload_test.go`/`browse_test.go` depend on it; deleting means rewiring those to a local mux.

### Native Apple client

- Apple `LurkerTransport` is 11 methods wide — every test double pays for all of it (see HistoryStubTransport above).
- Apple `FixtureTransport` 400 generated messages where ~150 prove the same paging.

## Verified clean — do not re-flag

`mirc/` (real parser, 3 consumers), `nickcolor/` OKLCH (bit-compatible parity with web JS), `internal/httpjson` (17 call sites), `preview/fediverse.go`+`youtube.go`, `cmd/seedtest`, web `scroll-stick.ts` (overflow-anchor doesn't cover late images), `formatBytes` (Intl has no auto-scaling bytes), nick identicon xorshift, `emoji-map.json`, Apple `ImageCache` (AsyncImage cancels on row teardown), `EndpointPolicy`, `RedirectGuard`, all `<symbol id="ic-*">` icons referenced. go.mod: no droppable deps while S3 backend stays (minio + 11 transitive is the only cluster, tied to a deliberate design decision).

## Original audit estimates

**Second-audit net: ~1,400 additional lines, −1 crate (`sha2`), −1 devDep (`@vitest/coverage-v8`), −3 Taskfile targets, −1 CI apt step, −4 files, −140 KB binaries.**

**Net (2026-08-06 list): ~2,900 lines, −3 web devDeps.** Plus second audit above: ~1,400 lines. **Combined: ~4,300 lines.** (Excludes TUI + S3 removals, ~5,400 more, ruled out as deliberate.)
