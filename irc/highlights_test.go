package irc

import "testing"

// Highlight tests mutate the package-global pattern set; keep them
// non-parallel and reset via t.Cleanup.
func setPatterns(t *testing.T, patterns []string) {
	t.Helper()
	SetHighlightPatterns(patterns)
	t.Cleanup(func() { SetHighlightPatterns(nil) })
}

func TestHighlightWordBoundary(t *testing.T) {
	setPatterns(t, []string{"go"})

	sem := ComputeMessageSemantics("privmsg", "alice", "I like go a lot", "", "me")
	if !sem.Highlight || sem.HighlightPattern != "go" {
		t.Fatalf("expected highlight on whole word, got %+v", sem)
	}
	sem = ComputeMessageSemantics("privmsg", "alice", "golang is fine", "", "me")
	if sem.Highlight {
		t.Fatalf("expected no highlight inside larger word, got %+v", sem)
	}
}

func TestHighlightCaseInsensitive(t *testing.T) {
	setPatterns(t, []string{"Lurker"})

	sem := ComputeMessageSemantics("privmsg", "alice", "have you tried LURKER yet", "", "me")
	if !sem.Highlight {
		t.Fatalf("expected case-insensitive match, got %+v", sem)
	}
	if sem.HighlightPattern != "Lurker" {
		t.Fatalf("expected original pattern reported, got %q", sem.HighlightPattern)
	}
}

func TestHighlightQuotesRegexMetachars(t *testing.T) {
	setPatterns(t, []string{"c++"})

	// \b after QuoteMeta("c++") sits between '+' and a non-word char, so
	// the literal must still match; the metachars must not act as regex.
	sem := ComputeMessageSemantics("privmsg", "alice", "ccc", "", "me")
	if sem.Highlight {
		t.Fatalf("metachars must be quoted, got %+v", sem)
	}
}

func TestHighlightFirstMatchWins(t *testing.T) {
	setPatterns(t, []string{"alpha", "beta"})

	sem := ComputeMessageSemantics("privmsg", "alice", "beta then alpha", "", "me")
	if !sem.Highlight || sem.HighlightPattern != "alpha" {
		t.Fatalf("expected first configured pattern to win, got %+v", sem)
	}
}

func TestHighlightExcludesSelf(t *testing.T) {
	setPatterns(t, []string{"deploy"})

	sem := ComputeMessageSemantics("privmsg", "me", "deploy is done", "", "me")
	if sem.Highlight {
		t.Fatalf("self-authored messages must not highlight, got %+v", sem)
	}
}

func TestHighlightEmptySet(t *testing.T) {
	setPatterns(t, nil)

	sem := ComputeMessageSemantics("privmsg", "alice", "anything at all", "", "me")
	if sem.Highlight || sem.HighlightPattern != "" {
		t.Fatalf("expected no highlight with empty set, got %+v", sem)
	}
}

func TestHighlightIndependentOfMention(t *testing.T) {
	setPatterns(t, []string{"kubernetes"})

	sem := ComputeMessageSemantics("privmsg", "alice", "me: kubernetes is down", "", "me")
	if !sem.MentionsMe || !sem.Highlight {
		t.Fatalf("mention and highlight should both fire, got %+v", sem)
	}
}
