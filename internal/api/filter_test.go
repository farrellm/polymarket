package api

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"
)

func num(v float64) Float { return Float{Value: v, Valid: true} }

func TestSortOrdersAreFoundByName(t *testing.T) {
	for _, o := range SortOrders {
		if got, ok := FindOrder(o.Name); !ok || got != o {
			t.Errorf("FindOrder(%q) = %+v, %v", o.Name, got, ok)
		}
		// Every order must be one a market in hand can be sorted by.
		m := Market{
			Volume: num(1), Volume24h: num(1), Volume1w: num(1), Volume1m: num(1), Liquidity: num(1),
			StartDate: Time{time.Unix(1, 0)}, EndDate: Time{time.Unix(1, 0)},
		}
		if _, ok := o.of(&m); !ok {
			t.Errorf("order %s reads no value from a market", o.Name)
		}
	}
	if _, ok := FindOrder("volumeNum"); ok {
		t.Error("an endpoint's own field name was taken for an order")
	}
}

func TestFilterQueries(t *testing.T) {
	day := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	order, _ := FindOrder("volume")
	f := Filter{Status: StatusClosed, Order: order, Ascending: true, VolumeMin: 10, LiquidityMin: 20, EndDateMin: day, EndDateMax: day.Add(time.Hour)}

	e := f.EventsQuery()
	wantE := EventsQuery{Order: "volume", Ascending: true, Status: StatusClosed, VolumeMin: 10, LiquidityMin: 20, EndDateMin: day, EndDateMax: day.Add(time.Hour)}
	if e != wantE {
		t.Errorf("events query = %+v\nwant           %+v", e, wantE)
	}
	m := f.MarketsQuery()
	wantM := MarketsQuery{Order: "volumeNum", Ascending: true, Status: StatusClosed, VolumeMin: 10, LiquidityMin: 20, EndDateMin: day, EndDateMax: day.Add(time.Hour)}
	if m != wantM {
		t.Errorf("markets query = %+v\nwant            %+v", m, wantM)
	}

	if d := DefaultFilter(); d.Status != StatusOpen || d.Order.Name != "volume24hr" || d.Ascending {
		t.Errorf("default filter = %+v, want open, by 24 hour volume, largest first", d)
	}
}

func TestFilterKeeps(t *testing.T) {
	day := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	open := Market{Volume: num(100), Liquidity: num(50), EndDate: Time{day}}
	closed := open
	closed.Closed = true
	untraded := Market{}

	tests := []struct {
		name   string
		filter Filter
		market Market
		want   bool
	}{
		{"open keeps an open one", Filter{}, open, true},
		{"open drops a closed one", Filter{}, closed, false},
		{"closed keeps a closed one", Filter{Status: StatusClosed}, closed, true},
		{"closed drops an open one", Filter{Status: StatusClosed}, open, false},
		{"all keeps both", Filter{Status: StatusAll}, closed, true},
		{"a floor met", Filter{VolumeMin: 100}, open, true},
		{"a floor missed", Filter{VolumeMin: 101}, open, false},
		{"a liquidity floor missed", Filter{LiquidityMin: 51}, open, false},
		{"no bounds keep a market with no figures", Filter{}, untraded, true},
		{"a floor drops a market with no volume", Filter{VolumeMin: 1}, untraded, false},
		{"ends on the bound", Filter{EndDateMin: day, EndDateMax: day}, open, true},
		{"ends too soon", Filter{EndDateMin: day.Add(time.Second)}, open, false},
		{"ends too late", Filter{EndDateMax: day.Add(-time.Second)}, open, false},
		{"a bound drops a market with no end date", Filter{EndDateMax: day}, untraded, false},
	}
	for _, tt := range tests {
		if got := tt.filter.Keeps(&tt.market); got != tt.want {
			t.Errorf("%s: Keeps = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestFilterCompare(t *testing.T) {
	markets := []Market{
		{ID: "small", Volume24h: num(1)},
		{ID: "none"},
		{ID: "large", Volume24h: num(9)},
		{ID: "zero", Volume24h: num(0)},
	}
	ids := func(f Filter) string {
		sorted := slices.Clone(markets)
		slices.SortStableFunc(sorted, func(a, b Market) int { return f.Compare(&a, &b) })
		out := make([]string, len(sorted))
		for i, m := range sorted {
			out[i] = m.ID
		}
		return strings.Join(out, " ")
	}
	// The one with no figure is last either way round: it is not a zero.
	if got := ids(DefaultFilter()); got != "large small zero none" {
		t.Errorf("largest first: %s", got)
	}
	up := DefaultFilter()
	up.Ascending = true
	if got := ids(up); got != "zero small large none" {
		t.Errorf("smallest first: %s", got)
	}
}

func TestMarketsOf(t *testing.T) {
	events := []Event{
		{ID: "e1", Slug: "one", Tags: []Tag{{ID: "2", Slug: "politics"}}, Markets: []Market{
			{ID: "a"}, {ID: "b", Closed: true}, {ID: "c", Tags: []Tag{{ID: "9", Slug: "own"}}},
		}},
		{ID: "e2", Slug: "two"},
		{ID: "e3", Slug: "three", Markets: []Market{{ID: "d"}}},
	}
	got := Filter{}.MarketsOf(events)
	if len(got) != 3 || got[0].ID != "a" || got[1].ID != "c" || got[2].ID != "d" {
		t.Fatalf("markets = %+v, want a, c and d", got)
	}
	a := got[0]
	if len(a.Events) != 1 || a.Events[0].Slug != "one" || a.Events[0].Markets != nil {
		t.Errorf("market a carries %+v, want its event without the event's markets", a.Events)
	}
	if len(a.Tags) != 1 || a.Tags[0].Slug != "politics" {
		t.Errorf("market a has tags %+v, want its event's", a.Tags)
	}
	if got[1].Tags[0].Slug != "own" {
		t.Errorf("market c has tags %+v, want its own kept", got[1].Tags)
	}
	// The events given are left as they were.
	if len(events[0].Markets) != 3 || events[0].Markets[0].Events != nil {
		t.Errorf("the events were changed: %+v", events[0])
	}
}

// searcher answers each page of a search from a list, and records the queries.
type searcher struct {
	pages   []SearchResult
	queries []SearchQuery
}

func (s *searcher) Search(_ context.Context, q SearchQuery) (*SearchResult, error) {
	s.queries = append(s.queries, q)
	if q.Page > len(s.pages) {
		return &SearchResult{More: true}, nil
	}
	return &s.pages[q.Page-1], nil
}

func TestSearchMarketsPagesByNumber(t *testing.T) {
	s := &searcher{pages: []SearchResult{
		{More: true, Events: []Event{{ID: "e1", Markets: []Market{{ID: "a"}, {ID: "b", Closed: true}}}}},
		// A page whose markets are all filtered out is not the end.
		{More: true, Events: []Event{{ID: "e2", Markets: []Market{{ID: "c", Closed: true}}}}},
		{More: true, Events: []Event{{ID: "e3", Markets: []Market{{ID: "d"}}}}},
	}}
	var ids []string
	pages := 0
	fetch := func(cursor string) ([]Market, string, error) {
		return SearchMarkets(t.Context(), s, Filter{}, "fed", "politics", cursor)
	}
	for page, err := range Pages(t.Context(), fetch) {
		if err != nil {
			t.Fatal(err)
		}
		pages++
		for _, m := range page {
			ids = append(ids, m.ID)
		}
	}
	// The fourth page holds no events, which ends it whatever it claims.
	if pages != 4 || len(s.queries) != 4 {
		t.Errorf("%d pages from %d searches, want 4 of each", pages, len(s.queries))
	}
	if got := slices.Compact(ids); len(got) != 2 || got[0] != "a" || got[1] != "d" {
		t.Errorf("markets = %q, want a and d", ids)
	}
	for i, q := range s.queries {
		want := SearchQuery{Text: "fed", Limit: SearchPageSize, Page: i + 1, Status: StatusOpen, TagSlug: "politics"}
		if q != want {
			t.Errorf("search %d = %+v, want %+v", i, q, want)
		}
	}
}

func TestParseDate(t *testing.T) {
	day := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	for in, want := range map[string]time.Time{
		"":                          {},
		"2026-11-03":                day,
		"2026-11-03T01:30:00+01:00": day.Add(30 * time.Minute),
	} {
		if got, err := ParseDate(in); err != nil || !got.Equal(want) {
			t.Errorf("ParseDate(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"next week", "2026-13-01", "03/11/2026"} {
		if got, err := ParseDate(in); err == nil {
			t.Errorf("ParseDate(%q) = %v, want an error", in, got)
		}
	}
}
