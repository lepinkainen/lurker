---
name: verifier-apple
description: Verify native Apple client changes end-to-end by driving the built macOS app — XCUITest against deterministic fixtures, or the real app against a seeded backend with screenshot evidence. Use when verifying changes under apple/ (SwiftUI rendering, sidebar, conversation view, unread bar, keyboard shortcuts) at the real app surface. Not for backend-only changes and not for running unit tests (task test-apple is CI's job).
---

# Verifier: Apple client

Runtime-verify `apple/` changes at the real app surface. Two modes — pick by what the change needs:

- **Mode A — XCUITest against fixtures**: deterministic, assertable, drivable input. Default for interaction changes (clicks, keys, state transitions).
- **Mode B — real app against seeded backend**: real REST + WebSocket + live events. For connection-path changes and visual evidence; input driving is weak here.

## Build

```bash
task build-apple               # Debug build → build/DerivedData/Build/Products/Debug/Lurker.app
```

- **Stale-build trap:** Mode B launches the app from DerivedData. Rebuild via `task build-apple` after every Swift edit; a running app never picks up source changes.
- macOS only (`platforms: [darwin]`); Apple silicon destination is hardcoded in the Taskfile.

## Mode A — XCUITest against fixtures

The app launched with `-ui-testing` swaps all network access for `FixtureTransport` (deterministic in-process data) and suppresses notification prompts (`ProcessInfo.isUITest`, `apple/Lurker/LurkerApp.swift`).

Write a throwaway (or keepable) test in `apple/LurkerUITests/LurkerUITests.swift` and run just it:

```bash
xcodebuild -quiet -project apple/Lurker.xcodeproj -scheme Lurker -configuration Debug \
  -destination 'platform=macOS,arch=arm64' -derivedDataPath build/DerivedData \
  ONLY_ACTIVE_ARCH=YES CODE_SIGN_IDENTITY=- CODE_SIGNING_REQUIRED=YES AD_HOC_CODE_SIGNING_ALLOWED=YES \
  -only-testing:LurkerUITests/LurkerUITests/testMyThrowaway test
```

(`task test-apple-ui` runs the whole UI suite with the same flags.) Requires Xcode to have UI-automation permission in System Settings; the runner is ad-hoc signed.

- **User must be present:** enabling automation mode triggers a Touch ID / password prompt that only the user can approve. Without it the run fails after a long hang with "The test runner failed to initialize for UI testing. (Underlying Error: Timed out while enabling automation mode.)". Ask the user to stand by before starting an XCUITest run; if they're away, fall back to `task test-apple` unit tests and Mode B for evidence.

Fixture data (`apple/Lurker/FixtureTransport.swift`): network `Libera`; `#lurker` (pinned, topic "Native client development…", messages from `tove`/`hilde`, a preview-card message) and `#lurker-full` (long backlog, `unread: 10`, `lastSeenID`/`markerID` set — the unread bar + "New messages" divider fixture). Extend the fixtures when your change needs data they lack.

Accessibility quirks that cost time (from the existing tests):

- `MessageRow` combines sender/time/content into ONE element: match `app.otherElements` with `NSPredicate(format: "label CONTAINS %@", "some content")`, not bare `staticTexts`.
- The header topic truncates (`lineLimit(1)`): match `value BEGINSWITH` a short prefix.
- Composer is a text field whose `placeholderValue` == the buffer name (`"#lurker"`).
- Channel switcher: `app.typeKey("k", modifierFlags: .command)`, field placeholder "Jump to a channel or conversation".
- The "New messages" divider (`UnreadSeparator`) has accessibility label "New messages begin here"; the unread bar is a `Button` labeled "N new messages" / "new since …".
- Link/cursor geometry is not accessible as elements — see the pointer-sweep technique in the existing suite and `ai-docs/apple.md`.

## Mode B — real app against seeded backend

```bash
task seed-test                 # reset + seed ./data-test
task dev-test                  # background this: backend on :8080 (readiness: curl -s localhost:8080/whoami)
open -n build/DerivedData/Build/Products/Debug/Lurker.app --args -mac.serverURL "http://localhost:8080"
```

- **Check for a running Lurker first** (`pgrep -lf "Lurker.app/Contents/MacOS/Lurker"`). The user runs their own client against their real bouncer from this same DerivedData bundle. If one is running: ask before rebuilding (`task build-apple` overwrites the bundle it launched from), and never assume a Lurker window on screen is yours — see Evidence below.
- `mac.serverURL` is read at `AppModel.start()`; supplying it bypasses the in-app connect form (which validates `/whoami` first). No URL at all means the app opens on the connect screen.
- **Pass the URL as a launch argument, not via `defaults write`.** `-key value` in `--args` populates NSUserDefaults' *argument domain*, which outranks the persistent domain, needs no backup/restore, and cannot clobber the user's real `mac.serverURL`. `defaults write xyz.endymion.lurker mac.serverURL …` is unreliable: observed the app launching on the user's real server across two launches while both the host domain *and* the sandbox container plist read `http://localhost:8080` — a stale cfprefsd read the write can't defeat. The app is sandboxed (`~/Library/Containers/xyz.endymion.lurker/`), so its real prefs live in the container; cfprefsd usually redirects a host-domain write there, which makes the failure look like success when you verify by reading the plist back.
- The app also persists `mac.selectedBuffer` / `mac.inspectorVisible` — stale values can change what you see at launch (a `mac.selectedBuffer` from the user's real server just won't resolve). Override these the same way, via `--args`.
- **The backend logs no HTTP or WebSocket requests.** Don't try to confirm the app connected by grepping the `dev-test` log — a connected client leaves no trace. Confirm from the app side (window title / screenshot) or by mutating state and reading `/api/state`.
- **Live message arrivals**: same fakeircd recipe as the TUI skill — `task fake-ircd` (IRC :6667, control :6668), create + connect a network via REST, then `echo "#verify :hello from bob" | nc -w1 127.0.0.1 6668`. The app receives it over the real WebSocket.
- Server-side assertions: `curl -s localhost:8080/api/state` — check `buffers[].last_seen_id` / `marker_id` / `unread` to see what the app actually told the backend (e.g. `mark_read` fires only on explicit ack: unread-bar click or Esc — never on buffer open/focus; see `ai-docs/behaviors/new-messages-marker.md`).
- Input driving in this mode is limited to `osascript` System Events keystrokes (needs Accessibility permission, may prompt, flaky). Prefer Mode A for anything interactive; use Mode B to observe real-backend behavior.

## Evidence

Screenshot the app window and Read it back to confirm it shows what you claim:

```bash
osascript -e 'tell app "Lurker" to activate' >/dev/null; sleep 1
B=$(osascript -e 'tell application "System Events" to tell process "Lurker" to get {position, size} of window 1')
python3 - "$B" /tmp/lurker-apple.png <<'EOF'
import subprocess, sys, re
n = [int(x) for x in re.findall(r"-?\d+", sys.argv[1])]
subprocess.run(["screencapture", "-x", "-R", f"{n[0]},{n[1]},{n[2]},{n[3]}", sys.argv[2]], check=True)
EOF
```

- Don't reach for `CGWindowListCopyWindowInfo` — **pyobjc (`import Quartz`) is not installed**, so window-id lookup fails outright. The `System Events` bounds above are the working substitute.
- **`screencapture -R` grabs whatever is on screen at that rectangle**, not a specific window. With two Lurker instances running you will capture the user's real client and mistake it for yours. Verify identity from the *content*: the seeded backend shows networks `libera`/`oftc` and channels `#go-nuts`/`#debian`/`#lurker`; anything else is the user's real bouncer. `System Events … get name of every window` is a cheap pre-check.
- Assertions worth preferring over pixels: `osascript … get name of every window` for the selected buffer, and for anything the sidebar sorts, read it from a second client — the web UI is served by `dev-test` at `localhost:8080`, so Playwright + `document.querySelectorAll` gives you an exact, diffable list, and doubles as proof that a WS broadcast reached other clients.

In Mode A, `XCUIScreen.main.screenshot()` inside the test (attach or write to a known path) is the deterministic alternative.

## Cleanup

```bash
osascript -e 'tell app "Lurker" to quit' 2>/dev/null
# stop the background dev-test / fake-ircd tasks (TaskStop <id>)
# revert any throwaway UI test unless it earned a place in the suite
```

Nothing to un-write if you passed the URL via `--args` — that is the point of the argument domain. If you *did* fall back to `defaults write`, back up `defaults read xyz.endymion.lurker` beforehand and restore it after; the user's real `mac.serverURL` lives in that domain. Tell the user to relaunch their own client when you're done, since verifying meant quitting it.

`data-test/` is gitignored and disposable; re-seed freely.
