package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// styles is every style the browser draws with.
//
// They use text attributes and the terminal's own sixteen colours rather than
// fixed ones, so they follow a light or a dark theme without asking the
// terminal which it is. Nothing is told apart by colour alone: the selected
// row has a marker, an error says that it is one.
type styles struct {
	border   lipgloss.Style
	app      lipgloss.Style
	crumb    lipgloss.Style
	here     lipgloss.Style // the last crumb: the screen being shown
	note     lipgloss.Style
	header   lipgloss.Style
	selected lipgloss.Style
	hintKey  lipgloss.Style
	hint     lipgloss.Style
	failure  lipgloss.Style
	faint    lipgloss.Style
	tab      lipgloss.Style // the tab on show
	up       lipgloss.Style // a price that rose
	down     lipgloss.Style // a price that fell
}

func newStyles() styles {
	return styles{
		border:   lipgloss.NewStyle().Faint(true),
		app:      lipgloss.NewStyle().Bold(true),
		crumb:    lipgloss.NewStyle(),
		here:     lipgloss.NewStyle().Bold(true),
		note:     lipgloss.NewStyle().Faint(true),
		header:   lipgloss.NewStyle().Bold(true),
		selected: lipgloss.NewStyle().Reverse(true),
		hintKey:  lipgloss.NewStyle().Bold(true),
		hint:     lipgloss.NewStyle().Faint(true),
		failure:  lipgloss.NewStyle().Foreground(lipgloss.Red),
		faint:    lipgloss.NewStyle().Faint(true),
		tab:      lipgloss.NewStyle().Bold(true),
		up:       lipgloss.NewStyle().Foreground(lipgloss.Green),
		down:     lipgloss.NewStyle().Foreground(lipgloss.Red),
	}
}

// width is the number of terminal columns s takes, styling aside.
func width(s string) int { return ansi.StringWidth(s) }

// clip cuts s to at most w columns, with no ellipsis: it keeps a line inside
// the frame rather than signalling that something was left out.
func clip(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if width(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "")
}

// elide cuts s to at most w columns, ending it in an ellipsis if it was cut.
func elide(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return ansi.Truncate(s, w, "…")
}

// fit makes s exactly w columns wide, cutting it or padding it with spaces,
// on the left if right is set and on the right otherwise.
func fit(s string, w int, right bool) string {
	s = clip(s, w)
	pad := strings.Repeat(" ", max(w-width(s), 0))
	if right {
		return pad + s
	}
	return s + pad
}
