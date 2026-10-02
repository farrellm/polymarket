package api

import (
	"context"
	"net/url"
	"strconv"
	"time"
)

// Status selects events or markets by whether they are still open.
type Status int

// The zero value is StatusOpen, which is what a browser wants by default.
const (
	StatusOpen Status = iota
	StatusClosed
	StatusAll
)

// EventsQuery selects and orders a page of events. The zero value is the
// first page of open events in the service's default order.
type EventsQuery struct {
	// Limit is the page size; the service allows at most 100.
	Limit int
	// Cursor is the next-page cursor of the previous page, or empty.
	Cursor string
	// Order is the field to sort by, such as volume24hr, volume, liquidity
	// or endDate. Ascending only applies when Order is set.
	Order     string
	Ascending bool

	Status Status
	// TagID and TagSlug each narrow the listing to one tag.
	TagID   string
	TagSlug string
	// TitleSearch keeps events whose title contains the text.
	TitleSearch string

	// Zero means no bound.
	VolumeMin    float64
	LiquidityMin float64
	EndDateMin   time.Time
	EndDateMax   time.Time
}

func (q EventsQuery) values() url.Values {
	v := url.Values{}
	setPaging(v, q.Limit, q.Cursor, q.Order, q.Ascending)
	setStatus(v, q.Status)
	setString(v, "tag_id", q.TagID)
	setString(v, "tag_slug", q.TagSlug)
	setString(v, "title_search", q.TitleSearch)
	setFloat(v, "volume_min", q.VolumeMin)
	setFloat(v, "liquidity_min", q.LiquidityMin)
	setTime(v, "end_date_min", q.EndDateMin)
	setTime(v, "end_date_max", q.EndDateMax)
	return v
}

// Events fetches one page of events, each with its markets and tags. next is
// the cursor of the following page, and empty on the last one.
func (c *Client) Events(ctx context.Context, q EventsQuery) (events []Event, next string, err error) {
	var resp struct {
		Events     []Event `json:"events"`
		NextCursor string  `json:"next_cursor"`
	}
	if err := c.get(ctx, c.gamma, "/events/keyset", q.values(), &resp); err != nil {
		return nil, "", err
	}
	return resp.Events, resp.NextCursor, nil
}

// MarketsQuery selects and orders a page of markets. The zero value is the
// first page of open markets in the service's default order.
//
// There is no text search here: the endpoint has no parameter for it. Search
// goes through Search instead.
type MarketsQuery struct {
	// Limit is the page size; the service allows at most 100.
	Limit int
	// Cursor is the next-page cursor of the previous page, or empty.
	Cursor string
	// Order is the field to sort by, such as volume24hr, volumeNum,
	// liquidityNum or endDate. Ascending only applies when Order is set.
	Order     string
	Ascending bool

	Status Status
	TagID  string
	// IncludeTags asks for each market's tags, which are otherwise left out.
	IncludeTags bool

	// Zero means no bound.
	VolumeMin    float64
	LiquidityMin float64
	EndDateMin   time.Time
	EndDateMax   time.Time
}

func (q MarketsQuery) values() url.Values {
	v := url.Values{}
	setPaging(v, q.Limit, q.Cursor, q.Order, q.Ascending)
	setStatus(v, q.Status)
	setString(v, "tag_id", q.TagID)
	if q.IncludeTags {
		v.Set("include_tag", "true")
	}
	setFloat(v, "volume_num_min", q.VolumeMin)
	setFloat(v, "liquidity_num_min", q.LiquidityMin)
	setTime(v, "end_date_min", q.EndDateMin)
	setTime(v, "end_date_max", q.EndDateMax)
	return v
}

// Markets fetches one page of markets, each with a summary of its event.
// next is the cursor of the following page, and empty on the last one.
func (c *Client) Markets(ctx context.Context, q MarketsQuery) (markets []Market, next string, err error) {
	var resp struct {
		Markets    []Market `json:"markets"`
		NextCursor string   `json:"next_cursor"`
	}
	if err := c.get(ctx, c.gamma, "/markets/keyset", q.values(), &resp); err != nil {
		return nil, "", err
	}
	return resp.Markets, resp.NextCursor, nil
}

// Event fetches one event by its ID.
func (c *Client) Event(ctx context.Context, id string) (*Event, error) {
	var e Event
	if err := c.get(ctx, c.gamma, "/events/"+url.PathEscape(id), nil, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

// Market fetches one market by its ID. Unlike a market in a listing, it
// carries neither its event nor its tags.
func (c *Client) Market(ctx context.Context, id string) (*Market, error) {
	var m Market
	if err := c.get(ctx, c.gamma, "/markets/"+url.PathEscape(id), nil, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// Tag looks a tag up by its slug. An unknown slug is an error that
// IsNotFound recognises.
func (c *Client) Tag(ctx context.Context, slug string) (*Tag, error) {
	var t Tag
	if err := c.get(ctx, c.gamma, "/tags/slug/"+url.PathEscape(slug), nil, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// RelatedTags lists the tags that narrow the given one, such as Trump and
// Midterms under politics, in the service's ranking. Only tags with open
// events are returned, each with its count of them.
func (c *Client) RelatedTags(ctx context.Context, slug string) ([]Tag, error) {
	q := url.Values{}
	q.Set("status", "active")
	q.Set("omit_empty", "true")
	var tags []Tag
	path := "/tags/slug/" + url.PathEscape(slug) + "/related-tags/tags"
	if err := c.get(ctx, c.gamma, path, q, &tags); err != nil {
		return nil, err
	}
	return tags, nil
}

// SearchQuery is a text search over events.
//
// The search endpoint honours fewer filters than the listings do. Checked
// against the live service: it takes a status and a tag, but it has no volume,
// liquidity or end-date bounds, and it ranks by relevance whatever order is
// asked for. It also pages by number rather than by cursor.
type SearchQuery struct {
	// Text is what to search for.
	Text string
	// Limit is the number of events per page.
	Limit int
	// Page counts from 1; zero means the first page.
	Page int
	// Status is open or closed (which the service calls resolved). All is
	// expressed by not asking.
	Status Status
	// TagSlug narrows the search to one tag.
	TagSlug string
}

func (q SearchQuery) values() url.Values {
	v := url.Values{}
	v.Set("q", q.Text)
	setInt(v, "limit_per_type", q.Limit)
	setInt(v, "page", q.Page)
	switch q.Status {
	case StatusOpen:
		v.Set("events_status", "active")
	case StatusClosed:
		v.Set("events_status", "resolved")
	case StatusAll:
	}
	setString(v, "events_tag", q.TagSlug)
	return v
}

// SearchResult is one page of a search.
type SearchResult struct {
	// Events each carry their markets and tags, as in a listing.
	Events []Event
	// More reports whether there is a page after this one.
	More bool
	// Total is the number of events matching, across all pages.
	Total int
}

// Search finds events by text. A search that matches nothing is an empty
// result, not an error.
func (c *Client) Search(ctx context.Context, q SearchQuery) (*SearchResult, error) {
	var resp struct {
		Events     []Event `json:"events"`
		Pagination struct {
			HasMore      bool `json:"hasMore"`
			TotalResults int  `json:"totalResults"`
		} `json:"pagination"`
	}
	if err := c.get(ctx, c.gamma, "/public-search", q.values(), &resp); err != nil {
		return nil, err
	}
	return &SearchResult{
		Events: resp.Events,
		More:   resp.Pagination.HasMore,
		Total:  resp.Pagination.TotalResults,
	}, nil
}

// setPaging sets what the two keyset listings share. They page by cursor
// only: an offset is answered with 422.
func setPaging(v url.Values, limit int, cursor, order string, ascending bool) {
	setInt(v, "limit", limit)
	setString(v, "after_cursor", cursor)
	if order != "" {
		v.Set("order", order)
		v.Set("ascending", strconv.FormatBool(ascending))
	}
}

func setStatus(v url.Values, s Status) {
	switch s {
	case StatusOpen:
		v.Set("closed", "false")
	case StatusClosed:
		v.Set("closed", "true")
	case StatusAll:
		// Leaving the parameter out returns both.
	}
}

func setString(v url.Values, key, s string) {
	if s != "" {
		v.Set(key, s)
	}
}

func setInt(v url.Values, key string, n int) {
	if n > 0 {
		v.Set(key, strconv.Itoa(n))
	}
}

func setFloat(v url.Values, key string, f float64) {
	if f > 0 {
		v.Set(key, strconv.FormatFloat(f, 'f', -1, 64))
	}
}

func setTime(v url.Values, key string, t time.Time) {
	if !t.IsZero() {
		v.Set(key, t.UTC().Format(time.RFC3339))
	}
}

// setUnix sets a time as Unix seconds, which is how the Data API takes them.
func setUnix(v url.Values, key string, t time.Time) {
	if !t.IsZero() {
		v.Set(key, strconv.FormatInt(t.Unix(), 10))
	}
}
