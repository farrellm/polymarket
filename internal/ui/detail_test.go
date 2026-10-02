package ui

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/farrellm/polymarket/internal/api"
)

func level(price, size float64) api.Level {
	return api.Level{Price: amount(price), Size: amount(size)}
}

func point(ago time.Duration, price float64) api.PricePoint {
	return api.PricePoint{Time: api.Time{Time: testNow.Add(-ago)}, Price: amount(price)}
}

func trade(ago time.Duration, side, outcome string, price, size float64) api.Trade {
	return api.Trade{
		Time: api.Time{Time: testNow.Add(-ago)}, Side: side, Outcome: outcome,
		Price: amount(price), Size: amount(size),
	}
}

const day = 24 * time.Hour

// bob opens the detail of Bob, the busiest market of nominee, from the list
// of its event's markets. Both his outcomes have a book; Yes has a history
// over a week and over a month, and No has none over either.
func bob(t *testing.T) (*Model, *fakeClient) {
	t.Helper()
	m, c := politics(t)
	c.books = map[string]api.Book{
		"m2-yes": {
			Time: api.Time{Time: testNow.Add(-30 * time.Second)},
			Bids: []api.Level{level(0.29, 1000), level(0.28, 2500)},
			Asks: []api.Level{level(0.31, 400)},
		},
		"m2-no": {
			Bids: []api.Level{level(0.69, 400)},
			Asks: []api.Level{level(0.71, 1000), level(0.72, 2500)},
		},
	}
	c.history = map[string][]api.PricePoint{
		"m2-yes 1w": {point(7*day, 0.25), point(5*day, 0.24), point(2*day, 0.28), point(0, 0.30)},
		"m2-yes 1m": {point(30*day, 0.5), point(0, 0.30)},
	}
	c.trades = []api.Trade{
		trade(90*time.Second, "BUY", "Yes", 0.30, 1500),
		trade(13*time.Hour, "SELL", "No", 0.69, 20),
		trade(400*day, "BUY", "Yes", 0.05, 12),
	}
	press(m, "enter", "enter")
	wantContains(t, "title bar", lines(m)[0], "Nominee 2028 ▸ Bob ─ [Market] About")
	return m, c
}

// find is the first line of the screen that holds the text.
func find(t *testing.T, m *Model, text string) string {
	t.Helper()
	for _, l := range lines(m) {
		if strings.Contains(l, text) {
			return l
		}
	}
	t.Fatalf("no line holds %q:\n%s", text, screenText(m))
	return ""
}

func TestMarketDetail(t *testing.T) {
	m, c := bob(t)
	got := lines(m)
	wantContains(t, "question", got[1], "Will Bob win?")
	wantContains(t, "facts", got[2], "open · ends 2026-11-11 (1mo) · vol 24h $300 · volume $3.0K · liq $600")

	// The first outcome's quote is the market's; the second is the same
	// book from the other side.
	wantContains(t, "header", got[4], "Outcome", "Price", "Bid", "Ask", "Spread", "Last", "24h Δ")
	wantContains(t, "first outcome", got[5], cursorMarker+"Yes", "30.0¢   29.0¢   31.0¢    2.0¢   30.0¢   -1.0¢")
	wantContains(t, "second outcome", got[6], "No", "70.0¢   69.0¢   71.0¢    2.0¢   70.0¢   +1.0¢")

	// The chart is of the outcome under the cursor, over a week to begin
	// with: where the price is, how far it has come, and its range.
	wantContains(t, "chart", find(t, m, "Price of"), "Price of Yes · 1w  30.0¢  +5.0¢  low 24.0¢ · high 30.0¢")
	if !strings.ContainsAny(screenText(m), string(sparkBlocks)) {
		t.Errorf("no chart was drawn:\n%s", screenText(m))
	}

	// The book best first, a side with fewer levels left blank; the trades
	// newest first, each dated as precisely as its age needs.
	wantContains(t, "panes", find(t, m, "Book"), "Book · Yes · 11:59:30", "Trades")
	wantContains(t, "tables", find(t, m, "Shares"), "Size", "Bid", "Ask", "Time", "Side", "Outcome", "Price")
	wantContains(t, "best level", find(t, m, "1.0K   29.0¢"), "1.0K   29.0¢   31.0¢     400", "11:58:30", "BUY", "1.5K")
	wantContains(t, "second level", find(t, m, "28.0¢"), "2.5K   28.0¢", "Oct 1 23:00", "SELL  No", "69.0¢", "20")
	wantContains(t, "an old trade", find(t, m, "2025-08-28"), "BUY   Yes", "5.0¢", "12")

	// One request each, for the outcome on show alone; the market itself
	// came with the event.
	if !slices.Equal(c.bookTokens, []string{"m2-yes"}) {
		t.Errorf("books asked for: %q, want the first outcome's", c.bookTokens)
	}
	if len(c.histories) != 1 || c.histories[0] != (api.HistoryQuery{TokenID: "m2-yes", Interval: "1w"}) {
		t.Errorf("histories asked for: %+v, want the first outcome's over a week", c.histories)
	}
	if len(c.tradeQueries) != 1 || c.tradeQueries[0] != (api.TradesQuery{ConditionID: "0xm2", Limit: tradesShown}) {
		t.Errorf("trades asked for: %+v, want the market's latest", c.tradeQueries)
	}
	if len(c.marketIDs) != 0 {
		t.Errorf("the market was fetched %d times on opening it", len(c.marketIDs))
	}
	wantContains(t, "status bar", statusLine(m), "tab about", "i interval", "r refresh", "o website", "esc back")

	// Esc leads back to the event's markets, the cursor where it was.
	press(m, "esc")
	wantContains(t, "title bar", lines(m)[0], "Tags ▸ Politics ▸ Nominee 2028 ─")
	wantContains(t, "cursor row", cursorLine(t, m), "Bob")
}

func TestOutcomeUnderTheCursorIsCharted(t *testing.T) {
	m, c := bob(t)

	press(m, "down")
	wantContains(t, "cursor row", cursorLine(t, m), "No")
	wantContains(t, "book", find(t, m, "Book"), "Book · No")
	wantContains(t, "best level", find(t, m, "400   69.0¢"), "400   69.0¢   71.0¢    1.0K")
	wantContains(t, "chart", find(t, m, "Price of"), "Price of No · 1w")
	wantContains(t, "chart", screenText(m), "no prices over the last week · i changes the interval")
	if !slices.Equal(c.bookTokens, []string{"m2-yes", "m2-no"}) || len(c.histories) != 2 {
		t.Errorf("asked for books %q and %d histories", c.bookTokens, len(c.histories))
	}

	// An outcome looked at before is shown again without asking.
	press(m, "up", "down", "up")
	wantContains(t, "book", find(t, m, "Book"), "Book · Yes")
	if len(c.bookTokens) != 2 || len(c.histories) != 2 || len(c.tradeQueries) != 1 {
		t.Errorf("moving between outcomes already seen made requests: %d books, %d histories, %d trades",
			len(c.bookTokens), len(c.histories), len(c.tradeQueries))
	}
}

func TestChartIntervalCycles(t *testing.T) {
	m, c := bob(t)

	press(m, "i")
	wantContains(t, "chart", find(t, m, "Price of"), "Price of Yes · 1m  30.0¢  -20.0¢  low 30.0¢ · high 50.0¢")
	press(m, "i")
	wantContains(t, "chart", find(t, m, "Price of"), "Price of Yes · max")
	wantContains(t, "chart", screenText(m), "no prices over the market's lifetime")
	press(m, "i")
	wantContains(t, "chart", find(t, m, "Price of"), "Price of Yes · 1d")
	press(m, "i")
	wantContains(t, "chart", find(t, m, "Price of"), "Price of Yes · 1w  30.0¢  +5.0¢")

	var asked []string
	for _, q := range c.histories {
		asked = append(asked, q.Interval)
	}
	// The week was in hand already when it came round again.
	if !slices.Equal(asked, []string{"1w", "1m", "max", "1d"}) {
		t.Errorf("intervals asked for: %q", asked)
	}
	if len(c.bookTokens) != 1 {
		t.Errorf("%d book requests, want the interval to leave the book alone", len(c.bookTokens))
	}
}

func TestRefreshOfAMarket(t *testing.T) {
	m, c := bob(t)
	press(m, "down", "up") // the other outcome's book is in hand too

	// A market fetched on its own comes without its event.
	fresh := market("m2", "Will Bob win?", "Bob", 0.35, 0.04, 800)
	c.market = map[string]api.Market{"m2": fresh}
	c.books["m2-yes"] = api.Book{Bids: []api.Level{level(0.34, 10)}, Asks: []api.Level{level(0.36, 10)}}
	books, histories := len(c.bookTokens), len(c.histories)

	_, cmd := m.Update(keyMsg("r"))
	// What is on screen stays until its replacement arrives.
	wantContains(t, "status bar", statusLine(m), "refreshing…")
	wantContains(t, "first outcome", lines(m)[5], "30.0¢")
	wantContains(t, "book", screenText(m), "29.0¢   31.0¢")
	settle(m, cmd)

	if !slices.Equal(c.marketIDs, []string{"m2"}) || len(c.tradeQueries) != 2 {
		t.Errorf("refresh asked for markets %q and %d lots of trades", c.marketIDs, len(c.tradeQueries))
	}
	if !slices.Equal(c.bookTokens[books:], []string{"m2-yes"}) || len(c.histories) != histories+1 {
		t.Errorf("refresh asked for books %q and %d histories, want those on show alone",
			c.bookTokens[books:], len(c.histories)-histories)
	}
	wantContains(t, "first outcome", lines(m)[5], "35.0¢   34.0¢   36.0¢", "+4.0¢")
	wantContains(t, "facts", lines(m)[2], "vol 24h $800")
	wantContains(t, "book", screenText(m), "10   34.0¢   36.0¢      10")
	if strings.Contains(statusLine(m), "refreshing") {
		t.Errorf("status bar = %q after the refresh", statusLine(m))
	}
	wantContains(t, "title bar", lines(m)[0], "Nominee 2028 ▸ Bob")

	// The event it was opened under is still where its page lives.
	press(m, "o")
	if !slices.Equal(c.opened, []string{"https://polymarket.com/event/nominee-2028/slug-m2"}) {
		t.Errorf("opened %q", c.opened)
	}

	// The other outcome's book was dropped, and is fetched when next shown.
	press(m, "down")
	if got := c.bookTokens[len(c.bookTokens)-1]; got != "m2-no" {
		t.Errorf("last book asked for: %q, want the second outcome's again", got)
	}
}

func TestPanesFailOnTheirOwn(t *testing.T) {
	m, c := politics(t)
	c.fail = errors.New("boom")
	press(m, "enter", "enter")

	// The market came with its event, so the screen stands; each pane says
	// what it could not load.
	wantContains(t, "first outcome", lines(m)[5], "Yes", "30.0¢")
	screen := screenText(m)
	wantContains(t, "screen", screen,
		"could not load the prices: boom · r retries",
		"could not load the book: bo",
		"could not load the trades: boom · r retries")

	// A refresh that fails says so in the status bar, and leaves the market.
	press(m, "r")
	wantContains(t, "status bar", statusLine(m), "could not refresh the market: boom")
	wantContains(t, "first outcome", lines(m)[5], "Yes", "30.0¢")

	c.fail = nil
	c.market = map[string]api.Market{"m2": nominee().Markets[1]}
	c.trades = []api.Trade{trade(time.Minute, "BUY", "Yes", 0.3, 10)}
	press(m, "r")
	screen = screenText(m)
	if strings.Contains(screen, "boom") {
		t.Errorf("the failures outlived the retry:\n%s", screen)
	}
	// A token with no book is a market that is not trading.
	wantContains(t, "screen", screen, "none: not trading", "11:59:00", "no prices over the last week")
}

func TestClosedMarketStartsOnItsWholeHistory(t *testing.T) {
	m, c := politics(t)
	shut := market("m3", "Will Cy win?", "Cy", 1, 0, 900)
	shut.Closed, shut.AcceptingOrders = true, false
	shut.EndDate = api.Time{Time: testNow.Add(-3 * day)}
	c.marketPages = map[string]fakeMarkets{"": {markets: []api.Market{shut}}}
	c.history = map[string][]api.PricePoint{"m3-yes max": {point(90*day, 0.2), point(3*day, 1)}}

	press(m, "tab", "enter")
	// Opened from the list of a tag's markets, it goes by its question.
	wantContains(t, "title bar", lines(m)[0], "Tags ▸ Politics ▸ Will Cy win? ─ [Market] About")
	wantContains(t, "facts", lines(m)[2], "closed · ended 2026-09-29")
	// The quote it closed with is no quote now; the last trade still is one.
	wantContains(t, "first outcome", lines(m)[5], "Yes", "100.0¢       –       –       –  100.0¢       –")
	wantContains(t, "second outcome", lines(m)[6], "No", "0.0¢       –       –       –    0.0¢       –")
	wantContains(t, "chart", find(t, m, "Price of"), "Price of Yes · max  100.0¢  +80.0¢")
	wantContains(t, "book", screenText(m), "none: not trading")
	if len(c.histories) != 1 || c.histories[0].Interval != "max" {
		t.Errorf("histories asked for: %+v, want the whole of it", c.histories)
	}
}

func TestEventWithOneMarketOpensOnIt(t *testing.T) {
	solo := event("Solo", 900, 100, "Politics")
	solo.Slug = "solo"
	solo.Markets = []api.Market{market("s1", "Will it happen?", "", 0.5, 0, 900)}
	c := onePage(append([]api.Event{solo}, sampleEvents()...)...)
	m := open(t, c)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	press(m, "down", "enter") // Politics
	wantContains(t, "cursor row", cursorLine(t, m), "Solo")

	press(m, "enter")
	if len(m.stack) != 3 {
		t.Fatalf("stack is %d deep, want the market straight under the tag", len(m.stack))
	}
	wantContains(t, "title bar", lines(m)[0], "Tags ▸ Politics ▸ Will it happen? ─ [Market] About")
	wantContains(t, "question", lines(m)[1], "Will it happen?")
	press(m, "o")
	if !slices.Equal(c.opened, []string{"https://polymarket.com/event/solo/slug-s1"}) {
		t.Errorf("opened %q", c.opened)
	}

	press(m, "esc")
	wantContains(t, "cursor row", cursorLine(t, m), "Solo")
	// An event with more is still a list.
	press(m, "down", "enter")
	wantContains(t, "header", lines(m)[1], "Market", "Price")
}

func TestAboutAMarket(t *testing.T) {
	m, c := politics(t)
	e := nominee()
	paragraphs := make([]string, 30)
	for i := range paragraphs {
		paragraphs[i] = "This market resolves to Yes if Bob is the nominee, according to the official count of the convention."
	}
	// The service's text comes with tabs and carriage returns in it.
	e.Markets[1].Description = strings.Join(paragraphs, "\t\t\r\n\r\n") + "\t"
	e.Markets[1].Slug = "will-bob-win-the-nomination-of-his-party-at-its-convention-in-the-summer-of-2028"
	e.Markets[1].StartDate = api.Time{Time: testNow.Add(-100 * day)}
	e.Markets[1].TickSize, e.Markets[1].MinOrderSize = amount(0.001), amount(5)
	c.pages[""] = fakePage{events: append([]api.Event{e}, sampleEvents()...)}
	press(m, "r", "enter", "enter")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	requests := len(c.done)

	press(m, "tab")
	wantContains(t, "title bar", lines(m)[0], "Market [About]")
	screen := screenText(m)
	wantContains(t, "about", screen,
		"Will Bob win?",
		"Event         Nominee 2028",
		"State         open",
		"Started       2026-06-24 12:00 UTC",
		"Ends          2026-11-11 12:00 UTC",
		"Orders        tick 0.1¢ · at least 5 shares",
		// A value too long for the line is broken at its end, and goes on
		// under itself.
		"│ Slug          will-bob-win-the-nomination-of-his-party-at-its-convention-in- │",
		"│               the-summer-of-2028",
		"Condition ID  0xm2",
		"Page          https://polymarket.com/event/nominee-2028/will-bob-win-the-nom │",
		// Wrapped at the edge of the window, between words.
		"│ This market resolves to Yes if Bob is the nominee, according to the official │",
		"│ count of the convention.")
	wantContains(t, "status bar", statusLine(m), "about the market · 0%", "tab market", "o website", "y copy slug")
	for i, l := range lines(m) {
		if w := width(l); w != 80 || strings.ContainsAny(l, "\t\r") {
			t.Errorf("line %d is %d columns, or holds a tab: %q", i, w, l)
		}
	}

	// The keys that move through a list move through the text.
	press(m, "down", "down")
	if strings.Contains(screenText(m), "Will Bob win?") {
		t.Errorf("two lines down, the question is still in view:\n%s", screenText(m))
	}
	press(m, "G")
	wantContains(t, "status bar", statusLine(m), "about the market · 100%")
	press(m, "g")
	wantContains(t, "about", screenText(m), "Will Bob win?")
	if len(c.done) != requests {
		t.Errorf("%d requests made by reading about the market", len(c.done)-requests)
	}

	press(m, "tab")
	wantContains(t, "title bar", lines(m)[0], "[Market] About")
	wantContains(t, "header", lines(m)[4], "Outcome", "Price")
}

func TestCopyAndOpen(t *testing.T) {
	m, c := bob(t)

	press(m, "y")
	wantContains(t, "status bar", statusLine(m), "copied the slug: slug-m2")
	press(m, "Y")
	wantContains(t, "status bar", statusLine(m), "copied the condition ID: 0xm2")

	press(m, "o")
	wantContains(t, "status bar", statusLine(m), "opening https://polymarket.com/event/nominee-2028/slug-m2")
	c.openErr = errors.New("no browser")
	press(m, "o")
	wantContains(t, "status bar", statusLine(m), "could not open the browser: no browser")
	// A message is for the moment it was shown in.
	press(m, "down")
	if strings.Contains(statusLine(m), "browser") {
		t.Errorf("status bar = %q after another key", statusLine(m))
	}
}

func TestMarketThatHasNotOpened(t *testing.T) {
	m, c := politics(t)
	press(m, "enter", "G", "enter") // Dee, who has no figures at all
	requests := len(c.done)

	got := lines(m)
	wantContains(t, "title bar", got[0], "Nominee 2028 ▸ Dee")
	wantContains(t, "facts", got[2], "open · not taking orders")
	if strings.Contains(got[2], "$") {
		t.Errorf("facts = %q, want no figure where the market has none", got[2])
	}
	wantContains(t, "outcomes", screenText(m), "the market lists no outcomes", "no outcome to chart",
		"no outcome to show", "the market has no condition ID")
	press(m, "down", "i", "y")
	wantContains(t, "status bar", statusLine(m), "the market has no slug")
	press(m, "o")
	wantContains(t, "status bar", statusLine(m), "no page to open")
	if len(c.done) != requests || len(c.opened) != 0 {
		t.Errorf("%d requests and %d pages opened for a market with nothing to ask about",
			len(c.done)-requests, len(c.opened))
	}
}

func TestStaleAnswersToAMarketAreDropped(t *testing.T) {
	m, c := bob(t)
	before := len(c.done)
	c.market = map[string]api.Market{"m2": market("m2", "Will Bob win?", "Bob", 0.9, 0, 1)}

	// A refresh overtaken by another: its answers, when they come, are of
	// requests since abandoned.
	_, first := m.Update(keyMsg("r"))
	stale := messages(first)
	overtaken := c.done[before:]
	c.market["m2"] = market("m2", "Will Bob win?", "Bob", 0.4, 0, 1)
	press(m, "r")
	for i, done := range overtaken {
		select {
		case <-done:
		default:
			t.Errorf("request %d of the first refresh was not cancelled by the second", i)
		}
	}
	wantContains(t, "first outcome", lines(m)[5], "40.0¢")
	for _, msg := range stale {
		m.Update(msg)
	}
	wantContains(t, "first outcome", lines(m)[5], "40.0¢")

	// Nor does a market take another's answers for its own.
	top := m.top().(*detail)
	other := newDetail(top.env, market("m9", "Another?", "", 0.1, 0, 1), false)
	m.Update(tradesMsg{owner: other, gen: top.gen})
	m.Update(marketMsg{owner: other, gen: top.gen, market: &api.Market{Question: "Another?"}})
	wantContains(t, "question", lines(m)[1], "Will Bob win?")
	wantContains(t, "trades", screenText(m), "11:58:30")

	// Leaving stops whatever is still under way.
	m.Update(keyMsg("r"))
	leaving := c.done[len(c.done)-4:]
	press(m, "esc")
	for i, done := range leaving {
		select {
		case <-done:
		default:
			t.Errorf("request %d was not cancelled on leaving the market", i)
		}
	}
}

func TestHelpOnAMarket(t *testing.T) {
	m, _ := bob(t)
	press(m, "h")
	screen := screenText(m)
	wantContains(t, "help", screen, "Moving", "Levels", "Market", "market / about", "interval", "refresh",
		"export", "website", "copy slug", "copy condition ID", "quit")
	// The keys of the lists do nothing here, and are left out.
	for _, absent := range []string{"Forms", "sub-tag", "find tag"} {
		if strings.Contains(screen, absent) {
			t.Errorf("help on a market mentions %q:\n%s", absent, screen)
		}
	}
	press(m, "esc")
	wantContains(t, "title bar", lines(m)[0], "Nominee 2028 ▸ Bob")
	// T still leads to the top.
	press(m, "T")
	if len(m.stack) != 1 {
		t.Errorf("stack is %d deep after T, want 1", len(m.stack))
	}
}

func TestBreadcrumbGivesWayFromTheTop(t *testing.T) {
	m, _ := bob(t)
	for _, tt := range []struct {
		width int
		want  string
	}{
		{120, "┌ polymarket ─ Tags ▸ Politics ▸ Nominee 2028 ▸ Bob ─ [Market] About ─"},
		{60, "┌ polymarket ─ … ▸ Nominee 2028 ▸ Bob ─ [Market] About ─"},
		{50, "┌ polymarket ─ … ▸ Bob ─ [Market] About ─"},
	} {
		m.Update(tea.WindowSizeMsg{Width: tt.width, Height: 24})
		title := lines(m)[0]
		wantContains(t, "title bar", title, tt.want)
		if w := width(title); w != tt.width {
			t.Errorf("title bar is %d columns, want %d: %q", w, tt.width, title)
		}
	}
}

func TestTallWindowGivesThePanesMoreRows(t *testing.T) {
	m, c := bob(t)
	for i := range 40 {
		c.trades = append(c.trades, trade(time.Duration(i)*time.Minute, "BUY", "Yes", 0.3, float64(100+i)))
	}
	c.market = map[string]api.Market{"m2": nominee().Markets[1]}
	press(m, "r")

	count := func() int {
		n := 0
		for _, l := range lines(m) {
			if strings.Contains(l, "BUY") || strings.Contains(l, "SELL") {
				n++
			}
		}
		return n
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if got := count(); got != 6 {
		t.Errorf("%d trades shown in 24 lines, want 6:\n%s", got, screenText(m))
	}
	if got := len(lines(m)); got != 24 {
		t.Errorf("the frame is %d lines, want 24", got)
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 48})
	// Twice the height: the chart grows to its most, the tables take the rest.
	if got := count(); got != 25 {
		t.Errorf("%d trades shown in 48 lines, want 25:\n%s", got, screenText(m))
	}
	if got := len(lines(m)); got != 48 {
		t.Errorf("the frame is %d lines, want 48", got)
	}
}

func TestQuoteOf(t *testing.T) {
	m := market("m", "?", "", 0.3, -0.02, 1)
	yes, no := quoteOf(&m, 0), quoteOf(&m, 1)
	near := func(got api.Float, want float64) bool {
		return got.Valid && got.Value > want-1e-9 && got.Value < want+1e-9
	}
	if !near(yes.bid, 0.29) || !near(yes.ask, 0.31) || !near(yes.spread, 0.02) || !near(yes.last, 0.3) || !near(yes.change, -0.02) {
		t.Errorf("first outcome = %+v", yes)
	}
	if !near(no.bid, 0.69) || !near(no.ask, 0.71) || !near(no.spread, 0.02) || !near(no.last, 0.7) || !near(no.change, 0.02) {
		t.Errorf("second outcome = %+v", no)
	}

	// With three outcomes the market's quote is of the first alone, and a
	// figure the service did not send is not made up.
	m.Outcomes = append(m.Outcomes, api.Outcome{Label: "Maybe"})
	if q := quoteOf(&m, 1); q != (quote{}) {
		t.Errorf("second outcome of three = %+v, want no quote", q)
	}
	bare := api.Market{Outcomes: []api.Outcome{{Label: "Yes"}, {Label: "No"}}}
	if q := quoteOf(&bare, 1); q != (quote{}) {
		t.Errorf("quote of a market with no figures = %+v", q)
	}
}

func TestResample(t *testing.T) {
	points := []api.PricePoint{point(4*time.Hour, 0.1), point(3*time.Hour, 0.2), point(0, 0.5)}
	// Each column holds the last price at or before its end: the long gap
	// repeats the price it began with.
	got := resample(points, 4)
	if !slices.Equal(got, []float64{0.2, 0.2, 0.2, 0.5}) {
		t.Errorf("resample = %v", got)
	}
	// More columns than points, a single point, and nothing at all.
	if got := resample(points[:1], 3); !slices.Equal(got, []float64{0.1, 0.1, 0.1}) {
		t.Errorf("resample of one point = %v", got)
	}
	if got := resample([]api.PricePoint{{Time: api.Time{Time: testNow}}}, 3); got != nil {
		t.Errorf("resample of a point with no price = %v", got)
	}
	if resample(nil, 3) != nil || resample(points, 0) != nil {
		t.Error("resample of nothing, or into no columns, is not nil")
	}
}

func TestSpark(t *testing.T) {
	// Two rows are sixteen steps: nought shows an eighth of the bottom row,
	// one fills both, and a half is just into the top one.
	got := spark([]float64{0, 0.5, 1}, 2)
	want := []string{" ▁█", "▁██"}
	if !slices.Equal(got, want) {
		t.Errorf("spark = %q, want %q", got, want)
	}
	// The scale is nought to one whatever the prices are: a narrow range
	// stays narrow, and a price that never moved runs at its own height.
	if got := spark([]float64{0.4, 0.5}, 2); !slices.Equal(got, []string{" ▁", "▇█"}) {
		t.Errorf("narrow spark = %q", got)
	}
	if got := spark([]float64{0.4, 0.4}, 1); !slices.Equal(got, []string{"▄▄"}) {
		t.Errorf("flat spark = %q", got)
	}
	if got := spark([]float64{0.9, 0.9}, 1); !slices.Equal(got, []string{"▇▇"}) {
		t.Errorf("flat spark near one = %q", got)
	}
	// A value off the scale is drawn at its nearer end.
	if got := spark([]float64{-0.5, 1.5}, 1); !slices.Equal(got, []string{"▁█"}) {
		t.Errorf("spark off the scale = %q", got)
	}
	if spark(nil, 2) != nil || spark([]float64{1}, 0) != nil {
		t.Error("spark of nothing, or in no rows, is not nil")
	}
}
