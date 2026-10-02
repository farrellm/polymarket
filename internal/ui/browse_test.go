package ui

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/farrellm/polymarket/internal/api"
)

// market is an open market with one price, its change over a day and its
// volume over one. short is its name within its event. It is quoted a cent
// either side of its price, and its slug, condition ID and tokens are named
// after its ID.
func market(id, question, short string, price, change, volume24h float64) api.Market {
	return api.Market{
		ID:              id,
		Slug:            "slug-" + id,
		ConditionID:     "0x" + id,
		Question:        question,
		GroupItemTitle:  short,
		AcceptingOrders: true,
		Outcomes: []api.Outcome{
			{Label: "Yes", Price: amount(price), TokenID: id + "-yes"},
			{Label: "No", Price: amount(1 - price), TokenID: id + "-no"},
		},
		BestBid:        amount(price - 0.01),
		BestAsk:        amount(price + 0.01),
		LastTradePrice: amount(price),
		Change1d:       amount(change),
		Volume24h:      amount(volume24h),
		Volume:         amount(volume24h * 10),
		Liquidity:      amount(volume24h * 2),
		EndDate:        api.Time{Time: testNow.Add(40 * 24 * time.Hour)},
	}
}

// nominee is an event under Politics with four markets: two open, one
// closed, and one that has never traded and so has no figures at all.
func nominee() api.Event {
	e := event("Nominee 2028", 500, 5000, "Politics")
	e.Slug = "nominee-2028"
	e.EndDate = api.Time{Time: testNow.Add(800 * 24 * time.Hour)}
	shut := market("m3", "Will Cy win?", "Cy", 0, 0, 900)
	shut.Closed = true
	e.Markets = []api.Market{
		market("m1", "Will Ann win?", "Ann", 0.6, 0.035, 100),
		market("m2", "Will Bob win?", "Bob", 0.3, -0.01, 300),
		shut,
		{ID: "m4", Question: "Will Dee win?", GroupItemTitle: "Dee"},
	}
	return e
}

// politics opens a browser on the Politics tag of a service that holds the
// sample events and nominee, with two sub-tags and a page of markets, in a
// window 120 columns wide.
func politics(t *testing.T) (*Model, *fakeClient) {
	t.Helper()
	e := nominee()
	c := onePage(append([]api.Event{e}, sampleEvents()...)...)
	c.related = map[string][]api.Tag{"politics": {
		{ID: "id-trump", Label: "Trump", Slug: "trump", ActiveEvents: 328},
		{ID: "id-midterms", Label: "Midterms", Slug: "midterms", ActiveEvents: 1196},
	}}
	c.marketPages = map[string]fakeMarkets{"": {markets: []api.Market{e.Markets[1], e.Markets[0]}}}
	c.event = map[string]api.Event{e.ID: e}
	m := open(t, c)
	// Wide enough for the title bar to hold a filter in full.
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	press(m, "down", "enter") // Politics is the busiest tag
	wantContains(t, "title bar", lines(m)[0], "Tags ▸ Politics")
	return m, c
}

func lastQuery(t *testing.T, c *fakeClient) api.EventsQuery {
	t.Helper()
	if len(c.queries) == 0 {
		t.Fatal("no events were asked for")
	}
	return c.queries[len(c.queries)-1]
}

func TestTagListingReplacesTheSample(t *testing.T) {
	c := onePage(sampleEvents()...)
	m := open(t, c)
	asked := len(c.queries)

	press(m, "down", "down") // Politics
	_, cmd := m.Update(keyMsg("enter"))
	// The tag's share of the sample is there before the listing answers.
	wantLabels(t, m, "Election", "Fed decision")
	wantContains(t, "status bar", statusLine(m), "2 of the busiest · loading the rest…")
	wantContains(t, "strip", lines(m)[1], "Sub-tags: loading…")

	settle(m, cmd)
	wantLabels(t, m, "Election", "Fed decision")
	wantContains(t, "status bar", statusLine(m), "2 loaded · end")
	if len(c.queries) != asked+1 {
		t.Fatalf("%d listing requests on opening the tag, want 1", len(c.queries)-asked)
	}
	q := lastQuery(t, c)
	if q.TagID != "id-politics" || q.Limit != pageSize || q.Order != "volume24hr" || q.Ascending ||
		q.Status != api.StatusOpen || q.Cursor != "" || q.TitleSearch != "" {
		t.Errorf("query = %+v, want the tag's open events, the busiest first", q)
	}
	if len(c.relatedSlugs) != 1 || c.relatedSlugs[0] != "politics" {
		t.Errorf("sub-tags asked for %q, want politics", c.relatedSlugs)
	}
}

func TestNextPageLoadsNearTheEnd(t *testing.T) {
	c := manyPages(3)
	m := open(t, c)
	asked := len(c.queries)

	press(m, "enter") // All
	wantContains(t, "status bar", statusLine(m), "100 loaded · more available")
	if len(c.queries) != asked+1 {
		t.Fatalf("%d requests on opening, want only the first page", len(c.queries)-asked)
	}

	// Moving about the top of the list asks for nothing.
	press(m, "pgdown", "pgdown")
	if len(c.queries) != asked+1 {
		t.Errorf("%d requests after two pages down, want still 1", len(c.queries)-asked)
	}

	// The 82nd row of 100 is within a screen of the last.
	for range 5 {
		press(m, "pgdown")
	}
	wantContains(t, "status bar", statusLine(m), "200 loaded · more available")
	if q := lastQuery(t, c); q.Cursor != "cursor1" {
		t.Errorf("second page asked with cursor %q, want cursor1", q.Cursor)
	}
	wantContains(t, "cursor row", cursorLine(t, m), "event 133")

	press(m, "G")
	wantContains(t, "status bar", statusLine(m), "300 loaded · end")
	wantContains(t, "cursor row", cursorLine(t, m), "event 199")
	press(m, "G", "G")
	wantContains(t, "cursor row", cursorLine(t, m), "event 299")
	if len(c.queries) != asked+3 {
		t.Errorf("%d requests in all, want one per page", len(c.queries)-asked)
	}
}

func TestSortIsAskedOfTheService(t *testing.T) {
	m, c := politics(t)
	press(m, "down")
	asked := len(c.queries)

	press(m, "s")
	if q := lastQuery(t, c); q.Order != "volume1wk" || q.Ascending || q.Cursor != "" {
		t.Errorf("query = %+v, want the first page by volume1wk, largest first", q)
	}
	wantContains(t, "title bar", lines(m)[0], "vol 1w ↓ · open")
	// A new order is a new list: the cursor starts again.
	wantContains(t, "cursor row", cursorLine(t, m), "Nominee 2028")

	press(m, "S")
	if q := lastQuery(t, c); q.Order != "volume1wk" || !q.Ascending {
		t.Errorf("query = %+v, want volume1wk, smallest first", q)
	}
	wantContains(t, "title bar", lines(m)[0], "vol 1w ↑ · open")

	// End dates start with the nearest; everything else with the largest.
	press(m, "s", "s", "s", "s")
	if q := lastQuery(t, c); q.Order != "endDate" || !q.Ascending {
		t.Errorf("query = %+v, want endDate, nearest first", q)
	}
	press(m, "s")
	if q := lastQuery(t, c); q.Order != "startDate" || q.Ascending {
		t.Errorf("query = %+v, want startDate, latest first", q)
	}
	press(m, "s")
	wantContains(t, "title bar", lines(m)[0], "vol 24h ↓")
	if got := len(c.queries) - asked; got != 8 {
		t.Errorf("%d requests for 8 changes of sort", got)
	}
}

func TestTabsKeepTheirOwnRows(t *testing.T) {
	m, c := politics(t)
	if len(c.marketQueries) != 0 {
		t.Fatalf("%d market requests before the tab was shown", len(c.marketQueries))
	}
	press(m, "down")

	press(m, "tab")
	got := lines(m)
	wantContains(t, "title bar", got[0], "Events [Markets]")
	wantContains(t, "header", got[2], "Market", "Price", "24h Δ", "Vol 24h", "Volume", "Liq", "Ends")
	wantLabels(t, m, "Will Bob win?", "Will Ann win?")
	wantContains(t, "first row", got[3], "30.0¢", "-1.0¢", "$300", "$3.0K", "$600", "1mo")
	wantContains(t, "second row", got[4], "60.0¢", "+3.5¢")
	wantContains(t, "status bar", statusLine(m), "2 loaded · end")
	q := c.marketQueries[0]
	if q.TagID != "id-politics" || q.Limit != pageSize || q.Order != "volume24hr" || q.Ascending || q.Status != api.StatusOpen {
		t.Errorf("query = %+v, want the tag's open markets, the busiest first", q)
	}

	// Markets sort by the numeric field, which goes by another name.
	press(m, "s", "s", "s")
	if q := c.marketQueries[len(c.marketQueries)-1]; q.Order != "volumeNum" {
		t.Errorf("markets sorted by %q, want volumeNum", q.Order)
	}

	// The sort changed under the other tab too, so it loads again; after
	// that, switching asks for nothing.
	events, markets := len(c.queries), len(c.marketQueries)
	press(m, "tab")
	wantContains(t, "title bar", lines(m)[0], "[Events] Markets", "volume ↓")
	if len(c.queries) != events+1 || lastQuery(t, c).Order != "volume" {
		t.Errorf("events were not asked for again in the new order: %+v", lastQuery(t, c))
	}
	press(m, "down", "tab", "tab")
	if len(c.queries) != events+1 || len(c.marketQueries) != markets {
		t.Errorf("switching tabs made requests: %d events, %d markets", len(c.queries)-events-1, len(c.marketQueries)-markets)
	}
	wantContains(t, "cursor row", cursorLine(t, m), "Election")

	// A market in the list opens on its detail, under its question.
	press(m, "tab", "enter")
	if len(m.stack) != 3 {
		t.Errorf("stack is %d deep after enter on a market, want 3", len(m.stack))
	}
	wantContains(t, "title bar", lines(m)[0], "Tags ▸ Politics ▸ Will Bob win? ─ [Market] About")
	press(m, "esc")
	wantContains(t, "title bar", lines(m)[0], "Events [Markets]")
	wantContains(t, "cursor row", cursorLine(t, m), "Will Bob win?")
}

func TestSearchOfEvents(t *testing.T) {
	m, c := politics(t)
	asked := len(c.queries)

	// Esc at the prompt leaves things as they were.
	press(m, "/")
	typeText(m, "zzz")
	wantContains(t, "status bar", statusLine(m), "/zzz", "enter apply", "esc cancel")
	press(m, "esc")
	if len(c.queries) != asked || len(m.stack) != 2 {
		t.Fatalf("a cancelled search made %d requests and left the stack %d deep", len(c.queries)-asked, len(m.stack))
	}

	press(m, "/")
	typeText(m, " fed ")
	press(m, "enter")
	q := lastQuery(t, c)
	if q.TitleSearch != "fed" || q.TagID != "id-politics" || q.Order != "volume24hr" {
		t.Errorf("query = %+v, want the tag searched for fed, sorted as before", q)
	}
	wantLabels(t, m, "Fed decision")
	wantContains(t, "title bar", lines(m)[0], `vol 24h ↓ · "fed" · open`)
	wantContains(t, "status bar", statusLine(m), "1 loaded · end", "esc clear")

	// The prompt opens on the search as it stands.
	press(m, "/")
	wantContains(t, "status bar", statusLine(m), "/fed")
	press(m, "esc")

	// Esc first clears the search, then goes up.
	press(m, "esc")
	if q := lastQuery(t, c); q.TitleSearch != "" {
		t.Errorf("query = %+v, want the search cleared", q)
	}
	wantLabels(t, m, "Nominee 2028", "Election", "Fed decision")
	press(m, "esc")
	if len(m.stack) != 1 {
		t.Errorf("stack is %d deep, want the second esc to go up", len(m.stack))
	}
}

func TestSearchWithNothingFound(t *testing.T) {
	m, _ := politics(t)
	press(m, "/")
	typeText(m, "nothing")
	press(m, "enter")
	wantContains(t, "the empty list", lines(m)[3], `no events match: "nothing" · open`)
}

func TestSearchOfMarketsGoesThroughTheEvents(t *testing.T) {
	m, c := politics(t)
	// An event with nothing in it that is open: a page may add no rows.
	shut := nominee()
	shut.Markets = shut.Markets[2:3]
	c.found = map[int]api.SearchResult{
		1: {Events: []api.Event{nominee()}, More: true, Total: 2},
		2: {Events: []api.Event{shut}, More: true, Total: 2},
		// More is still set on a page with nothing on it.
		3: {More: true, Total: 2},
	}
	press(m, "tab", "/")
	typeText(m, "win")
	press(m, "enter")

	// The closed market is left out here, since the search only knows
	// whether the event is open; the pages go on until the screen is full
	// or they run out.
	wantLabels(t, m, "Will Ann win?", "Will Bob win?", "Will Dee win?")
	wantContains(t, "title bar", lines(m)[0], `by relevance · "win" · open`)
	wantContains(t, "status bar", statusLine(m), "3 loaded · end")
	if len(c.searches) != 3 {
		t.Fatalf("%d searches, want 3 pages", len(c.searches))
	}
	for i, q := range c.searches {
		want := api.SearchQuery{Text: "win", Limit: api.SearchPageSize, Page: i + 1, Status: api.StatusOpen, TagSlug: "politics"}
		if q != want {
			t.Errorf("search %d = %+v, want %+v", i, q, want)
		}
	}

	// It cannot be sorted, and says so instead of pretending.
	markets := len(c.marketQueries)
	press(m, "s")
	wantContains(t, "status bar", statusLine(m), "ranked by relevance")
	press(m, "S")
	if len(c.searches) != 3 || len(c.marketQueries) != markets {
		t.Error("sorting a search of markets made a request")
	}

	// The search holds on the other tab, as a search of titles.
	press(m, "tab")
	if q := lastQuery(t, c); q.TitleSearch != "win" {
		t.Errorf("events query = %+v, want the same search", q)
	}
	wantContains(t, "title bar", lines(m)[0], "vol 24h ↓")
}

func TestFilterForm(t *testing.T) {
	m, c := politics(t)
	asked := len(c.queries)

	press(m, "f")
	screen := screenText(m)
	wantContains(t, "form", screen, "Filter", cursorMarker+"Show", "[open]", "Min volume", "Min liquidity", "Ends after", "Ends before")
	wantContains(t, "status bar", statusLine(m), "enter apply", "esc cancel")

	// Letters are text here, q included; space changes the choice.
	press(m, "right", "tab")
	typeText(m, "10k")
	press(m, "tab")
	typeText(m, "q")
	press(m, "enter")
	if len(m.stack) != 2 {
		t.Fatal("q in a field of the form left the screen")
	}
	wantContains(t, "form", screenText(m), "Min liquidity is not an amount")
	if len(c.queries) != asked {
		t.Fatal("a form that could not be read was applied")
	}

	press(m, "backspace", "down")
	typeText(m, "2026-12-01")
	press(m, "down")
	typeText(m, "2026-11-01")
	press(m, "enter")
	wantContains(t, "form", screenText(m), "Ends before is earlier than Ends after")
	for range len("2026-11-01") {
		press(m, "backspace")
	}
	press(m, "enter")

	q := lastQuery(t, c)
	day := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	if q.Status != api.StatusClosed || q.VolumeMin != 10000 || q.LiquidityMin != 0 ||
		!q.EndDateMin.Equal(day) || !q.EndDateMax.IsZero() || q.TagID != "id-politics" {
		t.Errorf("query = %+v, want closed, at least 10000 of volume, ending from 1 December", q)
	}
	wantContains(t, "title bar", lines(m)[0], "closed · vol ≥ $10.0K · ends ≥ 2026-12-01")
	wantContains(t, "header", lines(m)[2], "Event")

	// The form opens on the filter as it stands; applying it unchanged asks
	// for nothing, and so does leaving it.
	asked = len(c.queries)
	press(m, "f")
	wantContains(t, "form", screenText(m), "[closed]", "10000", "2026-12-01")
	press(m, "enter", "f", "shift+tab")
	typeText(m, "2027-01-01")
	press(m, "esc")
	if len(c.queries) != asked {
		t.Errorf("%d requests from a form applied unchanged and one cancelled", len(c.queries)-asked)
	}
	if strings.Contains(lines(m)[0], "2027") {
		t.Errorf("title bar = %q, want the cancelled change dropped", lines(m)[0])
	}

	// The markets tab takes the same filter.
	press(m, "tab")
	mq := c.marketQueries[len(c.marketQueries)-1]
	if mq.Status != api.StatusClosed || mq.VolumeMin != 10000 || !mq.EndDateMin.Equal(day) {
		t.Errorf("markets query = %+v, want the same filter", mq)
	}
}

func TestPasteIntoTheForm(t *testing.T) {
	m, c := politics(t)
	press(m, "f", "tab", "tab")
	settle(m, func() tea.Msg { return tea.PasteMsg{Content: "$2,500"} })
	press(m, "enter")
	if q := lastQuery(t, c); q.LiquidityMin != 2500 {
		t.Errorf("liquidity floor = %v, want the 2500 pasted", q.LiquidityMin)
	}
}

func TestParseAmount(t *testing.T) {
	for in, want := range map[string]float64{
		"": 0, "  ": 0, "5000": 5000, "10k": 1e4, "1.5M": 1.5e6, "2b": 2e9, "$2,500": 2500, " 7 ": 7,
	} {
		if got, err := parseAmount(in); err != nil || got != want {
			t.Errorf("parseAmount(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"lots", "-5", "k", "1kk", "1e"} {
		if got, err := parseAmount(in); err == nil {
			t.Errorf("parseAmount(%q) = %v, want an error", in, got)
		}
	}
}

func TestSubTagPicker(t *testing.T) {
	m, c := politics(t)
	wantContains(t, "strip", lines(m)[1], "Sub-tags: Trump · Midterms")
	wantContains(t, "status bar", statusLine(m), "t sub-tag")

	// A floor set here goes down with the sub-tag.
	press(m, "f", "tab")
	typeText(m, "500")
	press(m, "enter")

	press(m, "t")
	wantContains(t, "header", lines(m)[1], "Sub-tag", "Open events")
	wantLabels(t, m, "Trump", "Midterms")
	wantContains(t, "first row", lines(m)[2], cursorMarker+"Trump", "328")
	wantContains(t, "status bar", statusLine(m), "sub-tag:", "2 of 2 sub-tags", "enter open", "esc cancel")

	// Esc puts the list back.
	press(m, "esc")
	wantContains(t, "header", lines(m)[2], "Event")

	press(m, "t")
	typeText(m, "MID")
	wantLabels(t, m, "Midterms")
	wantContains(t, "status bar", statusLine(m), "1 of 2 sub-tags")
	press(m, "enter")
	wantContains(t, "title bar", lines(m)[0], "Tags ▸ Politics ▸ Midterms", "vol ≥ $500")
	if q := lastQuery(t, c); q.TagID != "id-midterms" || q.VolumeMin != 500 {
		t.Errorf("query = %+v, want the sub-tag with the floor", q)
	}
	if got := c.relatedSlugs[len(c.relatedSlugs)-1]; got != "midterms" {
		t.Errorf("sub-tags asked for %q, want midterms", got)
	}
	// Midterms has none of its own.
	wantContains(t, "strip", lines(m)[1], "Sub-tags: none")
	press(m, "t")
	wantContains(t, "status bar", statusLine(m), "Midterms has no sub-tags")

	press(m, "esc")
	wantContains(t, "title bar", lines(m)[0], "Tags ▸ Politics ─")
	press(m, "T")
	if len(m.stack) != 1 {
		t.Errorf("stack is %d deep after T, want 1", len(m.stack))
	}
}

func TestSubTagsThatFailToLoad(t *testing.T) {
	c := onePage(sampleEvents()...)
	m := open(t, c)
	c.fail = errors.New("boom")
	press(m, "down", "enter")
	wantContains(t, "strip", lines(m)[1], "Sub-tags: could not be loaded")

	c.fail = nil
	c.related = map[string][]api.Tag{"sports": {tag("Soccer")}}
	press(m, "r")
	wantContains(t, "strip", lines(m)[1], "Sub-tags: Soccer")
}

func TestMarketsOfAnEvent(t *testing.T) {
	m, c := politics(t)
	events, refreshes := len(c.queries), len(c.eventIDs)

	press(m, "enter")
	got := lines(m)
	wantContains(t, "title bar", got[0], "Tags ▸ Politics ▸ Nominee 2028", "vol 24h ↓ · open")
	if strings.Contains(got[0], "[") {
		t.Errorf("title bar = %q, want no tabs on an event", got[0])
	}
	wantContains(t, "header", got[1], "Market", "Price", "24h Δ")
	// The busiest first, by the short names; the closed one is left out,
	// and the one that never traded has no figure to sort by and goes last.
	wantLabels(t, m, "Bob", "Ann", "Dee")
	wantContains(t, "the market with no figures", got[4], "Dee", missing)
	if strings.Contains(got[4], "$0") || strings.Contains(got[4], "0.0¢") {
		t.Errorf("row = %q, want nothing shown as a zero", got[4])
	}
	wantContains(t, "status bar", statusLine(m), "3 of 4 markets", "/ search", "f filter", "s sort")
	if strings.Contains(statusLine(m), "sub-tag") {
		t.Errorf("status bar = %q, want no sub-tags on an event", statusLine(m))
	}

	// Sorting and searching are done on the markets in hand.
	press(m, "S")
	wantLabels(t, m, "Ann", "Bob", "Dee")
	press(m, "/")
	typeText(m, "bob")
	press(m, "enter")
	wantLabels(t, m, "Bob")
	wantContains(t, "status bar", statusLine(m), "1 of 4 markets")
	press(m, "esc")
	wantLabels(t, m, "Ann", "Bob", "Dee")

	// So is filtering: closed ones instead.
	press(m, "f", "right", "enter")
	wantLabels(t, m, "Cy")
	press(m, "f", "right", "enter")
	wantLabels(t, m, "Ann", "Bob", "Cy", "Dee")
	wantContains(t, "status bar", statusLine(m), "4 markets")
	if len(c.queries) != events || len(c.eventIDs) != refreshes {
		t.Errorf("an event's markets made %d listing requests and %d refreshes",
			len(c.queries)-events, len(c.eventIDs)-refreshes)
	}

	// A refresh fetches the event again and keeps the cursor on its market.
	press(m, "down")
	fresh := nominee()
	fresh.Markets = append([]api.Market{market("m5", "Will Eve win?", "Eve", 0.1, 0, 1)}, fresh.Markets...)
	c.event[fresh.ID] = fresh
	press(m, "r")
	if len(c.eventIDs) != refreshes+1 || c.eventIDs[refreshes] != fresh.ID {
		t.Errorf("refreshed %q, want the event", c.eventIDs[refreshes:])
	}
	wantLabels(t, m, "Eve", "Ann", "Bob", "Cy", "Dee")
	wantContains(t, "cursor row", cursorLine(t, m), "Bob")

	// The filter was this level's own: the one above still shows the open.
	press(m, "esc")
	wantContains(t, "title bar", lines(m)[0], "Tags ▸ Politics ─", "vol 24h ↓ · open")
	wantContains(t, "cursor row", cursorLine(t, m), "Nominee 2028")
}

func TestFailedRefreshOfAnEventKeepsTheRows(t *testing.T) {
	m, c := politics(t)
	press(m, "enter")
	c.fail = errors.New("boom")
	press(m, "r")
	wantLabels(t, m, "Bob", "Ann", "Dee")
	wantContains(t, "status bar", statusLine(m), "could not refresh the event: boom", "r retries")
}

func TestFailedListingKeepsTheRows(t *testing.T) {
	c := manyPages(2)
	m := open(t, c)

	c.fail = errors.New("boom")
	press(m, "enter") // All
	// The sample's rows stand in, and stay.
	wantContains(t, "status bar", statusLine(m), "200 of the busiest", "could not load the events: boom", "r retries")
	wantContains(t, "cursor row", cursorLine(t, m), "event 0")

	c.fail = nil
	press(m, "r")
	wantContains(t, "status bar", statusLine(m), "100 loaded · more available")
	if got := statusLine(m); strings.Contains(got, "could not") {
		t.Errorf("status bar = %q, want the failure gone", got)
	}

	// A later page that fails leaves the rows and is asked for again by
	// going on towards the end.
	c.fail = errors.New("boom")
	press(m, "G")
	wantContains(t, "status bar", statusLine(m), "100 loaded · more available", "could not load the events: boom")
	c.fail = nil
	press(m, "G")
	wantContains(t, "status bar", statusLine(m), "200 loaded · end")
}

func TestFailedFirstPageWithNoRows(t *testing.T) {
	m, c := politics(t)
	c.fail = errors.New("boom")
	press(m, "tab")
	wantContains(t, "the empty list", lines(m)[3], "the markets could not be loaded")
	wantContains(t, "status bar", statusLine(m), "could not load the markets: boom")
}

func TestPagesThatAreNotTheScreensOwnAreDropped(t *testing.T) {
	m, _ := politics(t)
	b := m.stack[1].(*browse)
	stray := []api.Event{event("Stray", 1, 1)}

	for what, msg := range map[string]pageMsg{
		"an abandoned load": {owner: b, tab: tabEvents, gen: b.lists[tabEvents].gen - 1, events: stray},
		"another screen":    {owner: newBrowse(b.env, tag("Other"), b.filter, nil), tab: tabEvents, gen: b.lists[tabEvents].gen, events: stray},
	} {
		settle(m, func() tea.Msg { return msg })
		if got := screenText(m); strings.Contains(got, "Stray") {
			t.Errorf("a page of %s was shown:\n%s", what, got)
		}
	}
}

func TestChangingTheSortAbandonsTheLoadUnderWay(t *testing.T) {
	m, c := politics(t)
	requests := len(c.done)

	_, first := m.Update(keyMsg("s"))
	_, second := m.Update(keyMsg("s"))
	settle(m, first)
	select {
	case <-c.done[requests]:
	default:
		t.Error("the overtaken request was not cancelled")
	}
	settle(m, second)
	wantContains(t, "title bar", lines(m)[0], "vol 1m ↓")
	wantLabels(t, m, "Nominee 2028", "Election", "Fed decision")

	// Leaving the screen stops what it had under way.
	_, cmd := m.Update(keyMsg("r"))
	asked := len(c.done)
	press(m, "esc")
	_ = messages(cmd)
	for i, done := range c.done[asked-1:] {
		select {
		case <-done:
		default:
			t.Errorf("request %d was not cancelled on leaving the screen", i)
		}
	}
}

func TestHelp(t *testing.T) {
	m, _ := politics(t)
	press(m, "h")
	screen := screenText(m)
	wantContains(t, "help", screen, "Moving", "half a page down", "Levels", "events / markets",
		"Lists", "find tag", "search", "filter", "sub-tag", "reverse", "export", "Forms", "next field", "quit")
	wantContains(t, "status bar", statusLine(m), "any key back")
	// The frame is the screen's own, and the key that closes the help does
	// nothing else.
	wantContains(t, "title bar", lines(m)[0], "Tags ▸ Politics")
	press(m, "esc")
	if len(m.stack) != 2 {
		t.Fatalf("stack is %d deep after closing the help, want 2", len(m.stack))
	}
	wantContains(t, "header", lines(m)[2], "Event")

	press(m, "?")
	wantContains(t, "help", screenText(m), "Moving")
	press(m, "q")

	// In a prompt, h is a letter.
	press(m, "/", "h")
	wantContains(t, "status bar", statusLine(m), "/h")
}

func TestStartInsideATag(t *testing.T) {
	c := onePage(sampleEvents()...)
	c.related = map[string][]api.Tag{"politics": {tag("Midterms")}}
	politics := tag("Politics")
	filter := api.DefaultFilter()
	filter.Status = api.StatusAll
	filter.VolumeMin = 75
	m := New(c, Options{Tag: &politics, Filter: filter, Now: func() time.Time { return testNow }})
	settle(m, m.Init())
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})

	wantContains(t, "title bar", lines(m)[0], "Tags ▸ Politics", "all · vol ≥ $75")
	wantContains(t, "strip", lines(m)[1], "Sub-tags: Midterms")
	wantLabels(t, m, "Election", "Fed decision")
	found := false
	for _, q := range c.queries {
		if q.TagID == "id-politics" {
			found = true
			if q.Status != api.StatusAll || q.VolumeMin != 75 {
				t.Errorf("query = %+v, want the filter the browser was opened with", q)
			}
		}
	}
	if !found {
		t.Error("the tag's events were never asked for")
	}

	// Esc leads up to the Tags level, which loaded underneath, and a tag
	// opened from there takes the same filter.
	press(m, "esc")
	wantLabels(t, m, "All", "Sports", "Politics", "Elections", "Economy", "Soccer")
	press(m, "down", "enter")
	if q := lastQuery(t, c); q.TagID != "id-sports" || q.VolumeMin != 75 {
		t.Errorf("query = %+v, want Sports with the same floor", q)
	}
	// The sample is of the open events only, so it is no stand-in here.
	if got := strconv.Itoa(len(rowLabels(m))); got != "2" {
		t.Errorf("%s rows, want Sports' two", got)
	}
}

func TestLongNoteGivesWayToTheBreadcrumb(t *testing.T) {
	m, _ := politics(t)
	press(m, "f", "tab")
	typeText(m, "12345")
	press(m, "tab")
	typeText(m, "67890")
	press(m, "tab")
	typeText(m, "2026-12-01")
	press(m, "enter")
	wantContains(t, "title bar", lines(m)[0], "vol 24h ↓ · open · vol ≥ $12.3K · liq ≥ $67.9K · ends ≥ 2026-12-01")

	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	title := lines(m)[0]
	wantContains(t, "title bar", title, "Tags ▸ Politics ─ [Events] Markets ─", "vol 24h ↓ · open · vol", "… ┐")
	if w := width(title); w != 80 {
		t.Errorf("title bar is %d columns, want 80: %q", w, title)
	}
}
