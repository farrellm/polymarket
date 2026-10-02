package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// Float is a number the API sends as a JSON number, as a numeric string, or
// not at all. Which of the three depends on the endpoint and the field: a
// market's volume is a string where an event's is a number, and a price
// change is simply absent when there is nothing to report.
//
// Valid is false for a value that was absent, null, empty or not a number, so
// a missing value can be told from a zero.
type Float struct {
	Value float64
	Valid bool
}

// UnmarshalJSON accepts a number, a string holding one, or null.
func (f *Float) UnmarshalJSON(b []byte) error {
	*f = Float{}
	s, err := scalar(b)
	if err != nil {
		return fmt.Errorf("a number: %w", err)
	}
	if v, err := strconv.ParseFloat(s, 64); err == nil {
		*f = Float{Value: v, Valid: true}
	}
	return nil
}

// Time is a moment the API sends as an RFC 3339 string, a date, or a Unix
// timestamp in seconds or milliseconds, the latter either as a number or as a
// string of digits. It is always in UTC, and zero when the value was absent
// or unreadable.
type Time struct {
	time.Time
}

// timeLayouts are the textual forms seen in responses, most common first.
var timeLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02",
	"2006-01-02 15:04:05.999999999Z07",
}

// UnmarshalJSON accepts any of the forms the type describes, or null.
func (t *Time) UnmarshalJSON(b []byte) error {
	*t = Time{}
	s, err := scalar(b)
	if err != nil {
		return fmt.Errorf("a time: %w", err)
	}
	if s == "" {
		return nil
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		*t = Time{fromUnix(n)}
		return nil
	}
	for _, layout := range timeLayouts {
		if v, err := time.Parse(layout, s); err == nil {
			*t = Time{v.UTC()}
			return nil
		}
	}
	return nil
}

// fromUnix reads a Unix timestamp in seconds or in milliseconds. The two do
// not overlap in practice: as seconds, the threshold is the year 5138.
func fromUnix(n int64) time.Time {
	const milliThreshold = 100_000_000_000
	if n >= milliThreshold {
		return time.UnixMilli(n).UTC()
	}
	return time.Unix(n, 0).UTC()
}

// scalar returns the text of a JSON number or string, and "" for null. Any
// other JSON value is an error: it means the field is not what it was.
func scalar(b []byte) (string, error) {
	b = bytes.TrimSpace(b)
	switch {
	case len(b) == 0, bytes.Equal(b, []byte("null")):
		return "", nil
	case b[0] == '"':
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return "", err
		}
		return s, nil
	case b[0] == '{', b[0] == '[', b[0] == 't', b[0] == 'f':
		return "", fmt.Errorf("unexpected JSON value %s", abbreviate(b))
	}
	return string(b), nil
}

func abbreviate(b []byte) string {
	const limit = 40
	if len(b) > limit {
		return string(b[:limit]) + "…"
	}
	return string(b)
}

// Tag is a label events are filed under.
type Tag struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Slug  string `json:"slug"`
	// ActiveEvents is only filled in by RelatedTags.
	ActiveEvents int `json:"activeEventsCount"`
}

// Event groups one or more markets about the same question.
type Event struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	Ticker      string `json:"ticker"`
	Title       string `json:"title"`
	Description string `json:"description"`

	StartDate Time `json:"startDate"`
	EndDate   Time `json:"endDate"`

	Active     bool `json:"active"`
	Closed     bool `json:"closed"`
	Archived   bool `json:"archived"`
	Featured   bool `json:"featured"`
	Restricted bool `json:"restricted"`
	NegRisk    bool `json:"negRisk"`

	Liquidity    Float `json:"liquidity"`
	OpenInterest Float `json:"openInterest"`
	Volume       Float `json:"volume"`
	Volume24h    Float `json:"volume24hr"`
	Volume1w     Float `json:"volume1wk"`
	Volume1m     Float `json:"volume1mo"`
	Volume1y     Float `json:"volume1yr"`

	CommentCount int `json:"commentCount"`

	// Markets and Tags are embedded by the event endpoints. An event found
	// inside a market (Market.Events) carries neither.
	Markets []Market `json:"markets"`
	Tags    []Tag    `json:"tags"`
}

// Outcome is one side of a market.
type Outcome struct {
	Label string
	// Price is between 0 and 1. It is not valid for a market that has not
	// opened yet, which is listed with no prices.
	Price Float
	// TokenID identifies the outcome to the CLOB and the price history. It
	// is a 77-digit decimal, far too large for any integer type.
	TokenID string
}

// Market is a single question with tradeable outcomes.
type Market struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	Question    string `json:"question"`
	Description string `json:"description"`
	// ConditionID is what the Data API knows the market by.
	ConditionID string `json:"conditionId"`
	// GroupItemTitle is the market's short name within its event, such as
	// "50+ bps decrease".
	GroupItemTitle string `json:"groupItemTitle"`

	StartDate Time `json:"startDate"`
	EndDate   Time `json:"endDate"`

	Active          bool `json:"active"`
	Closed          bool `json:"closed"`
	Archived        bool `json:"archived"`
	AcceptingOrders bool `json:"acceptingOrders"`
	NegRisk         bool `json:"negRisk"`

	// Outcomes is assembled from three parallel arrays; see UnmarshalJSON.
	Outcomes []Outcome `json:"-"`

	BestBid        Float `json:"bestBid"`
	BestAsk        Float `json:"bestAsk"`
	LastTradePrice Float `json:"lastTradePrice"`
	Spread         Float `json:"spread"`

	Change1h Float `json:"oneHourPriceChange"`
	Change1d Float `json:"oneDayPriceChange"`
	Change1w Float `json:"oneWeekPriceChange"`
	Change1m Float `json:"oneMonthPriceChange"`

	// The volumes are not valid for a market that has never traded: inside
	// an event it is listed with no volume members at all.
	Liquidity Float `json:"liquidity"`
	Volume    Float `json:"volume"`
	Volume24h Float `json:"volume24hr"`
	Volume1w  Float `json:"volume1wk"`
	Volume1m  Float `json:"volume1mo"`
	Volume1y  Float `json:"volume1yr"`

	TickSize     Float `json:"orderPriceMinTickSize"`
	MinOrderSize Float `json:"orderMinSize"`

	// Events is filled in by the market endpoints, and Tags by Markets when
	// asked to include them. A market found inside an event carries neither.
	Events []Event `json:"events"`
	Tags   []Tag   `json:"tags"`
}

// UnmarshalJSON decodes a market, absorbing two quirks of the Gamma API:
// outcomes, outcomePrices and clobTokenIds are JSON arrays encoded as
// strings, and volume and liquidity are strings with the numbers alongside
// as volumeNum and liquidityNum.
func (m *Market) UnmarshalJSON(b []byte) error {
	// plain has Market's fields without its methods, so decoding into it
	// does not come back here.
	type plain Market
	aux := struct {
		*plain
		Outcomes      json.RawMessage `json:"outcomes"`
		OutcomePrices json.RawMessage `json:"outcomePrices"`
		TokenIDs      json.RawMessage `json:"clobTokenIds"`
		VolumeNum     Float           `json:"volumeNum"`
		LiquidityNum  Float           `json:"liquidityNum"`
	}{plain: (*plain)(m)}
	if err := json.Unmarshal(b, &aux); err != nil {
		return err
	}

	if !m.Volume.Valid {
		m.Volume = aux.VolumeNum
	}
	if !m.Liquidity.Valid {
		m.Liquidity = aux.LiquidityNum
	}

	labels, err := stringList(aux.Outcomes)
	if err != nil {
		return fmt.Errorf("market %s: outcomes: %w", m.ID, err)
	}
	prices, err := stringList(aux.OutcomePrices)
	if err != nil {
		return fmt.Errorf("market %s: outcomePrices: %w", m.ID, err)
	}
	tokens, err := stringList(aux.TokenIDs)
	if err != nil {
		return fmt.Errorf("market %s: clobTokenIds: %w", m.ID, err)
	}

	// The arrays are parallel, but not always all present: a market that
	// never traded on the CLOB has no token IDs.
	m.Outcomes = nil
	for i := range max(len(labels), len(prices), len(tokens)) {
		var o Outcome
		if i < len(labels) {
			o.Label = labels[i]
		}
		if i < len(prices) {
			if v, err := strconv.ParseFloat(prices[i], 64); err == nil {
				o.Price = Float{Value: v, Valid: true}
			}
		}
		if i < len(tokens) {
			o.TokenID = tokens[i]
		}
		m.Outcomes = append(m.Outcomes, o)
	}
	return nil
}

// stringList decodes a JSON array of strings or numbers that may itself be
// wrapped in a JSON string, returning each element's text.
func stringList(raw json.RawMessage) ([]string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	if raw[0] == '"' {
		var inner string
		if err := json.Unmarshal(raw, &inner); err != nil {
			return nil, err
		}
		if inner == "" {
			return nil, nil
		}
		raw = []byte(inner)
	}

	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	out := make([]string, len(items))
	for i, item := range items {
		s, err := scalar(item)
		if err != nil {
			return nil, err
		}
		out[i] = s
	}
	return out, nil
}

// Level is one price level of an order book.
type Level struct {
	Price Float `json:"price"`
	Size  Float `json:"size"`
}

// Book is a snapshot of one outcome's order book.
type Book struct {
	// ConditionID is the market the outcome belongs to.
	ConditionID string `json:"market"`
	TokenID     string `json:"asset_id"`
	Time        Time   `json:"timestamp"`
	Hash        string `json:"hash"`

	// Bids and Asks are ordered best first: the highest bid, the lowest ask.
	Bids []Level `json:"bids"`
	Asks []Level `json:"asks"`

	TickSize       Float `json:"tick_size"`
	MinOrderSize   Float `json:"min_order_size"`
	LastTradePrice Float `json:"last_trade_price"`
	NegRisk        bool  `json:"neg_risk"`
}

// PricePoint is the price of an outcome at a moment.
type PricePoint struct {
	Time  Time  `json:"timestamp"`
	Price Float `json:"price"`
	// ResolutionSeconds is the width of the bucket the point stands for.
	ResolutionSeconds int `json:"resolution_seconds"`
}

// Trade is one fill.
type Trade struct {
	Time Time `json:"timestamp"`
	// Side is BUY or SELL, from the taker's point of view.
	Side  string `json:"side"`
	Size  Float  `json:"size"`
	Price Float  `json:"price"`

	Outcome      string `json:"outcome"`
	OutcomeIndex int    `json:"outcome_index"`
	TokenID      string `json:"token_id"`
	ConditionID  string `json:"condition_id"`

	// Title and Slug are the market's; EventSlug is its event's.
	Title     string `json:"title"`
	Slug      string `json:"slug"`
	EventSlug string `json:"event_slug"`

	ProxyWallet     string `json:"proxy_wallet"`
	Name            string `json:"name"`
	Pseudonym       string `json:"pseudonym"`
	TransactionHash string `json:"transaction_hash"`
}
