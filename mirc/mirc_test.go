package mirc

import (
	"reflect"
	"testing"
)

func seg(text string, mut ...func(*Segment)) Segment {
	s := Segment{Text: text}
	for _, m := range mut {
		m(&s)
	}
	return s
}

func bold(s *Segment)      { s.Bold = true }
func italic(s *Segment)    { s.Italic = true }
func underline(s *Segment) { s.Underline = true }
func strike(s *Segment)    { s.Strike = true }
func mono(s *Segment)      { s.Mono = true }
func fg(n int) func(*Segment) {
	return func(s *Segment) { s.Fg = &n }
}
func bg(n int) func(*Segment) {
	return func(s *Segment) { s.Bg = &n }
}

func TestParse(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []Segment
	}{
		{"empty", "", nil},
		{"plain text", "hello world", []Segment{seg("hello world")}},
		{"bold run", "a\x02bold\x02b", []Segment{seg("a"), seg("bold", bold), seg("b")}},
		{"italic", "\x1di\x1d", []Segment{seg("i", italic)}},
		{"underline", "\x1fu\x1f", []Segment{seg("u", underline)}},
		{"strike", "\x1es\x1e", []Segment{seg("s", strike)}},
		{"mono", "\x11m\x11", []Segment{seg("m", mono)}},
		{"overlapping styles", "\x02\x1dboth\x02\x1d", []Segment{seg("both", bold, italic)}},
		{
			"styles change mid-string",
			"\x02a\x1db\x02c\x1d",
			[]Segment{seg("a", bold), seg("b", bold, italic), seg("c", italic)},
		},
		{"reset clears all", "\x02\x1dab\x0fcd", []Segment{seg("ab", bold, italic), seg("cd")}},
		{"foreground color", "\x034red\x03plain", []Segment{seg("red", fg(4)), seg("plain")}},
		{"fg and bg color", "\x033,1fgbg\x03x", []Segment{seg("fgbg", fg(3), bg(1)), seg("x")}},
		{
			"bare color code resets colors only",
			"\x02\x034ab\x03cd\x02",
			[]Segment{seg("ab", bold, fg(4)), seg("cd", bold)},
		},
		{"out-of-range color ignored", "\x0399x", []Segment{seg("x")}},
		{"out-of-range keeps previous color", "\x034a\x0399b", []Segment{seg("a", fg(4)), seg("b", fg(4))}},
		{"unterminated style runs to end", "\x02unterminated", []Segment{seg("unterminated", bold)}},
		{"comma without bg digits stays text", "\x034,x", []Segment{seg(",x", fg(4))}},
		{"two-digit color", "\x0312blue\x03end", []Segment{seg("blue", fg(12)), seg("end")}},
		{"color code with no digits before text", "a\x03bcdef", []Segment{seg("a"), seg("bcdef")}},
		{"reverse video consumed", "a\x16b", []Segment{seg("ab")}},
		{"hex color consumed", "\x04ff0000RED\x04", []Segment{seg("RED")}},
		{"hex fg and bg consumed", "\x04AABBCC,112233both\x04", []Segment{seg("both")}},
		{"utf8 preserved", "Yön \x02Simo\x02 日本語", []Segment{seg("Yön "), seg("Simo", bold), seg(" 日本語")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Parse(tt.input)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Parse(%q) = %#v, want %#v", tt.input, got, tt.want)
			}
		})
	}
}

func TestStrip(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"plain", "Riku Lindblad", "Riku Lindblad"},
		{"passthrough punctuation", "Jonas Berlin · http://xkr47.space/", "Jonas Berlin · http://xkr47.space/"},
		{"color wrapped", "\x034RED\x03", "RED"},
		{"color with tail", "\x033green\x03 tail", "green tail"},
		{"two-digit color", "\x0312blue\x03end", "blueend"},
		{"fg bg color", "\x034,1RED ON BLACK\x03", "RED ON BLACK"},
		{"padded fg bg", "\x0304,01both\x03", "both"},
		{"bare color terminator", "foo\x03bar", "foobar"},
		{"hex color", "\x04ff0000RED\x04", "RED"},
		{"hex fg bg", "\x04AABBCC,112233both\x04", "both"},
		{"bold", "\x02bold\x02", "bold"},
		{"italic", "\x1ditalic\x1d", "italic"},
		{"underline", "\x1funder\x1f", "under"},
		{"reset", "a\x0fb", "ab"},
		{"reverse", "a\x16b", "ab"},
		{"strike", "a\x1eb", "ab"},
		{"mono", "a\x11b", "ab"},
		{"stacked codes", "\x02\x034,1Hello\x03\x02 World", "Hello World"},
		{"repeated colors", "a\x0312b\x0312c", "abc"},
		{"color no digits", "a\x03bcdef", "abcdef"},
		{"unicode", "Yön Simo", "Yön Simo"},
		{"cjk", "日本語", "日本語"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Strip(tt.input); got != tt.want {
				t.Errorf("Strip(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
