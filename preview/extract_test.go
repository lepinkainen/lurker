package preview

import (
	"reflect"
	"testing"
)

func TestExtractURLs(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"none", "hello world", nil},
		{"single", "see https://example.com", []string{"https://example.com"}},
		{"trailing dot", "look at https://example.com/path.", []string{"https://example.com/path"}},
		{"trailing paren", "(https://example.com)", []string{"https://example.com"}},
		{"trailing punct combo", "https://example.com/a.,;", []string{"https://example.com/a"}},
		{"multiple unique", "a https://one.test b http://two.test c",
			[]string{"https://one.test", "http://two.test"}},
		{"dedup", "https://dup.test and again https://dup.test",
			[]string{"https://dup.test"}},
		{"ignored scheme", "ftp://nope.test", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractURLs(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ExtractURLs(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
