package main

import (
	"strings"
	"testing"
)

// Regression for f301094 (fix(tui): flush mIRC run before toggling style).
// Before the fix, applyToggleCode mutated state before flush — so leading
// text rendered with the *next* style instead of the current one. The
// rendered output for "bold\x02plain" wrapped "bold" with bold styling
// instead of leaving it plain.
func TestMircFormatFlushesBeforeToggle(t *testing.T) {
	// Toggle bold off: "plain\x02bold\x02plain". The middle run "bold"
	// must be the only bolded segment. ANSI bold = ESC[1m.
	got := mircFormat("plain\x02bold\x02plain")
	if !strings.Contains(got, "plain") {
		t.Fatalf("missing literal text: %q", got)
	}
	// The leading "plain" must not be wrapped in a bold escape. ANSI bold
	// SGR sequence is "\x1b[1m". The substring up to "bold" should not
	// contain it; the substring containing "bold" must.
	boldStart := strings.Index(got, "bold")
	if boldStart < 0 {
		t.Fatalf("missing 'bold' segment: %q", got)
	}
	leading := got[:boldStart]
	if strings.Contains(leading, "\x1b[1m") {
		t.Fatalf("leading text incorrectly carries bold style; got %q", got)
	}
}

func TestMircFormatPassthroughNoCodes(t *testing.T) {
	if got := mircFormat("hello world"); got != "hello world" {
		t.Errorf("plain passthrough: got %q", got)
	}
}

func TestMircFormatResetClearsStyle(t *testing.T) {
	// \x02bold\x0fplain — reset must clear bold so trailing text is unstyled.
	got := mircFormat("\x02bold\x0fplain")
	tail := got[strings.LastIndex(got, "plain"):]
	if strings.Contains(tail, "\x1b[1m") {
		t.Errorf("trailing text after reset still bold: %q", got)
	}
}
