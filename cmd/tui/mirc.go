package main

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// mIRC control characters.
const (
	mircBold      = '\x02'
	mircItalic    = '\x1d'
	mircUnderline = '\x1f'
	mircStrike    = '\x1e'
	mircMono      = '\x11'
	mircReset     = '\x0f'
	mircColor     = '\x03'
)

// mIRC 16-color palette tuned for dark background.
var mircColors = [16]string{
	"#FFFFFF", // 0 white
	"#000000", // 1 black
	"#3683B3", // 2 blue
	"#7BC47F", // 3 green
	"#E25B5B", // 4 red
	"#A0522D", // 5 brown
	"#B294BB", // 6 magenta
	"#E5A07B", // 7 orange
	"#F0B33C", // 8 yellow
	"#9CDC8E", // 9 light green
	"#7CC0CC", // 10 cyan
	"#A0E0E8", // 11 light cyan
	"#5BA8D8", // 12 light blue
	"#E2A4D0", // 13 pink
	"#7F8FA3", // 14 grey
	"#C5D2E0", // 15 light grey
}

type mircState struct {
	bold, italic, underline, strike, mono bool
	fg, bg                                int // -1 = unset
}

func newMircState() mircState { return mircState{fg: -1, bg: -1} }

func (s mircState) active() bool {
	return s.bold || s.italic || s.underline || s.strike || s.fg >= 0 || s.bg >= 0
}

func (s mircState) style() lipgloss.Style {
	st := lipgloss.NewStyle()
	if s.bold {
		st = st.Bold(true)
	}
	if s.italic {
		st = st.Italic(true)
	}
	if s.underline {
		st = st.Underline(true)
	}
	if s.strike {
		st = st.Strikethrough(true)
	}
	if s.fg >= 0 && s.fg < len(mircColors) {
		st = st.Foreground(lipgloss.Color(mircColors[s.fg]))
	}
	if s.bg >= 0 && s.bg < len(mircColors) {
		st = st.Background(lipgloss.Color(mircColors[s.bg]))
	}
	return st
}

// mircFormat parses mIRC control codes and emits a lipgloss-styled string.
func mircFormat(input string) string {
	if !strings.ContainsAny(input, "\x02\x03\x0f\x11\x1d\x1e\x1f") {
		return input
	}
	var out, run strings.Builder
	st := newMircState()

	flush := func() {
		if run.Len() == 0 {
			return
		}
		if st.active() {
			out.WriteString(st.style().Render(run.String()))
		} else {
			out.WriteString(run.String())
		}
		run.Reset()
	}

	i := 0
	for i < len(input) {
		ch := input[i]
		if applied, next := applyToggleCode(&st, ch, i); applied {
			flush()
			i = next
			continue
		}
		if ch == mircColor {
			flush()
			i = applyColorCode(&st, input, i+1)
			continue
		}
		run.WriteByte(ch)
		i++
	}
	flush()
	return out.String()
}

// applyToggleCode handles single-byte toggle/reset codes. Returns (true, nextIdx)
// when the byte at idx was a control code consumed here, (false, idx) otherwise.
func applyToggleCode(st *mircState, ch byte, idx int) (applied bool, next int) {
	switch ch {
	case mircBold:
		st.bold = !st.bold
	case mircItalic:
		st.italic = !st.italic
	case mircUnderline:
		st.underline = !st.underline
	case mircStrike:
		st.strike = !st.strike
	case mircMono:
		st.mono = !st.mono
	case mircReset:
		*st = newMircState()
	default:
		return false, idx
	}
	return true, idx + 1
}

// applyColorCode parses a color spec at input[i:] and returns the new index.
// Empty digit run resets colors; otherwise sets fg and optionally bg.
func applyColorCode(st *mircState, input string, i int) int {
	fg, next := parseMircDigits(input, i)
	if fg < 0 {
		st.fg = -1
		st.bg = -1
		return i
	}
	if fg < len(mircColors) {
		st.fg = fg
	}
	i = next
	if i >= len(input) || input[i] != ',' {
		return i
	}
	bg, nbg := parseMircDigits(input, i+1)
	if bg < 0 {
		return i
	}
	if bg < len(mircColors) {
		st.bg = bg
	}
	return nbg
}

func parseMircDigits(s string, start int) (val, next int) {
	val = 0
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
