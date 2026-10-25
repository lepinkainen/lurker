---- MODULE ClientSync ----
(***************************************************************************)
(* Model of the sequenced event stream (hub/hub.go) and a client resuming  *)
(* over it (api/ws.go handshake; web connection.ts, cmd/tui, apple         *)
(* LurkerAPI).                                                             *)
(*                                                                         *)
(* Server: every published event gets the next seq. With Atomic = TRUE a   *)
(* DB write and the event describing it happen under one lock (hub.Do), so *)
(* stream order equals DB order and a snapshot taken under the same lock   *)
(* reflects exactly the events up to the seq it carries. Atomic = FALSE    *)
(* models the code before: commit and publish as separate steps, and an    *)
(* ack whose counts are tallied after its store but published separately.  *)
(*                                                                         *)
(* Client: connects, sends hello{since = cursor}. The server replays the   *)
(* events after since when its ring still holds them, else answers with a  *)
(* state snapshot; live events follow. The client drops any sequenced     *)
(* frame at or below its cursor and advances the cursor on every other.    *)
(*                                                                         *)
(* Property: once everything is delivered, the client's unread count       *)
(* equals the server's and it holds every message the window covers.      *)
(***************************************************************************)
EXTENDS Naturals, Sequences, FiniteSets

CONSTANTS MaxMsgs, Window, RingSize, Atomic

VARIABLES
  committed,    \* server: message ids 1..committed exist
  lastSeen,     \* server: buffers.last_seen_id (0 = never acked)
  seq,          \* server: last assigned sequence number
  stream,       \* server: every published event, in seq order (the ring, unbounded here)
  unpublished,  \* Atomic = FALSE: committed ids whose event hasn't been published
  ackPos,       \* Atomic = FALSE: stored ack awaiting its tally+publish (0 = none)
  connected,    \* client: socket open and hello answered
  inbox,        \* client: frames in flight on the socket, in order
  cursor,       \* client: seq of the last applied frame
  cmsgs, cunread, clastSeen

vars == <<committed, lastSeen, seq, stream, unpublished, ackPos, connected, inbox, cursor, cmsgs, cunread, clastSeen>>

Msg(i, s) == [type |-> "message", id |-> i, seq |-> s]
Upd(ls, u, s) == [type |-> "buffer_update", ls |-> ls, unread |-> u, seq |-> s]
Snap(k, ls, s) == [type |-> "state", k |-> k, ls |-> ls, seq |-> s]

Init ==
  /\ committed = 0 /\ lastSeen = 0 /\ seq = 0 /\ stream = <<>>
  /\ unpublished = {} /\ ackPos = 0
  /\ connected = FALSE /\ inbox = <<>> /\ cursor = 0
  /\ cmsgs = {} /\ cunread = 0 /\ clastSeen = 0

\* Publish stamps ev with the next seq, appends it to the stream, and hands it
\* to the connected client's socket.
Publish(ev) ==
  /\ seq' = seq + 1
  /\ stream' = Append(stream, ev)
  /\ inbox' = IF connected THEN Append(inbox, ev) ELSE inbox

--------------------------------------------------------------------------
\* Server, Atomic = TRUE: hub.Do(insert + publish), hub.Do(store + tally + publish)

CommitAndPublish ==
  /\ Atomic /\ committed < MaxMsgs
  /\ committed' = committed + 1
  /\ Publish(Msg(committed + 1, seq + 1))
  /\ UNCHANGED <<lastSeen, unpublished, ackPos, connected, cursor, cmsgs, cunread, clastSeen>>

AckAtomic ==
  /\ Atomic /\ committed > lastSeen
  /\ lastSeen' = committed
  /\ Publish(Upd(committed, 0, seq + 1))
  /\ UNCHANGED <<committed, unpublished, ackPos, connected, cursor, cmsgs, cunread, clastSeen>>

--------------------------------------------------------------------------
\* Server, Atomic = FALSE: the pre-hub.Do publishers

Commit ==
  /\ ~Atomic /\ committed < MaxMsgs
  /\ committed' = committed + 1
  /\ unpublished' = unpublished \cup {committed + 1}
  /\ UNCHANGED <<lastSeen, seq, stream, ackPos, connected, inbox, cursor, cmsgs, cunread, clastSeen>>

PublishMsg ==
  /\ unpublished # {}
  /\ LET i == CHOOSE i \in unpublished : \A j \in unpublished : i <= j
     IN /\ unpublished' = unpublished \ {i}
        /\ Publish(Msg(i, seq + 1))
  /\ UNCHANGED <<committed, lastSeen, ackPos, connected, cursor, cmsgs, cunread, clastSeen>>

AckStore ==
  /\ ~Atomic /\ ackPos = 0 /\ committed > lastSeen
  /\ lastSeen' = committed
  /\ ackPos' = committed
  /\ UNCHANGED <<committed, seq, stream, unpublished, connected, inbox, cursor, cmsgs, cunread, clastSeen>>

AckPublish ==
  /\ ackPos # 0
  /\ Publish(Upd(lastSeen, committed - lastSeen, seq + 1))
  /\ ackPos' = 0
  /\ UNCHANGED <<committed, lastSeen, unpublished, connected, cursor, cmsgs, cunread, clastSeen>>

--------------------------------------------------------------------------
\* Client

\* Everything above cursor that the ring still has, else a snapshot.
Replayable == cursor <= seq /\ seq - cursor <= RingSize

Replay == SubSeq(stream, cursor + 1, seq)

\* Hello: the server answers from the stream (seq unchanged); live frames
\* published from now on follow in the inbox.
Connect ==
  /\ ~connected
  /\ connected' = TRUE
  /\ inbox' = IF Replayable THEN Replay ELSE <<Snap(committed, lastSeen, seq)>>
  /\ UNCHANGED <<committed, lastSeen, seq, stream, unpublished, ackPos, cursor, cmsgs, cunread, clastSeen>>

Disconnect ==
  /\ connected
  /\ connected' = FALSE
  /\ inbox' = <<>>
  /\ UNCHANGED <<committed, lastSeen, seq, stream, unpublished, ackPos, cursor, cmsgs, cunread, clastSeen>>

SnapWindow(k) == {i \in 1..k : i > k - Window}

ApplyEv(ev) ==
  CASE ev.type = "state" ->
         /\ cmsgs' = SnapWindow(ev.k)
         /\ cunread' = ev.k - ev.ls
         /\ clastSeen' = ev.ls
    [] ev.type = "message" ->
         /\ cmsgs' = cmsgs \cup {ev.id}
         /\ cunread' = IF ev.id \notin cmsgs /\ ev.id > clastSeen THEN cunread + 1 ELSE cunread
         /\ UNCHANGED clastSeen
    [] ev.type = "buffer_update" ->
         /\ cunread' = ev.unread
         /\ clastSeen' = ev.ls
         /\ UNCHANGED cmsgs

\* The one rule: at or below the cursor it is already applied.
Deliver ==
  /\ connected /\ inbox # <<>>
  /\ LET ev == Head(inbox) IN
     /\ inbox' = Tail(inbox)
     /\ IF ev.seq <= cursor
          THEN UNCHANGED <<cursor, cmsgs, cunread, clastSeen>>
          ELSE /\ cursor' = ev.seq
               /\ ApplyEv(ev)
  /\ UNCHANGED <<committed, lastSeen, seq, stream, unpublished, ackPos, connected>>

Next ==
  \/ CommitAndPublish \/ AckAtomic
  \/ Commit \/ PublishMsg \/ AckStore \/ AckPublish
  \/ Connect \/ Disconnect \/ Deliver

Spec == Init /\ [][Next]_vars

--------------------------------------------------------------------------
Quiescent ==
  /\ committed = MaxMsgs /\ unpublished = {} /\ ackPos = 0
  /\ connected /\ inbox = <<>>

UnreadAgrees == Quiescent => cunread = committed - lastSeen

MessagesAgree == Quiescent => \A i \in 1..committed : i > committed - Window => i \in cmsgs

\* The cursor never runs ahead of the stream.
CursorBounded == cursor <= seq
====
