package main

import (
	"strings"
	"testing"
)

func TestFormatMessagePresenceUserhost(t *testing.T) {
	join := formatMessage(messageDTO{Kind: "join", Sender: "NFM-8", Userhost: "~nfm8@fuksi.nfm-8.info"}, "me")
	if !strings.Contains(join, "NFM-8 joined (~nfm8@fuksi.nfm-8.info)") {
		t.Errorf("join = %q", join)
	}
	quit := formatMessage(messageDTO{Kind: "quit", Sender: "NFM-8", Userhost: "~nfm8@fuksi.nfm-8.info", Content: "Quit: leaving"}, "me")
	if !strings.Contains(quit, "NFM-8 quit (~nfm8@fuksi.nfm-8.info) (Quit: leaving)") {
		t.Errorf("quit = %q", quit)
	}
	bare := formatMessage(messageDTO{Kind: "join", Sender: "x"}, "me")
	if !strings.Contains(bare, "x joined") || strings.Contains(bare, "(") {
		t.Errorf("bare join = %q", bare)
	}
}
