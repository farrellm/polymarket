package ui

import (
	"context"
	"iter"
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/farrellm/polymarket/internal/api"
	"github.com/farrellm/polymarket/internal/export"
	"github.com/farrellm/polymarket/internal/format"
)

// pageSize is the most rows the listings hand out per request.
const pageSize = 100

// crumbWidth is the most of an event's title the breadcrumb shows.
const crumbWidth = 28

// tab is one of the two views of a tag: its events, or its markets with the
// event level skipped.
type tab int

const (
	tabEvents tab = iota
	tabMarkets
	numTabs
)

func (t tab) noun() string {
	if t == tabEvents {
		return "events"
	}
	return "markets"
}

// pageMsg is one page of a listing, or the failure to fetch it.
type pageMsg struct {
	// owner is the screen that asked: there may be several of the kind on
	// the stack, a sub-tag under its tag.
	owner *browse
	tab   tab
	gen   int

	events  []api.Event
	markets []api.Market
	// asked is the cursor the page was asked for with, and next the one
	// after it, empty on the last page.
	asked, next string
	err         error
}

// subTagsMsg answers the request for a tag's sub-tags.
type subTagsMsg struct {
	owner *browse
	tags  []api.Tag
	err   error
}

// eventMsg answers the refresh of the event whose markets are on show.
type eventMsg struct {
	owner *browse
	gen   int
	event *api.Event
	err   error
}

// listing is the rows of one tab and how far the loading of them has got.
type listing struct {
	list    list
	events  []api.Event
	markets []api.Market

	// started is false for a listing that has yet to be asked for, or whose
	// rows a change of sort, filter or search has thrown away.
	started bool
	// seeded is true while the rows are the ones the Tags level held, which
	// stand in until the first page of the real listing arrives.
	seeded  bool
	loading bool
	// fresh is true until the first page of a load arrives; that page
	// replaces the rows, and the ones after it add to them.
	fresh bool
	// next is the cursor of the page after the rows, empty at the end.
	next    string
	failure string
	// gen numbers the loads, so a page of one since abandoned is dropped.
	gen   int
	fetch func(cursor string) tea.Cmd
	stop  context.CancelFunc
}

func (l *listing) abandon() {
	if l.stop != nil {
		l.stop()
	}
	l.gen++
	l.loading = false
}

// browse is a level under Tags: the events and the markets filed under one
// tag, or, further down, the markets of one event.
//
// Under a tag the rows come from the service a page at a time, sorted and
// filtered there. An event arrives with its markets in it, so that level
// sorts and filters what it holds and asks for nothing but a refresh.
type browse struct {
	env
	// tag is the zero Tag for the All row: every event.
	tag api.Tag
	// event is set on the level that lists one event's markets.
	event *api.Event

	filter api.Filter
	// search is the text the rows are narrowed to, empty for none.
	search string

	tab   tab
	lists [numTabs]listing

	// subTags are the tags that narrow this one. subState says how the
	// request for them stands.
	subTags  []api.Tag
	subState subState
	stopSub  context.CancelFunc

	// The overlays: at most one is open.
	input     textinput.Model
	searching bool
	form      *filterForm
	picker    *picker

	flash         string
	width, height int
}

type subState int

const (
	subLoading subState = iota
	subLoaded
	subFailed
)

// newBrowse opens a tag. seed is the events the Tags level holds for it,
// which are shown until the tag's own listing arrives.
func newBrowse(e env, tag api.Tag, filter api.Filter, seed []api.Event) *browse {
	b := &browse{env: e, tag: tag, filter: filter, input: newPrompt("/"), width: 80, height: 20}
	b.lists[tabEvents].list = newList(e.st, eventColumns)
	b.lists[tabMarkets].list = newList(e.st, marketColumns)
	// The sample is the busiest open events, which is the head of the
	// listing only as long as nothing else was asked for.
	if len(seed) > 0 && filter == api.DefaultFilter() {
		b.lists[tabEvents].events = seed
		b.lists[tabEvents].seeded = true
	}
	b.layout()
	b.render(tabEvents)
	b.render(tabMarkets)
	return b
}

// newEventBrowse opens an event: its markets, which it came with.
func newEventBrowse(e env, event api.Event, filter api.Filter) *browse {
	b := &browse{env: e, event: &event, filter: filter, tab: tabMarkets, input: newPrompt("/"), width: 80, height: 20}
	b.lists[tabEvents].list = newList(e.st, eventColumns)
	b.lists[tabMarkets].list = newList(e.st, marketColumns)
	b.lists[tabMarkets].started = true
	b.layout()
	b.render(tabMarkets)
	return b
}

var eventColumns = []column{
	{title: "Event"},
	{title: "Markets", width: 7, right: true},
	{title: "Vol 24h", width: 8, right: true},
	{title: "Volume", width: 8, right: true},
	{title: "Liq", width: 8, right: true},
	{title: "Ends", width: 5, right: true},
}

var marketColumns = []column{
	{title: "Market"},
	{title: "Price", width: 6, right: true},
	{title: "24h Δ", width: 6, right: true},
	{title: "Vol 24h", width: 8, right: true},
	{title: "Volume", width: 8, right: true},
	{title: "Liq", width: 8, right: true},
	{title: "Ends", width: 5, right: true},
}

// missing stands in for a value the service did not send. It is not a zero,
// and is not shown as one.
const missing = "–"

func money(f api.Float) string {
	if !f.Valid {
		return missing
	}
	return format.Money(f.Value)
}

func (b *browse) ends(t api.Time) string {
	if t.IsZero() {
		return missing
	}
	return format.Until(t.Time, b.now())
}

func (b *browse) eventRow(e *api.Event) []string {
	return []string{
		e.Title,
		strconv.Itoa(len(e.Markets)),
		money(e.Volume24h),
		money(e.Volume),
		money(e.Liquidity),
		b.ends(e.EndDate),
	}
}

func (b *browse) marketRow(m *api.Market) []string {
	name := m.Question
	// Inside its event a market goes by what tells it from its siblings.
	if b.event != nil && m.GroupItemTitle != "" {
		name = m.GroupItemTitle
	}
	first := missing
	if len(m.Outcomes) > 0 {
		first = price(m.Outcomes[0].Price)
	}
	return []string{
		name,
		first,
		b.st.delta(m.Change1d),
		money(m.Volume24h),
		money(m.Volume),
		money(m.Liquidity),
		b.ends(m.EndDate),
	}
}

func (b *browse) cur() *listing { return &b.lists[b.tab] }

// tagged reports whether this is the level of a real tag, which has
// sub-tags, rather than of All or of an event.
func (b *browse) tagged() bool { return b.event == nil && b.tag.ID != "" }

func (b *browse) init() tea.Cmd {
	if b.event != nil {
		return nil
	}
	return tea.Batch(b.load(b.cur().seeded), b.loadSubTags())
}

func (b *browse) close() {
	for i := range b.lists {
		b.lists[i].abandon()
	}
	if b.stopSub != nil {
		b.stopSub()
	}
}

// load starts the listing on show afresh, abandoning any load under way.
// With keep the rows stay until the first page arrives to replace them;
// without, they go at once, as they must when they are no longer an answer
// to what is being asked.
func (b *browse) load(keep bool) tea.Cmd {
	if b.event != nil {
		return b.refreshEvent()
	}
	l := b.cur()
	l.abandon()
	ctx, cancel := context.WithCancel(context.Background())
	l.stop = cancel
	l.started, l.loading, l.fresh = true, true, true
	l.failure, l.next = "", ""
	if !keep {
		l.events, l.markets, l.seeded = nil, nil, false
	}

	msg := pageMsg{owner: b, tab: b.tab, gen: l.gen}
	asked := b.asked()
	l.fetch = func(cursor string) tea.Cmd {
		return func() tea.Msg {
			msg := msg
			msg.asked = cursor
			if msg.tab == tabEvents {
				msg.events, msg.next, msg.err = asked.events(ctx, cursor)
			} else {
				msg.markets, msg.next, msg.err = asked.markets(ctx, cursor)
			}
			return msg
		}
	}
	b.render(b.tab)
	return l.fetch("")
}

// question is what a tag's listings are asked: the tag, the filter and the
// search, as they stood when the asking began. The pages of a load and the
// rows of an export are both answers to one.
type question struct {
	client Client
	tag    api.Tag
	filter api.Filter
	search string
}

func (b *browse) asked() question {
	return question{client: b.client, tag: b.tag, filter: b.filter, search: b.search}
}

// events fetches the page of events after a cursor.
func (q question) events(ctx context.Context, cursor string) ([]api.Event, string, error) {
	eq := q.filter.EventsQuery()
	// The ID, not the slug: an event may embed a tag under a slug that
	// differs in case from the tag's own.
	eq.Limit, eq.Cursor, eq.TagID, eq.TitleSearch = pageSize, cursor, q.tag.ID, q.search
	events, next, err := q.client.Events(ctx, eq)
	if len(events) == 0 {
		next = ""
	}
	return events, next, err
}

// markets fetches the page of markets after a cursor, each with its tags,
// which an export of them writes out.
func (q question) markets(ctx context.Context, cursor string) ([]api.Market, string, error) {
	if q.search != "" {
		// The markets listing has no text search: the events found are
		// flattened into their markets instead.
		return api.SearchMarkets(ctx, q.client, q.filter, q.search, q.tag.Slug, cursor)
	}
	mq := q.filter.MarketsQuery()
	mq.Limit, mq.Cursor, mq.TagID, mq.IncludeTags = pageSize, cursor, q.tag.ID, true
	markets, next, err := q.client.Markets(ctx, mq)
	if len(markets) == 0 {
		next = ""
	}
	return markets, next, err
}

// refreshEvent fetches the event on show again, for its markets as they now
// stand.
func (b *browse) refreshEvent() tea.Cmd {
	l := &b.lists[tabMarkets]
	l.abandon()
	ctx, cancel := context.WithCancel(context.Background())
	l.stop = cancel
	l.loading, l.failure = true, ""

	msg := eventMsg{owner: b, gen: l.gen}
	client, id := b.client, b.event.ID
	return func() tea.Msg {
		msg.event, msg.err = client.Event(ctx, id)
		return msg
	}
}

func (b *browse) loadSubTags() tea.Cmd {
	if !b.tagged() {
		return nil
	}
	if b.stopSub != nil {
		b.stopSub()
	}
	ctx, cancel := context.WithCancel(context.Background())
	b.stopSub = cancel
	b.subState = subLoading

	msg := subTagsMsg{owner: b}
	client, slug := b.client, b.tag.Slug
	return func() tea.Msg {
		msg.tags, msg.err = client.RelatedTags(ctx, slug)
		return msg
	}
}

// changed is called when the sort, the filter or the search has: the rows in
// hand answer a question no longer being asked.
func (b *browse) changed() tea.Cmd {
	if b.event != nil {
		b.render(tabMarkets)
		b.lists[tabMarkets].list.setCursor(0)
		return nil
	}
	for i := range b.lists {
		l := &b.lists[i]
		l.abandon()
		*l = listing{list: l.list, gen: l.gen}
		b.render(tab(i))
		l.list.setCursor(0)
	}
	return b.load(false)
}

// wantMore asks for the next page of a listing once the cursor has come
// within a screen of the last row loaded.
func (b *browse) wantMore(t tab) tea.Cmd {
	l := &b.lists[t]
	if l.loading || l.next == "" || l.fetch == nil {
		return nil
	}
	if l.list.cursor+l.list.pageSize() < len(l.list.rows) {
		return nil
	}
	l.loading = true
	return l.fetch(l.next)
}

// addPage takes in a page of a listing.
func (b *browse) addPage(msg pageMsg) tea.Cmd {
	l := &b.lists[msg.tab]
	if msg.owner != b || msg.gen != l.gen {
		return nil // another screen's, or a page of a load since abandoned
	}
	l.loading = false
	err := msg.err
	if err == nil && msg.next != "" && msg.next == msg.asked {
		err = api.ErrCursorRepeated
	}
	if err != nil {
		// The rows already loaded stay, and so does the cursor to go on
		// from: moving towards the end asks for the page again.
		l.failure = "could not load the " + msg.tab.noun() + ": " + err.Error() + " · r retries"
		b.render(msg.tab)
		return nil
	}

	l.failure = ""
	if l.fresh {
		// The cursor stays on its row if the new rows still hold it.
		was := b.selectedID(msg.tab)
		l.events, l.markets = msg.events, msg.markets
		l.fresh, l.seeded = false, false
		l.next = msg.next
		b.render(msg.tab)
		l.list.setCursor(max(b.indexOf(msg.tab, was), 0))
	} else {
		l.events = append(l.events, msg.events...)
		l.markets = append(l.markets, msg.markets...)
		l.next = msg.next
		b.render(msg.tab)
	}
	return b.wantMore(msg.tab)
}

// refreshed takes in the event fetched again.
func (b *browse) refreshed(msg eventMsg) {
	l := &b.lists[tabMarkets]
	if msg.owner != b || msg.gen != l.gen {
		return
	}
	l.loading = false
	if msg.err != nil {
		l.failure = "could not refresh the event: " + msg.err.Error() + " · r retries"
		return
	}
	was := b.selectedID(tabMarkets)
	b.event = msg.event
	b.render(tabMarkets)
	l.list.setCursor(max(b.indexOf(tabMarkets, was), 0))
}

// selectedID is the ID of the row under a listing's cursor, or empty.
func (b *browse) selectedID(t tab) string {
	l := &b.lists[t]
	at := l.list.cursor
	switch {
	case t == tabEvents && at < len(l.events):
		return l.events[at].ID
	case t == tabMarkets && at < len(l.markets):
		return l.markets[at].ID
	}
	return ""
}

// indexOf is the row a listing holds an ID at, or -1.
func (b *browse) indexOf(t tab, id string) int {
	if id == "" {
		return -1
	}
	l := &b.lists[t]
	if t == tabEvents {
		return slices.IndexFunc(l.events, func(e api.Event) bool { return e.ID == id })
	}
	return slices.IndexFunc(l.markets, func(m api.Market) bool { return m.ID == id })
}

// kept are the markets of the event on show that the filter and the search
// leave, sorted as the filter says.
func (b *browse) kept() []api.Market {
	needle := strings.ToLower(b.search)
	var out []api.Market
	for i := range b.event.Markets {
		m := &b.event.Markets[i]
		if !b.filter.Keeps(m) {
			continue
		}
		if needle != "" && !strings.Contains(strings.ToLower(m.Question), needle) &&
			!strings.Contains(strings.ToLower(m.GroupItemTitle), needle) {
			continue
		}
		out = append(out, *m)
	}
	slices.SortStableFunc(out, func(x, y api.Market) int { return b.filter.Compare(&x, &y) })
	return out
}

// render rebuilds a listing's rows from what it holds.
func (b *browse) render(t tab) {
	l := &b.lists[t]
	if b.event != nil && t == tabMarkets {
		l.markets = b.kept()
	}

	var rows [][]string
	if t == tabEvents {
		rows = make([][]string, len(l.events))
		for i := range l.events {
			rows[i] = b.eventRow(&l.events[i])
		}
	} else {
		rows = make([][]string, len(l.markets))
		for i := range l.markets {
			rows[i] = b.marketRow(&l.markets[i])
		}
	}

	switch {
	case l.loading:
		l.list.empty = "loading the " + t.noun() + "…"
	case l.failure != "":
		// The status bar says what went wrong.
		l.list.empty = "the " + t.noun() + " could not be loaded"
	case b.event != nil && len(b.event.Markets) > 0:
		l.list.empty = "none of the event's " + strconv.Itoa(len(b.event.Markets)) + " markets matches: " + b.describe()
	default:
		l.list.empty = "no " + t.noun() + " match: " + b.describe()
	}
	l.list.setRows(rows)
}

func (b *browse) resize(width, height int) {
	b.width, b.height = width, height
	b.layout()
}

func (b *browse) layout() {
	h := b.height
	if b.tagged() {
		h-- // the strip of sub-tags
	}
	for i := range b.lists {
		b.lists[i].list.setSize(b.width, max(h, 1))
	}
	if b.picker != nil {
		b.picker.list.setSize(b.width, b.height)
	}
}

func (b *browse) view() string {
	switch {
	case b.form != nil:
		return b.form.view(b.width)
	case b.picker != nil:
		return b.picker.list.view()
	case b.tagged():
		return b.st.faint.Render(fit(elide(b.strip(), b.width), b.width, false)) + "\n" + b.cur().list.view()
	}
	return b.cur().list.view()
}

// strip is the line under the title that names the tag's sub-tags.
func (b *browse) strip() string {
	const lead = " Sub-tags: "
	switch {
	case b.subState == subLoading:
		return lead + "loading…"
	case b.subState == subFailed:
		return lead + "could not be loaded · r retries"
	case len(b.subTags) == 0:
		return lead + "none"
	}
	labels := make([]string, len(b.subTags))
	for i, t := range b.subTags {
		labels[i] = t.Label
	}
	return lead + strings.Join(labels, " · ")
}

func (b *browse) crumb() string {
	switch {
	case b.event != nil:
		return elide(b.event.Title, crumbWidth)
	case b.tag.ID == "":
		return "All"
	}
	return b.tag.Label
}

func (b *browse) tabs() string {
	if b.event != nil {
		return ""
	}
	names := []string{"Events", "Markets"}
	for i, n := range names {
		if tab(i) == b.tab {
			names[i] = b.st.tab.Render("[" + n + "]")
		} else {
			names[i] = b.st.crumb.Render(n)
		}
	}
	return strings.Join(names, " ")
}

// orderLabels name the sort orders as briefly as the title bar needs.
var orderLabels = map[string]string{
	"volume24hr": "vol 24h",
	"volume1wk":  "vol 1w",
	"volume1mo":  "vol 1m",
	"volume":     "volume",
	"liquidity":  "liquidity",
	"endDate":    "end",
	"startDate":  "start",
}

// byRelevance reports whether the rows on show are in the search's order
// rather than the filter's: a search of markets cannot be sorted.
func (b *browse) byRelevance() bool {
	return b.event == nil && b.tab == tabMarkets && b.search != ""
}

// describe says what the rows are narrowed to: the search and the filter.
func (b *browse) describe() string {
	var parts []string
	if b.search != "" {
		parts = append(parts, strconv.Quote(b.search))
	}
	switch b.filter.Status {
	case api.StatusOpen:
		parts = append(parts, "open")
	case api.StatusClosed:
		parts = append(parts, "closed")
	case api.StatusAll:
		parts = append(parts, "all")
	}
	f := b.filter
	if f.VolumeMin > 0 {
		parts = append(parts, "vol ≥ "+format.Money(f.VolumeMin))
	}
	if f.LiquidityMin > 0 {
		parts = append(parts, "liq ≥ "+format.Money(f.LiquidityMin))
	}
	if !f.EndDateMin.IsZero() {
		parts = append(parts, "ends ≥ "+dateText(f.EndDateMin))
	}
	if !f.EndDateMax.IsZero() {
		parts = append(parts, "ends ≤ "+dateText(f.EndDateMax))
	}
	return strings.Join(parts, " · ")
}

// note leads with the sort, which is what the keys change most often and so
// what must survive a title bar too narrow for the rest.
func (b *browse) note() string {
	note := "by relevance"
	if !b.byRelevance() {
		note = orderLabels[b.filter.Order.Name] + " ↓"
		if b.filter.Ascending {
			note = orderLabels[b.filter.Order.Name] + " ↑"
		}
	}
	return note + " · " + b.describe()
}

func (b *browse) typing() bool { return b.searching || b.form != nil || b.picker != nil }

// exports offers the rows of the tab on show: its events, or its markets,
// which are also to be had an outcome to a row. Under a tag each can be the
// rows loaded or all that the filter and the search select; an event's
// markets are all in hand.
func (b *browse) exports() []exportChoice {
	l := b.cur()
	asked := b.asked()
	const loaded, matching = " loaded", "all that match"

	if b.tab == tabEvents {
		var scopes []exportScope
		if events := slices.Clone(l.events); len(events) > 0 {
			scopes = append(scopes, exportScope{
				label: "the " + strconv.Itoa(len(events)) + loaded,
				open: func(context.Context) export.Dataset {
					return export.Events(export.Loaded(events))
				},
			})
		}
		scopes = append(scopes, exportScope{
			label: matching,
			open: func(ctx context.Context) export.Dataset {
				return export.Events(api.Pages(ctx, func(cursor string) ([]api.Event, string, error) {
					return asked.events(ctx, cursor)
				}))
			},
		})
		return []exportChoice{{name: "events", scopes: scopes}}
	}

	markets := slices.Clone(l.markets)
	held := "the " + strconv.Itoa(len(markets)) + loaded
	if b.event != nil {
		// A market inside its event is sent without it, and without tags.
		for i := range markets {
			markets[i] = within(markets[i], b.event)
			if markets[i].Tags == nil {
				markets[i].Tags = b.event.Tags
			}
		}
		held = "the " + strconv.Itoa(len(markets)) + " listed"
	}
	datasets := []struct {
		name string
		of   func(pages iter.Seq2[[]api.Market, error]) export.Dataset
	}{{"markets", export.Markets}, {"outcomes", export.Outcomes}}

	var choices []exportChoice
	for _, d := range datasets {
		var scopes []exportScope
		if len(markets) > 0 {
			scopes = append(scopes, exportScope{
				label: held,
				open: func(context.Context) export.Dataset {
					return d.of(export.Loaded(markets))
				},
			})
		}
		if b.event == nil {
			scopes = append(scopes, exportScope{
				label: matching,
				open: func(ctx context.Context) export.Dataset {
					return d.of(api.Pages(ctx, func(cursor string) ([]api.Market, string, error) {
						return asked.markets(ctx, cursor)
					}))
				},
			})
		}
		if len(scopes) > 0 {
			choices = append(choices, exportChoice{name: d.name, scopes: scopes})
		}
	}
	return choices
}

func (b *browse) hints() []key.Binding {
	k := b.keys
	switch {
	case b.form != nil:
		return []key.Binding{k.Apply, k.Cancel, k.Next}
	case b.picker != nil:
		return []key.Binding{k.Open, k.Cancel}
	case b.searching:
		return []key.Binding{k.Apply, k.Cancel}
	}
	var hints []key.Binding
	if b.search != "" {
		hints = append(hints, k.ClearFind)
	}
	hints = append(hints, k.Search, k.Filter)
	if b.tagged() {
		hints = append(hints, k.SubTag)
	}
	return append(hints, k.Sort, k.Export, k.Help, k.Back, k.Quit)
}

func (b *browse) status() status {
	l := b.cur()
	s := status{failure: l.failure}
	switch {
	case b.form != nil:
		s.text = "filter"
		return s
	case b.picker != nil:
		s.text = b.picker.status()
		return s
	case b.searching:
		s.text = b.input.View()
		return s
	case b.flash != "":
		s.text = b.flash
		return s
	}

	rows := len(l.list.rows)
	switch {
	case b.event != nil:
		all := len(b.event.Markets)
		s.text = strconv.Itoa(all) + " markets"
		if all == 1 {
			s.text = "1 market"
		}
		if rows != all {
			s.text = strconv.Itoa(rows) + " of " + s.text
		}
		if l.loading {
			s.text += " · refreshing…"
		}
	case l.seeded && l.loading:
		s.text = strconv.Itoa(rows) + " of the busiest · loading the rest…"
	case l.seeded:
		s.text = strconv.Itoa(rows) + " of the busiest"
	case l.fresh && l.loading && rows == 0:
		s.text = "loading…"
	case l.fresh && l.loading:
		s.text = strconv.Itoa(rows) + " loaded · refreshing…"
	case l.loading:
		s.text = strconv.Itoa(rows) + " loaded · loading more…"
	case l.next != "":
		s.text = strconv.Itoa(rows) + " loaded · more available"
	case !l.started:
		s.text = ""
	default:
		s.text = strconv.Itoa(rows) + " loaded · end"
	}
	return s
}

func (b *browse) update(msg tea.Msg) (tea.Cmd, nav) {
	switch msg := msg.(type) {
	case pageMsg:
		return b.addPage(msg), nav{}
	case eventMsg:
		b.refreshed(msg)
		return nil, nav{}
	case subTagsMsg:
		if msg.owner == b {
			b.subTags, b.subState = msg.tags, subLoaded
			if msg.err != nil {
				b.subTags, b.subState = nil, subFailed
			}
		}
		return nil, nav{}
	case tea.KeyPressMsg:
		switch {
		case b.form != nil:
			return b.updateForm(msg), nav{}
		case b.picker != nil:
			return b.updatePicker(msg)
		case b.searching:
			return b.updateSearch(msg), nav{}
		}
		return b.handleKey(msg)
	}

	// Text pasted into a prompt arrives as a message of its own.
	var cmd tea.Cmd
	switch {
	case b.form != nil:
		cmd = b.form.edit(msg)
	case b.picker != nil:
		cmd = b.picker.edit(msg)
	case b.searching:
		b.input, cmd = b.input.Update(msg)
	}
	return cmd, nav{}
}

func (b *browse) handleKey(msg tea.KeyPressMsg) (tea.Cmd, nav) {
	// A message is for the moment it was shown in.
	b.flash = ""

	k := b.keys
	l := b.cur()
	switch {
	case l.list.handle(msg, k):
		return b.wantMore(b.tab), nav{}
	case key.Matches(msg, k.Open):
		return nil, b.open()
	case key.Matches(msg, k.Back):
		// Esc first undoes the search; with none set it goes up a level.
		if b.search == "" {
			return nil, nav{pop: true}
		}
		b.search = ""
		return b.changed(), nav{}
	case key.Matches(msg, k.Tab):
		if b.event != nil {
			break
		}
		b.tab = (b.tab + 1) % numTabs
		if !b.cur().started {
			return b.load(false), nav{}
		}
	case key.Matches(msg, k.Search):
		b.searching = true
		b.input.SetValue(b.search)
		b.input.CursorEnd()
		// Focus only has a command to return for a cursor that blinks.
		_ = b.input.Focus()
	case key.Matches(msg, k.Filter):
		b.form = newFilterForm(b.env, b.filter)
	case key.Matches(msg, k.SubTag):
		return b.pick(), nav{}
	case key.Matches(msg, k.Sort):
		if b.byRelevance() {
			b.flash = "a search of markets is ranked by relevance; esc clears the search"
			break
		}
		at := slices.Index(api.SortOrders, b.filter.Order)
		b.filter.Order = api.SortOrders[(at+1)%len(api.SortOrders)]
		// Each order starts the way round that is useful: the largest
		// figure, the latest start, the nearest end.
		b.filter.Ascending = b.filter.Order.Name == "endDate"
		return b.changed(), nav{}
	case key.Matches(msg, k.Reverse):
		if b.byRelevance() {
			b.flash = "a search of markets is ranked by relevance; esc clears the search"
			break
		}
		b.filter.Ascending = !b.filter.Ascending
		return b.changed(), nav{}
	case key.Matches(msg, k.Refresh):
		var subs tea.Cmd
		if b.subState == subFailed {
			subs = b.loadSubTags()
		}
		// What is on screen stays until its replacement arrives.
		return tea.Batch(b.load(true), subs), nav{}
	}
	return nil, nav{}
}

// open goes down a level from the row under the cursor: to the markets of an
// event, or to the detail of a market. An event with only the one market has
// no list worth showing, and opens on that market.
func (b *browse) open() nav {
	l := b.cur()
	at := l.list.cursor
	switch {
	case b.tab == tabEvents && at < len(l.events):
		event := &l.events[at]
		if len(event.Markets) == 1 {
			return nav{push: newDetail(b.env, within(event.Markets[0], event), false)}
		}
		return nav{push: newEventBrowse(b.env, *event, b.filter)}
	case b.tab == tabMarkets && at < len(l.markets) && b.event != nil:
		return nav{push: newDetail(b.env, within(l.markets[at], b.event), true)}
	case b.tab == tabMarkets && at < len(l.markets):
		return nav{push: newDetail(b.env, l.markets[at], false)}
	}
	return nav{}
}

// within gives a market that arrived inside an event that event, as the
// markets listing would have sent it: its page on the site lives under it.
func within(m api.Market, event *api.Event) api.Market {
	parent := *event
	parent.Markets = nil
	m.Events = []api.Event{parent}
	return m
}

// updateSearch drives the search prompt. Unlike the find on the Tags level
// it asks the service, so it waits for enter.
func (b *browse) updateSearch(msg tea.KeyPressMsg) tea.Cmd {
	k := b.keys
	switch {
	case key.Matches(msg, k.Apply):
		b.searching = false
		b.input.Blur()
		if text := strings.TrimSpace(b.input.Value()); text != b.search {
			b.search = text
			return b.changed()
		}
	case key.Matches(msg, k.Cancel):
		b.searching = false
		b.input.Blur()
	default:
		var cmd tea.Cmd
		b.input, cmd = b.input.Update(msg)
		return cmd
	}
	return nil
}

func (b *browse) updateForm(msg tea.KeyPressMsg) tea.Cmd {
	cmd, outcome := b.form.update(msg)
	switch outcome {
	case formOpen:
		return cmd
	case formApplied:
		filter := b.form.filter
		b.form = nil
		if filter != b.filter {
			b.filter = filter
			return b.changed()
		}
	case formCancelled:
		b.form = nil
	}
	return nil
}

// pick opens the sub-tag picker, if there are sub-tags to pick from.
func (b *browse) pick() tea.Cmd {
	switch {
	case !b.tagged():
	case b.subState == subFailed:
		return b.loadSubTags()
	case b.subState == subLoading:
		b.flash = "the sub-tags are still loading"
	case len(b.subTags) == 0:
		b.flash = b.tag.Label + " has no sub-tags"
	default:
		b.picker = newPicker(b.env, b.subTags)
		b.layout()
	}
	return nil
}

func (b *browse) updatePicker(msg tea.KeyPressMsg) (tea.Cmd, nav) {
	cmd, chosen, done := b.picker.update(msg)
	if !done {
		return cmd, nav{}
	}
	b.picker = nil
	if chosen == nil {
		return nil, nav{}
	}
	// The filter goes down with it; the search was for this tag.
	return nil, nav{push: newBrowse(b.env, *chosen, b.filter, nil)}
}
