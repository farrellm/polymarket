package export

import (
	"context"
	"fmt"
	"iter"

	"github.com/farrellm/polymarket/internal/api"
)

// HistorySource is where a price history is fetched from. *api.Client is one.
type HistorySource interface {
	PriceHistory(ctx context.Context, q api.HistoryQuery) (points []api.PricePoint, next string, err error)
}

// BookSource is where an order book is fetched from. *api.Client is one.
type BookSource interface {
	Book(ctx context.Context, tokenID string) (*api.Book, error)
}

// TradeSource is where a market's trades are fetched from. *api.Client is one.
type TradeSource interface {
	Trades(ctx context.Context, q api.TradesQuery) (trades []api.Trade, next string, err error)
}

// Tradable are the outcomes of a market that can be asked about, by their
// index: those with a token. A market that has not opened has none.
func Tradable(m *api.Market) []int {
	var out []int
	for i, o := range m.Outcomes {
		if o.TokenID != "" {
			out = append(out, i)
		}
	}
	return out
}

// HistoryPages pages through the price history of each of the outcomes in
// turn, over the interval, limit points to a page. It is what History is
// handed to fetch as it goes.
func HistoryPages(ctx context.Context, c HistorySource, m *api.Market, outcomes []int, interval string, limit int) iter.Seq2[[]Series, error] {
	return func(yield func([]Series, error) bool) {
		for _, index := range outcomes {
			q := api.HistoryQuery{TokenID: m.Outcomes[index].TokenID, Interval: interval, Limit: limit}
			pages := api.Pages(ctx, func(cursor string) ([]api.PricePoint, string, error) {
				q.Cursor = cursor
				return c.PriceHistory(ctx, q)
			})
			for points, err := range pages {
				if err != nil {
					yield(nil, err)
					return
				}
				if !yield([]Series{{Market: m, Outcome: index, Points: points}}, nil) {
					return
				}
			}
		}
	}
}

// TradePages pages through the trades of a market, newest first, keeping
// those from q.Start up to but not including q.End. The service takes both
// bounds and ignores them, so they are applied here: a trade at or after End
// is dropped, and the paging stops at the first one before Start, which an
// extension of a file relies on to ask for one page rather than all of them.
func TradePages(ctx context.Context, c TradeSource, q api.TradesQuery) iter.Seq2[[]api.Trade, error] {
	return func(yield func([]api.Trade, error) bool) {
		pages := api.Pages(ctx, func(cursor string) ([]api.Trade, string, error) {
			q.Cursor = cursor
			return c.Trades(ctx, q)
		})
		for trades, err := range pages {
			if err != nil {
				yield(nil, err)
				return
			}
			var kept []api.Trade
			past := false
			for _, t := range trades {
				switch {
				case !q.End.IsZero() && !t.Time.Before(q.End):
				case !q.Start.IsZero() && t.Time.Before(q.Start):
					past = true
				default:
					kept = append(kept, t)
				}
			}
			if !yield(kept, nil) || past {
				return
			}
		}
	}
}

// BookPages fetches the order book of each of the outcomes in turn. A market
// that is no longer trading has none, which the service reports as not
// found, and which is an error here rather than a file of headers.
func BookPages(ctx context.Context, c BookSource, m *api.Market, outcomes []int) iter.Seq2[[]Depth, error] {
	return func(yield func([]Depth, error) bool) {
		for _, index := range outcomes {
			book, err := c.Book(ctx, m.Outcomes[index].TokenID)
			if api.IsNotFound(err) {
				err = fmt.Errorf("the market %s has no order book: it is not trading", m.Slug)
			}
			if err != nil {
				yield(nil, err)
				return
			}
			if !yield([]Depth{{Market: m, Outcome: index, Book: book}}, nil) {
				return
			}
		}
	}
}
