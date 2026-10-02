package ui

import (
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// column is one column of a list.
type column struct {
	title string
	// width is the room the column takes. Zero means whatever the others
	// leave, which one column of a list may ask for.
	width int
	// right aligns the column to the right, as numbers are.
	right bool
}

const (
	// listMargin is the column on the left the cursor's marker sits in, and
	// the blank one on the right that balances it.
	listMargin = 1
	// listGutter is the space between two columns.
	listGutter = 2
	// minFlexWidth is the least the column of leftover width is given before
	// the rightmost column is dropped to make room for it.
	minFlexWidth = 16

	cursorMarker = "▸"
)

// list is a table of rows with a header and a cursor, scrolled so the cursor
// stays in view. It is what every level above the market detail is drawn as.
//
// It is not bubbles' table: that one leaves the cursor outside the window
// when the cursor is set rather than moved, which is what re-sorting a list
// under the cursor does, and it has no way to align a column of numbers.
type list struct {
	st   styles
	cols []column
	// rows hold one cell per column, in the columns' order.
	rows [][]string
	// empty stands in for the rows when there are none.
	empty string

	cursor int
	// top is the first row in view.
	top int
	// height counts the header line as well as the rows.
	width, height int
}

func newList(st styles, cols []column) list {
	return list{st: st, cols: cols, width: 80, height: 20}
}

func (l *list) setSize(w, h int) {
	l.width, l.height = w, h
	l.setCursor(l.cursor)
}

func (l *list) setColumns(cols []column) { l.cols = cols }

// setRows replaces the rows, keeping the cursor on the row number it was on
// as far as there still is one.
func (l *list) setRows(rows [][]string) {
	l.rows = rows
	l.setCursor(l.cursor)
}

// pageSize is the number of rows in view at once.
func (l *list) pageSize() int { return max(l.height-1, 1) }

// setCursor puts the cursor on a row, or on the nearest there is, and scrolls
// just far enough to show it.
func (l *list) setCursor(row int) {
	l.cursor = max(min(row, len(l.rows)-1), 0)
	page := l.pageSize()
	if l.cursor < l.top {
		l.top = l.cursor
	}
	if l.cursor >= l.top+page {
		l.top = l.cursor - page + 1
	}
	// A taller window, or fewer rows, must not leave blank lines below the
	// last row while rows above the first are out of view.
	l.top = max(min(l.top, len(l.rows)-page), 0)
}

func (l *list) moveBy(rows int) { l.setCursor(l.cursor + rows) }

// handle moves the cursor if msg is one of the movement keys, and reports
// whether it was.
func (l *list) handle(msg tea.KeyPressMsg, k keyMap) bool {
	switch {
	case key.Matches(msg, k.Up):
		l.moveBy(-1)
	case key.Matches(msg, k.Down):
		l.moveBy(+1)
	case key.Matches(msg, k.PageUp):
		l.moveBy(-l.pageSize())
	case key.Matches(msg, k.PageDown):
		l.moveBy(+l.pageSize())
	case key.Matches(msg, k.HalfUp):
		l.moveBy(-max(l.pageSize()/2, 1))
	case key.Matches(msg, k.HalfDown):
		l.moveBy(+max(l.pageSize()/2, 1))
	case key.Matches(msg, k.Top):
		l.setCursor(0)
	case key.Matches(msg, k.Bottom):
		l.setCursor(len(l.rows) - 1)
	default:
		return false
	}
	return true
}

// layout gives the columns their widths for the room there is. A window too
// narrow for them all loses columns from the right, so that the first column,
// which names the row, keeps a readable width.
func (l *list) layout() []column {
	cols := slices.Clone(l.cols)
	for {
		room := l.width - 2*listMargin - listGutter*(len(cols)-1)
		flex := -1
		for i, c := range cols {
			if c.width == 0 {
				flex = i
			}
			room -= c.width
		}
		least := 0
		if flex >= 0 {
			least = minFlexWidth
		}
		if room >= least || len(cols) <= 1 {
			if flex >= 0 {
				cols[flex].width = max(room, 1)
			}
			return cols
		}
		cols = cols[:len(cols)-1]
	}
}

// view renders the header and the rows in view, each exactly as wide as the
// list.
func (l *list) view() string {
	cols := l.layout()
	titles := make([]string, len(cols))
	for i, c := range cols {
		titles[i] = c.title
	}

	var b strings.Builder
	b.WriteString(l.st.header.Render(l.line(cols, titles, " ")))
	if len(l.rows) == 0 {
		if l.empty != "" {
			b.WriteByte('\n')
			b.WriteString(l.st.faint.Render(fit(" "+l.empty, l.width, false)))
		}
		return b.String()
	}
	for i := l.top; i < min(l.top+l.pageSize(), len(l.rows)); i++ {
		b.WriteByte('\n')
		if i == l.cursor {
			// A cell's own colour would end the highlight where the cell does.
			b.WriteString(l.st.selected.Render(ansi.Strip(l.line(cols, l.rows[i], cursorMarker))))
		} else {
			b.WriteString(l.line(cols, l.rows[i], " "))
		}
	}
	return b.String()
}

// line lays one row's cells out under the columns.
func (l *list) line(cols []column, cells []string, marker string) string {
	var b strings.Builder
	b.WriteString(marker)
	for i, c := range cols {
		if i > 0 {
			b.WriteString(strings.Repeat(" ", listGutter))
		}
		cell := ""
		if i < len(cells) {
			cell = cells[i]
		}
		b.WriteString(fit(elide(cell, c.width), c.width, c.right))
	}
	return fit(b.String(), l.width, false)
}
