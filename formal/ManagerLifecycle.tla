---- MODULE ManagerLifecycle ----
(***************************************************************************)
(* Model of irc.Manager's per-network runtime lifecycle (irc/manager.go):  *)
(* StartNetwork / StopNetwork, the runNetwork goroutine per runtime        *)
(* generation, attemptConnect's setConnectingState + markDisconnected, and *)
(* girc's CONNECTED callback (handleSelfNickConnected).                    *)
(*                                                                         *)
(* One network. Each StartNetwork installs a runtime generation g whose    *)
(* goroutine runs: init -> (ctx check) -> setConnecting -> dialing ->      *)
(* markDisconnected -> backoff -> loop or exit.                            *)
(*                                                                         *)
(* Invariant: m.state[id] must only ever claim connecting/connected while a *)
(* runtime is installed, and "connected" only for the runtime that owns    *)
(* m.conn[id].                                                             *)
(*                                                                         *)
(* GenChecks = TRUE models Manager.ownsRuntimeLocked gating                *)
(* setConnectingState and handleSelfNickConnected (the fix). FALSE models  *)
(* the code before it; TLC then violates both invariants (StopNetwork      *)
(* between the loop's ctx check and setConnectingState; CONNECTED landing  *)
(* after StopNetwork). Pinned by TestStoppedRuntimeWritesAreIgnored.       *)
(***************************************************************************)
EXTENDS Naturals

CONSTANTS
  MaxGen,                  \* bound on StartNetwork calls
  GenChecks                \* runtime-generation guards on state writes

NONE == 0
Gens == 1..MaxGen

VARIABLES
  runtime,    \* gen installed in m.runtime[id], or NONE
  state,      \* m.state[id]
  conn,       \* gen whose client sits in m.conn[id], or NONE
  pc,         \* program counter of each runtime goroutine
  cancelled,  \* ctx cancelled (StopNetwork ran) per gen
  fired,      \* CONNECTED callback already delivered per gen
  nextGen

vars == <<runtime, state, conn, pc, cancelled, fired, nextGen>>

Init ==
  /\ runtime = NONE
  /\ state = "disconnected"
  /\ conn = NONE
  /\ pc = [g \in Gens |-> "idle"]
  /\ cancelled = [g \in Gens |-> FALSE]
  /\ fired = [g \in Gens |-> FALSE]
  /\ nextGen = 1

\* StartNetwork: no-op while a runtime is installed.
Start ==
  /\ runtime = NONE
  /\ nextGen <= MaxGen
  /\ runtime' = nextGen
  /\ state' = "disconnected"
  /\ pc' = [pc EXCEPT ![nextGen] = "init"]
  /\ nextGen' = nextGen + 1
  /\ UNCHANGED <<conn, cancelled, fired>>

\* StopNetwork: drop the runtime entry, force disconnected, cancel ctx.
Stop ==
  /\ IF runtime # NONE
       THEN /\ cancelled' = [cancelled EXCEPT ![runtime] = TRUE]
            /\ runtime' = NONE
       ELSE UNCHANGED <<cancelled, runtime>>
  /\ state' = "disconnected"
  /\ UNCHANGED <<conn, pc, fired, nextGen>>

\* runNetwork loop head: `if ctx.Err() != nil { return }`.
CheckCtx(g) ==
  /\ pc[g] = "init"
  /\ IF cancelled[g]
       THEN /\ pc' = [pc EXCEPT ![g] = "done"]
            /\ runtime' = IF runtime = g THEN NONE ELSE runtime
       ELSE /\ pc' = [pc EXCEPT ![g] = "checked"]
            /\ UNCHANGED runtime
  /\ UNCHANGED <<state, conn, cancelled, fired, nextGen>>

\* attemptConnect -> setConnectingState. With GenChecks a runtime that no
\* longer owns the network skips the dial (attemptConnect returns early and
\* the loop exits on its cancelled ctx).
SetConnecting(g) ==
  /\ pc[g] = "checked"
  /\ IF GenChecks /\ runtime # g
       THEN /\ pc' = [pc EXCEPT ![g] = "backoff"]
            /\ UNCHANGED <<conn, state>>
       ELSE /\ pc' = [pc EXCEPT ![g] = "dialing"]
            /\ conn' = g
            /\ state' = "connecting"
  /\ UNCHANGED <<runtime, cancelled, fired, nextGen>>

\* girc CONNECTED -> connectedHook -> handleSelfNickConnected. Fires at most
\* once per client, only while that client's Connect() is still running.
Connected(g) ==
  /\ pc[g] = "dialing"
  /\ ~fired[g]
  /\ fired' = [fired EXCEPT ![g] = TRUE]
  /\ state' = IF GenChecks /\ runtime # g THEN state ELSE "connected"
  /\ UNCHANGED <<runtime, conn, pc, cancelled, nextGen>>

\* connector returns -> markDisconnected (with its gen guard).
ConnReturn(g) ==
  /\ pc[g] = "dialing"
  /\ pc' = [pc EXCEPT ![g] = "backoff"]
  /\ IF runtime # NONE /\ runtime # g
       THEN UNCHANGED <<conn, state>>          \* newer runtime owns the entries
       ELSE /\ conn' = NONE
            /\ state' = IF runtime # NONE THEN "disconnected" ELSE state
  /\ UNCHANGED <<runtime, cancelled, fired, nextGen>>

\* After the attempt: exit on cancel (deferred cleanup), else retry.
Backoff(g) ==
  /\ pc[g] = "backoff"
  /\ IF cancelled[g]
       THEN /\ pc' = [pc EXCEPT ![g] = "done"]
            /\ runtime' = IF runtime = g THEN NONE ELSE runtime
       ELSE /\ pc' = [pc EXCEPT ![g] = "init"]
            /\ UNCHANGED runtime
  /\ UNCHANGED <<state, conn, cancelled, fired, nextGen>>

Next ==
  \/ Start
  \/ Stop
  \/ \E g \in Gens : CheckCtx(g) \/ SetConnecting(g) \/ Connected(g) \/ ConnReturn(g) \/ Backoff(g)

Spec == Init /\ [][Next]_vars

\* A non-disconnected state always has a live runtime behind it.
StateHasRuntime == state # "disconnected" => runtime # NONE

\* "connected" is only ever claimed by the runtime that owns m.conn.
ConnectedOwnsConn == state = "connected" => (runtime # NONE /\ conn = runtime)

TypeOK ==
  /\ runtime \in Gens \cup {NONE}
  /\ conn \in Gens \cup {NONE}
  /\ state \in {"disconnected", "connecting", "connected"}
====
