package irc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"
)

func sortedSetKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestSemanticKindsContract locks the server-side IRC kind sets to the shared
// cross-language fixture. The web client consumes server-computed display_kind
// and the is_self/mentions_me/counts_as_unread flags directly (see
// web/src/messages.ts msgDisplayKind), so the only kind set still mirrored
// client-side is presenceKinds (PRESENCE_KINDS). Both languages assert against
// testdata/semantic-kinds.json: changing a set on one side without updating the
// fixture — and the other side — fails here or in
// web/tests/semantics-contract.test.ts.
func TestSemanticKindsContract(t *testing.T) {
	path := filepath.Join("..", "testdata", "semantic-kinds.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var fx struct {
		PresenceKinds  []string `json:"presenceKinds"`
		NonUnreadKinds []string `json:"nonUnreadKinds"`
		SysKinds       []string `json:"sysKinds"`
	}
	if err := json.Unmarshal(data, &fx); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	for _, c := range []struct {
		name      string
		got, want []string
	}{
		{"presenceKinds", sortedSetKeys(presenceKinds), fx.PresenceKinds},
		{"nonUnreadKinds", sortedSetKeys(nonUnreadKinds), fx.NonUnreadKinds},
		{"sysKinds", sortedSetKeys(sysKinds), fx.SysKinds},
	} {
		if !slices.Equal(c.got, c.want) {
			t.Errorf("%s drifted from testdata/semantic-kinds.json:\n  go      = %v\n  fixture = %v\nupdate the fixture AND web/src/messages.ts PRESENCE_KINDS together", c.name, c.got, c.want)
		}
	}
}
