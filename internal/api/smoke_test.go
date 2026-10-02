//go:build live

package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestLive makes one request per endpoint against the real services and
// checks only the shape of the answers, to catch the API drifting away from
// what the client and the fixtures assume. Run it with `make smoke`; it is
// not part of `make check`, and CI never runs it.
func TestLive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	c := New(Options{UserAgent: "polymarket-tui/smoke"})

	// Everything after the listing follows one market that is trading.
	var (
		event  *Event
		market *Market
	)

	t.Run("events", func(t *testing.T) {
		events, next, err := c.Events(ctx, EventsQuery{Limit: 5, Order: "volume24hr"})
		if err != nil {
			t.Fatal(err)
		}
		if len(events) != 5 || next == "" {
			t.Fatalf("%d events, next = %q", len(events), next)
		}
		if !anyTraded(events) {
			t.Error("no market in the listing has both a volume and prices")
		}
		for i := range events {
			checkEvent(t, &events[i])
			if i > 0 && events[i].Volume24h.Value > events[i-1].Volume24h.Value {
				t.Errorf("event %d has more 24 h volume than the one before", i)
			}
		}
		for i := range events {
			for j := range events[i].Markets {
				if m := &events[i].Markets[j]; m.AcceptingOrders && traded(m) && event == nil {
					event, market = &events[i], m
				}
			}
		}

		second, _, err := c.Events(ctx, EventsQuery{Limit: 5, Order: "volume24hr", Cursor: next})
		if err != nil {
			t.Fatal(err)
		}
		if len(second) == 0 || second[0].ID == events[0].ID {
			t.Errorf("the second page repeats the first, or is empty")
		}
	})
	if market == nil {
		t.Fatal("no market accepting orders among the top events")
	}
	tokenID := market.Outcomes[0].TokenID

	t.Run("events title search", func(t *testing.T) {
		events, _, err := c.Events(ctx, EventsQuery{Limit: 5, TagSlug: "politics", TitleSearch: "senate"})
		if err != nil {
			t.Fatal(err)
		}
		if len(events) == 0 {
			t.Error("no politics events with senate in the title")
		}
	})

	t.Run("markets", func(t *testing.T) {
		markets, next, err := c.Markets(ctx, MarketsQuery{Limit: 5, Order: "volume24hr", IncludeTags: true})
		if err != nil {
			t.Fatal(err)
		}
		if len(markets) != 5 || next == "" {
			t.Fatalf("%d markets, next = %q", len(markets), next)
		}
		if !traded(&markets[0]) {
			t.Errorf("the market with the most 24 h volume: volume %+v, outcomes %+v", markets[0].Volume, markets[0].Outcomes)
		}
		for i := range markets {
			checkMarket(t, &markets[i])
			if len(markets[i].Events) == 0 {
				t.Errorf("market %s names no event", markets[i].ID)
			}
		}
	})

	t.Run("keyset offset is refused", func(t *testing.T) {
		err := c.get(ctx, c.gamma, "/events/keyset", map[string][]string{"offset": {"5"}}, nil)
		var apiErr *Error
		if !errors.As(err, &apiErr) || apiErr.Status != http.StatusUnprocessableEntity {
			t.Errorf("error = %v, want a 422", err)
		}
	})

	t.Run("event", func(t *testing.T) {
		e, err := c.Event(ctx, event.ID)
		if err != nil {
			t.Fatal(err)
		}
		if e.ID != event.ID {
			t.Errorf("id = %q, want %q", e.ID, event.ID)
		}
		checkEvent(t, e)
	})

	t.Run("market", func(t *testing.T) {
		m, err := c.Market(ctx, market.ID)
		if err != nil {
			t.Fatal(err)
		}
		if m.ID != market.ID || m.ConditionID != market.ConditionID {
			t.Errorf("market = %s %s, want %s %s", m.ID, m.ConditionID, market.ID, market.ConditionID)
		}
		checkMarket(t, m)
		if !traded(m) {
			t.Errorf("volume %+v, outcomes %+v", m.Volume, m.Outcomes)
		}
	})

	// What `export --market` rests on: a slug finds the market, with its
	// event, only in the case it is written in, and a number finds it by ID.
	t.Run("market by slug", func(t *testing.T) {
		m, err := c.FindMarket(ctx, market.Slug)
		if err != nil {
			t.Fatal(err)
		}
		if m.ID != market.ID || len(m.Events) != 1 || m.Events[0].Slug != event.Slug {
			t.Errorf("market = %s in %+v, want %s in %s", m.ID, m.Events, market.ID, event.Slug)
		}
		if m, err := c.FindMarket(ctx, market.ID); err != nil || m.Slug != market.Slug {
			t.Errorf("by ID: %v, %v", m, err)
		}
		if _, err := c.MarketBySlug(ctx, strings.ToUpper(market.Slug)); !IsNotFound(err) {
			t.Errorf("slug in capitals: error = %v, want not found", err)
		}
		if _, err := c.FindMarket(ctx, "999999999"); !IsNotFound(err) {
			t.Errorf("unknown ID: error = %v, want not found", err)
		}
		// An ID too long to be one is refused rather than not found.
		var apiErr *Error
		if _, err := c.FindMarket(ctx, "999999999999"); !errors.As(err, &apiErr) || apiErr.Status != http.StatusUnprocessableEntity {
			t.Errorf("an ID of twelve digits: error = %v, want a 422", err)
		}
	})

	t.Run("tag", func(t *testing.T) {
		tag, err := c.Tag(ctx, "politics")
		if err != nil {
			t.Fatal(err)
		}
		if tag.ID == "" || tag.Slug != "politics" || tag.Label == "" {
			t.Errorf("tag = %+v", tag)
		}
		if _, err := c.Tag(ctx, "no-such-tag-xyzzy"); !IsNotFound(err) {
			t.Errorf("unknown slug: error = %v, want not found", err)
		}
	})

	t.Run("related tags", func(t *testing.T) {
		tags, err := c.RelatedTags(ctx, "politics")
		if err != nil {
			t.Fatal(err)
		}
		if len(tags) == 0 {
			t.Fatal("politics has no related tags")
		}
		for _, tag := range tags {
			if tag.ID == "" || tag.Slug == "" || tag.Label == "" || tag.ActiveEvents <= 0 {
				t.Errorf("tag = %+v", tag)
			}
		}
	})

	t.Run("search", func(t *testing.T) {
		res, err := c.Search(ctx, SearchQuery{Text: "election", Limit: 3, TagSlug: "politics"})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Events) == 0 || res.Total < len(res.Events) {
			t.Fatalf("%d events of %d", len(res.Events), res.Total)
		}
		for i := range res.Events {
			checkEvent(t, &res.Events[i])
		}
	})

	// What the browser's search of markets rests on: a page is cut to
	// SearchPageSize, and the events it finds carry their markets.
	t.Run("search of markets", func(t *testing.T) {
		res, err := c.Search(ctx, SearchQuery{Text: "election", Limit: 2 * SearchPageSize})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Events) != SearchPageSize || !res.More {
			t.Errorf("%d events, more %v; want a full page of %d and more to come", len(res.Events), res.More, SearchPageSize)
		}
		markets, next, err := SearchMarkets(ctx, c, DefaultFilter(), "election", "politics", "")
		if err != nil {
			t.Fatal(err)
		}
		if len(markets) == 0 || next != "2" {
			t.Fatalf("%d markets, next page %q; want some, and page 2", len(markets), next)
		}
		for i := range markets {
			m := &markets[i]
			if m.Closed || len(m.Events) != 1 || m.Events[0].Slug == "" || m.Question == "" {
				t.Errorf("market %s: closed %v, events %+v", m.ID, m.Closed, m.Events)
			}
		}
	})

	t.Run("book", func(t *testing.T) {
		b, err := c.Book(ctx, tokenID)
		if err != nil {
			t.Fatal(err)
		}
		if b.TokenID != tokenID || b.ConditionID != market.ConditionID {
			t.Errorf("book is for %s in %s", b.TokenID, b.ConditionID)
		}
		if time.Since(b.Time.Time).Abs() > 24*time.Hour {
			t.Errorf("time = %v, want about now", b.Time)
		}
		if !b.TickSize.Valid || len(b.Bids)+len(b.Asks) == 0 {
			t.Errorf("tick size %+v, %d bids, %d asks", b.TickSize, len(b.Bids), len(b.Asks))
		}
	})

	t.Run("price history", func(t *testing.T) {
		points, next, err := c.PriceHistory(ctx, HistoryQuery{
			TokenID: tokenID, Interval: "1w", BucketSeconds: 3600, Limit: 10,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(points) != 10 || next == "" {
			t.Fatalf("%d points, next = %q", len(points), next)
		}
		for _, p := range points {
			if !p.Price.Valid || p.Time.IsZero() || p.ResolutionSeconds != 3600 {
				t.Errorf("point = %+v", p)
			}
		}

		// The 15-day cap on an explicit range.
		_, _, err = c.PriceHistory(ctx, HistoryQuery{
			TokenID: tokenID, Start: time.Now().AddDate(0, 0, -60), End: time.Now(),
		})
		var apiErr *Error
		if !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadRequest {
			t.Errorf("a 60-day range: error = %v, want a 400", err)
		}
	})

	// What the chart rests on: every interval is one page when no limit is
	// asked for, oldest first, and a token with no book is not found.
	t.Run("price history by interval", func(t *testing.T) {
		for _, interval := range HistoryIntervals {
			points, next, err := c.PriceHistory(ctx, HistoryQuery{TokenID: tokenID, Interval: interval})
			if err != nil {
				t.Fatal(err)
			}
			if len(points) == 0 || next != "" {
				t.Errorf("%s: %d points, next = %q; want some, in one page", interval, len(points), next)
				continue
			}
			if first, last := points[0], points[len(points)-1]; !last.Time.After(first.Time.Time) {
				t.Errorf("%s: runs from %v to %v", interval, first.Time, last.Time)
			}
		}
		if _, err := c.Book(ctx, "123"); !IsNotFound(err) {
			t.Errorf("book of an unknown token: error = %v, want not found", err)
		}
	})

	t.Run("trades", func(t *testing.T) {
		trades, _, err := c.Trades(ctx, TradesQuery{ConditionID: market.ConditionID, Limit: 5})
		if err != nil {
			t.Fatal(err)
		}
		if len(trades) == 0 {
			t.Fatal("no trades in a market taken from the top of the volume ranking")
		}
		for _, tr := range trades {
			if tr.ConditionID != market.ConditionID || tr.Time.IsZero() || !tr.Price.Valid || !tr.Size.Valid {
				t.Errorf("trade = %+v", tr)
			}
		}
	})
}
