package ui

import (
	"cmp"
	"context"
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/farrellm/polymarket/internal/api"
	"github.com/farrellm/polymarket/internal/format"
)

const (
	// sampleSize is how many open events, the busiest over the last 24 hours
	// first, the tags are ranked over. There is no listing of tags worth
	// showing, so the tag level is what these events are filed under.
	sampleSize = 500
	// samplePage is the most events the listing hands out per request.
	samplePage = 100
)

// tagRow is one row of the Tags level: a tag and what the sampled events
// filed under it add up to.
type tagRow struct {
	// tag is the zero Tag on the All row, which stands for every event.
	tag       api.Tag
	events    int
	volume24h float64
	liquidity float64
}

func (r tagRow) isAll() bool { return r.tag.ID == "" }

func (r tagRow) label() string {
	if r.isAll() {
		return "All"
	}
	return r.tag.Label
}

// matches reports whether the row's name contains the text typed at the find
// prompt, which arrives in lower case.
func (r tagRow) matches(needle string) bool {
	return needle == "" ||
		strings.Contains(strings.ToLower(r.label()), needle) ||
		strings.Contains(strings.ToLower(r.tag.Slug), needle)
}

// aggregate adds the events up per tag. all is the row for the events taken
// together, which is not the sum of the others: an event is filed under
// several tags and counts towards each of them.
//
// The rows come back in the order the tags were first met.
func aggregate(events []api.Event) (all tagRow, rows []tagRow) {
	index := make(map[string]int)
	for i := range events {
		e := &events[i]
		// An event with no figure for either adds nothing to the sum.
		volume, liquidity := e.Volume24h.Value, e.Liquidity.Value
		all.events++
		all.volume24h += volume
		all.liquidity += liquidity

		for j, tag := range e.Tags {
			// A tag with no ID would be taken for the All row, and one
			// listed twice on an event still holds it only once.
			if tag.ID == "" || slices.ContainsFunc(e.Tags[:j], func(t api.Tag) bool { return t.ID == tag.ID }) {
				continue
			}
			at, ok := index[tag.ID]
			if !ok {
				at = len(rows)
				index[tag.ID] = at
				rows = append(rows, tagRow{tag: tag})
			}
			rows[at].events++
			rows[at].volume24h += volume
			rows[at].liquidity += liquidity
		}
	}
	return all, rows
}

// eventsUnder picks the events filed under a tag, in the order given. The
// empty ID is the All row's, and picks every one.
func eventsUnder(events []api.Event, tagID string) []api.Event {
	if tagID == "" {
		return events
	}
	var out []api.Event
	for i := range events {
		if slices.ContainsFunc(events[i].Tags, func(t api.Tag) bool { return t.ID == tagID }) {
			out = append(out, events[i])
		}
	}
	return out
}

// tagOrder is what the Tags level is sorted by.
type tagOrder int

// The orders, in the order the sort key cycles through them. Each starts
// the way round that is useful: the largest figure first, names from A.
const (
	byVolume tagOrder = iota
	byEvents
	byLiquidity
	byName
	numTagOrders
)

func compareNames(a, b tagRow) int {
	return cmp.Or(
		cmp.Compare(strings.ToLower(a.label()), strings.ToLower(b.label())),
		cmp.Compare(a.tag.Slug, b.tag.Slug),
		cmp.Compare(a.tag.ID, b.tag.ID),
	)
}

func (o tagOrder) compare(a, b tagRow) int {
	switch o {
	case byVolume:
		return cmp.Compare(b.volume24h, a.volume24h)
	case byEvents:
		return cmp.Compare(b.events, a.events)
	case byLiquidity:
		return cmp.Compare(b.liquidity, a.liquidity)
	default:
		return compareNames(a, b)
	}
}

// sortTags orders the rows. Rows that tie go by name whichever way round the
// sort is, so the same sample always reads the same.
func sortTags(rows []tagRow, o tagOrder, reversed bool) {
	slices.SortFunc(rows, func(a, b tagRow) int {
		c := o.compare(a, b)
		if reversed {
			c = -c
		}
		return cmp.Or(c, compareNames(a, b))
	})
}

// samplePageMsg is one page of the sample of events, or the failure to
// fetch it.
type samplePageMsg struct {
	gen    int
	events []api.Event
	next   string
	err    error
}

// tagMsg answers the lookup of a tag typed by name.
type tagMsg struct {
	gen  int
	slug string
	tag  *api.Tag
	err  error
}

// tags is the top level: one row per tag, ranked over a sample of the
// busiest open events.
type tags struct {
	env
	// filter is what a level opened from here starts out narrowed by.
	filter api.Filter

	list  list
	input textinput.Model
	// finding is true while the find prompt is open. The text typed there
	// goes on narrowing the list after the prompt is closed with enter.
	finding bool

	// events is the sample the rows are drawn from. A lower level opened
	// from here starts with the ones filed under its tag.
	events []api.Event
	// fresh is the sample being fetched. On the first load it is shown as
	// it grows; on a refresh the previous one stays until it is complete.
	fresh    []api.Event
	complete bool
	loading  bool
	failure  string
	// gen numbers the loads, so a page of one since abandoned is dropped.
	gen int
	// more fetches the page after a cursor within the load under way.
	more   func(cursor string) tea.Cmd
	cursor string
	stop   context.CancelFunc

	all tagRow
	// ranked is every tag in the sample; shown is the rows on screen, the
	// All row first, then those of ranked the find text leaves, sorted.
	ranked   []tagRow
	shown    []tagRow
	order    tagOrder
	reversed bool

	// looking is the slug being looked up, if one is; lookups numbers the
	// lookups as gen does the loads.
	looking    string
	lookups    int
	stopLookup context.CancelFunc
	flash      string
}

func newTags(e env, filter api.Filter) *tags {
	t := &tags{
		env:    e,
		filter: filter,
		input:  newPrompt("/"),
		// Nothing is shown before the first load, so it is as good as begun.
		loading: true,
	}
	t.list = newList(e.st, t.columns())
	t.refresh()
	return t
}

// newPrompt is a one-line prompt for the status bar.
func newPrompt(prompt string) textinput.Model {
	in := textinput.New()
	in.Prompt = prompt
	in.CharLimit = 64
	// A steady cursor: a blinking one is a timer that redraws the screen for
	// as long as the prompt is open.
	inputStyles := in.Styles()
	inputStyles.Cursor.Blink = false
	in.SetStyles(inputStyles)
	return in
}

func (t *tags) init() tea.Cmd { return t.reload() }

// reload starts fetching the sample afresh, abandoning any load under way.
func (t *tags) reload() tea.Cmd {
	if t.stop != nil {
		t.stop()
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.stop = cancel
	t.gen++
	t.fresh = nil
	t.loading = true
	t.failure = ""

	gen, client := t.gen, t.client
	t.more = func(cursor string) tea.Cmd {
		return func() tea.Msg {
			events, next, err := client.Events(ctx, api.EventsQuery{
				Limit:  samplePage,
				Cursor: cursor,
				Order:  "volume24hr",
				Status: api.StatusOpen,
			})
			return samplePageMsg{gen: gen, events: events, next: next, err: err}
		}
	}
	t.cursor = ""
	t.refresh()
	return t.more("")
}

func (t *tags) close() {
	if t.stop != nil {
		t.stop()
	}
	if t.stopLookup != nil {
		t.stopLookup()
	}
}

func (t *tags) resize(width, height int) {
	t.list.setSize(width, height)
}

func (t *tags) view() string { return t.list.view() }

func (t *tags) crumb() string { return "Tags" }

func (t *tags) tabs() string { return "" }

func (t *tags) note() string {
	if len(t.events) == 0 {
		return ""
	}
	return "ranked over the top " + strconv.Itoa(len(t.events)) + " open events"
}

func (t *tags) typing() bool { return t.finding }

// needle is the find text as it is matched: trimmed, in lower case.
func (t *tags) needle() string {
	return strings.ToLower(strings.TrimSpace(t.input.Value()))
}

func (t *tags) hints() []key.Binding {
	k := t.keys
	switch {
	case t.finding:
		return []key.Binding{k.Open, k.ClearFind}
	case t.needle() != "":
		return []key.Binding{k.ClearFind, k.Find, k.Sort, k.Refresh, k.Quit}
	default:
		return []key.Binding{k.Find, k.Sort, k.Refresh, k.Quit}
	}
}

func (t *tags) status() status {
	s := status{failure: t.failure}

	count := strconv.Itoa(len(t.ranked)) + " tags"
	if len(t.ranked) == 1 {
		count = "1 tag"
	}
	if t.needle() != "" {
		matching := 0
		for _, r := range t.shown {
			if !r.isAll() {
				matching++
			}
		}
		count = strconv.Itoa(matching) + " of " + count
	}

	switch {
	case t.finding:
		s.text = t.input.View() + "  " + t.st.faint.Render(count)
		return s
	case t.flash != "":
		s.text = t.flash
	case t.looking != "":
		s.text = "looking up the tag " + strconv.Quote(t.looking) + "…"
	case t.needle() != "":
		s.text = count + " matching " + strconv.Quote(t.needle())
	default:
		s.text = count
	}

	switch {
	case !t.loading:
	case t.complete:
		s.text += " · refreshing…"
	default:
		s.text += " · loading: " + strconv.Itoa(len(t.fresh)) + " of " + strconv.Itoa(sampleSize) + " events"
	}
	return s
}

// columns are the list's columns, with an arrow on the one sorted by.
func (t *tags) columns() []column {
	arrow := func(o tagOrder) string {
		if t.order != o {
			return ""
		}
		// Names start from A; every figure starts from the largest.
		if (o == byName) != t.reversed {
			return " ↑"
		}
		return " ↓"
	}
	return []column{
		{title: "Tag" + arrow(byName)},
		{title: "Events" + arrow(byEvents), width: 8, right: true},
		{title: "Vol 24h" + arrow(byVolume), width: 9, right: true},
		{title: "Liquidity" + arrow(byLiquidity), width: 11, right: true},
	}
}

// selected is the row under the cursor, if there is one.
func (t *tags) selected() (tagRow, bool) {
	if t.list.cursor >= len(t.shown) {
		return tagRow{}, false
	}
	return t.shown[t.list.cursor], true
}

// refresh rebuilds the rows on screen from the sample, the sort and the find
// text. The cursor stays on the tag it was on, wherever that has moved to,
// and goes to the first row if the tag is no longer listed.
func (t *tags) refresh() {
	was, had := t.selected()

	t.all, t.ranked = aggregate(t.events)
	sortTags(t.ranked, t.order, t.reversed)

	needle := t.needle()
	t.shown = t.shown[:0]
	if len(t.events) > 0 && t.all.matches(needle) {
		t.shown = append(t.shown, t.all)
	}
	for _, r := range t.ranked {
		if r.matches(needle) {
			t.shown = append(t.shown, r)
		}
	}

	rows := make([][]string, len(t.shown))
	cursor := 0
	for i, r := range t.shown {
		rows[i] = []string{
			r.label(),
			strconv.Itoa(r.events),
			format.Money(r.volume24h),
			format.Money(r.liquidity),
		}
		if had && r.tag.ID == was.tag.ID {
			cursor = i
		}
	}

	switch {
	case needle != "":
		t.list.empty = "no tag here matches " + strconv.Quote(needle) + "; enter looks it up by its slug"
	case t.loading:
		t.list.empty = "loading the top " + strconv.Itoa(sampleSize) + " open events…"
	case t.failure != "":
		// The status bar says what went wrong.
		t.list.empty = "the events could not be loaded"
	default:
		t.list.empty = "no open events"
	}
	t.list.setColumns(t.columns())
	t.list.setRows(rows)
	t.list.setCursor(cursor)
}

func (t *tags) update(msg tea.Msg) (tea.Cmd, nav) {
	switch msg := msg.(type) {
	case samplePageMsg:
		return t.addPage(msg), nav{}
	case tagMsg:
		return nil, t.found(msg)
	case tea.KeyPressMsg:
		if t.finding {
			return t.updateFind(msg)
		}
		return t.handleKey(msg)
	}
	if t.finding {
		// Text pasted into the prompt arrives as a message of its own.
		return t.edit(msg), nav{}
	}
	return nil, nav{}
}

// addPage takes in a page of the sample and asks for the next, until the
// sample is full or the listing ends.
func (t *tags) addPage(msg samplePageMsg) tea.Cmd {
	if msg.gen != t.gen {
		return nil // a page of a load since abandoned
	}
	err := msg.err
	if err == nil && msg.next != "" && msg.next == t.cursor {
		err = api.ErrCursorRepeated
	}
	if err != nil {
		// What is on screen stays: the part of a first load that arrived,
		// or the whole of the sample a refresh was to replace.
		t.loading = false
		t.failure = "could not load the events: " + err.Error() + " · r retries"
		t.refresh()
		return nil
	}

	t.fresh = append(t.fresh, msg.events...)
	if len(t.fresh) > sampleSize {
		t.fresh = t.fresh[:sampleSize]
	}
	done := msg.next == "" || len(msg.events) == 0 || len(t.fresh) == sampleSize
	if done {
		t.loading = false
		t.complete = true
	}
	if done || !t.complete {
		// fresh is only ever appended to from here, so sharing it is safe.
		t.events = t.fresh
	}
	t.refresh()
	if done {
		return nil
	}
	t.cursor = msg.next
	return t.more(msg.next)
}

func (t *tags) handleKey(msg tea.KeyPressMsg) (tea.Cmd, nav) {
	// A message is for the moment it was shown in.
	t.flash = ""

	k := t.keys
	switch {
	case t.list.handle(msg, k):
	case key.Matches(msg, k.Open):
		return nil, t.open()
	case key.Matches(msg, k.Back):
		// Esc first undoes the find; with none set it goes up a level.
		if t.input.Value() == "" {
			return nil, nav{pop: true}
		}
		t.input.SetValue("")
		t.refresh()
	case key.Matches(msg, k.Find):
		t.finding = true
		t.input.CursorEnd()
		// Focus only has a command to return for a cursor that blinks.
		_ = t.input.Focus()
	case key.Matches(msg, k.Sort):
		t.order = (t.order + 1) % numTagOrders
		t.reversed = false
		t.refresh()
	case key.Matches(msg, k.Reverse):
		t.reversed = !t.reversed
		t.refresh()
	case key.Matches(msg, k.Refresh):
		return t.reload(), nav{}
	}
	return nil, nav{}
}

// updateFind drives the find prompt: the list narrows with every letter, and
// the keys that are not letters still move through it.
func (t *tags) updateFind(msg tea.KeyPressMsg) (tea.Cmd, nav) {
	t.flash = ""

	k := t.keys
	switch {
	case key.Matches(msg, k.Open):
		t.finding = false
		t.input.Blur()
		if len(t.shown) > 0 {
			return nil, t.open()
		}
		// Nothing in the sample matches, which does not mean there is no
		// such tag: only that none of the busiest events is filed under it.
		if slug := slugOf(t.input.Value()); slug != "" {
			return t.lookUp(slug), nav{}
		}
	case key.Matches(msg, k.Back):
		t.finding = false
		t.input.Blur()
		t.input.SetValue("")
		t.refresh()
	case key.Matches(msg, k.Up):
		t.list.moveBy(-1)
	case key.Matches(msg, k.Down):
		t.list.moveBy(+1)
	case msg.String() == "pgup":
		t.list.moveBy(-t.list.pageSize())
	case msg.String() == "pgdown":
		t.list.moveBy(+t.list.pageSize())
	default:
		return t.edit(msg), nav{}
	}
	return nil, nav{}
}

// edit hands the prompt a message that is its to read, and narrows the list
// to whatever the text has become.
func (t *tags) edit(msg tea.Msg) tea.Cmd {
	before := t.input.Value()
	var cmd tea.Cmd
	t.input, cmd = t.input.Update(msg)
	if t.input.Value() != before {
		t.refresh()
		// The best match is the first row, and enter should open it.
		t.list.setCursor(0)
	}
	return cmd
}

// slugOf turns a tag's name as typed into the slug it would have. The lookup
// itself ignores case, but refuses a space.
func slugOf(typed string) string {
	return strings.Join(strings.Fields(strings.ToLower(typed)), "-")
}

// open goes down to the events of the tag under the cursor.
func (t *tags) open() nav {
	row, ok := t.selected()
	if !ok {
		return nav{}
	}
	// A lookup still under way has been overtaken.
	t.lookups++
	t.looking = ""
	return nav{push: newBrowse(t.env, row.tag, t.filter, eventsUnder(t.events, row.tag.ID))}
}

// lookUp asks the service for a tag by its slug.
func (t *tags) lookUp(slug string) tea.Cmd {
	if t.stopLookup != nil {
		t.stopLookup()
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.stopLookup = cancel
	t.lookups++
	t.looking = slug

	gen, client := t.lookups, t.client
	return func() tea.Msg {
		tag, err := client.Tag(ctx, slug)
		return tagMsg{gen: gen, slug: slug, tag: tag, err: err}
	}
}

// found opens the tag a lookup turned up, or says that there is none.
func (t *tags) found(msg tagMsg) nav {
	if msg.gen != t.lookups {
		return nav{} // overtaken by another lookup, or by opening a row
	}
	t.looking = ""
	switch {
	case api.IsNotFound(msg.err):
		t.flash = "there is no tag with the slug " + strconv.Quote(msg.slug)
	case msg.err != nil:
		t.flash = "could not look up " + strconv.Quote(msg.slug) + ": " + msg.err.Error()
	default:
		return nav{push: newBrowse(t.env, *msg.tag, t.filter, eventsUnder(t.events, msg.tag.ID))}
	}
	return nav{}
}
