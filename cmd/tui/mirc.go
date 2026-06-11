package main

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/lepinkainen/lurker/mirc"
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

// mircFormat renders mIRC-formatted text as a lipgloss-styled string.
// Parsing is the shared server-side parser (mirc.Parse); this only maps
// segments onto terminal styles.
func mircFormat(input string) string {
	segs := mirc.Parse(input)
	if len(segs) == 1 && !segs[0].Styled() {
		return segs[0].Text
	}
	var out strings.Builder
	for _, seg := range segs {
		if !seg.Styled() {
			out.WriteString(seg.Text)
			continue
		}
		out.WriteString(segmentStyle(seg).Render(seg.Text))
	}
	return out.String()
}

func segmentStyle(seg mirc.Segment) lipgloss.Style {
	st := lipgloss.NewStyle()
	if seg.Bold {
		st = st.Bold(true)
	}
	if seg.Italic {
		st = st.Italic(true)
	}
	if seg.Underline {
		st = st.Underline(true)
	}
	if seg.Strike {
		st = st.Strikethrough(true)
	}
	if seg.Fg != nil && *seg.Fg >= 0 && *seg.Fg < len(mircColors) {
		st = st.Foreground(lipgloss.Color(mircColors[*seg.Fg]))
	}
	if seg.Bg != nil && *seg.Bg >= 0 && *seg.Bg < len(mircColors) {
		st = st.Background(lipgloss.Color(mircColors[*seg.Bg]))
	}
	return st
}
