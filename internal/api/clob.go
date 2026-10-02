package api

import (
	"cmp"
	"context"
	"net/url"
	"slices"
)

// Book fetches the order book of one outcome, by its token ID. A token with
// no book, such as one in a resolved market, is an error that IsNotFound
// recognises.
func (c *Client) Book(ctx context.Context, tokenID string) (*Book, error) {
	q := url.Values{}
	q.Set("token_id", tokenID)
	var b Book
	if err := c.get(ctx, c.clob, "/book", q, &b); err != nil {
		return nil, err
	}

	// The service sends both sides worst first. Best first is what a depth
	// display and an export both want, and sorting does not depend on the
	// service keeping to its order.
	slices.SortStableFunc(b.Bids, func(x, y Level) int {
		return cmp.Compare(y.Price.Value, x.Price.Value)
	})
	slices.SortStableFunc(b.Asks, func(x, y Level) int {
		return cmp.Compare(x.Price.Value, y.Price.Value)
	})
	return &b, nil
}
