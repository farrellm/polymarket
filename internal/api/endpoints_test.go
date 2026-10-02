package api

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The fixtures are re-recorded from whatever is trading that day, so these
// tests check the shape of what was decoded rather than particular values.

// checkEvent checks what every event from an event endpoint has.
func checkEvent(t *testing.T, e *Event) {
	t.Helper()
	if e.ID == "" || e.Slug == "" || e.Title == "" {
		t.Errorf("event %q: missing id, slug or title: %q %q", e.ID, e.Slug, e.Title)
	}
	// An event's volume is a JSON number, where a market's is a string.
	if !e.Volume.Valid {
		t.Errorf("event %s: no volume", e.ID)
	}
	if len(e.Markets) == 0 {
		t.Errorf("event %s: no markets", e.ID)
	}
	if len(e.Tags) == 0 {
		t.Errorf("event %s: no tags", e.ID)
	}
	for _, tag := range e.Tags {
		if tag.ID == "" || tag.Slug == "" || tag.Label == "" {
			t.Errorf("event %s: incomplete tag %+v", e.ID, tag)
		}
	}
	for i := range e.Markets {
		checkMarket(t, &e.Markets[i])
	}
}

// checkMarket checks what every market has, wherever it was found.
func checkMarket(t *testing.T, m *Market) {
	t.Helper()
	if m.ID == "" || m.Slug == "" || m.Question == "" {
		t.Errorf("market %q: missing id, slug or question", m.ID)
	}
	if !strings.HasPrefix(m.ConditionID, "0x") {
		t.Errorf("market %s: condition ID %q", m.ID, m.ConditionID)
	}
	if len(m.Outcomes) < 2 {
		t.Fatalf("market %s: outcomes = %+v, want at least two", m.ID, m.Outcomes)
	}
	for _, o := range m.Outcomes {
		if o.Label == "" {
			t.Errorf("market %s: an outcome has no label", m.ID)
		}
		if o.Price.Value < 0 || o.Price.Value > 1 {
			t.Errorf("market %s: outcome %s has price %+v", m.ID, o.Label, o.Price)
		}
		// A token ID has to survive as text: it is far beyond a float64.
		if len(o.TokenID) < 70 || strings.Trim(o.TokenID, "0123456789") != "" {
			t.Errorf("market %s: outcome %s has token ID %q", m.ID, o.Label, o.TokenID)
		}
	}
}

// traded reports whether a market has a volume and a price for each outcome.
// Not every market does: one that has never traded is listed with no volume
// member at all, and one that has not opened yet with no prices. So the
// checks above require neither, and each listing is checked for holding at
// least one market that has both, which is what proves they were decoded.
func traded(m *Market) bool {
	if !m.Volume.Valid {
		return false
	}
	for _, o := range m.Outcomes {
		if !o.Price.Valid {
			return false
		}
	}
	return true
}

func anyTraded(events []Event) bool {
	for i := range events {
		for j := range events[i].Markets {
			if traded(&events[i].Markets[j]) {
				return true
			}
		}
	}
	return false
}

func TestEvents(t *testing.T) {
	c := newTestClient(t, serve(t, http.StatusOK, fixture(t, "events.json"), func(r *http.Request) {
		if r.URL.Path != "/gamma/events/keyset" {
			t.Errorf("path = %s", r.URL.Path)
		}
		wantQuery(t, r, map[string]string{
			"limit":         "100",
			"order":         "volume24hr",
			"ascending":     "false",
			"closed":        "false",
			"tag_slug":      "politics",
			"title_search":  "senate",
			"after_cursor":  "abc",
			"volume_min":    "10000",
			"liquidity_min": "0.5",
			"end_date_min":  "2026-10-10T00:00:00Z",
			"end_date_max":  "2026-10-20T00:00:00Z",
		}, "offset", "tag_id")
	}))

	events, next, err := c.Events(context.Background(), EventsQuery{
		Limit:        100,
		Cursor:       "abc",
		Order:        "volume24hr",
		TagSlug:      "politics",
		TitleSearch:  "senate",
		VolumeMin:    10000,
		LiquidityMin: 0.5,
		EndDateMin:   time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC),
		// A local time is sent as UTC.
		EndDateMax: time.Date(2026, 10, 20, 2, 0, 0, 0, time.FixedZone("", 2*60*60)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 {
		t.Fatal("no events")
	}
	if next == "" {
		t.Error("no next cursor, though the recorded page is not the last")
	}
	if !anyTraded(events) {
		t.Error("no market in the listing has both a volume and prices")
	}
	for i := range events {
		checkEvent(t, &events[i])
		if events[i].Closed {
			t.Errorf("event %s is closed in a listing of open events", events[i].ID)
		}
	}
}

func TestQueryDefaults(t *testing.T) {
	tests := []struct {
		name   string
		call   func(c *Client) error
		want   map[string]string
		absent []string
	}{
		{
			name: "events: the zero query asks for open events and nothing else",
			call: func(c *Client) error {
				_, _, err := c.Events(context.Background(), EventsQuery{})
				return err
			},
			want: map[string]string{"closed": "false"},
			absent: []string{
				"limit", "order", "ascending", "after_cursor", "tag_id", "tag_slug",
				"title_search", "volume_min", "liquidity_min", "end_date_min", "end_date_max",
			},
		},
		{
			name: "events: closed, ascending",
			call: func(c *Client) error {
				_, _, err := c.Events(context.Background(), EventsQuery{
					Status: StatusClosed, Order: "endDate", Ascending: true, TagID: "2",
				})
				return err
			},
			want: map[string]string{"closed": "true", "order": "endDate", "ascending": "true", "tag_id": "2"},
		},
		{
			name: "events: all leaves closed out",
			call: func(c *Client) error {
				_, _, err := c.Events(context.Background(), EventsQuery{Status: StatusAll})
				return err
			},
			absent: []string{"closed"},
		},
		{
			name: "markets: the zero query",
			call: func(c *Client) error {
				_, _, err := c.Markets(context.Background(), MarketsQuery{})
				return err
			},
			want:   map[string]string{"closed": "false"},
			absent: []string{"limit", "order", "include_tag", "volume_num_min", "liquidity_num_min"},
		},
		{
			name: "search: closed is called resolved",
			call: func(c *Client) error {
				_, err := c.Search(context.Background(), SearchQuery{Text: "fed", Status: StatusClosed})
				return err
			},
			want:   map[string]string{"q": "fed", "events_status": "resolved"},
			absent: []string{"page", "limit_per_type", "events_tag"},
		},
		{
			name: "search: all leaves the status out",
			call: func(c *Client) error {
				_, err := c.Search(context.Background(), SearchQuery{Text: "fed", Status: StatusAll})
				return err
			},
			absent: []string{"events_status"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestClient(t, serve(t, http.StatusOK, []byte(`{}`), func(r *http.Request) {
				wantQuery(t, r, tt.want, tt.absent...)
			}))
			if err := tt.call(c.Client); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMarkets(t *testing.T) {
	c := newTestClient(t, serve(t, http.StatusOK, fixture(t, "markets.json"), func(r *http.Request) {
		if r.URL.Path != "/gamma/markets/keyset" {
			t.Errorf("path = %s", r.URL.Path)
		}
		wantQuery(t, r, map[string]string{
			"limit":             "50",
			"order":             "volume24hr",
			"ascending":         "false",
			"closed":            "false",
			"tag_id":            "2",
			"include_tag":       "true",
			"volume_num_min":    "1000",
			"liquidity_num_min": "250",
			"end_date_max":      "2026-12-31T00:00:00Z",
		}, "offset", "end_date_min", "after_cursor")
	}))

	markets, next, err := c.Markets(context.Background(), MarketsQuery{
		Limit:        50,
		Order:        "volume24hr",
		TagID:        "2",
		IncludeTags:  true,
		VolumeMin:    1000,
		LiquidityMin: 250,
		EndDateMax:   time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(markets) == 0 {
		t.Fatal("no markets")
	}
	if next == "" {
		t.Error("no next cursor, though the recorded page is not the last")
	}
	if !slices.ContainsFunc(markets, func(m Market) bool { return traded(&m) }) {
		t.Error("no market in the listing has both a volume and prices")
	}
	for i := range markets {
		m := &markets[i]
		checkMarket(t, m)
		// A listed market names its event, which is how an export gets
		// event_id and event_title without another request.
		if len(m.Events) == 0 || m.Events[0].ID == "" || m.Events[0].Title == "" {
			t.Errorf("market %s: events = %+v", m.ID, m.Events)
		}
		if len(m.Tags) == 0 {
			t.Errorf("market %s: no tags, though they were asked for", m.ID)
		}
	}
}

func TestEvent(t *testing.T) {
	c := newTestClient(t, serve(t, http.StatusOK, fixture(t, "event.json"), func(r *http.Request) {
		if r.URL.Path != "/gamma/events/606422" {
			t.Errorf("path = %s", r.URL.Path)
		}
	}))
	e, err := c.Event(context.Background(), "606422")
	if err != nil {
		t.Fatal(err)
	}
	checkEvent(t, e)
	if e.EndDate.IsZero() {
		t.Error("no end date")
	}
}

func TestMarket(t *testing.T) {
	c := newTestClient(t, serve(t, http.StatusOK, fixture(t, "market.json"), func(r *http.Request) {
		if r.URL.Path != "/gamma/markets/2589810" {
			t.Errorf("path = %s", r.URL.Path)
		}
	}))
	m, err := c.Market(context.Background(), "2589810")
	if err != nil {
		t.Fatal(err)
	}
	checkMarket(t, m)
	// The recorder follows a market that is trading.
	if !traded(m) {
		t.Errorf("volume %+v, outcomes %+v", m.Volume, m.Outcomes)
	}
	if m.EndDate.IsZero() || !m.Liquidity.Valid || !m.TickSize.Valid {
		t.Errorf("end date %v, liquidity %+v, tick size %+v", m.EndDate, m.Liquidity, m.TickSize)
	}
}

func TestTag(t *testing.T) {
	c := newTestClient(t, serve(t, http.StatusOK, fixture(t, "tag.json"), func(r *http.Request) {
		// A slug typed by hand may hold anything; it must stay one segment.
		if r.URL.EscapedPath() != "/gamma/tags/slug/a%2Fb" {
			t.Errorf("path = %s", r.URL.EscapedPath())
		}
	}))
	tag, err := c.Tag(context.Background(), "a/b")
	if err != nil {
		t.Fatal(err)
	}
	if tag.ID == "" || tag.Label != "Politics" || tag.Slug != "politics" {
		t.Errorf("tag = %+v", tag)
	}
}

func TestRelatedTags(t *testing.T) {
	c := newTestClient(t, serve(t, http.StatusOK, fixture(t, "related_tags.json"), func(r *http.Request) {
		if r.URL.Path != "/gamma/tags/slug/politics/related-tags/tags" {
			t.Errorf("path = %s", r.URL.Path)
		}
		wantQuery(t, r, map[string]string{"status": "active", "omit_empty": "true"})
	}))
	tags, err := c.RelatedTags(context.Background(), "politics")
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) == 0 {
		t.Fatal("no related tags")
	}
	for _, tag := range tags {
		if tag.ID == "" || tag.Label == "" || tag.Slug == "" {
			t.Errorf("incomplete tag %+v", tag)
		}
		if tag.ActiveEvents <= 0 {
			t.Errorf("tag %s: %d active events, though empty tags were left out", tag.Slug, tag.ActiveEvents)
		}
	}
}

func TestSearch(t *testing.T) {
	c := newTestClient(t, serve(t, http.StatusOK, fixture(t, "search.json"), func(r *http.Request) {
		if r.URL.Path != "/gamma/public-search" {
			t.Errorf("path = %s", r.URL.Path)
		}
		wantQuery(t, r, map[string]string{
			"q":              "election night",
			"limit_per_type": "20",
			"page":           "2",
			"events_status":  "active",
			"events_tag":     "politics",
		})
	}))
	res, err := c.Search(context.Background(), SearchQuery{
		Text: "election night", Limit: 20, Page: 2, TagSlug: "politics",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Events) == 0 {
		t.Fatal("no events")
	}
	if !anyTraded(res.Events) {
		t.Error("no market in the results has both a volume and prices")
	}
	if res.Total < len(res.Events) {
		t.Errorf("total = %d with %d events on the page", res.Total, len(res.Events))
	}
	if !res.More {
		t.Error("More is false, though the recorded page is not the last")
	}
	for i := range res.Events {
		checkEvent(t, &res.Events[i])
	}
}

// A search that matches nothing answers with no events member at all.
func TestSearchWithNoResults(t *testing.T) {
	body := []byte(`{"pagination":{"hasMore":false,"totalResults":0}}`)
	c := newTestClient(t, serve(t, http.StatusOK, body, nil))
	res, err := c.Search(context.Background(), SearchQuery{Text: "zzzqqq"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Events) != 0 || res.More || res.Total != 0 {
		t.Errorf("result = %+v", res)
	}
}

func TestBook(t *testing.T) {
	c := newTestClient(t, serve(t, http.StatusOK, fixture(t, "book.json"), func(r *http.Request) {
		if r.URL.Path != "/clob/book" {
			t.Errorf("path = %s", r.URL.Path)
		}
		wantQuery(t, r, map[string]string{"token_id": token})
	}))
	b, err := c.Book(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	if b.TokenID == "" || !strings.HasPrefix(b.ConditionID, "0x") {
		t.Errorf("token ID %q, condition ID %q", b.TokenID, b.ConditionID)
	}
	// The snapshot's time is milliseconds in a string.
	if b.Time.Year() < 2026 {
		t.Errorf("time = %v", b.Time)
	}
	if !b.TickSize.Valid || !b.MinOrderSize.Valid {
		t.Errorf("tick size %+v, minimum order size %+v", b.TickSize, b.MinOrderSize)
	}
	if len(b.Bids) == 0 || len(b.Asks) == 0 {
		t.Fatalf("%d bids and %d asks", len(b.Bids), len(b.Asks))
	}
	for _, level := range slices.Concat(b.Bids, b.Asks) {
		if !level.Price.Valid || !level.Size.Valid || level.Size.Value <= 0 {
			t.Errorf("level %+v", level)
		}
	}
	if !slices.IsSortedFunc(b.Bids, func(x, y Level) int { return cmp.Compare(y.Price.Value, x.Price.Value) }) {
		t.Error("bids are not highest first")
	}
	if !slices.IsSortedFunc(b.Asks, func(x, y Level) int { return cmp.Compare(x.Price.Value, y.Price.Value) }) {
		t.Error("asks are not lowest first")
	}
	if b.Bids[0].Price.Value >= b.Asks[0].Price.Value {
		t.Errorf("best bid %v is not below best ask %v", b.Bids[0].Price.Value, b.Asks[0].Price.Value)
	}
}

func TestPriceHistory(t *testing.T) {
	c := newTestClient(t, serve(t, http.StatusOK, fixture(t, "prices_history.json"), func(r *http.Request) {
		if r.URL.Path != "/data/v2/prices-history" {
			t.Errorf("path = %s", r.URL.Path)
		}
		wantQuery(t, r, map[string]string{
			"token_id":       token,
			"interval":       "1d",
			"bucket_seconds": "3600",
			"limit":          "12",
			"cursor":         "abc",
		}, "start", "end")
	}))
	points, next, err := c.PriceHistory(context.Background(), HistoryQuery{
		TokenID: token, Interval: "1d", BucketSeconds: 3600, Limit: 12, Cursor: "abc",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(points) == 0 {
		t.Fatal("no points")
	}
	if next == "" {
		t.Error("no next cursor, though the recorded page is not the last")
	}
	for i, p := range points {
		if !p.Price.Valid || p.Price.Value < 0 || p.Price.Value > 1 {
			t.Errorf("point %d: price %+v", i, p.Price)
		}
		if p.ResolutionSeconds != 3600 {
			t.Errorf("point %d: resolution %d", i, p.ResolutionSeconds)
		}
		if i > 0 && !p.Time.After(points[i-1].Time.Time) {
			t.Errorf("point %d at %v is not after the one before", i, p.Time)
		}
	}
}

// A start to end range is sent in Unix seconds, and one longer than 15 days
// is refused by the service, in words worth passing on.
func TestPriceHistoryRange(t *testing.T) {
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	body := fixture(t, "error_history_range.json")
	c := newTestClient(t, serve(t, http.StatusBadRequest, body, func(r *http.Request) {
		wantQuery(t, r, map[string]string{
			"start": strconv.FormatInt(start.Unix(), 10),
			"end":   strconv.FormatInt(end.Unix(), 10),
		}, "interval")
	}))

	_, _, err := c.PriceHistory(context.Background(), HistoryQuery{TokenID: token, Start: start, End: end})
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadRequest {
		t.Fatalf("error = %v, want a 400", err)
	}
	if !strings.Contains(apiErr.Message(), "at most 15 days") {
		t.Errorf("message = %q", apiErr.Message())
	}
}

func TestTrades(t *testing.T) {
	const condition = "0xa69ef420d8b4075ad8ba8611d02c63cb2ece46f5c8c643af223c272689b0c96f"
	since := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	c := newTestClient(t, serve(t, http.StatusOK, fixture(t, "trades.json"), func(r *http.Request) {
		if r.URL.Path != "/data/v2/trades" {
			t.Errorf("path = %s", r.URL.Path)
		}
		wantQuery(t, r, map[string]string{
			"condition": condition,
			"limit":     "5",
			"side":      "BUY",
			"start":     strconv.FormatInt(since.Unix(), 10),
		}, "end", "cursor")
	}))
	trades, next, err := c.Trades(context.Background(), TradesQuery{
		ConditionID: condition, Limit: 5, Side: "BUY", Start: since,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(trades) == 0 {
		t.Fatal("no trades")
	}
	if next == "" {
		t.Error("no next cursor, though the recorded page is not the last")
	}
	for i, tr := range trades {
		if tr.Side != "BUY" && tr.Side != "SELL" {
			t.Errorf("trade %d: side %q", i, tr.Side)
		}
		if !tr.Price.Valid || !tr.Size.Valid || tr.Size.Value <= 0 {
			t.Errorf("trade %d: price %+v, size %+v", i, tr.Price, tr.Size)
		}
		if tr.Outcome == "" || tr.TokenID == "" || tr.ConditionID == "" || tr.TransactionHash == "" {
			t.Errorf("trade %d: incomplete: %+v", i, tr)
		}
		if tr.Time.Year() < 2026 {
			t.Errorf("trade %d: time %v", i, tr.Time)
		}
		if i > 0 && tr.Time.After(trades[i-1].Time.Time) {
			t.Errorf("trade %d at %v is newer than the one before", i, tr.Time)
		}
	}
}

// The last page of a Data API listing can still carry a cursor; has_more is
// what says whether to use it.
func TestDataPagingEndsOnHasMore(t *testing.T) {
	body := []byte(`{"data":[{"timestamp":1,"price":0.5}],"pagination":{"has_more":false,"next_cursor":"stale"}}`)
	c := newTestClient(t, serve(t, http.StatusOK, body, nil))
	points, next, err := c.PriceHistory(context.Background(), HistoryQuery{TokenID: token, Interval: "1d"})
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 1 || next != "" {
		t.Errorf("%d points, next = %q, want one point and no cursor", len(points), next)
	}
}

// Paging a keyset listing to its end: each page is asked for with the cursor
// of the one before, never with an offset, and the page without a cursor is
// the last.
func TestEventsPagedToExhaustion(t *testing.T) {
	pages := map[string]string{
		"":   `{"events":[{"id":"1"},{"id":"2"}],"next_cursor":"c1"}`,
		"c1": `{"events":[{"id":"3"},{"id":"4"}],"next_cursor":"c2"}`,
		"c2": `{"events":[{"id":"5"}]}`,
	}
	var (
		mu      sync.Mutex
		cursors []string
	)
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantQuery(t, r, map[string]string{"limit": "2", "tag_slug": "politics"}, "offset")
		cursor := r.URL.Query().Get("after_cursor")
		mu.Lock()
		cursors = append(cursors, cursor)
		mu.Unlock()
		w.Write([]byte(pages[cursor]))
	}))

	ctx := context.Background()
	var ids []string
	for page, err := range Pages(ctx, func(cursor string) ([]Event, string, error) {
		return c.Events(ctx, EventsQuery{Limit: 2, TagSlug: "politics", Cursor: cursor})
	}) {
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range page {
			ids = append(ids, e.ID)
		}
	}
	if !slices.Equal(ids, []string{"1", "2", "3", "4", "5"}) {
		t.Errorf("ids = %q", ids)
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(cursors, []string{"", "c1", "c2"}) {
		t.Errorf("cursors = %q", cursors)
	}
}
