package irc

import "testing"

func TestEnsureUTF8(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"ascii", "hello world", "hello world"},
		{"valid_utf8", "räntää", "räntää"},
		{"latin1_finnish", "r\xe4nt\xe4\xe4", "räntää"},
		{"latin1_oo", "v\xe4ke\xe4 usein", "väkeä usein"},
		{"latin1_mixed_chars", "k\xe4ytt\xf6ik\xe4\xe4", "käyttöikää"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ensureUTF8(tc.in)
			if got != tc.want {
				t.Fatalf("ensureUTF8(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
