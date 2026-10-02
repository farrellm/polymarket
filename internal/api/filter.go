package api

import (
	"cmp"
	"context"
	"errors"
	"strconv"
	"time"
)

// SortOrder is a field a listing can be sorted by. The two listings do not
// always call it the same thing.
type SortOrder struct {
	Name, Desc string
	// Events and Markets are the field's names on the two endpoints.
	Events, Markets string
}

// SortOrders are the fields the listings sort by, all checked against the
// live service. On /markets/keyset, order=volume compares the volume as text,
// so the numeric fields are asked for instead.
var SortOrders = []SortOrder{
	{"volume24hr", "volume over the last 24 hours", "volume24hr", "volume24hr"},
	{"volume1wk", "volume over the last week", "volume1wk", "volume1wk"},
	{"volume1mo", "volume over the last month", "volume1mo", "volume1mo"},
	{"volume", "volume since the start", "volume", "volumeNum"},
	{"liquidity", "liquidity", "liquidity", "liquidityNum"},
	{"endDate", "end date", "endDate", "endDate"},
	{"startDate", "start date", "startDate", "startDate"},
}

// FindOrder looks a sort order up by its name.
func FindOrder(name string) (SortOrder, bool) {
	for _, o := range SortOrders {
		if o.Name == name {
			return o, true
		}
	}
	return SortOrder{}, false
}

// of is the value a market is sorted by, and whether it has one.
func (o SortOrder) of(m *Market) (float64, bool) {
	amount := func(f Float) (float64, bool) { return f.Value, f.Valid }
	moment := func(t Time) (float64, bool) { return float64(t.Unix()), !t.IsZero() }
	switch o.Name {
	case "volume24hr":
		return amount(m.Volume24h)
	case "volume1wk":
		return amount(m.Volume1w)
	case "volume1mo":
		return amount(m.Volume1m)
	case "volume":
		return amount(m.Volume)
	case "liquidity":
		return amount(m.Liquidity)
	case "endDate":
		return moment(m.EndDate)
	case "startDate":
		return moment(m.StartDate)
	}
	return 0, false
}

// Filter is what a listing is narrowed and sorted by, in terms the events and
// the markets listings share. It is the browser's filter state and what the
// export flags add up to.
type Filter struct {
	Status    Status
	Order     SortOrder
	Ascending bool

	// Zero means no bound.
	VolumeMin    float64
	LiquidityMin float64
	EndDateMin   time.Time
	EndDateMax   time.Time
}

// DefaultFilter is the open ones, the busiest over the last 24 hours first.
// The service's own default order is by ID, oldest first.
func DefaultFilter() Filter {
	return Filter{Order: SortOrders[0]}
}

// EventsQuery is the first page of the events the filter selects.
func (f Filter) EventsQuery() EventsQuery {
	return EventsQuery{
		Order:        f.Order.Events,
		Ascending:    f.Ascending,
		Status:       f.Status,
		VolumeMin:    f.VolumeMin,
		LiquidityMin: f.LiquidityMin,
		EndDateMin:   f.EndDateMin,
		EndDateMax:   f.EndDateMax,
	}
}

// MarketsQuery is the first page of the markets the filter selects.
func (f Filter) MarketsQuery() MarketsQuery {
	return MarketsQuery{
		Order:        f.Order.Markets,
		Ascending:    f.Ascending,
		Status:       f.Status,
		VolumeMin:    f.VolumeMin,
		LiquidityMin: f.LiquidityMin,
		EndDateMin:   f.EndDateMin,
		EndDateMax:   f.EndDateMax,
	}
}

// Keeps reports whether a market passes the filter. It is for markets that
// did not come from the markets listing, which applies the filter itself:
// those embedded in an event, and those a search turned up.
//
// A market with no figure for a bounded field does not pass: it has never
// traded, or has no end date to compare.
func (f Filter) Keeps(m *Market) bool {
	switch {
	case f.Status == StatusOpen && m.Closed,
		f.Status == StatusClosed && !m.Closed,
		f.VolumeMin > 0 && (!m.Volume.Valid || m.Volume.Value < f.VolumeMin),
		f.LiquidityMin > 0 && (!m.Liquidity.Valid || m.Liquidity.Value < f.LiquidityMin),
		!f.EndDateMin.IsZero() && (m.EndDate.IsZero() || m.EndDate.Before(f.EndDateMin)),
		!f.EndDateMax.IsZero() && (m.EndDate.IsZero() || m.EndDate.After(f.EndDateMax)):
		return false
	}
	return true
}

// Compare orders two markets as the filter sorts them. A market with no
// value for the field goes last whichever way round the sort is.
func (f Filter) Compare(a, b *Market) int {
	x, xok := f.Order.of(a)
	y, yok := f.Order.of(b)
	switch {
	case xok && !yok:
		return -1
	case !xok && yok:
		return 1
	}
	c := cmp.Compare(x, y)
	if !f.Ascending {
		c = -c
	}
	return c
}

// MarketsOf flattens events into those of their markets the filter keeps, in
// the order given. It is what a search of markets goes through: the search
// finds events, and applies none of the filter but the status of the event.
//
// Each market is given its event and its event's tags, as the markets
// listing would have sent it.
func (f Filter) MarketsOf(events []Event) []Market {
	var out []Market
	for i := range events {
		parent := events[i]
		parent.Markets = nil
		for _, m := range events[i].Markets {
			if !f.Keeps(&m) {
				continue
			}
			m.Events = []Event{parent}
			if m.Tags == nil {
				m.Tags = parent.Tags
			}
			out = append(out, m)
		}
	}
	return out
}

// SearchPageSize is the most events a search hands out per page: a larger
// limit_per_type is cut to it without complaint.
const SearchPageSize = 50

// Searcher is the search of events that SearchMarkets pages through.
// *Client is one.
type Searcher interface {
	Search(ctx context.Context, q SearchQuery) (*SearchResult, error)
}

// SearchMarkets fetches one page of the markets of the events a search finds
// under a tag (or under any, with no slug), narrowed by the filter. It pages
// as the listings do, with the page number for a cursor, so it can be handed
// to Pages. A page may hold no markets and still have a next one.
//
// The filter's sort order does not apply: a search ranks by relevance.
func SearchMarkets(ctx context.Context, s Searcher, f Filter, text, tagSlug, cursor string) (markets []Market, next string, err error) {
	page := searchPage(cursor)
	res, err := s.Search(ctx, SearchQuery{
		Text:    text,
		Limit:   SearchPageSize,
		Page:    page,
		Status:  f.Status,
		TagSlug: tagSlug,
	})
	if err != nil {
		return nil, "", err
	}
	// A page with no events ends the paging whatever More says, so a flag
	// set wrongly cannot page for ever.
	if res.More && len(res.Events) > 0 {
		next = strconv.Itoa(page + 1)
	}
	return f.MarketsOf(res.Events), next, nil
}

// searchPage is the page number a search cursor stands for. The first page
// is 1, which the empty cursor asks for.
func searchPage(cursor string) int {
	if n, err := strconv.Atoi(cursor); err == nil && n > 0 {
		return n
	}
	return 1
}

// ParseDate reads a day, taken as its first moment in UTC, or a full RFC 3339
// timestamp. Empty is the zero time: no bound.
func ParseDate(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	for _, layout := range []string{time.DateOnly, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, errors.New("neither a date (2026-11-03) nor a timestamp (2026-11-03T12:00:00Z)")
}
