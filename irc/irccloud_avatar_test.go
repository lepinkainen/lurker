package irc

import "testing"

func TestIRCCloudAvatarURL(t *testing.T) {
	tests := []struct {
		name    string
		ident   string
		host    string
		wantURL string
		wantOK  bool
	}{
		{
			name:    "sid ident",
			ident:   "sid305680",
			host:    "helmsley.irccloud.com",
			wantURL: "https://www.irccloud.com/avatar-redirect/s{size}/305680",
			wantOK:  true,
		},
		{
			name:    "uid ident",
			ident:   "uid123",
			host:    "foo.irccloud.com",
			wantURL: "https://www.irccloud.com/avatar-redirect/s{size}/123",
			wantOK:  true,
		},
		{
			name:    "leading tilde stripped",
			ident:   "~sid42",
			host:    "x.irccloud.com",
			wantURL: "https://www.irccloud.com/avatar-redirect/s{size}/42",
			wantOK:  true,
		},
		{
			name:    "bare irccloud.com host",
			ident:   "sid7",
			host:    "irccloud.com",
			wantURL: "https://www.irccloud.com/avatar-redirect/s{size}/7",
			wantOK:  true,
		},
		{
			name:   "non-irccloud host",
			ident:  "sid305680",
			host:   "example.com",
			wantOK: false,
		},
		{
			name:   "irccloud host, non-matching ident",
			ident:  "someguy",
			host:   "x.irccloud.com",
			wantOK: false,
		},
		{
			name:   "empty ident and host",
			ident:  "",
			host:   "",
			wantOK: false,
		},
		{
			name:    "case-insensitive host",
			ident:   "sid305680",
			host:    "X.IRCCloud.Com",
			wantURL: "https://www.irccloud.com/avatar-redirect/s{size}/305680",
			wantOK:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url, ok := irccloudAvatarURL(tt.ident, tt.host)
			if ok != tt.wantOK {
				t.Fatalf("irccloudAvatarURL(%q, %q) ok = %v, want %v", tt.ident, tt.host, ok, tt.wantOK)
			}
			if url != tt.wantURL {
				t.Fatalf("irccloudAvatarURL(%q, %q) url = %q, want %q", tt.ident, tt.host, url, tt.wantURL)
			}
		})
	}
}
