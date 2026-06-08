package main

import (
	"hash/fnv"

	"github.com/charmbracelet/lipgloss"
)

// IRCCloud Midnight-inspired palette. Single hardcoded theme; no switcher.
const (
	colorPanel       = "#1E232C"
	colorPanelAlt    = "#2C3344"
	colorBorder      = "#3A4458"
	colorText        = "#C5D2E0"
	colorTextDim     = "#7F8FA3"
	colorTextMuted   = "#5B6573"
	colorAccent      = "#3683B3"
	colorAccentBold  = "#5BA8D8"
	colorMention     = "#F0B33C"
	colorMentionBg   = "#4A3A1F"
	colorUnread      = "#FFFFFF"
	colorOk          = "#7BC47F"
	colorWarn        = "#F0B33C"
	colorBad         = "#E25B5B"
	colorSelf        = "#A1D490"
	colorActionEvent = "#7F8FA3"
)

// nickPalette: hues chosen to be readable on a dark background.
var nickPalette = []string{
	"#7CC0CC", // teal
	"#E27878", // red
	"#A1D490", // green
	"#F0B33C", // amber
	"#B294BB", // mauve
	"#9CC4E4", // sky
	"#E2A4D0", // pink
	"#D9C189", // tan
	"#7FB069", // moss
	"#E5A07B", // peach
	"#6FA8DC", // blue
	"#C088E0", // violet
}

func nickColor(nick string) lipgloss.Color {
	if nick == "" {
		return lipgloss.Color(colorText)
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(nick))
	idx := int(h.Sum32()&0x7fffffff) % len(nickPalette)
	return lipgloss.Color(nickPalette[idx])
}

// Styles. Constructed once at package init.
var (
	styleSidebar = lipgloss.NewStyle().
			Width(sidebarWidth).
			BorderRight(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color(colorBorder)).
			Background(lipgloss.Color(colorPanel))

	styleNetHeader = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(colorAccentBold)).
			Background(lipgloss.Color(colorPanel)).
			PaddingLeft(1)

	styleBufferItem = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorText)).
			Background(lipgloss.Color(colorPanel)).
			PaddingLeft(2)

	styleBufferUnread = lipgloss.NewStyle().
				Foreground(lipgloss.Color(colorUnread)).
				Background(lipgloss.Color(colorPanel)).
				Bold(true).
				PaddingLeft(2)

	styleBufferMention = lipgloss.NewStyle().
				Foreground(lipgloss.Color(colorMention)).
				Background(lipgloss.Color(colorPanel)).
				Bold(true).
				PaddingLeft(2)

	styleSelected = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorUnread)).
			Background(lipgloss.Color(colorBorder)).
			Bold(true).
			PaddingLeft(2)

	styleHeader = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(colorText)).
			Background(lipgloss.Color(colorPanelAlt)).
			BorderBottom(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color(colorBorder)).
			Padding(0, 1)

	styleStatus = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorTextDim)).
			Italic(true)

	styleSeparator = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorBorder))

	styleStatusOk   = lipgloss.NewStyle().Foreground(lipgloss.Color(colorOk)).Bold(true)
	styleStatusWarn = lipgloss.NewStyle().Foreground(lipgloss.Color(colorWarn)).Bold(true)
	styleStatusBad  = lipgloss.NewStyle().Foreground(lipgloss.Color(colorBad)).Bold(true)

	styleStatusBox = lipgloss.NewStyle().
			Background(lipgloss.Color(colorPanel)).
			Foreground(lipgloss.Color(colorText)).
			PaddingLeft(1).
			Width(sidebarWidth - 2)

	styleTimestamp = lipgloss.NewStyle().Foreground(lipgloss.Color(colorTextMuted))

	styleAction = lipgloss.NewStyle().Foreground(lipgloss.Color(colorActionEvent)).Italic(true)

	styleMentionLine = lipgloss.NewStyle().
				Foreground(lipgloss.Color(colorMention)).
				Background(lipgloss.Color(colorMentionBg)).
				Bold(true)

	styleSelfLine = lipgloss.NewStyle().Foreground(lipgloss.Color(colorSelf))

	styleMembersPane = lipgloss.NewStyle().
				BorderLeft(true).
				BorderStyle(lipgloss.NormalBorder()).
				BorderForeground(lipgloss.Color(colorBorder)).
				Background(lipgloss.Color(colorPanel)).
				Foreground(lipgloss.Color(colorText)).
				PaddingLeft(1)

	styleMemberOp    = lipgloss.NewStyle().Foreground(lipgloss.Color(colorMention)).Background(lipgloss.Color(colorPanel))
	styleMemberVoice = lipgloss.NewStyle().Foreground(lipgloss.Color(colorAccentBold)).Background(lipgloss.Color(colorPanel))
	styleMemberAway  = lipgloss.NewStyle().Foreground(lipgloss.Color(colorTextMuted)).Background(lipgloss.Color(colorPanel)).Italic(true)

	styleSwitcherBox = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color(colorAccent)).
				Background(lipgloss.Color(colorPanelAlt)).
				Foreground(lipgloss.Color(colorText)).
				Padding(0, 1)

	styleSwitcherItem = lipgloss.NewStyle().
				Foreground(lipgloss.Color(colorText)).
				Background(lipgloss.Color(colorPanelAlt))

	styleSwitcherItemSel = lipgloss.NewStyle().
				Foreground(lipgloss.Color(colorUnread)).
				Background(lipgloss.Color(colorBorder)).
				Bold(true)
)

// styledSender renders a nickname in its hashed palette color.
func styledSender(nick string, isSelf bool) string {
	if isSelf {
		return styleSelfLine.Render("<" + nick + ">")
	}
	return lipgloss.NewStyle().
		Foreground(nickColor(nick)).
		Bold(true).
		Render("<" + nick + ">")
}
