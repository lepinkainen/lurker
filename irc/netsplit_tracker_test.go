package irc

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNetsplitTrackerQualification(t *testing.T) {
	tr := newNetsplitTracker()
	buf := uuid.New()
	t0 := time.Now()
	reason := "irc.example.net hub.example.net"

	q1 := uuid.New()
	meta, retro := tr.OnQuit(buf, q1, "alice", reason, t0)
	if meta != nil || retro != nil {
		t.Fatalf("first quit: meta=%v retro=%v, want nil/nil (below MinQuits)", meta, retro)
	}

	q2 := uuid.New()
	meta, retro = tr.OnQuit(buf, q2, "bob", reason, t0.Add(2*time.Second))
	if meta == nil {
		t.Fatal("second quit should qualify the cluster")
	}
	if len(retro) != 1 || retro[0] != q1 {
		t.Fatalf("retro = %v, want [%v] (the unannotated first quit)", retro, q1)
	}
	if meta.ServerA != "hub.example.net" && meta.ServerA != "irc.example.net" {
		t.Fatalf("ServerA = %q", meta.ServerA)
	}

	// Third quit: annotated inline, no further retro patching.
	meta3, retro3 := tr.OnQuit(buf, uuid.New(), "carol", reason, t0.Add(5*time.Second))
	if meta3 == nil || meta3.ID != meta.ID {
		t.Fatalf("third quit meta = %v, want same group %q", meta3, meta.ID)
	}
	if retro3 != nil {
		t.Fatalf("retro3 = %v, want nil", retro3)
	}
}

func TestNetsplitTrackerRejoins(t *testing.T) {
	tr := newNetsplitTracker()
	buf := uuid.New()
	t0 := time.Now()
	reason := "a.example.net b.example.net"
	tr.OnQuit(buf, uuid.New(), "Alice", reason, t0)
	meta, _ := tr.OnQuit(buf, uuid.New(), "bob", reason, t0.Add(time.Second))
	if meta == nil {
		t.Fatal("cluster should qualify")
	}

	// Case-folded rejoin matches ({} vs [] per RFC1459 also folds, plain
	// case here), once per nick.
	if got := tr.OnJoin(buf, "ALICE", t0.Add(time.Minute)); got == nil || got.ID != meta.ID {
		t.Fatalf("rejoin = %v, want group %q", got, meta.ID)
	}
	if got := tr.OnJoin(buf, "alice", t0.Add(2*time.Minute)); got != nil {
		t.Fatalf("second rejoin by same nick = %v, want nil", got)
	}
	// Unrelated nick never matches.
	if got := tr.OnJoin(buf, "mallory", t0.Add(time.Minute)); got != nil {
		t.Fatalf("unrelated join = %v, want nil", got)
	}
	// Outside the rejoin window: no match.
	if got := tr.OnJoin(buf, "bob", t0.Add(NetsplitRejoinWindow+time.Minute)); got != nil {
		t.Fatalf("late join = %v, want nil", got)
	}
}

func TestNetsplitTrackerIgnoresNonNetsplitQuits(t *testing.T) {
	tr := newNetsplitTracker()
	buf := uuid.New()
	if meta, retro := tr.OnQuit(buf, uuid.New(), "alice", "Quit: leaving", time.Now()); meta != nil || retro != nil {
		t.Fatalf("plain quit tracked: meta=%v retro=%v", meta, retro)
	}
}

func TestNetsplitTrackerBuffersAreIndependent(t *testing.T) {
	tr := newNetsplitTracker()
	bufA, bufB := uuid.New(), uuid.New()
	t0 := time.Now()
	reason := "a.example.net b.example.net"
	tr.OnQuit(bufA, uuid.New(), "alice", reason, t0)
	// One quit in each buffer: neither cluster qualifies.
	if meta, _ := tr.OnQuit(bufB, uuid.New(), "bob", reason, t0); meta != nil {
		t.Fatalf("cross-buffer clustering: %v", meta)
	}
}

func TestNetsplitTrackerQuitWindowRollsCluster(t *testing.T) {
	tr := newNetsplitTracker()
	buf := uuid.New()
	t0 := time.Now()
	reason := "a.example.net b.example.net"
	tr.OnQuit(buf, uuid.New(), "alice", reason, t0)
	// Past the quit window: starts a fresh cluster, so still below MinQuits.
	if meta, _ := tr.OnQuit(buf, uuid.New(), "bob", reason, t0.Add(NetsplitQuitWindow+time.Second)); meta != nil {
		t.Fatalf("stale cluster reused: %v", meta)
	}
}

func TestMetaForNetsplitStableID(t *testing.T) {
	ts := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)
	g := &NetsplitGroup{ServerA: "a.example.net", ServerB: "b.example.net", SplitTS: ts}
	m1 := MetaForNetsplit(g)
	m2 := MetaForNetsplit(&NetsplitGroup{ServerA: "b.example.net", ServerB: "a.example.net", SplitTS: ts})
	if m1.ID != m2.ID {
		t.Fatalf("ID not symmetric in server order: %q vs %q", m1.ID, m2.ID)
	}
}
