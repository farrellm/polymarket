// Package export writes Polymarket data as CSV.
//
// A Dataset is a named table with a fixed set of columns; Run and File write
// one out. Nothing here knows about the terminal, so the browser and the
// headless `polymarket export` command share it.
package export

import (
	"iter"
	"strconv"
	"strings"
	"time"

	"github.com/farrellm/polymarket/internal/api"
)

// Kind says what a column holds, which decides whether its cells may be
// altered on the way out: only Text is ever guarded against a spreadsheet
// reading it as a formula.
type Kind int

// The kinds of column.
const (
	// Text is free text, an ID or a slug.
	Text Kind = iota
	// Number is a decimal, written in its shortest form that reads back as
	// the same value. A negative one starts with a minus sign, so it must
	// never be mistaken for text.
	Number
	// Bool is true or false.
	Bool
	// Time is an RFC 3339 timestamp in UTC.
	Time
)

// Column is one column of a dataset.
type Column struct {
	Name string
	Kind Kind
}

// Dataset is a table to export. Its columns never change from one run to the
// next: they are a contract with whatever reads the file.
type Dataset interface {
	// Name is what the dataset is called on the command line and in a
	// default file name.
	Name() string
	Columns() []Column
	// Rows yields one row at a time, each with a cell per column and an
	// empty cell for a missing value. An error ends the iteration and is
	// yielded as its last element. A dataset over a live listing fetches as
	// it goes, so stopping early stops the requests.
	Rows() iter.Seq2[[]string, error]
	// Notes describes anything the rows read so far left out. It is meant
	// to be asked once the rows have been read.
	Notes() []string
}

// Loaded presents rows already in memory as a one-page listing, for
// exporting what is on screen without another request.
func Loaded[T any](items []T) iter.Seq2[[]T, error] {
	return func(yield func([]T, error) bool) {
		yield(items, nil)
	}
}

// table is a Dataset built from a row iterator.
type table struct {
	name    string
	columns []Column
	rows    iter.Seq2[[]string, error]
	notes   func() []string
}

func (t *table) Name() string                     { return t.name }
func (t *table) Columns() []Column                { return t.columns }
func (t *table) Rows() iter.Seq2[[]string, error] { return t.rows }

func (t *table) Notes() []string {
	if t.notes == nil {
		return nil
	}
	return t.notes()
}

// flatten turns pages of items into rows. An item may make any number of
// rows; taken is called for an item once all of them have been accepted.
func flatten[T any](pages iter.Seq2[[]T, error], rows func(*T) [][]string, taken func(*T)) iter.Seq2[[]string, error] {
	return func(yield func([]string, error) bool) {
		for page, err := range pages {
			if err != nil {
				yield(nil, err)
				return
			}
			for i := range page {
				for _, row := range rows(&page[i]) {
					if !yield(row, nil) {
						return
					}
				}
				if taken != nil {
					taken(&page[i])
				}
			}
		}
	}
}

// outcomesPerMarket is how many outcomes the markets dataset has columns for.
const outcomesPerMarket = 2

var marketColumns = []Column{
	{"id", Text},
	{"slug", Text},
	{"question", Text},
	{"event_id", Text},
	{"event_slug", Text},
	{"event_title", Text},
	{"condition_id", Text},
	{"active", Bool},
	{"closed", Bool},
	{"accepting_orders", Bool},
	{"neg_risk", Bool},
	{"start_date", Time},
	{"end_date", Time},
	{"outcome_1", Text},
	{"price_1", Number},
	{"token_id_1", Text},
	{"outcome_2", Text},
	{"price_2", Number},
	{"token_id_2", Text},
	{"best_bid", Number},
	{"best_ask", Number},
	{"last_trade_price", Number},
	{"spread", Number},
	{"change_1h", Number},
	{"change_1d", Number},
	{"change_1w", Number},
	{"volume", Number},
	{"volume_24h", Number},
	{"volume_1w", Number},
	{"volume_1m", Number},
	{"liquidity", Number},
	{"tags", Text},
	{"url", Text},
}

// Markets is the markets dataset: one row per market, wide, with columns for
// its first two outcomes. A market with more is counted in the notes; the
// outcomes dataset has all of them.
func Markets(pages iter.Seq2[[]api.Market, error]) Dataset {
	wide := 0
	return &table{
		name:    "markets",
		columns: marketColumns,
		rows: flatten(pages,
			func(m *api.Market) [][]string { return [][]string{marketRow(m)} },
			func(m *api.Market) {
				if len(m.Outcomes) > outcomesPerMarket {
					wide++
				}
			}),
		notes: func() []string {
			if wide == 0 {
				return nil
			}
			return []string{plural(wide, "market has", "markets have") +
				" more than two outcomes, of which only the first two were written; the outcomes dataset has them all"}
		},
	}
}

func marketRow(m *api.Market) []string {
	ev := eventOf(m)
	row := []string{
		m.ID,
		m.Slug,
		m.Question,
		ev.ID,
		ev.Slug,
		ev.Title,
		m.ConditionID,
		boolean(m.Active),
		boolean(m.Closed),
		boolean(m.AcceptingOrders),
		boolean(m.NegRisk),
		timestamp(m.StartDate),
		timestamp(m.EndDate),
	}
	for i := range outcomesPerMarket {
		var o api.Outcome
		if i < len(m.Outcomes) {
			o = m.Outcomes[i]
		}
		row = append(row, o.Label, number(o.Price), o.TokenID)
	}
	return append(row,
		number(m.BestBid),
		number(m.BestAsk),
		number(m.LastTradePrice),
		number(m.Spread),
		number(m.Change1h),
		number(m.Change1d),
		number(m.Change1w),
		number(m.Volume),
		number(m.Volume24h),
		number(m.Volume1w),
		number(m.Volume1m),
		number(m.Liquidity),
		tagList(m.Tags),
		marketURL(m),
	)
}

var outcomeColumns = []Column{
	{"market_id", Text},
	{"market_slug", Text},
	{"question", Text},
	{"event_id", Text},
	{"event_slug", Text},
	{"event_title", Text},
	{"condition_id", Text},
	{"active", Bool},
	{"closed", Bool},
	{"end_date", Time},
	{"outcome_index", Number},
	{"outcome", Text},
	{"price", Number},
	{"token_id", Text},
}

// Outcomes is the outcomes dataset: the markets in long layout, one row per
// market and outcome. outcome_index counts from zero, as the trades do.
func Outcomes(pages iter.Seq2[[]api.Market, error]) Dataset {
	return &table{
		name:    "outcomes",
		columns: outcomeColumns,
		rows:    flatten(pages, outcomeRows, nil),
	}
}

func outcomeRows(m *api.Market) [][]string {
	ev := eventOf(m)
	rows := make([][]string, len(m.Outcomes))
	for i, o := range m.Outcomes {
		rows[i] = []string{
			m.ID,
			m.Slug,
			m.Question,
			ev.ID,
			ev.Slug,
			ev.Title,
			m.ConditionID,
			boolean(m.Active),
			boolean(m.Closed),
			timestamp(m.EndDate),
			strconv.Itoa(i),
			o.Label,
			number(o.Price),
			o.TokenID,
		}
	}
	return rows
}

var eventColumns = []Column{
	{"id", Text},
	{"slug", Text},
	{"title", Text},
	{"active", Bool},
	{"closed", Bool},
	{"neg_risk", Bool},
	{"start_date", Time},
	{"end_date", Time},
	{"markets", Number},
	{"volume", Number},
	{"volume_24h", Number},
	{"volume_1w", Number},
	{"volume_1m", Number},
	{"liquidity", Number},
	{"open_interest", Number},
	{"comment_count", Number},
	{"tags", Text},
	{"url", Text},
}

// Events is the events dataset: one row per event.
func Events(pages iter.Seq2[[]api.Event, error]) Dataset {
	return &table{
		name:    "events",
		columns: eventColumns,
		rows: flatten(pages,
			func(e *api.Event) [][]string { return [][]string{eventRow(e)} },
			nil),
	}
}

func eventRow(e *api.Event) []string {
	return []string{
		e.ID,
		e.Slug,
		e.Title,
		boolean(e.Active),
		boolean(e.Closed),
		boolean(e.NegRisk),
		timestamp(e.StartDate),
		timestamp(e.EndDate),
		strconv.Itoa(len(e.Markets)),
		number(e.Volume),
		number(e.Volume24h),
		number(e.Volume1w),
		number(e.Volume1m),
		number(e.Liquidity),
		number(e.OpenInterest),
		strconv.Itoa(e.CommentCount),
		tagList(e.Tags),
		eventURL(e.Slug),
	}
}

// eventOf is the event a market belongs to, or an empty one for a market
// that arrived without it.
func eventOf(m *api.Market) *api.Event {
	if len(m.Events) == 0 {
		return &api.Event{}
	}
	return &m.Events[0]
}

const siteURL = "https://polymarket.com"

func eventURL(slug string) string {
	if slug == "" {
		return ""
	}
	return siteURL + "/event/" + slug
}

// marketURL is the market's page, which lives under its event. Without the
// event, the site's /market/ address redirects there.
func marketURL(m *api.Market) string {
	switch ev := eventOf(m); {
	case m.Slug == "":
		return ""
	case ev.Slug == "":
		return siteURL + "/market/" + m.Slug
	default:
		return eventURL(ev.Slug) + "/" + m.Slug
	}
}

// number writes a value in the shortest form that reads back as the same
// one, without an exponent. A value the API did not send is an empty cell,
// not a zero.
func number(f api.Float) string {
	if !f.Valid {
		return ""
	}
	return strconv.FormatFloat(f.Value, 'f', -1, 64)
}

func boolean(b bool) string {
	return strconv.FormatBool(b)
}

func timestamp(t api.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// tagList joins the tags' slugs, which unlike their labels cannot contain
// the separator.
func tagList(tags []api.Tag) string {
	slugs := make([]string, len(tags))
	for i, t := range tags {
		slugs[i] = t.Slug
	}
	return strings.Join(slugs, "|")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}
