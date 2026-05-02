package db

import "testing"

func TestNetworkConnectCommandsPreserveOrderAndReplace(t *testing.T) {
	ctx := t.Context()
	d, err := OpenControl(t.TempDir() + "/control.db")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()

	n, err := CreateNetwork(ctx, d, Network{Name: "Libera", Host: "irc.libera.chat", Port: 6697, TLS: true, Nick: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	setErr := SetNetworkConnectCommands(ctx, d, n.ID, []string{"  PRIVMSG NickServ :IDENTIFY secret  ", "", "MODE tester +x"})
	if setErr != nil {
		t.Fatal(setErr)
	}
	got, err := ListNetworkConnectCommands(ctx, d, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"PRIVMSG NickServ :IDENTIFY secret", "MODE tester +x"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("commands = %#v, want %#v", got, want)
	}

	setErr = SetNetworkConnectCommands(ctx, d, n.ID, []string{"PRIVMSG ChanServ :INVITE #private"})
	if setErr != nil {
		t.Fatal(setErr)
	}
	got, err = ListNetworkConnectCommands(ctx, d, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "PRIVMSG ChanServ :INVITE #private" {
		t.Fatalf("commands after replace = %#v", got)
	}
}

func TestNetworkConnectCommandsCascadeOnNetworkDelete(t *testing.T) {
	ctx := t.Context()
	d, err := OpenControl(t.TempDir() + "/control.db")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()

	n, err := CreateNetwork(ctx, d, Network{Name: "Libera", Host: "irc.libera.chat", Port: 6697, TLS: true, Nick: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	if err := SetNetworkConnectCommands(ctx, d, n.ID, []string{"MODE tester +x"}); err != nil {
		t.Fatal(err)
	}
	if err := DeleteNetwork(ctx, d, n.ID); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM network_connect_commands WHERE network_id = ?`, n.ID[:]).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("connect commands after delete = %d, want 0", count)
	}
}
