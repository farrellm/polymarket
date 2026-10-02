package api

import (
	"context"
	"net/url"
	"time"
)

// page is the envelope the Data API wraps a listing in.
type page[T any] struct {
	Data       []T `json:"data"`
	Pagination struct {
		HasMore    bool   `json:"has_more"`
		NextCursor string `json:"next_cursor"`
	} `json:"pagination"`
}

// next is the cursor of the following page, and empty on the last one.
func (p *page[T]) next() string {
	if !p.Pagination.HasMore {
		return ""
	}
	return p.Pagination.NextCursor
}

// HistoryIntervals are how far back a price history can be asked to go,
// shortest first. The service picks the spacing of the points to suit: a
// minute for a day, five for a week, half an hour for a month, and half a
// day for everything.
var HistoryIntervals = []string{"1h", "6h", "1d", "1w", "1m", "max"}

// HistoryQuery selects the price history of one outcome.
//
// The service needs a time component: either Interval or Start. A Start to
// End range may span at most 15 days, which the service enforces with a 400;
// for anything longer, use an Interval.
type HistoryQuery struct {
	TokenID string
	// Interval is how far back to go: 1h, 6h, 1d, 1w, 1m or max.
	Interval string
	// Start and End bound the history instead of Interval.
	Start time.Time
	End   time.Time
	// BucketSeconds is the spacing of the points; zero leaves it to the
	// service, which picks one to suit the interval.
	BucketSeconds int
	// Limit is the page size; the service allows at most 10000.
	Limit int
	// Cursor is the next-page cursor of the previous page, or empty.
	Cursor string
}

func (q HistoryQuery) values() url.Values {
	v := url.Values{}
	v.Set("token_id", q.TokenID)
	setString(v, "interval", q.Interval)
	setUnix(v, "start", q.Start)
	setUnix(v, "end", q.End)
	setInt(v, "bucket_seconds", q.BucketSeconds)
	setInt(v, "limit", q.Limit)
	setString(v, "cursor", q.Cursor)
	return v
}

// PriceHistory fetches one page of an outcome's price history, oldest first.
// next is the cursor of the following page, and empty on the last one.
func (c *Client) PriceHistory(ctx context.Context, q HistoryQuery) (points []PricePoint, next string, err error) {
	var resp page[PricePoint]
	if err := c.get(ctx, c.data, "/prices-history", q.values(), &resp); err != nil {
		return nil, "", err
	}
	return resp.Data, resp.next(), nil
}

// TradesQuery selects the trades of one market.
type TradesQuery struct {
	// ConditionID is the market's, as in Market.ConditionID.
	ConditionID string
	// Limit is the page size; the service allows at most 1000.
	Limit int
	// Cursor is the next-page cursor of the previous page, or empty.
	Cursor string
	// Start and End bound the trades in time; zero means no bound.
	Start time.Time
	End   time.Time
	// Side is BUY or SELL; empty means both.
	Side string
}

func (q TradesQuery) values() url.Values {
	v := url.Values{}
	v.Set("condition", q.ConditionID)
	setInt(v, "limit", q.Limit)
	setString(v, "cursor", q.Cursor)
	setUnix(v, "start", q.Start)
	setUnix(v, "end", q.End)
	setString(v, "side", q.Side)
	return v
}

// Trades fetches one page of a market's trades, newest first. next is the
// cursor of the following page, and empty on the last one.
func (c *Client) Trades(ctx context.Context, q TradesQuery) (trades []Trade, next string, err error) {
	var resp page[Trade]
	if err := c.get(ctx, c.data, "/trades", q.values(), &resp); err != nil {
		return nil, "", err
	}
	return resp.Data, resp.next(), nil
}
