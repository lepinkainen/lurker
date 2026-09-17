package irc

import (
	"testing"

	"github.com/lepinkainen/lurker/nickcolor"
)

func TestComputeMessageSemantics(t *testing.T) {
	tests := []struct {
		name    string
		kind    string
		sender  string
		content string
		nick    string
		want    MessageSemantics
	}{
		{
			name:    "plain privmsg counts as unread",
			kind:    "privmsg",
			sender:  "alice",
			content: "hi there",
			nick:    "bob",
			want:    MessageSemantics{DisplayKind: "message", CountsAsUnread: true},
		},
		{
			name:    "mention sets MentionsMe and CountsAsUnread",
			kind:    "privmsg",
			sender:  "alice",
			content: "hey bob, ping",
			nick:    "bob",
			want:    MessageSemantics{DisplayKind: "message", CountsAsUnread: true, MentionsMe: true},
		},
		{
			name:    "case-insensitive nick mention",
			kind:    "privmsg",
			sender:  "alice",
			content: "BoB",
			nick:    "bob",
			want:    MessageSemantics{DisplayKind: "message", CountsAsUnread: true, MentionsMe: true},
		},
		{
			name:    "substring is not a mention (word boundary)",
			kind:    "privmsg",
			sender:  "alice",
			content: "bobcat is fluffy",
			nick:    "bob",
			want:    MessageSemantics{DisplayKind: "message", CountsAsUnread: true, MentionsMe: false},
		},
		{
			name:    "self-authored sets IsSelf",
			kind:    "privmsg",
			sender:  "Bob",
			content: "hi",
			nick:    "bob",
			want:    MessageSemantics{DisplayKind: "message", CountsAsUnread: true, IsSelf: true},
		},
		{
			name:    "join is sys and does not count as unread",
			kind:    "join",
			sender:  "alice",
			content: "",
			nick:    "bob",
			want:    MessageSemantics{DisplayKind: "sys", CountsAsUnread: false},
		},
		{
			name:    "notice display_kind",
			kind:    "notice",
			sender:  "alice",
			content: "heads up",
			nick:    "bob",
			want:    MessageSemantics{DisplayKind: "notice", CountsAsUnread: true},
		},
		{
			name:    "ctcp display_kind, not unread",
			kind:    "ctcp",
			sender:  "alice",
			content: "VERSION",
			nick:    "bob",
			want:    MessageSemantics{DisplayKind: "ctcp", CountsAsUnread: true},
		},
		{
			name:    "action display_kind",
			kind:    "action",
			sender:  "alice",
			content: "waves",
			nick:    "bob",
			want:    MessageSemantics{DisplayKind: "action", CountsAsUnread: true},
		},
		{
			name:    "empty nick suppresses mention/self detection",
			kind:    "privmsg",
			sender:  "alice",
			content: "hi bob",
			nick:    "",
			want:    MessageSemantics{DisplayKind: "message", CountsAsUnread: true},
		},
		{
			name:    "connected status is sys, not unread",
			kind:    "connected",
			sender:  "*",
			content: "",
			nick:    "bob",
			want:    MessageSemantics{DisplayKind: "sys", CountsAsUnread: false},
		},
		{
			name:    "nick with regex metachar . does not match arbitrary char",
			kind:    "privmsg",
			sender:  "alice",
			content: "hi boba",
			nick:    "bo.",
			want:    MessageSemantics{DisplayKind: "message", CountsAsUnread: true, MentionsMe: false},
		},
		{
			name:    "server-sender welcome numeric does not mention or highlight",
			kind:    "notice",
			sender:  "underworld.no.quakenet.org",
			content: "Welcome to the QuakeNet IRC Network, Shrike",
			nick:    "Shrike",
			want:    MessageSemantics{DisplayKind: "notice", CountsAsUnread: true, MentionsMe: false, Highlight: false},
		},
		{
			name:    "normal user sender still mentions",
			kind:    "privmsg",
			sender:  "alice",
			content: "hi Shrike",
			nick:    "Shrike",
			want:    MessageSemantics{DisplayKind: "message", CountsAsUnread: true, MentionsMe: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeMessageSemantics(tt.kind, tt.sender, tt.content, "", tt.nick)
			// Color annotations are nick-derived and covered by
			// TestComputeMessageSemanticsColors; blank them so the flag
			// cases stay value-comparable.
			got.SenderColor = nil
			got.TargetColor = nil
			if got != tt.want {
				t.Fatalf("ComputeMessageSemantics(%q,%q,%q,%q) = %+v, want %+v",
					tt.kind, tt.sender, tt.content, tt.nick, got, tt.want)
			}
		})
	}
}

func TestComputeMessageSemanticsColors(t *testing.T) {
	colorOf := func(nick string) int { return nickcolor.Index(nick) }

	got := ComputeMessageSemantics("privmsg", "alice", "hi", "", "me")
	if got.SenderColor == nil || *got.SenderColor != colorOf("alice") {
		t.Fatalf("SenderColor = %v, want %d", got.SenderColor, colorOf("alice"))
	}
	if got.TargetColor != nil {
		t.Fatalf("TargetColor = %v, want nil for privmsg", got.TargetColor)
	}

	// kick/nick targets are nicks and get colored; other targets don't.
	kick := ComputeMessageSemantics("kick", "op", "bye", "victim", "me")
	if kick.TargetColor == nil || *kick.TargetColor != colorOf("victim") {
		t.Fatalf("kick TargetColor = %v, want %d", kick.TargetColor, colorOf("victim"))
	}
	rename := ComputeMessageSemantics("nick", "old", "", "new", "me")
	if rename.TargetColor == nil || *rename.TargetColor != colorOf("new") {
		t.Fatalf("nick TargetColor = %v, want %d", rename.TargetColor, colorOf("new"))
	}
	mode := ComputeMessageSemantics("mode", "op", "+o", "#chan", "me")
	if mode.TargetColor != nil {
		t.Fatalf("mode TargetColor = %v, want nil", mode.TargetColor)
	}

	// No sender (connected/disconnected sys lines) -> no color.
	sys := ComputeMessageSemantics("connected", "", "", "", "me")
	if sys.SenderColor != nil {
		t.Fatalf("SenderColor = %v, want nil for empty sender", sys.SenderColor)
	}
}
