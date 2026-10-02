// Package ui is the terminal browser: a stack of screens that drills down
// from tags to events to markets to one market in detail, each drawn inside
// one frame with a breadcrumb above it and a status bar below.
//
// Nothing here blocks: every request runs in a tea.Cmd and comes back as a
// message.
package ui

import (
	"context"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/farrellm/polymarket/internal/api"
)

// Client is what the browser asks of the Polymarket services. *api.Client is
// the real one; the tests stand in a recorded one.
type Client interface {
	Events(ctx context.Context, q api.EventsQuery) (events []api.Event, next string, err error)
	Markets(ctx context.Context, q api.MarketsQuery) (markets []api.Market, next string, err error)
	Event(ctx context.Context, id string) (*api.Event, error)
	Market(ctx context.Context, id string) (*api.Market, error)
	Tag(ctx context.Context, slug string) (*api.Tag, error)
	RelatedTags(ctx context.Context, slug string) ([]api.Tag, error)
	Search(ctx context.Context, q api.SearchQuery) (*api.SearchResult, error)
	Book(ctx context.Context, tokenID string) (*api.Book, error)
	PriceHistory(ctx context.Context, q api.HistoryQuery) (points []api.PricePoint, next string, err error)
	Trades(ctx context.Context, q api.TradesQuery) (trades []api.Trade, next string, err error)
}

// Options are what the browser is opened on.
type Options struct {
	// Tag, if set, is the tag to start inside, with the Tags level above it.
	Tag *api.Tag
	// Filter is what the lists under the Tags level start out narrowed and
	// sorted by. The zero value stands for api.DefaultFilter.
	Filter api.Filter
	// Now is the clock the end dates are measured against; nil is time.Now.
	Now func() time.Time
	// OpenURL shows a page of polymarket.com; nil hands it to the system's
	// browser.
	OpenURL func(url string) error
}

// env is what every screen is built with.
type env struct {
	client Client
	keys   keyMap
	st     styles
	now    func() time.Time
	// openURL shows a page in the user's browser.
	openURL func(url string) error
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
	// tabs are the views the screen switches between, the one on show
	// marked, or empty for a screen with only the one.
	tabs() string
	// note goes at the right of the title bar: what the rows are a list of.
	note() string
	status() status
	// hints are the keys worth advertising in the status bar.
	hints() []key.Binding
	// typing reports that a prompt is open, so letters are text, not keys.
	typing() bool
	// exports are the datasets the screen can write out, the one most its
	// own first, or none if there is nothing to write yet.
	exports() []exportChoice
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
	// env is what the screens are built with, and the export dialog.
	env  env
	keys keyMap
	st   styles

	width, height int

	// stack is the levels drilled down through, the Tags level first. The
	// last one is on show; the others keep their state for the way back up.
	stack []screen
	// help is true while the keys are listed over the screen.
	help bool

	// dialog is the export dialog while it is open, and job the export
	// under way, if one is. Both are the root's rather than a screen's: an
	// export goes on while the levels are moved through.
	dialog *exportDialog
	job    *exportJob
	// notice is what the status bar says until the next key: how an export
	// ended.
	notice status
}

// New creates the browser, opened on the Tags level, or inside the tag the
// options name.
func New(client Client, o Options) *Model {
	m := &Model{
		keys: defaultKeyMap(),
		st:   newStyles(),
		// A sane default until the first WindowSizeMsg arrives.
		width:  80,
		height: 24,
	}
	e := env{client: client, keys: m.keys, st: m.st, now: o.Now, openURL: o.OpenURL}
	if e.now == nil {
		e.now = time.Now
	}
	if e.openURL == nil {
		e.openURL = openInBrowser
	}
	filter := o.Filter
	if filter.Order.Name == "" {
		filter.Order = api.DefaultFilter().Order
	}
	m.env = e
	m.stack = []screen{newTags(e, filter)}
	if o.Tag != nil {
		// The sample has not arrived, so there are no rows to start with.
		m.stack = append(m.stack, newBrowse(e, *o.Tag, filter, nil))
	}
	m.layout()
	return m
}

func (m *Model) Init() tea.Cmd {
	cmds := make([]tea.Cmd, len(m.stack))
	for i, s := range m.stack {
		cmds[i] = s.init()
	}
	return tea.Batch(cmds...)
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil

	case tea.KeyPressMsg:
		// A message is for the moment it was shown in.
		m.notice = status{}
		top := m.top()
		switch {
		case key.Matches(msg, m.keys.Interrupt):
			return m, m.quit()
		case m.help:
			// Any key puts the screen back.
			m.help = false
			return m, nil
		case m.dialog != nil:
			return m, m.updateExport(msg)
		case top.typing():
			// Everything else is the prompt's to read.
		case key.Matches(msg, m.keys.Help):
			m.help = true
			return m, nil
		case key.Matches(msg, m.keys.Quit):
			return m, m.quit()
		case key.Matches(msg, m.keys.Tags):
			m.popTo(1)
			return m, nil
		case key.Matches(msg, m.keys.Export):
			m.openExport()
			return m, nil
		case m.job != nil && key.Matches(msg, m.keys.Back):
			// Esc first stops the export; with none running it is the
			// screen's.
			m.job.stopping = true
			m.job.cancel()
			return m, nil
		}
		cmd, n := top.update(msg)
		return m, tea.Batch(cmd, m.navigate(n))
	}

	if cmd, ok := m.exported(msg); ok {
		return m, cmd
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
	if m.dialog != nil {
		// Text pasted into the dialog arrives as a message of its own.
		cmds = append(cmds, m.dialog.edit(msg))
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
	job := m.job
	if job == nil {
		return tea.Quit
	}
	// An export stopped half way takes its temporary file with it, which
	// it must be given the time to do.
	return func() tea.Msg {
		job.finish()
		return tea.QuitMsg{}
	}
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
	content := m.top().view()
	if m.help {
		_, market := m.top().(*detail)
		content = helpView(m.keys, m.st, inner, market)
	} else if m.dialog != nil {
		content = m.dialog.view(inner)
	}
	body := strings.Split(content, "\n")
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
	lead := m.st.border.Render("┌") + " " + m.st.app.Render("polymarket") + " " + m.st.border.Render("─") + " "
	tabs := ""
	if t := m.top().tabs(); t != "" {
		tabs = m.st.border.Render("─") + " " + t + " "
	}
	// Four levels down the breadcrumb is wider than a narrow window: the
	// levels furthest up give way, so that where one is and the tabs there
	// stay in view.
	head := ""
	for skip := range crumbs {
		trail := strings.Join(crumbs[skip:], crumbSeparator)
		if skip > 0 {
			trail = m.st.crumb.Render("…") + crumbSeparator + trail
		}
		head = lead + trail + " " + tabs
		if width(head) < m.width {
			break
		}
	}
	corner := m.st.border.Render("┐")

	// The note gives way first, losing its end and then going altogether;
	// after it, the end of the breadcrumb.
	const leastNote = 8
	tail := corner
	if note := m.top().note(); note != "" {
		// A rule's width is kept between the two, so they do not run together.
		if room := m.width - width(head) - width(corner) - 3; room >= min(leastNote, width(note)) {
			tail = " " + m.st.note.Render(elide(note, room)) + " " + corner
		}
	}
	head = clip(head, m.width-width(tail))
	return head + m.rule(m.width-width(head)-width(tail)) + tail
}

// statusBar is the line under the screen: where things stand on the left,
// the keys on the right.
func (m *Model) statusBar(w int) string {
	s := m.top().status()
	hints := m.top().hints()
	switch {
	case m.help:
		s, hints = status{text: "the keys"}, []key.Binding{m.keys.closeHelp()}
	case m.dialog != nil:
		s, hints = status{text: "export"}, []key.Binding{m.keys.startExport(), m.keys.Cancel, m.keys.Next}
	case m.top().typing():
		// A prompt is being typed into there.
	case m.notice != (status{}):
		s = m.notice
	case m.job != nil:
		// Esc is the export's while it runs, not the screen's.
		s.text = m.job.running()
		rest := hints
		hints = []key.Binding{m.keys.stopExport()}
		for _, b := range rest {
			if !slices.Contains(b.Keys(), "esc") {
				hints = append(hints, b)
			}
		}
	}
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
