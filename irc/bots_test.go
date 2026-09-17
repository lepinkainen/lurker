package irc

import (
	"testing"

	"github.com/lrstanley/girc"
)

// botTestHandler builds a handler wired only with the state the bot-mode
// paths touch (no DB, no hub).
func botTestHandler(client *girc.Client) *handler {
	return &handler{bots: newBotTracker(), userChannels: newUserChannels(), client: client}
}

func withISUPPORT(t *testing.T, tokens ...string) *girc.Client {
	t.Helper()
	client := newTestClient(t)
	params := append([]string{"tester"}, tokens...)
	e := girc.Event{Command: girc.RPL_ISUPPORT, Params: append(params, "are supported by this server")}
	runClientEvent(client, e)
	return client
}

func TestBotModeCharFromISUPPORT(t *testing.T) {
	if got := botModeChar(withISUPPORT(t, "BOT=B", "WHOX")); got != "B" {
		t.Fatalf("botModeChar = %q, want %q", got, "B")
	}
	if got := botModeChar(withISUPPORT(t, "WHOX")); got != "" {
		t.Fatalf("botModeChar without BOT token = %q, want empty", got)
	}
	// Multi-character values are malformed; treat the server as bot-mode-less
	// rather than matching a substring of a WHO flags field.
	if got := botModeChar(withISUPPORT(t, "BOT=BB")); got != "" {
		t.Fatalf("botModeChar for malformed token = %q, want empty", got)
	}
}

func TestWhoxBotReplyMarksAndClearsBot(t *testing.T) {
	client := withISUPPORT(t, "BOT=B", "WHOX")
	h := botTestHandler(client)

	h.onWhoxBotReply(client, mustEvent(t, ":fake 354 tester 9 helperbot H@B"))
	if !h.bots.isBot("HelperBot") {
		t.Fatal("helperbot not marked as bot from WHOX flags")
	}

	// Our own query is authoritative: a later reply without the mode clears it.
	h.onWhoxBotReply(client, mustEvent(t, ":fake 354 tester 9 helperbot H@"))
	if h.bots.isBot("helperbot") {
		t.Fatal("helperbot still marked as bot after flags dropped the mode char")
	}
}

func TestWhoxBotReplyIgnoresForeignQueryType(t *testing.T) {
	client := withISUPPORT(t, "BOT=B", "WHOX")
	h := botTestHandler(client)
	// Query type 1 is girc's own auto-WHO ("%tacuhnr"), whose fields differ.
	h.onWhoxBotReply(client, mustEvent(t, ":fake 354 tester 1 helperbot H@B"))
	if h.bots.isBot("helperbot") {
		t.Fatal("marked bot from a WHOX reply we did not request")
	}
}

func TestWhoReplyMarksBotButNeverClears(t *testing.T) {
	client := withISUPPORT(t, "BOT=B")
	h := botTestHandler(client)

	h.onWhoBotReply(client, mustEvent(t, ":fake 352 tester #test u host fake helperbot H@B :0 Helper Bot"))
	if !h.bots.isBot("helperbot") {
		t.Fatal("helperbot not marked as bot from RPL_WHOREPLY flags")
	}

	// A plain WHO reply that omits the flag may simply not carry it, so it
	// must not undo a previous positive observation.
	h.onWhoBotReply(client, mustEvent(t, ":fake 352 tester #test u host fake helperbot H@ :0 Helper Bot"))
	if !h.bots.isBot("helperbot") {
		t.Fatal("plain RPL_WHOREPLY cleared bot status")
	}
}

func TestWhoReplyIgnoredWithoutISUPPORTBot(t *testing.T) {
	client := newTestClient(t)
	h := botTestHandler(client)
	h.onWhoBotReply(client, mustEvent(t, ":fake 352 tester #test u host fake helperbot H@B :0 Helper Bot"))
	if h.bots.isBot("helperbot") {
		t.Fatal("marked bot on a server that never advertised BOT")
	}
}

func TestAwayFlagIsNotMistakenForBotMode(t *testing.T) {
	// A network using "G" as its bot mode char must not match every away user.
	client := withISUPPORT(t, "BOT=G", "WHOX")
	h := botTestHandler(client)
	h.onWhoxBotReply(client, mustEvent(t, ":fake 354 tester 9 someone G@"))
	if h.bots.isBot("someone") {
		t.Fatal("away flag matched the bot mode char")
	}
	h.onWhoxBotReply(client, mustEvent(t, ":fake 354 tester 9 someone G@G"))
	if !h.bots.isBot("someone") {
		t.Fatal("bot mode char after the away flag was not detected")
	}
}

func TestWhoisBotNumericMarksBot(t *testing.T) {
	client := withISUPPORT(t, "BOT=B")
	h := botTestHandler(client)
	h.onWhoisBot(client, mustEvent(t, ":fake 335 tester helperbot :is a bot on this network"))
	if !h.bots.isBot("helperbot") {
		t.Fatal("RPL_WHOISBOT did not mark the nick as a bot")
	}
}

func TestBotMessageTagMarksBot(t *testing.T) {
	client := withISUPPORT(t, "BOT=B")
	h := botTestHandler(client)
	h.noteBotTag(mustEvent(t, "@bot :helperbot!u@h PRIVMSG #test :hi"))
	if !h.bots.isBot("helperbot") {
		t.Fatal("bot message tag did not mark the nick as a bot")
	}

	h.noteBotTag(mustEvent(t, "@draft/bot :draftbot!u@h PRIVMSG #test :hi"))
	if !h.bots.isBot("draftbot") {
		t.Fatal("draft/bot message tag did not mark the nick as a bot")
	}

	h.noteBotTag(mustEvent(t, ":human!u@h PRIVMSG #test :hi"))
	if h.bots.isBot("human") {
		t.Fatal("untagged message marked the sender as a bot")
	}
}

func TestBotTrackerFollowsNickChangesAndQuits(t *testing.T) {
	bots := newBotTracker()
	bots.mark("HelperBot")
	bots.rename("helperbot", "HelperBot2")
	if bots.isBot("helperbot") {
		t.Fatal("old nick still marked after rename")
	}
	if !bots.isBot("helperbot2") {
		t.Fatal("new nick not marked after rename")
	}
	bots.unmark("HELPERBOT2")
	if bots.isBot("helperbot2") {
		t.Fatal("nick still marked after unmark")
	}
}

func TestChannelMembersCarryBotFlag(t *testing.T) {
	client := newTestClient(t)
	runClientEvent(client, mustEvent(t, ":helperbot!u@h JOIN #test"))
	runClientEvent(client, mustEvent(t, ":human!u@h JOIN #test"))
	runClientEvent(client, mustEvent(t, ":fake 353 tester = #test :helperbot human"))

	bots := newBotTracker()
	bots.mark("helperbot")
	members := buildChannelMembers(client, "#test", bots, nil)
	if len(members) != 2 {
		t.Fatalf("len(members) = %d, want 2", len(members))
	}
	for _, m := range members {
		want := m.Nick == "helperbot"
		if m.Bot != want {
			t.Fatalf("member %q bot = %v, want %v", m.Nick, m.Bot, want)
		}
	}
}

func TestMemberListHashCoversBotFlag(t *testing.T) {
	plain := []ChannelUser{{Nick: "helperbot"}}
	bot := []ChannelUser{{Nick: "helperbot", Bot: true}}
	if hashMemberList(plain) == hashMemberList(bot) {
		t.Fatal("member list hash ignores the bot flag, so republishes would be suppressed")
	}
}
