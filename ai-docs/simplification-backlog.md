# Simplification backlog

Repo-wide over-engineering audit, 2026-08-06. Ranked biggest cut first. Correctness/security/perf out of scope — this is complexity only.

Tags: `delete` dead code / speculative feature · `stdlib` hand-rolled stdlib · `native` platform already does it · `yagni` abstraction with one implementation · `shrink` same logic, fewer lines.

**Not on this list (deliberate design decisions, see ARCHITECTURE.md):** the TUI as a third client, and the S3 media backend. Do not re-propose.

## Subsystem removal

- [x] **updates/** — simplified, not deleted (2026-08-06): OCI registry dance (token challenge, manifest index, platform select, config blob; 4 HTTP calls) replaced with one GitHub Actions API call comparing the latest successful `release.yml` run's `head_sha` against `main.gitHash`. 456 → ~200 prod lines, tests rewritten, dropped `UPDATE_CHECK_IMAGE/TAG` + `GHCR_USERNAME/TOKEN`. Also moots the `updates.Platform.Variant` dead-symbol finding below.

## Structural

- [ ] `delete` `scripts/migrate_int_ids_to_uuidv7.py` — one-off migration; schema moved to UUIDv7 in `8eb90b0`, zero refs. 576 lines.
- [ ] `delete` Netsplit clustering exists twice: batch `GroupPresence`/`nsCluster`/`PresenceGroup`/`NetsplitGroup` vs live incremental `netsplitTracker`, hand-kept-in-sync and pinned by a contract test. Keep the live tracker (already stamps `MessageCore.Netsplit`), persist/serve that annotation, drop the batch path. `irc/netsplit.go:63-250`, `api/state.go:314-348`, `cmd/tui/model.go:1585`. ~450 lines incl tests.
- [ ] `native` Apple sidebar drag-and-drop stack — `SidebarDropDelegate`, drag-cancel detection polling `NSEvent.pressedMouseButtons` every 250ms, hand-rolled `SidebarOrdering.moving`, six near-identical drag predicates, three near-identical `commitXDrop`. Replace with `List` + `.onMove`, or `.draggable`/`.dropDestination` + `Transferable`; `Array.move(fromOffsets:toOffset:)` is stdlib. `apple/Lurker/SidebarView.swift:6-548`. ~400 lines incl 16 test cases.
- [ ] `yagni` api: 7 interfaces, 1 implementation (`*irc.Manager`) — `manager` = `wsManager`(=`messageSender`+`channelOps`+`presenceOps`+`modeOps`) + `stateManager` + `networkManager`; sub-interfaces exist only for per-slice test mocks. Collapse to one interface, one mock struct. `api/server.go:23-27`, `api/ws.go:82-126`, `api/state.go:80-85`, `api/networks.go:36-40`. ~245 lines incl tests.
- [ ] `yagni` `datasource.Source` interface + `datasource.Manager` — one implementation (bluesky); Manager is Register/Start/Wait/Names over a one-element slice; `Post.Target` always `""`. Construct `bluesky.Source` directly in `main.go:171-200`. ~130 lines.
- [ ] `delete` bluesky `uriLRU` + `parentCache` — DB unique index `(buffer_id, msgid)` already dedupes on insert (`inserted=false`); parentCache only needs to survive one poll → plain map in `pollOnce`. `datasource/bluesky/source.go:527-627`. ~100 lines, drops `container/list`.
- [ ] `shrink` Apple `Network`/`Buffer` hand-written `CodingKeys` + memberwise inits — byte-identical to synthesized; move `init(from:)` to an extension and synthesis returns. `apple/Lurker/Models.swift:39-69,119-165`. ~80 lines.
- [ ] `delete` Apple `AppModel.previewSidebar()` — 70-line fixture feeding one `#Preview`; `FixtureTransport.snapshot()` already covers it. `apple/Lurker/AppModel.swift:1037-1106`. ~75 lines.
- [ ] `yagni` web `connection.ts` DI type tower — 9 type aliases (`Renderer`, `Navigation`, `Transport`, `*Deps`) describing one wiring built once in `app-core.ts:88-107`; `createConnection` re-shreds its own deps into `Pick<>` subsets. Pass `AppView` + `sendCmd` directly. `web/src/connection.ts:19-79,161-193`. ~75 lines.
- [ ] `yagni` irc handler: 9 closure seams (`connectedHook nickFn memberListHook clearMemberListHook setJoinedHook drainJoinedHook hasCap sendRaw historyLimit`) all wired from one call site, nil-guarded at ~14 sites. Store `mgr *Manager` + `client *girc.Client`, call methods. `irc/handler.go:24-29`, `irc/manager.go:809-850`. ~60 lines.
- [ ] `native` theme/ dir loading + `THEMES_DIR` env + runtime re-read for 6 static YAMLs → `//go:embed themes/*.yaml`. `theme/theme.go`, `main.go:85`. ~60 lines.
- [ ] `native` Apple `LurkerCodingKey` + `WireKeyTransform` — reimplements snake_case; Foundation ships `.convertFromSnakeCase`/`.convertToSnakeCase`; rename 10 props `bufferID`→`bufferId`. `apple/Lurker/Models.swift:483-556`. ~55 lines.
- [ ] `native` web hand-rolled popup positioning + dismiss → Popover API (`popover="auto"` gives light dismiss + Esc + top layer) + CSS anchor positioning. `web/src/user-popup.ts:380-422`. ~55 lines. Conservative variant: popover only, keep JS positioning (~30).
- [ ] `yagni` web `sidebar-model.ts` — view-model layer with one consumer (`renderSidebar`); 3 types describing one immediately-destructured object. Fold into `sidebar.ts`. ~55 lines.
- [ ] `delete` bluesky reserved channel kinds (`ChannelSearch/List/Feed/Notifications`) parse only to error "reserved"; `ChannelConfig.Query`/`.URI` never read; `defaultInterval` ignores its param. `datasource/bluesky/channel.go:11-58`, `config.go:399-404`. ~50 lines.
- [ ] `yagni` web `create*` factory/thunk layer — `createSetActive({getDom: () => dom})` closures over module-level state in the same file, read back through getter thunks, one caller each. Plain module functions. `web/src/app-core.ts:30-38`, `active-buffer.ts`, `read-tracker.ts`, `scroll-stick.ts`. ~40 lines.
- [ ] `yagni` `Media*FileConfig` mirror structs + manual copy loop → yaml tags on the real structs (as `PreviewConfig` does). `config.go:63-124,274-320`. ~40 lines.
- [ ] `yagni` `tls_max_version` YAML knob — never set; automatic TLS-1.2 retry at `irc/manager.go:680` covers legacy ircds. Drop field, `parseTLSMaxVersion`, `tlsMaxVersionLabel`; keep internal `ServerConfig.TLSMaxVersion` for the fallback. `config.go:419-430`. ~40 lines.
- [ ] `yagni` Apple `HistoryStubTransport` — 9 of 11 `LurkerTransport` methods `throw Unsupported()`; add cursor recording to `FixtureTransport` instead. `apple/LurkerTests/AppModelTests.swift:1141-1178`. ~38 lines.

## Mechanical

- [ ] `delete` `isExplicitlyHandledEvent` — hand-copied second list of `register()`'s 30 handler keys; build a map inside `register()`. `irc/handler_register.go:88-124`. ~35
- [ ] `shrink` web `THEME_VARS` 31-entry reset array → `root.style.cssText = ""` (`theme.ts` is sole writer of `documentElement.style`). `web/src/theme.ts:18-53`. ~35
- [ ] `delete` Apple `SidebarBufferOccurrence` wrapper — carries no data, only a composite ForEach id; `ForEach(buffers, id: \.id)`. `apple/Lurker/SidebarView.swift:40-57`. ~45
- [ ] `stdlib` FNV member-list change hashing → store previous `[]ChannelUser`, `slices.Equal`. `irc/handler_presence.go:168-208`. ~32
- [ ] `delete` Go dead symbols (verified vs web/apple/cmd/tests): `db/store.go` (package-only file), `PreviewStore.PurgeExpired`, `LogStore.String`, `LookupLogBuffer`, `MediaStore.Now`+`now()`, `peekLogBufferID`, `LogBufferRow`/`LogMessageRow` aliases, `preview.DefaultConfig`, `preview.Config.UserAgent`/`.QueueCapacity` (unsettable), bluesky `Client.DID()`, `media.Service.Handler()` (test-only), `updates.Platform.Variant` (moot if updates/ dies), `var _ = json.Marshal` at `api/ws.go:857`, `MessageSemantics` json tags. ~90
- [ ] `delete` Apple dead wire fields — 14 decoded-never-read: `Network.nickColor`, `Message.msgid/.account/.targetColor`, `MircSegment.mono/.bg`, `Preview.width/.height/.mime`, `NetsplitInfo.id`, `HistoryBackfillEvent.count`, `MemberListEvent.channel`, `TailscaleStatus.remoteIP`, `Buffer.createdAt`. ~30
- [ ] `shrink` Apple `LurkerAPI` 4× repeated POST-JSON-decode → one generic `post<B,T>`; collapse `get`→`request`→`requestJSON` chain. `apple/Lurker/LurkerAPI.swift:120-151,276-296`. ~30
- [ ] `shrink` db duplicate helpers: `nullStr`==`nullableString`, `parseFetchedAt`==`parseMediaTime`, `boolInt` re-inlined at `control.go:63,120`. ~23
- [ ] `shrink` `recentRowsToMessages`/`beforeRowsToMessages` byte-identical over two sqlc row types → one generic or unified query columns. `db/logstore.go:177-214`. ~22
- [ ] `stdlib` `newMediaID` base62 + rejection sampling → `crypto/rand.Text()`. `media/upload.go:290-312`. ~24
- [ ] `stdlib` `sort.Slice` in ~10 files → `slices.SortFunc` + `cmp.Or`; tui hand-rolled insertion sort → `slices.SortStableFunc`. `cmd/tui/switcher.go:50-62`, `theme/theme.go:60`, `cmd/tui/state.go:121,152,158`, `irc/*`, `db/db.go`. ~50
- [ ] `delete` web `dialog.ts` + `keyboard-dialogs.ts` — two parallel `<dialog>` helpers with identical backdrop-close listener → one function, two optional args. ~25
- [ ] `yagni` Go pure-delegation wrappers: `stateString`, `markNonYAMLNetworksDisabled`, `handler.publishNetworkState`/`publishBufferCreated`, `closeFunc`, `botTracker.set`, `parsedConfig`, `internal/closeutil` (5-line func, own package, 3 callers). Inline. ~50
- [ ] `shrink` preview `CheckURL` resolves DNS then discards result — `pinningDialContext` re-resolves and enforces on every dial anyway. Reduce to scheme/port/IP-literal checks. `preview/ssrf.go:44-58`. ~20 + 1 DNS lookup per URL
- [ ] `shrink` Apple misc: `command→send`, `previewImageURL`/`inlineImageURL`→`normalizedImageURL`, `isPresence`, `SidebarBufferGroups.all`, `EmojiCatalog` struct for one dict, `CachedAsyncImage` third generic (one user), `AnyShapeStyle` triple-wrap → `HierarchicalShapeStyle`, `nickHues` 48-element table → `i%48*7.5`, dead `ISO8601DateFormatter` fallback in `parseTimestamp`, `FixtureTransport.buildFullMessages` 10 templates → 3, `NetworkHeaderRow` duplicate action params + TODO menu, write-only `ClientCommand.before/.limit/.reqID` correlation channel, `toggleSidebar()` (unreferenced; `SidebarCommands()` covers it), `ChannelSwitcher.results` subsumed filter clauses, `ComposerPopupHeightKey` PreferenceKey → `.onGeometryChange`. ~180
- [ ] `stdlib` web `formatTime` → `toLocaleTimeString`, `dayKeyOf` → `toDateString()` (already used in `daySeparator`), today/yesterday → `Intl.RelativeTimeFormat` (pattern in `settings-dialog.ts:16`). `web/src/format.ts:59-73`, `messages.ts:718-742`. ~17
- [ ] `stdlib` misc Go: `fmt.Sscanf`→`strconv.Atoi` (`irc/handler_list.go:16`), `envVarRE`→`strings.CutPrefix`/`CutSuffix` (`config.go:345-357`), migration listing → `fs.Glob` (`db/db.go:93-108`), `splitFields`→`strings.Fields`, `splitFirstSpace`→`strings.Cut` (`api/input.go:145-171`), `nickcolor` hexByte→`fmt.Sprintf("#%02x%02x%02x")`, `sha256HexRe`→`hex.DecodeString` len check (`media/browse.go:16`), `trimTrailing`→`strings.TrimRight` (`preview/extract.go:38-51`), `ensureParentDir` `"."` guard. ~55
- [ ] `yagni` web small files: `main.ts`/`bootstrap.ts` re-export chain (3 files, 2 ≤6 lines), `navigation.ts` 9-line wrapper, `alias()` one caller (`slash-commands.ts:23-37`), `handleBufferLifecycleCmd` reachable only from identical switch's default (`api/ws.go:294-311`). ~45
- [ ] `delete` web dead: `closeAllDrawers` (`ui-shell.ts:27`), `applyThemeDefaults` re-setting hardcoded density, `data-density="balanced|comfortable"` CSS never set, `.flash` class, `ignorelist_result` console.log-only case + union member + `/ignorelist` command (surface or cut), 20 needless `export` keywords. ~33
- [ ] `delete` web devDeps: `stylelint` + `stylelint-config-standard` (Biome 2.4 lints CSS, already installed), `@vitest/browser` (never imported; transitive of `@vitest/browser-playwright`). −3 deps. Borderline: `@vitest/coverage-v8` if `test:coverage` unused.
- [ ] `shrink` duplicate URL regex byte-identical in `preview/extract.go:9` and `cmd/tui/model.go:395` — export one.
- [ ] fix stale comment: `media/store.go:40-41` claims Q/Kind unimplemented; `db/media_store.go:144` implements both.

## Noted, low yield / judgment calls

- `preview.Resolver` interface + `FetcherConfig.SSRFCheck` are two overlapping test seams for the same need (`customCheck` flag exists to reconcile them) — one would do. `preview/fetcher.go:23-27`.
- `media.Store` interface justified only by a "separate process later" comment — one impl + test fake.
- Apple `LurkerTransport` is 11 methods wide — every test double pays for all of it (see HistoryStubTransport above).
- Apple `FixtureTransport` 400 generated messages where ~150 prove the same paging.

## Verified clean — do not re-flag

`mirc/` (real parser, 3 consumers), `nickcolor/` OKLCH (bit-compatible parity with web JS), `internal/httpjson` (17 call sites), `preview/fediverse.go`+`youtube.go`, `cmd/seedtest`, web `scroll-stick.ts` (overflow-anchor doesn't cover late images), `formatBytes` (Intl has no auto-scaling bytes), nick identicon xorshift, `emoji-map.json`, Apple `ImageCache` (AsyncImage cancels on row teardown), `EndpointPolicy`, `RedirectGuard`, all `<symbol id="ic-*">` icons referenced. go.mod: no droppable deps while S3 backend stays (minio + 11 transitive is the only cluster, tied to a deliberate design decision).

**Net: ~2,900 lines, −3 web devDeps.** (Excludes TUI + S3 removals, ~5,400 more, ruled out as deliberate.)
