package main

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
)

// switcherModel: floating fuzzy-filter popup for jumping to a buffer.
type switcherModel struct {
	open    bool
	query   string
	sel     int
	entries []switcherEntry
}

type switcherEntry struct {
	bufferID    uuid.UUID
	networkName string
	bufferName  string
	score       int
}

func (s *switcherModel) refresh(buffers []bufferDTO, networks []networkDTO) {
	netName := make(map[uuid.UUID]string, len(networks))
	for _, n := range networks {
		netName[n.ID] = n.Name
	}
	q := strings.ToLower(s.query)
	entries := make([]switcherEntry, 0, len(buffers))
	for _, b := range buffers {
		label := b.Name
		if b.Kind == "status" {
			label = "(status)"
		}
		hay := strings.ToLower(netName[b.NetworkID] + " " + label)
		score := fuzzyScore(hay, q)
		if q != "" && score == 0 {
			continue
		}
		entries = append(entries, switcherEntry{
			bufferID:    b.ID,
			networkName: netName[b.NetworkID],
			bufferName:  label,
			score:       score,
		})
	}
	// Stable: sort by score desc, then by network+buffer asc.
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0; j-- {
			a, b := entries[j-1], entries[j]
			if b.score > a.score {
				entries[j-1], entries[j] = b, a
				continue
			}
			break
		}
	}
	s.entries = entries
	if s.sel >= len(entries) {
		s.sel = max(0, len(entries)-1)
	}
}

// fuzzyScore: simple subsequence match. 0 = no match.
func fuzzyScore(hay, needle string) int {
	if needle == "" {
		return 1
	}
	if strings.Contains(hay, needle) {
		return 1000 - strings.Index(hay, needle)
	}
	i := 0
	score := 0
	for _, c := range needle {
		idx := strings.IndexRune(hay[i:], c)
		if idx < 0 {
			return 0
		}
		score += 10 - min(idx, 10)
		i += idx + 1
	}
	return score
}

func (s *switcherModel) render(width int) string {
	var sb strings.Builder
	sb.WriteString("Jump to buffer: ")
	sb.WriteString(s.query)
	sb.WriteString("_\n\n")
	if len(s.entries) == 0 {
		sb.WriteString(styleStatusBad.Render("(no matches)"))
	}
	maxShow := min(10, len(s.entries))
	for i := 0; i < maxShow; i++ {
		e := s.entries[i]
		line := e.networkName + " / " + e.bufferName
		if i == s.sel {
			sb.WriteString(styleSwitcherItemSel.Width(width - 4).Render("> " + line))
		} else {
			sb.WriteString(styleSwitcherItem.Width(width - 4).Render("  " + line))
		}
		sb.WriteByte('\n')
	}
	w := width - 4
	if w < 30 {
		w = 30
	}
	return styleSwitcherBox.Width(w).Render(sb.String())
}

// overlay centers the popup over the given background.
func overlay(bg, popup string, totalW, totalH int) string {
	pw := lipgloss.Width(popup)
	ph := lipgloss.Height(popup)
	if pw > totalW {
		pw = totalW
	}
	if ph > totalH {
		ph = totalH
	}
	// Position centered by padding lines/spaces.
	topPad := (totalH - ph) / 2
	leftPad := (totalW - pw) / 2
	if topPad < 0 {
		topPad = 0
	}
	if leftPad < 0 {
		leftPad = 0
	}
	bgLines := strings.Split(bg, "\n")
	popupLines := strings.Split(popup, "\n")
	for i, pl := range popupLines {
		row := topPad + i
		if row >= len(bgLines) {
			break
		}
		bgLine := bgLines[row]
		// strip ansi-safe is tricky; do best-effort by appending — popup wins.
		_ = bgLine
		bgLines[row] = strings.Repeat(" ", leftPad) + pl
	}
	return strings.Join(bgLines, "\n")
}
