package ui

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/farrellm/polymarket/internal/api"
	"github.com/farrellm/polymarket/internal/export"
	"github.com/farrellm/polymarket/internal/format"
)

const (
	// tradesShown is how many of a market's latest trades are asked for:
	// more than a tall window has lines for.
	tradesShown = 50
	// maxOutcomeRows is the most outcomes listed at once; the rest scroll.
	maxOutcomeRows = 4
	// maxChartRows is the most lines the chart is given, however tall the
	// window.
	maxChartRows = 8
	// bookWidth is the room the book takes, to the left of the trades.
	bookWidth = 32
	// paneGap is the space between the two.
	paneGap = 2

	// historyPageSize and tradesPageSize are the most the Data API hands
	// out per request of each, which is what an export of all of either
	// asks for.
	historyPageSize = 10000
	tradesPageSize  = 1000
)

// detailTab is one of the two views of a market: its prices, or what it is
// about.
type detailTab int

const (
	tabMarket detailTab = iota
	tabAbout
	numDetailTabs
)

// chartIntervals are how far back the chart can go, in the order the key
// cycles through them. span is the interval in words.
var chartIntervals = []struct{ name, span string }{
	{"1d", "the last day"},
	{"1w", "the last week"},
	{"1m", "the last month"},
	{"max", "the market's lifetime"},
}

const (
	// weekInterval is what the chart of a market that is trading starts on.
	weekInterval = 1
	// maxInterval is what the chart of a closed market starts on: it has
	// no prices in the last week, or month, to show.
	maxInterval = 3
)

// bookMsg answers the request for one outcome's order book.
type bookMsg struct {
	owner *detail
	gen   int
	token string
	book  *api.Book
	err   error
}

// chartKey names one price history: an outcome's, over an interval.
type chartKey struct {
	token, interval string
}

// historyMsg answers the request for one price history.
type historyMsg struct {
	owner  *detail
	gen    int
	key    chartKey
	points []api.PricePoint
	err    error
}

// tradesMsg answers the request for the market's latest trades.
type tradesMsg struct {
	owner  *detail
	gen    int
	trades []api.Trade
	err    error
}

// marketMsg answers the refresh of the market itself.
type marketMsg struct {
	owner  *detail
	gen    int
	market *api.Market
	err    error
}

// openedMsg reports how handing the market's page to the browser went.
type openedMsg struct {
	owner *detail
	err   error
}

// pane is how the loading of one part of the screen stands. Each part is
// asked for on its own, and says for itself that it is loading or has failed.
type pane struct {
	loading bool
	// loaded is true once an answer has arrived, whatever it held.
	loaded  bool
	failure string
}

// waiting reports that there is nothing to show yet.
func (p *pane) waiting() bool { return p.loading && !p.loaded }

type bookPane struct {
	pane
	book *api.Book
	// none is true for an outcome with no book at all, which is what the
	// service says of a market that is no longer trading.
	none bool
}

type chartPane struct {
	pane
	points []api.PricePoint
}

type tradesPane struct {
	pane
	trades []api.Trade
}

// detail is the bottom level: one market. Its outcomes are listed with the
// prices the market came with; under them are the price history and the
// order book of the outcome the cursor is on, and the market's latest
// trades. What it is about is on a second tab.
type detail struct {
	env
	market api.Market
	// inEvent is true for a market opened from the list of its event's
	// markets, where it goes by its short name.
	inEvent bool

	tab      detailTab
	outcomes list
	// interval indexes chartIntervals.
	interval int

	// books are keyed by token ID, and charts by token ID and interval: an
	// outcome looked at before is shown again without a request.
	books  map[string]*bookPane
	charts map[chartKey]*chartPane
	trades tradesPane

	about viewport.Model

	// refreshing is true while the market itself is being fetched again.
	refreshing bool
	failure    string
	// gen numbers the refreshes, so an answer to a request made before the
	// last one is dropped.
	gen   int
	stops []context.CancelFunc

	flash         string
	width, height int
}

// newDetail opens a market as it arrived in a listing or inside its event.
func newDetail(e env, m api.Market, inEvent bool) *detail {
	d := &detail{
		env:      e,
		market:   m,
		inEvent:  inEvent,
		interval: weekInterval,
		books:    map[string]*bookPane{},
		charts:   map[chartKey]*chartPane{},
		about:    viewport.New(),
		width:    80,
		height:   20,
	}
	if m.Closed {
		d.interval = maxInterval
	}
	d.outcomes = newList(e.st, []column{
		{title: "Outcome"},
		{title: "Price", width: 6, right: true},
		{title: "Bid", width: 6, right: true},
		{title: "Ask", width: 6, right: true},
		{title: "Spread", width: 6, right: true},
		{title: "Last", width: 6, right: true},
		{title: "24h Δ", width: 6, right: true},
	})
	d.outcomes.empty = "the market lists no outcomes"
	d.render()
	d.layout()
	return d
}

func (d *detail) init() tea.Cmd {
	return tea.Batch(d.wanted(), d.loadTrades())
}

func (d *detail) close() { d.stop() }

// stop cancels every request under way.
func (d *detail) stop() {
	for _, cancel := range d.stops {
		cancel()
	}
	d.stops = nil
}

// request runs fetch under a context of its own, which leaving the screen or
// refreshing it cancels.
func (d *detail) request(fetch func(ctx context.Context) tea.Msg) tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	d.stops = append(d.stops, cancel)
	return func() tea.Msg { return fetch(ctx) }
}

// selected is the outcome under the cursor, and its index.
func (d *detail) selected() (api.Outcome, int, bool) {
	at := d.outcomes.cursor
	if at >= len(d.market.Outcomes) {
		return api.Outcome{}, 0, false
	}
	return d.market.Outcomes[at], at, true
}

func (d *detail) keyOf(o api.Outcome) chartKey {
	return chartKey{token: o.TokenID, interval: chartIntervals[d.interval].name}
}

// wanted asks for the book and the price history of the outcome under the
// cursor, as far as they are not in hand already.
func (d *detail) wanted() tea.Cmd {
	o, _, ok := d.selected()
	if !ok || o.TokenID == "" {
		// An outcome has no token until its market opens for trading.
		return nil
	}
	var cmds []tea.Cmd
	if d.books[o.TokenID] == nil {
		cmds = append(cmds, d.loadBook(o.TokenID))
	}
	if d.charts[d.keyOf(o)] == nil {
		cmds = append(cmds, d.loadChart(d.keyOf(o)))
	}
	return tea.Batch(cmds...)
}

func (d *detail) loadBook(token string) tea.Cmd {
	p := d.books[token]
	if p == nil {
		p = &bookPane{}
		d.books[token] = p
	}
	p.loading, p.failure = true, ""

	msg := bookMsg{owner: d, gen: d.gen, token: token}
	client := d.client
	return d.request(func(ctx context.Context) tea.Msg {
		msg.book, msg.err = client.Book(ctx, token)
		return msg
	})
}

func (d *detail) loadChart(k chartKey) tea.Cmd {
	p := d.charts[k]
	if p == nil {
		p = &chartPane{}
		d.charts[k] = p
	}
	p.loading, p.failure = true, ""

	msg := historyMsg{owner: d, gen: d.gen, key: k}
	client := d.client
	return d.request(func(ctx context.Context) tea.Msg {
		// One page is all of it: the service spaces the points to suit the
		// interval, and sends a couple of thousand at most.
		msg.points, _, msg.err = client.PriceHistory(ctx, api.HistoryQuery{TokenID: k.token, Interval: k.interval})
		return msg
	})
}

func (d *detail) loadTrades() tea.Cmd {
	if d.market.ConditionID == "" {
		return nil
	}
	d.trades.loading, d.trades.failure = true, ""

	msg := tradesMsg{owner: d, gen: d.gen}
	client, condition := d.client, d.market.ConditionID
	return d.request(func(ctx context.Context) tea.Msg {
		msg.trades, _, msg.err = client.Trades(ctx, api.TradesQuery{ConditionID: condition, Limit: tradesShown})
		return msg
	})
}

func (d *detail) loadMarket() tea.Cmd {
	if d.market.ID == "" {
		return nil
	}
	d.refreshing, d.failure = true, ""

	msg := marketMsg{owner: d, gen: d.gen}
	client, id := d.client, d.market.ID
	return d.request(func(ctx context.Context) tea.Msg {
		msg.market, msg.err = client.Market(ctx, id)
		return msg
	})
}

// refresh fetches everything on show again. What is on screen stays until
// its replacement arrives; what was looked at earlier and is not on show is
// forgotten, and asked for again when it next is.
func (d *detail) refresh() tea.Cmd {
	d.stop()
	d.gen++

	o, _, ok := d.selected()
	for token := range d.books {
		if !ok || token != o.TokenID {
			delete(d.books, token)
		}
	}
	for k := range d.charts {
		if !ok || k != d.keyOf(o) {
			delete(d.charts, k)
		}
	}
	cmds := []tea.Cmd{d.loadMarket(), d.loadTrades()}
	if ok && o.TokenID != "" {
		cmds = append(cmds, d.loadBook(o.TokenID), d.loadChart(d.keyOf(o)))
	}
	return tea.Batch(cmds...)
}

func (d *detail) update(msg tea.Msg) (tea.Cmd, nav) {
	switch msg := msg.(type) {
	case bookMsg:
		p := d.books[msg.token]
		if msg.owner != d || msg.gen != d.gen || p == nil {
			break
		}
		p.pane = pane{loaded: true}
		p.book, p.none = msg.book, false
		switch {
		case api.IsNotFound(msg.err):
			p.none = true
		case msg.err != nil:
			p.failure = "could not load the book: " + msg.err.Error() + " · r retries"
		}
	case historyMsg:
		p := d.charts[msg.key]
		if msg.owner != d || msg.gen != d.gen || p == nil {
			break
		}
		p.pane = pane{loaded: true}
		p.points = msg.points
		if msg.err != nil {
			p.failure = "could not load the prices: " + msg.err.Error() + " · r retries"
		}
	case tradesMsg:
		if msg.owner != d || msg.gen != d.gen {
			break
		}
		d.trades = tradesPane{pane: pane{loaded: true}, trades: msg.trades}
		if msg.err != nil {
			d.trades.failure = "could not load the trades: " + msg.err.Error() + " · r retries"
		}
	case marketMsg:
		if msg.owner != d || msg.gen != d.gen {
			break
		}
		d.refreshing = false
		if msg.err != nil {
			d.failure = "could not refresh the market: " + msg.err.Error() + " · r retries"
			break
		}
		d.replace(*msg.market)
		return d.wanted(), nav{}
	case openedMsg:
		if msg.owner == d && msg.err != nil {
			d.flash = "could not open the browser: " + msg.err.Error()
		}
	case tea.KeyPressMsg:
		return d.handleKey(msg)
	}
	return nil, nav{}
}

// replace takes in the market fetched again. Fetched on its own it comes
// with neither its event nor its tags, so those it was opened with are kept.
func (d *detail) replace(m api.Market) {
	if len(m.Events) == 0 {
		m.Events = d.market.Events
	}
	if len(m.Tags) == 0 {
		m.Tags = d.market.Tags
	}
	d.market = m
	d.render()
	d.layout()
}

func (d *detail) handleKey(msg tea.KeyPressMsg) (tea.Cmd, nav) {
	// A message is for the moment it was shown in.
	d.flash = ""

	k := d.keys
	switch {
	case key.Matches(msg, k.Back):
		return nil, nav{pop: true}
	case key.Matches(msg, k.About):
		d.tab = (d.tab + 1) % numDetailTabs
	case key.Matches(msg, k.Refresh):
		return d.refresh(), nav{}
	case key.Matches(msg, k.Browse):
		return d.browse(), nav{}
	case key.Matches(msg, k.Copy):
		return d.copy("slug", d.market.Slug), nav{}
	case key.Matches(msg, k.CopyID):
		return d.copy("condition ID", d.market.ConditionID), nav{}
	case d.tab == tabAbout:
		d.scroll(msg)
	case key.Matches(msg, k.Interval):
		d.interval = (d.interval + 1) % len(chartIntervals)
		return d.wanted(), nav{}
	case d.outcomes.handle(msg, k):
		// The book and the chart are of the outcome the cursor is on.
		return d.wanted(), nav{}
	}
	return nil, nav{}
}

// scroll moves through the description with the keys that move through a
// list.
func (d *detail) scroll(msg tea.KeyPressMsg) {
	k := d.keys
	switch {
	case key.Matches(msg, k.Up):
		d.about.ScrollUp(1)
	case key.Matches(msg, k.Down):
		d.about.ScrollDown(1)
	case key.Matches(msg, k.PageUp):
		d.about.PageUp()
	case key.Matches(msg, k.PageDown):
		d.about.PageDown()
	case key.Matches(msg, k.HalfUp):
		d.about.HalfPageUp()
	case key.Matches(msg, k.HalfDown):
		d.about.HalfPageDown()
	case key.Matches(msg, k.Top):
		d.about.GotoTop()
	case key.Matches(msg, k.Bottom):
		d.about.GotoBottom()
	}
}

// browse opens the market's page on polymarket.com.
func (d *detail) browse() tea.Cmd {
	url := export.MarketURL(&d.market)
	if url == "" {
		d.flash = "the market has no slug, and so no page to open"
		return nil
	}
	d.flash = "opening " + url
	open := d.openURL
	return func() tea.Msg { return openedMsg{owner: d, err: open(url)} }
}

// copy puts one of the market's identifiers on the clipboard, through the
// terminal.
func (d *detail) copy(what, value string) tea.Cmd {
	if value == "" {
		d.flash = "the market has no " + what
		return nil
	}
	d.flash = "copied the " + what + ": " + value
	return tea.SetClipboard(value)
}

// quote is what the market itself says of one outcome's prices.
type quote struct {
	bid, ask, spread, last, change api.Float
}

// quoteOf reads an outcome's quote off its market, which states one: that of
// its first outcome. The second of two is the same book seen from the other
// side, where selling the one is buying the other at what is left of a
// dollar. An outcome beyond those has no quote but its book.
//
// A closed market is still sent with the bid, the ask and the change of its
// last hours, which say nothing of it now: only its last trade is kept.
func quoteOf(m *api.Market, index int) quote {
	var q quote
	switch {
	case m.Closed && index == 0:
		return quote{last: m.LastTradePrice}
	case m.Closed && index == 1 && len(m.Outcomes) == 2:
		return quote{last: complement(m.LastTradePrice)}
	case index == 0:
		q = quote{bid: m.BestBid, ask: m.BestAsk, last: m.LastTradePrice, change: m.Change1d}
	case index == 1 && len(m.Outcomes) == 2:
		q = quote{
			bid:    complement(m.BestAsk),
			ask:    complement(m.BestBid),
			last:   complement(m.LastTradePrice),
			change: api.Float{Value: -m.Change1d.Value, Valid: m.Change1d.Valid},
		}
	default:
		return q
	}
	q.spread = m.Spread
	if !q.spread.Valid && q.bid.Valid && q.ask.Valid {
		q.spread = api.Float{Value: q.ask.Value - q.bid.Value, Valid: true}
	}
	return q
}

func complement(f api.Float) api.Float {
	if !f.Valid {
		return f
	}
	return api.Float{Value: 1 - f.Value, Valid: true}
}

func price(f api.Float) string {
	if !f.Valid {
		return missing
	}
	return format.Price(f.Value)
}

// render rebuilds what is drawn from the market alone: the outcomes and the
// text of the second tab.
func (d *detail) render() {
	rows := make([][]string, len(d.market.Outcomes))
	for i, o := range d.market.Outcomes {
		q := quoteOf(&d.market, i)
		rows[i] = []string{
			o.Label,
			price(o.Price),
			price(q.bid),
			price(q.ask),
			price(q.spread),
			price(q.last),
			d.st.delta(q.change),
		}
	}
	d.outcomes.setRows(rows)
}

func (d *detail) resize(width, height int) {
	d.width, d.height = width, height
	d.layout()
}

// outcomeRows is how many outcomes are in view at once.
func (d *detail) outcomeRows() int {
	return max(min(len(d.market.Outcomes), maxOutcomeRows), 1)
}

func (d *detail) layout() {
	d.outcomes.setSize(d.width, 1+d.outcomeRows())
	d.about.SetWidth(d.width)
	d.about.SetHeight(d.height)
	d.about.SetContent(d.aboutText())
}

// split divides the lines the fixed parts leave between the chart and the
// tables under it: a third to the chart.
func (d *detail) split() (chartRows, tableRows int) {
	// The question, the facts, the header of the outcomes, the titles of
	// the chart and of the panes, the header of their tables, and three
	// blank lines between.
	const fixed = 9
	spare := d.height - fixed - d.outcomeRows()
	chartRows = max(min(spare/3, maxChartRows), 1)
	return chartRows, max(spare-chartRows, 1)
}

func (d *detail) view() string {
	if d.tab == tabAbout {
		return d.about.View()
	}
	chartRows, tableRows := d.split()
	lines := []string{
		" " + d.st.header.Render(elide(d.market.Question, d.width-2)),
		" " + d.st.faint.Render(elide(d.facts(), d.width-2)),
		"",
	}
	lines = append(lines, strings.Split(d.outcomes.view(), "\n")...)
	lines = append(lines, "")
	lines = append(lines, d.chartLines(chartRows)...)
	lines = append(lines, "")

	book, trades := d.bookLines(tableRows), d.tradeLines(tableRows)
	for i := range max(len(book), len(trades)) {
		var left, right string
		if i < len(book) {
			left = book[i]
		}
		if i < len(trades) {
			right = trades[i]
		}
		lines = append(lines, fit(left, bookWidth, false)+strings.Repeat(" ", paneGap)+right)
	}
	return strings.Join(lines, "\n")
}

// state says whether the market is trading.
func state(m *api.Market) string {
	switch {
	case m.Closed:
		return "closed"
	case !m.AcceptingOrders:
		return "open · not taking orders"
	}
	return "open"
}

// facts is the line under the question: where the market stands.
func (d *detail) facts() string {
	m := &d.market
	parts := []string{state(m)}
	if !m.EndDate.IsZero() {
		day := m.EndDate.Format(time.DateOnly)
		if now := d.now(); m.EndDate.After(now) {
			parts = append(parts, "ends "+day+" ("+format.Until(m.EndDate.Time, now)+")")
		} else {
			parts = append(parts, "ended "+day)
		}
	}
	figures := []struct {
		name  string
		value api.Float
	}{{"vol 24h", m.Volume24h}, {"volume", m.Volume}, {"liq", m.Liquidity}}
	for _, f := range figures {
		// A market that has never traded is listed with no figures at all.
		if f.value.Valid {
			parts = append(parts, f.name+" "+format.Money(f.value.Value))
		}
	}
	return strings.Join(parts, " · ")
}

// said is a line of a pane that stands in for its contents.
func (d *detail) said(text string, failed bool, w int) string {
	style := d.st.faint
	if failed {
		style = d.st.failure
	}
	return " " + style.Render(elide(text, w-2))
}

// extent is the first, the last, the least and the greatest price of a
// history.
func extent(points []api.PricePoint) (first, last, low, high float64, ok bool) {
	for _, p := range points {
		if !p.Price.Valid {
			continue
		}
		v := p.Price.Value
		if !ok {
			first, low, high, ok = v, v, v, true
		}
		last, low, high = v, min(low, v), max(high, v)
	}
	return first, last, low, high, ok
}

// chartLines draws the price history of the outcome under the cursor: a
// title, and rows lines under it.
func (d *detail) chartLines(rows int) []string {
	iv := chartIntervals[d.interval]
	o, _, ok := d.selected()
	title := " " + d.st.header.Render("Price")
	if ok {
		title = " " + d.st.header.Render("Price of "+o.Label) + d.st.faint.Render(" · "+iv.name)
	}
	lines := make([]string, 1+rows)

	p := d.charts[d.keyOf(o)]
	switch {
	case !ok:
		lines[1] = d.said("no outcome to chart", false, d.width)
	case o.TokenID == "":
		lines[1] = d.said("no prices yet: the market has not opened for trading", false, d.width)
	case p == nil || p.waiting():
		lines[1] = d.said("loading the prices…", false, d.width)
	case p.failure != "":
		lines[1] = d.said(p.failure, true, d.width)
	default:
		first, last, low, high, priced := extent(p.points)
		if !priced {
			lines[1] = d.said("no prices over "+iv.span+" · i changes the interval", false, d.width)
			break
		}
		title += "  " + format.Price(last) + "  " + d.st.delta(api.Float{Value: last - first, Valid: true}) +
			d.st.faint.Render("  low "+format.Price(low)+" · high "+format.Price(high))
		for i, l := range spark(resample(p.points, d.width-2), rows) {
			lines[1+i] = " " + l
		}
	}
	lines[0] = title
	return lines
}

// table lays rows out under columns in a pane w wide, as many as fit.
func (d *detail) table(cols []column, cells [][]string, w, rows int) []string {
	t := newList(d.st, cols)
	t.setSize(w, 1+rows)
	t.setRows(cells)
	return t.plain()
}

// bookLines draws the order book of the outcome under the cursor, best
// prices first: a title, and a table of at most rows levels.
func (d *detail) bookLines(rows int) []string {
	o, _, ok := d.selected()
	title := " " + d.st.header.Render("Book")
	p := d.books[o.TokenID]
	switch {
	case !ok:
		return []string{title, d.said("no outcome to show", false, bookWidth)}
	case o.TokenID == "":
		return []string{title, d.said("none yet: not open", false, bookWidth)}
	case p == nil || p.waiting():
		return []string{title, d.said("loading the book…", false, bookWidth)}
	case p.failure != "":
		return []string{title, d.said(p.failure, true, bookWidth)}
	case p.none || p.book == nil:
		// The service has no book for a market that has stopped trading.
		return []string{title, d.said("none: not trading", false, bookWidth)}
	}

	title += d.st.faint.Render(" · " + o.Label)
	if !p.book.Time.IsZero() {
		title += d.st.faint.Render(" · " + format.Clock(p.book.Time.Time, d.now()))
	}
	side := func(levels []api.Level, i int) (price, size string) {
		if i >= len(levels) {
			return "", ""
		}
		return format.Price(levels[i].Price.Value), format.Quantity(levels[i].Size.Value)
	}
	cells := make([][]string, min(max(len(p.book.Bids), len(p.book.Asks)), rows))
	for i := range cells {
		bid, bidSize := side(p.book.Bids, i)
		ask, askSize := side(p.book.Asks, i)
		cells[i] = []string{bidSize, bid, ask, askSize}
	}
	if len(cells) == 0 {
		return []string{title, d.said("no orders on either side", false, bookWidth)}
	}
	return append([]string{title}, d.table([]column{
		{title: "Size", width: 6, right: true},
		{title: "Bid", width: 6, right: true},
		{title: "Ask", width: 6, right: true},
		{title: "Size", width: 6, right: true},
	}, cells, bookWidth, rows)...)
}

// tradeLines draws the market's latest trades, newest first: a title, and a
// table of at most rows of them.
func (d *detail) tradeLines(rows int) []string {
	w := max(d.width-bookWidth-paneGap, 1)
	title := " " + d.st.header.Render("Trades")
	p := &d.trades
	switch {
	case d.market.ConditionID == "":
		return []string{title, d.said("the market has no condition ID to ask by", false, w)}
	case p.waiting():
		return []string{title, d.said("loading the trades…", false, w)}
	case p.failure != "":
		return []string{title, d.said(p.failure, true, w)}
	case len(p.trades) == 0:
		return []string{title, d.said("none yet", false, w)}
	}

	now := d.now()
	cells := make([][]string, min(len(p.trades), rows))
	for i := range cells {
		t := &p.trades[i]
		side := t.Side
		// The taker bought or sold; the colour is not the only sign of which.
		switch side {
		case "BUY":
			side = d.st.up.Render(side)
		case "SELL":
			side = d.st.down.Render(side)
		}
		cells[i] = []string{
			format.Clock(t.Time.Time, now),
			side,
			t.Outcome,
			price(t.Price),
			format.Quantity(t.Size.Value),
		}
	}
	return append([]string{title}, d.table([]column{
		{title: "Time", width: 11},
		{title: "Side", width: 4},
		{title: "Outcome", width: 7},
		{title: "Price", width: 6, right: true},
		{title: "Shares", width: 6, right: true},
	}, cells, w, rows)...)
}

// aboutText is the second tab: what identifies the market, and its
// description, wrapped to the window.
func (d *detail) aboutText() string {
	m := &d.market
	w := max(d.width-2, 1)
	const labelWidth = 14

	var lines []string
	// A fact's value keeps to its own column: a slug or an address longer
	// than the window is broken where the line ends, not at its hyphens.
	fact := func(label, value string) {
		if value == "" {
			return
		}
		lead := fit(label, labelWidth, false)
		for _, l := range strings.Split(ansi.Hardwrap(value, max(w-labelWidth, 1), true), "\n") {
			lines = append(lines, " "+lead+l)
			lead = strings.Repeat(" ", labelWidth)
		}
	}
	moment := func(t api.Time) string {
		if t.IsZero() {
			return ""
		}
		return t.UTC().Format("2006-01-02 15:04 UTC")
	}

	lines = append(lines, "")
	for _, l := range strings.Split(ansi.Wrap(m.Question, w, ""), "\n") {
		lines = append(lines, " "+d.st.header.Render(l))
	}
	lines = append(lines, "")
	if len(m.Events) > 0 {
		fact("Event", m.Events[0].Title)
	}
	fact("State", state(m))
	fact("Started", moment(m.StartDate))
	fact("Ends", moment(m.EndDate))
	if m.TickSize.Valid {
		tick := "tick " + format.Price(m.TickSize.Value)
		if m.MinOrderSize.Valid {
			tick += " · at least " + format.Quantity(m.MinOrderSize.Value) + " shares"
		}
		fact("Orders", tick)
	}
	fact("Slug", m.Slug)
	fact("Condition ID", m.ConditionID)
	fact("Page", export.MarketURL(m))
	lines = append(lines, "")

	if text := plainText(m.Description); text != "" {
		for _, l := range strings.Split(ansi.Wrap(text, w, ""), "\n") {
			lines = append(lines, " "+l)
		}
	} else {
		lines = append(lines, " "+d.st.faint.Render("the market has no description"))
	}
	return strings.Join(lines, "\n")
}

// plainText readies text the service sent for the screen: a tab is a space,
// since a terminal would move the rest of the line by it and the frame with
// it, other control characters go, and so do the spaces a line ends in.
func plainText(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i, l := range lines {
		l = strings.Map(func(r rune) rune {
			switch {
			case r == '\t':
				return ' '
			case r < ' ' || r == 0x7f:
				return -1
			}
			return r
		}, l)
		lines[i] = strings.TrimRight(l, " ")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func (d *detail) crumb() string {
	if d.inEvent && d.market.GroupItemTitle != "" {
		return elide(d.market.GroupItemTitle, crumbWidth)
	}
	return elide(d.market.Question, crumbWidth)
}

func (d *detail) tabs() string {
	names := []string{"Market", "About"}
	for i, n := range names {
		if detailTab(i) == d.tab {
			names[i] = d.st.tab.Render("[" + n + "]")
		} else {
			names[i] = d.st.crumb.Render(n)
		}
	}
	return strings.Join(names, " ")
}

func (d *detail) note() string { return "" }

func (d *detail) typing() bool { return false }

// exports offers what the panes show: the price history, the trades and the
// order book. Each can be what is on screen, as it is held, or the whole of
// it, fetched: every outcome's, and the trades back to the first.
func (d *detail) exports() []exportChoice {
	market := d.market
	m := &market
	client := d.client
	o, index, ok := d.selected()
	interval := chartIntervals[d.interval].name
	// Only an outcome with a token can be asked about.
	tradable := export.Tradable(m)

	var history, trades, book []exportScope
	if p := d.charts[d.keyOf(o)]; ok && p != nil && p.failure == "" && len(p.points) > 0 {
		points := slices.Clone(p.points)
		history = append(history, exportScope{
			label: o.Label + " · " + interval + ", as charted",
			open: func(context.Context) export.Dataset {
				return export.History(export.Loaded([]export.Series{{Market: m, Outcome: index, Points: points}}))
			},
		})
	}
	if len(tradable) > 0 {
		history = append(history, exportScope{
			label: "every outcome · " + interval,
			open: func(ctx context.Context) export.Dataset {
				return export.History(export.HistoryPages(ctx, client, m, tradable, interval, historyPageSize))
			},
		})
	}

	if held := slices.Clone(d.trades.trades); len(held) > 0 {
		trades = append(trades, exportScope{
			label: "the " + strconv.Itoa(len(held)) + " latest",
			open: func(context.Context) export.Dataset {
				return export.Trades(export.Loaded(held))
			},
		})
	}
	if m.ConditionID != "" {
		trades = append(trades, exportScope{
			label: "all of them",
			open: func(ctx context.Context) export.Dataset {
				q := api.TradesQuery{ConditionID: m.ConditionID, Limit: tradesPageSize}
				return export.Trades(api.Pages(ctx, func(cursor string) ([]api.Trade, string, error) {
					q.Cursor = cursor
					return client.Trades(ctx, q)
				}))
			},
		})
	}

	if p := d.books[o.TokenID]; ok && p != nil && p.failure == "" && p.book != nil {
		held := p.book
		book = append(book, exportScope{
			label: o.Label + ", as shown",
			open: func(context.Context) export.Dataset {
				return export.Book(export.Loaded([]export.Depth{{Market: m, Outcome: index, Book: held}}))
			},
		})
	}
	// A market that has closed has no book to fetch.
	if len(tradable) > 0 && !m.Closed {
		book = append(book, exportScope{
			label: "every outcome",
			open: func(ctx context.Context) export.Dataset {
				return export.Book(export.BookPages(ctx, client, m, tradable))
			},
		})
	}

	var choices []exportChoice
	for _, c := range []exportChoice{{"history", history}, {"trades", trades}, {"book", book}} {
		if len(c.scopes) > 0 {
			choices = append(choices, c)
		}
	}
	return choices
}

func (d *detail) hints() []key.Binding {
	k := d.keys
	if d.tab == tabAbout {
		// The same key, named for where it leads from here.
		back := key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "market"))
		return []key.Binding{back, k.Browse, k.Copy, k.Export, k.Help, k.Back, k.Quit}
	}
	about := key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "about"))
	return []key.Binding{about, k.Interval, k.Refresh, k.Browse, k.Export, k.Help, k.Back, k.Quit}
}

func (d *detail) status() status {
	s := status{failure: d.failure}
	switch {
	case d.flash != "":
		s.text = d.flash
		return s
	case d.tab == tabAbout:
		s.text = "about the market"
		if d.about.TotalLineCount() > d.about.VisibleLineCount() {
			s.text += " · " + strconv.Itoa(int(d.about.ScrollPercent()*100)) + "%"
		}
	}
	// The market tab says nothing here as a rule: the cursor shows which
	// outcome the panes are of, and the keys need the room.
	if d.refreshing {
		if s.text != "" {
			s.text += " · "
		}
		s.text += "refreshing…"
	}
	return s
}
