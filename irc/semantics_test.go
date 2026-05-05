package irc

import "testing"

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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeMessageSemantics(tt.kind, tt.sender, tt.content, tt.nick)
			if got != tt.want {
				t.Fatalf("ComputeMessageSemantics(%q,%q,%q,%q) = %+v, want %+v",
					tt.kind, tt.sender, tt.content, tt.nick, got, tt.want)
			}
		})
	}
}
