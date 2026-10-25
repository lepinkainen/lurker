# Formal models

TLA+ specifications of the parts of Lurker where interleavings matter. Each
spec documents the Go/TypeScript it abstracts; the unit tests that pin the
counterexamples reference the spec by name.

Run with `task tlc` (Java + `tla2tools.jar`, see `ai-docs/testing-and-build.md`).
The committed `.cfg` files are the configurations that must pass. The
"expected to fail" configurations below reproduce the bugs the models found;
run them by editing the constants.

## ManagerLifecycle.tla — `irc/manager.go`

StartNetwork / StopNetwork, the per-generation `runNetwork` goroutine,
`setConnectingState`, `markDisconnected` and girc's CONNECTED callback.

Invariants: `StateHasRuntime` (never "connecting"/"connected" without an
installed runtime) and `ConnectedOwnsConn` ("connected" only for the runtime
that owns `m.conn`). `GenChecks = TRUE` (committed) models the generation
guards in `Manager.ownsRuntimeLocked` and passes. `GenChecks = FALSE` models
the code before them and violates both: StopNetwork between the loop's ctx
check and `setConnectingState`, and a CONNECTED callback landing after
StopNetwork. Pinned by `TestStoppedRuntimeWritesAreIgnored`.

## ClientSync.tla — the sequenced stream (`hub/hub.go`, `api/ws.go`) and a resuming client

Server: every published event gets the next `seq`; `Atomic = TRUE` runs each
DB write and its event under one lock (`hub.Do`) and reads snapshots under
the same lock. Client: `hello{since}` answered by a replay (when the ring
reaches) or a `state` snapshot, then one rule — drop frames at or below the
cursor, apply and advance on the rest. Disconnect/reconnect is modeled, so
both resume paths are exercised with `RingSize` smaller than the stream.

| Atomic | Result |
|---|---|
| TRUE (committed) | passes `UnreadAgrees`, `MessagesAgree`, `CursorBounded` |
| FALSE (publishers before `hub.Do`) | **fails** `UnreadAgrees`: a message committed between an ack's store and its tally is in the `buffer_update` counts and then arrives as its own event |

Pinned by `api/ws_handshake_test.go` (snapshot seq, replay, unknown cursor,
hello required), `hub/hub_test.go` (stamping, ring, snapshot seq),
`web/tests/connection.test.ts` (hello, gate, resume) and `cmd/tui/model_test.go`
(`TestSequencedFramesGateOnCursor`, `TestConnectSendsHelloWithCursor`).
