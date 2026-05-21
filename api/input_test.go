package api

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestParseInput(t *testing.T) {
	bufID := uuid.New()
	netID := uuid.New()
	buf := inputBuffer{ID: bufID, NetworkID: netID, Name: "#foo"}

	tests := []struct {
		name     string
		content  string
		want     clientCmd
		wantErr  error
		errCheck func(error) bool
	}{
		{
			name:    "plain text becomes send",
			content: "hello world",
			want:    clientCmd{Type: "send", BufferID: bufID, Content: "hello world"},
		},
		{
			name:    "trims surrounding whitespace before send",
			content: "  hi  ",
			want:    clientCmd{Type: "send", BufferID: bufID, Content: "hi"},
		},
		{
			name:    "join with channel",
			content: "/join #bar",
			want:    clientCmd{Type: "join", BufferID: bufID, NetworkID: netID, Channel: "#bar"},
		},
		{
			name:    "msg target+content",
			content: "/msg alice hi  there",
			want:    clientCmd{Type: "msg", BufferID: bufID, NetworkID: netID, Target: "alice", Content: "hi  there"},
		},
		{
			name:    "me action",
			content: "/me waves at the room",
			want:    clientCmd{Type: "me", BufferID: bufID, NetworkID: netID, Content: "waves at the room"},
		},
		{
			name:    "part with reason",
			content: "/part bye all",
			want:    clientCmd{Type: "part", BufferID: bufID, NetworkID: netID, Content: "bye all"},
		},
		{
			name:    "invite without channel falls back to active buffer",
			content: "/invite alice",
			want:    clientCmd{Type: "invite", BufferID: bufID, NetworkID: netID, Target: "alice", Channel: "#foo"},
		},
		{
			name:    "invite with explicit channel",
			content: "/invite alice #other",
			want:    clientCmd{Type: "invite", BufferID: bufID, NetworkID: netID, Target: "alice", Channel: "#other"},
		},
		{
			name:    "kick with reason",
			content: "/kick alice spam",
			want:    clientCmd{Type: "kick", BufferID: bufID, NetworkID: netID, Target: "alice", Content: "spam"},
		},
		{
			name:    "kickban with reason",
			content: "/kickban alice spam",
			want:    clientCmd{Type: "kickban", BufferID: bufID, NetworkID: netID, Target: "alice", Content: "spam"},
		},
		{
			name:    "ctcp with args",
			content: "/ctcp alice VERSION foo bar",
			want:    clientCmd{Type: "ctcp", BufferID: bufID, NetworkID: netID, Target: "alice", Content: "VERSION foo bar"},
		},
		{
			name:    "cycle aliases rejoin",
			content: "/cycle",
			want:    clientCmd{Type: "rejoin", BufferID: bufID, NetworkID: netID},
		},
		{
			name:    "back command",
			content: "/back",
			want:    clientCmd{Type: "back", BufferID: bufID, NetworkID: netID},
		},
		{
			name:    "op target",
			content: "/op alice",
			want:    clientCmd{Type: "op", BufferID: bufID, NetworkID: netID, Target: "alice"},
		},
		{
			name:    "list with filter",
			content: "/list >5",
			want:    clientCmd{Type: "list", BufferID: bufID, NetworkID: netID, Content: ">5"},
		},
		{
			name:    "case-insensitive command name",
			content: "/JOIN #x",
			want:    clientCmd{Type: "join", BufferID: bufID, NetworkID: netID, Channel: "#x"},
		},
		{
			name:    "empty content is error",
			content: "   ",
			wantErr: ErrEmptyInput,
		},
		{
			name:    "unknown slash command is error",
			content: "/notarealcommand foo",
			wantErr: ErrUnknownSlashCommand,
		},
		{
			name:    "ctcp without args is error",
			content: "/ctcp alice",
			errCheck: func(err error) bool {
				return err != nil && err.Error() == "ctcp requires <nick> <command>"
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseInput(tt.content, buf)
			checkParseInputResult(t, got, err, tt.want, tt.wantErr, tt.errCheck)
		})
	}
}

func checkParseInputResult(t *testing.T, got clientCmd, err error, want clientCmd, wantErr error, errCheck func(error) bool) {
	t.Helper()
	if wantErr != nil {
		if !errors.Is(err, wantErr) {
			t.Fatalf("err = %v, want %v", err, wantErr)
		}
		return
	}
	if errCheck != nil {
		if !errCheck(err) {
			t.Fatalf("err = %v, did not match check", err)
		}
		return
	}
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}
