// Package ui is the terminal browser: a stack of screens that drills down
// from tags to events to markets, each drawn inside one frame with a
// breadcrumb above it and a status bar below.
//
// Nothing here blocks: every request runs in a tea.Cmd and comes back as a
// message.
package ui

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/farrellm/polymarket/internal/api"
)

// Client is what the browser asks of the Polymarket services. *api.Client is
// the real one; the tests stand in a recorded one.
type Client interface {
	Events(ctx context.Context, q api.EventsQuery) (events []api.Event, next string, err error)
	Tag(ctx context.Context, slug string) (*api.Tag, error)
}

// screen is one level of the browser.
type screen interface {
	// init starts whatever the screen needs loaded when it is first shown.
	init() tea.Cmd
	// update handles a key, or the result of a request, and says where to
	// go next. Results reach every screen on the stack, so each must
	// recognise its own.
	update(msg tea.Msg) (tea.Cmd, nav)
	// resize gives the screen the room inside the frame.
	resize(width, height int)
	// view draws the screen, in at most the lines it was given.
	view() string

	// crumb is the screen's part of the breadcrumb.
	crumb() string
	// note goes at the right of the title bar: what the rows are a list of.
	note() string
	status() status
	// hints are the keys worth advertising in the status bar.
	hints() []key.Binding
	// typing reports that a prompt is open, so letters are text, not keys.
	typing() bool
	// close stops the screen's requests. It is called on leaving the screen.
	close()
}

// status is the left of the status bar.
type status struct {
	text string
	// failure is what went wrong with the last request, if anything did. It
	// is shown beside the text, never instead of the screen.
	failure string
}

// nav is where a screen asks to go after handling a message. The zero value
// is to stay put.
type nav struct {
	// push goes down a level, to this screen.
	push screen
	// pop goes back up one.
	pop bool
}

// Model is the Bubble Tea model for the browser.
type Model struct {
	keys keyMap
	st   styles

	width, height int

	// stack is the levels drilled down through, the Tags level first. The
	// last one is on show; the others keep their state for the way back up.
	stack []screen
}

// New creates the browser, opened on the Tags level.
func New(client Client) *Model {
	m := &Model{
		keys: defaultKeyMap(),
		st:   newStyles(),
		// A sane default until the first WindowSizeMsg arrives.
		width:  80,
		height: 24,
	}
	m.stack = []screen{newTags(client, m.keys, m.st)}
	m.layout()
	return m
}

func (m *Model) Init() tea.Cmd { return m.stack[0].init() }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil

	case tea.KeyPressMsg:
		top := m.top()
		switch {
		case key.Matches(msg, m.keys.Interrupt):
			return m, m.quit()
		case top.typing():
			// Everything else is the prompt's to read.
		case key.Matches(msg, m.keys.Quit):
			return m, m.quit()
		case key.Matches(msg, m.keys.Tags):
			m.popTo(1)
			return m, nil
		}
		cmd, n := top.update(msg)
		return m, tea.Batch(cmd, m.navigate(n))
	}

	// The result of a request. It may belong to a screen further up the
	// stack, which goes on loading while a lower level is on show, so every
	// screen sees it. Only the one on show may navigate.
	var (
		cmds []tea.Cmd
		next nav
	)
	for i, s := range m.stack {
		cmd, n := s.update(msg)
		cmds = append(cmds, cmd)
		if i == len(m.stack)-1 {
			next = n
		}
	}
	cmds = append(cmds, m.navigate(next))
	return m, tea.Batch(cmds...)
}

func (m *Model) top() screen { return m.stack[len(m.stack)-1] }

// navigate moves down or up the stack as a screen asked.
func (m *Model) navigate(n nav) tea.Cmd {
	switch {
	case n.push != nil:
		m.stack = append(m.stack, n.push)
		m.layout()
		return n.push.init()
	case n.pop:
		// Going up from the Tags level leads nowhere.
		m.popTo(max(len(m.stack)-1, 1))
	}
	return nil
}

// popTo goes back up until the stack is depth screens deep. The screens left
// behind are closed; the one arrived at is as it was left, cursor included.
func (m *Model) popTo(depth int) {
	for len(m.stack) > depth {
		m.top().close()
		m.stack = m.stack[:len(m.stack)-1]
	}
	// The window may have changed size while the screen was out of view.
	m.layout()
}

func (m *Model) quit() tea.Cmd {
	for _, s := range m.stack {
		s.close()
	}
	return tea.Quit
}

// The frame takes a column on either side, and four lines: the title bar,
// the rule above the status bar, the status bar, and the bottom edge.
const (
	frameColumns = 2
	frameLines   = 4
)

func (m *Model) innerWidth() int { return max(m.width-frameColumns, 1) }
func (m *Model) bodyHeight() int { return max(m.height-frameLines, 1) }
func (m *Model) rule(n int) string {
	return m.st.border.Render(strings.Repeat("─", max(n, 0)))
}

// layout gives the screen on show the room inside the frame. The screens
// under it are sized when they come back into view.
func (m *Model) layout() {
	m.top().resize(m.innerWidth(), m.bodyHeight())
}

// View renders the whole frame.
func (m *Model) View() tea.View {
	var v tea.View
	v.AltScreen = true

	inner := m.innerWidth()
	edge := m.st.border.Render("│")
	lines := make([]string, 0, m.bodyHeight()+frameLines)
	lines = append(lines, m.titleBar())
	body := strings.Split(m.top().view(), "\n")
	for i := range m.bodyHeight() {
		line := ""
		if i < len(body) {
			line = body[i]
		}
		lines = append(lines, edge+fit(line, inner, false)+edge)
	}
	lines = append(lines,
		m.st.border.Render("├")+m.rule(inner)+m.st.border.Render("┤"),
		edge+m.statusBar(inner)+edge,
		m.st.border.Render("└")+m.rule(inner)+m.st.border.Render("┘"),
	)

	// A terminal smaller than the frame loses the bottom and the right of
	// it rather than wrapping, which would break up everything else.
	lines = lines[:min(len(lines), max(m.height, 1))]
	for i, l := range lines {
		lines[i] = clip(l, m.width)
	}
	v.Content = strings.Join(lines, "\n")
	return v
}

const crumbSeparator = " ▸ "

// titleBar is the top edge of the frame, with the breadcrumb set into it on
// the left and the screen's note on the right.
func (m *Model) titleBar() string {
	crumbs := make([]string, len(m.stack))
	for i, s := range m.stack {
		style := m.st.crumb
		if i == len(m.stack)-1 {
			style = m.st.here
		}
		crumbs[i] = style.Render(s.crumb())
	}
	head := m.st.border.Render("┌") + " " + m.st.app.Render("polymarket") + " " +
		m.st.border.Render("─") + " " + strings.Join(crumbs, crumbSeparator) + " "
	corner := m.st.border.Render("┐")

	tail := corner
	if note := m.top().note(); note != "" {
		tail = " " + m.st.note.Render(note) + " " + corner
	}
	// The note gives way first, then the end of the breadcrumb.
	if width(head)+width(tail) >= m.width {
		tail = corner
	}
	head = clip(head, m.width-width(tail))
	return head + m.rule(m.width-width(head)-width(tail)) + tail
}

// statusBar is the line under the screen: where things stand on the left,
// the keys on the right.
func (m *Model) statusBar(w int) string {
	s := m.top().status()
	left := s.text
	if s.failure != "" {
		if left != "" {
			left += " · "
		}
		left += m.st.failure.Render(s.failure)
	}
	left = " " + left

	// The hints give way to the status, the last of them first.
	const gap = 2
	hints := m.top().hints()
	right := ""
	for ; len(hints) > 0; hints = hints[:len(hints)-1] {
		right = m.hintLine(hints) + " "
		if width(left)+gap+width(right) <= w {
			break
		}
		right = ""
	}
	return fit(left, w-width(right), false) + right
}

func (m *Model) hintLine(bindings []key.Binding) string {
	parts := make([]string, len(bindings))
	for i, b := range bindings {
		h := b.Help()
		parts[i] = m.st.hintKey.Render(h.Key) + " " + m.st.hint.Render(h.Desc)
	}
	return strings.Join(parts, "  ")
}
