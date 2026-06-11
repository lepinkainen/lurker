// Package mirc parses mIRC formatting control codes into structured
// segments. It is the single authoritative parser: the API ships parsed
// segments to clients (web renders HTML, TUI renders terminal styles) and
// Strip cleans control codes out of free-text fields such as realnames.
package mirc

import "strings"

// Control characters.
const (
	codeBold      = '\x02'
	codeColor     = '\x03'
	codeHexColor  = '\x04'
	codeReset     = '\x0f'
	codeMono      = '\x11'
	codeReverse   = '\x16'
	codeItalic    = '\x1d'
	codeStrike    = '\x1e'
	codeUnderline = '\x1f'
)

const controlChars = "\x02\x03\x04\x0f\x11\x16\x1d\x1e\x1f"

// MaxColor is the highest indexed-palette color code that takes effect.
// Codes above it are consumed but leave the current color unchanged.
const MaxColor = 15

// Segment is a run of text with uniform formatting. Fg/Bg are mIRC palette
// indexes 0-15; nil means unset. Hex colors (\x04) are consumed by the
// parser but not carried: neither client renders them.
type Segment struct {
	Text      string `json:"text"`
	Bold      bool   `json:"bold,omitzero"`
	Italic    bool   `json:"italic,omitzero"`
	Underline bool   `json:"underline,omitzero"`
	Strike    bool   `json:"strike,omitzero"`
	Mono      bool   `json:"mono,omitzero"`
	Fg        *int   `json:"fg,omitempty"`
	Bg        *int   `json:"bg,omitempty"`
}

// Styled reports whether the segment carries any formatting; renderers can
// emit unstyled segments as plain text.
func (s Segment) Styled() bool {
	return s.Bold || s.Italic || s.Underline || s.Strike || s.Mono || s.Fg != nil || s.Bg != nil
}

// Parse splits input into segments of uniform formatting. Plain input
// yields a single unstyled segment; empty input yields nil.
func Parse(input string) []Segment {
	if input == "" {
		return nil
	}
	if !strings.ContainsAny(input, controlChars) {
		return []Segment{{Text: input}}
	}

	var out []Segment
	var run strings.Builder
	st := Segment{}

	flush := func() {
		if run.Len() == 0 {
			return
		}
		seg := st
		seg.Text = run.String()
		out = append(out, seg)
		run.Reset()
	}

	i := 0
	for i < len(input) {
		ch := input[i]
		switch ch {
		case codeBold, codeItalic, codeUnderline, codeStrike, codeMono:
			flush()
			toggle(&st, ch)
			i++
		case codeReset:
			flush()
			st = Segment{}
			i++
		case codeReverse:
			// Reverse video: consumed but not rendered by any client.
			i++
		case codeColor:
			flush()
			i = applyColor(&st, input, i+1)
		case codeHexColor:
			i = skipHexColor(input, i+1)
		default:
			run.WriteByte(ch)
			i++
		}
	}
	flush()
	return out
}

// SegmentsForWire parses message content for transport. Returns nil when
// the content carries no formatting codes, so plain messages (the vast
// majority) add nothing to the payload; clients fall back to rendering the
// raw content.
func SegmentsForWire(content string) []Segment {
	if !strings.ContainsAny(content, controlChars) {
		return nil
	}
	return Parse(content)
}

// Strip removes all mIRC formatting codes, returning plain text.
func Strip(input string) string {
	if !strings.ContainsAny(input, controlChars) {
		return input
	}
	var b strings.Builder
	b.Grow(len(input))
	for _, seg := range Parse(input) {
		b.WriteString(seg.Text)
	}
	return b.String()
}

func toggle(st *Segment, ch byte) {
	switch ch {
	case codeBold:
		st.Bold = !st.Bold
	case codeItalic:
		st.Italic = !st.Italic
	case codeUnderline:
		st.Underline = !st.Underline
	case codeStrike:
		st.Strike = !st.Strike
	case codeMono:
		st.Mono = !st.Mono
	}
}

// applyColor parses an indexed color spec at input[i:] and returns the new
// index. No digits resets both colors. Out-of-range codes are consumed but
// leave the current color unchanged. A comma not followed by digits is left
// as text.
func applyColor(st *Segment, input string, i int) int {
	fg, next := parseDigits(input, i)
	if fg < 0 {
		st.Fg = nil
		st.Bg = nil
		return i
	}
	if fg <= MaxColor {
		st.Fg = new(fg)
	}
	i = next
	if i >= len(input) || input[i] != ',' {
		return i
	}
	bg, nbg := parseDigits(input, i+1)
	if bg < 0 {
		return i
	}
	if bg <= MaxColor {
		st.Bg = new(bg)
	}
	return nbg
}

// skipHexColor consumes \x04RRGGBB[,RRGGBB] color specs (1-6 hex digits per
// component, comma without digits left as text).
func skipHexColor(input string, i int) int {
	n := 0
	for n < 6 && i < len(input) && isHex(input[i]) {
		i++
		n++
	}
	if n == 0 || i >= len(input) || input[i] != ',' {
		return i
	}
	j, m := i+1, 0
	for m < 6 && j < len(input) && isHex(input[j]) {
		j++
		m++
	}
	if m == 0 {
		return i
	}
	return j
}

func parseDigits(s string, start int) (val, next int) {
	consumed := 0
	for consumed < 2 && start+consumed < len(s) {
		c := s[start+consumed]
		if c < '0' || c > '9' {
			break
		}
		val = val*10 + int(c-'0')
		consumed++
	}
	if consumed == 0 {
		return -1, start
	}
	return val, start + consumed
}

func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'A' && c <= 'F') || (c >= 'a' && c <= 'f')
}
