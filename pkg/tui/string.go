package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func truncateToWidth(s string, maxWidth int) string {
	s = stripANSI(s)
	if maxWidth <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= maxWidth {
		return s
	}
	if maxWidth == 1 {
		return "…"
	}
	runes := []rune(s)
	var b strings.Builder
	for _, r := range runes {
		next := b.String() + string(r)
		if lipgloss.Width(next)+1 > maxWidth {
			break
		}
		b.WriteRune(r)
	}
	return b.String() + "…"
}

func clipToWidth(s string, maxWidth int) string {
	s = stripANSI(s)
	if maxWidth <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= maxWidth {
		return s
	}
	runes := []rune(s)
	var b strings.Builder
	for _, r := range runes {
		next := b.String() + string(r)
		if lipgloss.Width(next) > maxWidth {
			break
		}
		b.WriteRune(r)
	}
	return b.String()
}

func stripANSI(s string) string {
	return ansiEscapeRe.ReplaceAllString(s, "")
}
