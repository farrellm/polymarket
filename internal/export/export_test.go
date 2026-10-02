package export

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/farrellm/polymarket/internal/api"
)

var update = flag.Bool("update", false, "rewrite the golden CSVs in testdata/golden")

// testdata is the repository's, shared with the api package.
func testdata(name ...string) string {
	return filepath.Join(append([]string{"..", "..", "testdata"}, name...)...)
}

// The golden inputs are written out here rather than read from the recorded
// fixtures, which hold different markets after every `make fixtures`.
//
// Between them the markets cover: an ordinary one; one inside no event, that
// has never traded and has not opened (no volumes, no prices, no tokens); one
// with three outcomes; and one whose text needs quoting and guarding.
const goldenMarkets = `[
  {
    "id": "601825",
    "slug": "will-anna-win",
    "question": "Will Anna win the 2026 election?",
    "conditionId": "0x05297f854d3b757d5e51a1a29c7f225a80b14b2a161d6b7f9a61677da7a80ced",
    "active": true, "closed": false, "acceptingOrders": true, "negRisk": true,
    "startDate": "2025-09-18T20:08:04.123Z",
    "endDate": "2026-10-05T03:59:00Z",
    "outcomes": "[\"Yes\", \"No\"]",
    "outcomePrices": "[\"0.5505\", \"0.4495\"]",
    "clobTokenIds": "[\"10987686843795058436998738440635625993951919311725346501234567890123456789012\", \"30630994248667897740988010928640156930000000000000000000000000000000000000001\"]",
    "bestBid": 0.55, "bestAsk": 0.551, "lastTradePrice": 0.55, "spread": 0.001,
    "oneHourPriceChange": 0.0005, "oneDayPriceChange": -0.012, "oneWeekPriceChange": 0.1,
    "volume": "15774130.868446993", "volumeNum": 15774130.868446993,
    "volume24hr": 757103.517774, "volume1wk": 2000000, "volume1mo": 1e7,
    "liquidity": "700511.84223", "liquidityNum": 700511.84223,
    "events": [{"id": "45915", "slug": "the-2026-election", "title": "The 2026 Election"}],
    "tags": [{"id": "2", "label": "Politics", "slug": "politics"}, {"id": "144", "label": "Elections", "slug": "elections"}]
  },
  {
    "id": "700001",
    "slug": "not-open-yet",
    "question": "Will it open?",
    "conditionId": "0xabc",
    "active": true, "closed": false, "acceptingOrders": false, "negRisk": false,
    "endDate": "2027-01-01",
    "outcomes": "[\"Yes\", \"No\"]",
    "outcomePrices": null
  },
  {
    "id": "700002",
    "slug": "who-wins",
    "question": "Who wins?",
    "conditionId": "0xdef",
    "active": true, "closed": true, "acceptingOrders": false, "negRisk": false,
    "startDate": "2026-01-01T00:00:00Z",
    "endDate": "2026-06-01T12:00:00+02:00",
    "outcomes": "[\"Red\", \"Green\", \"Blue\"]",
    "outcomePrices": "[\"1\", \"0\", \"0\"]",
    "clobTokenIds": "[\"111\", \"222\", \"333\"]",
    "lastTradePrice": 1,
    "volume": "0", "volumeNum": 0,
    "events": [{"id": "9", "slug": "colours", "title": "Colours"}]
  },
  {
    "id": "700003",
    "slug": "-odd",
    "question": "=SUM(A1) or \"quoted\", with a comma\nand a second line?",
    "conditionId": "0x123",
    "active": false, "closed": false, "acceptingOrders": true, "negRisk": false,
    "outcomes": "[\"+10% or more\", \"-10% or less\"]",
    "outcomePrices": "[\"0.25\", \"0.75\"]",
    "clobTokenIds": "[\"444\", \"555\"]",
    "oneDayPriceChange": -0.5,
    "events": [{"id": "10", "slug": "odd", "title": "@odd: the event"}],
    "tags": [{"id": "7", "label": "Odd | ones", "slug": "odd-ones"}]
  }
]`

// The events are an ordinary one, and one with nothing but an ID and a title.
const goldenEvents = `[
  {
    "id": "45915",
    "slug": "the-2026-election",
    "title": "The 2026 Election",
    "active": true, "closed": false, "negRisk": true,
    "startDate": "2025-09-18T20:07:57Z",
    "endDate": "2026-10-05T03:59:00Z",
    "volume": 161489102.917339, "volume24hr": 2564548.1361579993,
    "volume1wk": 6503485.280204999, "volume1mo": 19379023.63874301,
    "liquidity": 23047892.85716, "openInterest": 8589543.173147,
    "commentCount": 21741,
    "markets": [{"id": "601825"}, {"id": "601826"}],
    "tags": [{"id": "2", "label": "Politics", "slug": "politics"}, {"id": "144", "label": "Elections", "slug": "elections"}]
  },
  {
    "id": "16044",
    "title": "-1.5 goals, home side"
  }
]`

// The detail datasets follow the first golden market. The history is of
// both its outcomes, the second ending on the price now; the trades include
// a trader whose name a spreadsheet would evaluate; and the book has a side
// with one level, as a market priced near zero does.
const (
	goldenHistoryYes = `[
  {"timestamp": 1790859600, "price": 0.5425, "resolution_seconds": 3600},
  {"timestamp": 1790863200, "price": 0.55, "resolution_seconds": 3600}
]`
	goldenHistoryNo = `[
  {"timestamp": 1790859600, "price": 0.4575, "resolution_seconds": 3600},
  {"timestamp": 1790865012, "price": 0.4495, "resolution_seconds": 0}
]`
	goldenTrades = `[
  {
    "timestamp": 1790947065, "side": "SELL", "size": 5000.0, "price": 0.55,
    "outcome": "Yes", "outcome_index": 0,
    "token_id": "10987686843795058436998738440635625993951919311725346501234567890123456789012",
    "condition_id": "0x05297f854d3b757d5e51a1a29c7f225a80b14b2a161d6b7f9a61677da7a80ced",
    "title": "Will Anna win the 2026 election?", "slug": "will-anna-win", "event_slug": "the-2026-election",
    "proxy_wallet": "0x54b56146656e7eef9da02b3a030c18e06e924b31", "name": "pup1", "pseudonym": "Grand-Reasoning",
    "transaction_hash": "0x25aadacb8b5eecdf51dca68b3847b62f9c7ce8215ff001ae79ada91eb39a9c08"
  },
  {
    "timestamp": 1790946122, "side": "BUY", "size": 333.333334, "price": 0.449,
    "outcome": "No", "outcome_index": 1,
    "token_id": "30630994248667897740988010928640156930000000000000000000000000000000000000001",
    "condition_id": "0x05297f854d3b757d5e51a1a29c7f225a80b14b2a161d6b7f9a61677da7a80ced",
    "title": "Will Anna win the 2026 election?", "slug": "will-anna-win", "event_slug": "the-2026-election",
    "proxy_wallet": "0x01a2a8841398211c2a5d7304b186a29651f77977", "name": "=HYPERLINK(\"x\")", "pseudonym": "",
    "transaction_hash": "0x3c4c9f4f5a589a1189066003e905f039f2083bd84d5eeb26a6e9ee17350252df"
  }
]`
	goldenBook = `{
  "market": "0x05297f854d3b757d5e51a1a29c7f225a80b14b2a161d6b7f9a61677da7a80ced",
  "asset_id": "10987686843795058436998738440635625993951919311725346501234567890123456789012",
  "timestamp": "1790957665098",
  "bids": [{"price": "0.55", "size": "291246.63"}],
  "asks": [{"price": "0.551", "size": "97435.68"}, {"price": "0.56", "size": "5.21"}],
  "tick_size": "0.001", "min_order_size": "5", "last_trade_price": "0.55", "neg_risk": true
}`
)

func decodeOne[T any](t *testing.T, text string) T {
	t.Helper()
	var item T
	if err := json.Unmarshal([]byte(text), &item); err != nil {
		t.Fatal(err)
	}
	return item
}

func decode[T any](t *testing.T, text string) []T {
	t.Helper()
	var items []T
	if err := json.Unmarshal([]byte(text), &items); err != nil {
		t.Fatal(err)
	}
	return items
}

func goldenDatasets(t *testing.T) []Dataset {
	t.Helper()
	markets := decode[api.Market](t, goldenMarkets)
	anna := &markets[0]
	book := decodeOne[api.Book](t, goldenBook)
	return []Dataset{
		Markets(Loaded(markets)),
		Outcomes(Loaded(markets)),
		Events(Loaded(decode[api.Event](t, goldenEvents))),
		History(Loaded([]Series{
			{Market: anna, Outcome: 0, Points: decode[api.PricePoint](t, goldenHistoryYes)},
			{Market: anna, Outcome: 1, Points: decode[api.PricePoint](t, goldenHistoryNo)},
		})),
		Trades(Loaded(decode[api.Trade](t, goldenTrades))),
		// The second outcome has no book to show: it writes no rows.
		Book(Loaded([]Depth{
			{Market: anna, Outcome: 0, Book: &book},
			{Market: anna, Outcome: 1, Book: &api.Book{}},
		})),
		// A tag whose label needs quoting and guarding, and one under which
		// nothing has traded.
		Tags(Loaded([]TagStat{
			{Tag: api.Tag{ID: "2", Slug: "politics", Label: "Politics"}, Events: 88, Volume24h: 12345678.9, Liquidity: 71e6},
			{Tag: api.Tag{ID: "101", Slug: "plus-ev", Label: "+EV, \"sharp\""}, Events: 1},
		})),
	}
}

// TestGolden pins each dataset's columns and formatting. A deliberate change
// is recorded with `go test ./internal/export -update`.
func TestGolden(t *testing.T) {
	for _, d := range goldenDatasets(t) {
		t.Run(d.Name(), func(t *testing.T) {
			var buf bytes.Buffer
			if _, err := Run(context.Background(), d, &buf, Options{}); err != nil {
				t.Fatal(err)
			}

			path := testdata("golden", d.Name()+".csv")
			if *update {
				if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := buf.String(); got != string(want) {
				t.Errorf("%s differs from %s (-update rewrites it)\ngot:\n%s\nwant:\n%s", d.Name(), path, got, want)
			}
		})
	}
}

// export runs a dataset and reads the CSV back.
func export(t *testing.T, d Dataset, o Options) (records [][]string, sum Summary) {
	t.Helper()
	var buf bytes.Buffer
	sum, err := Run(context.Background(), d, &buf, o)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "\r\n") {
		t.Error("the output has CRLF line endings, want LF")
	}
	records, err = csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatalf("the output does not read back as CSV: %v", err)
	}
	return records, sum
}

// column returns the named column of the data rows.
func column(t *testing.T, records [][]string, name string) []string {
	t.Helper()
	for i, header := range records[0] {
		if header != name {
			continue
		}
		var cells []string
		for _, rec := range records[1:] {
			cells = append(cells, rec[i])
		}
		return cells
	}
	t.Fatalf("no column %q in %q", name, records[0])
	return nil
}

func wantCells(t *testing.T, records [][]string, name string, want ...string) {
	t.Helper()
	got := column(t, records, name)
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("column %s = %q, want %q", name, got, want)
	}
}

func TestMarketsCells(t *testing.T) {
	records, sum := export(t, Markets(Loaded(decode[api.Market](t, goldenMarkets))), Options{})

	if sum.Rows != 4 || sum.Capped || sum.Dataset != "markets" {
		t.Errorf("summary = %+v, want 4 uncapped rows of markets", sum)
	}
	// A number is shortened, never rounded, and never given an exponent.
	wantCells(t, records, "volume", "15774130.868446993", "", "0", "")
	wantCells(t, records, "volume_1m", "10000000", "", "", "")
	// A value that was not sent is an empty cell, where a sent zero is 0.
	wantCells(t, records, "price_1", "0.5505", "", "1", "0.25")
	wantCells(t, records, "price_2", "0.4495", "", "0", "0.75")
	// A negative number is left alone by the formula guard.
	wantCells(t, records, "change_1d", "-0.012", "", "", "-0.5")
	// Token IDs are text: 77 digits survive untouched.
	wantCells(t, records, "token_id_1",
		"10987686843795058436998738440635625993951919311725346501234567890123456789012", "", "111", "444")
	// Timestamps are RFC 3339 in UTC, whatever form and zone they came in.
	wantCells(t, records, "start_date", "2025-09-18T20:08:04Z", "", "2026-01-01T00:00:00Z", "")
	wantCells(t, records, "end_date", "2026-10-05T03:59:00Z", "2027-01-01T00:00:00Z", "2026-06-01T10:00:00Z", "")
	wantCells(t, records, "tags", "politics|elections", "", "", "odd-ones")
	wantCells(t, records, "url",
		"https://polymarket.com/event/the-2026-election/will-anna-win",
		"https://polymarket.com/market/not-open-yet",
		"https://polymarket.com/event/colours/who-wins",
		"https://polymarket.com/event/odd/-odd")

	want := "1 market has more than two outcomes"
	if len(sum.Notes) != 1 || !strings.HasPrefix(sum.Notes[0], want) {
		t.Errorf("notes = %q, want one starting %q", sum.Notes, want)
	}
}

func TestOutcomesHasEveryOutcome(t *testing.T) {
	records, sum := export(t, Outcomes(Loaded(decode[api.Market](t, goldenMarkets))), Options{})

	wantCells(t, records, "market_id",
		"601825", "601825", "700001", "700001", "700002", "700002", "700002", "700003", "700003")
	wantCells(t, records, "outcome_index", "0", "1", "0", "1", "0", "1", "2", "0", "1")
	wantCells(t, records, "outcome", "Yes", "No", "Yes", "No", "Red", "Green", "Blue", "'+10% or more", "'-10% or less")
	wantCells(t, records, "price", "0.5505", "0.4495", "", "", "1", "0", "0", "0.25", "0.75")
	if len(sum.Notes) != 0 {
		t.Errorf("notes = %q, want none: nothing is left out of outcomes", sum.Notes)
	}
}

func TestFormulaGuard(t *testing.T) {
	markets := decode[api.Market](t, goldenMarkets)[3:]

	records, _ := export(t, Markets(Loaded(markets)), Options{})
	wantCells(t, records, "question", "'=SUM(A1) or \"quoted\", with a comma\nand a second line?")
	wantCells(t, records, "slug", "'-odd")
	wantCells(t, records, "event_title", "'@odd: the event")
	wantCells(t, records, "outcome_1", "'+10% or more")
	wantCells(t, records, "change_1d", "-0.5")

	records, _ = export(t, Markets(Loaded(markets)), Options{Raw: true})
	wantCells(t, records, "question", "=SUM(A1) or \"quoted\", with a comma\nand a second line?")
	wantCells(t, records, "slug", "-odd")
	wantCells(t, records, "event_title", "@odd: the event")
	wantCells(t, records, "outcome_1", "+10% or more")
}

func TestHeaderOnlyWhenEmpty(t *testing.T) {
	for _, d := range []Dataset{
		Markets(Loaded[api.Market](nil)),
		Outcomes(Loaded[api.Market](nil)),
		Events(Loaded[api.Event](nil)),
	} {
		records, sum := export(t, d, Options{})
		if len(records) != 1 || len(records[0]) != len(d.Columns()) {
			t.Errorf("%s: %d records, want just the header", d.Name(), len(records))
		}
		if sum.Rows != 0 {
			t.Errorf("%s: %d rows, want 0", d.Name(), sum.Rows)
		}
	}
}

func TestColumnNamesAreUnique(t *testing.T) {
	for _, d := range goldenDatasets(t) {
		seen := map[string]bool{}
		for _, c := range d.Columns() {
			if seen[c.Name] {
				t.Errorf("%s: column %s is there twice", d.Name(), c.Name)
			}
			seen[c.Name] = true
			if c.Name != strings.ToLower(c.Name) || strings.ContainsAny(c.Name, " -") {
				t.Errorf("%s: column %q is not snake_case", d.Name(), c.Name)
			}
		}
	}
}

// TestFixtures runs the recorded responses through each dataset. The
// recordings change, so this checks shape: every row is complete and
// identifies what it describes.
func TestFixtures(t *testing.T) {
	var markets struct {
		Markets []api.Market `json:"markets"`
	}
	readFixture(t, "markets.json", &markets)
	var events struct {
		Events []api.Event `json:"events"`
	}
	readFixture(t, "events.json", &events)
	if len(markets.Markets) == 0 || len(events.Events) == 0 {
		t.Fatal("the fixtures hold no markets or no events")
	}
	// The markets inside an event are the ones with the gaps.
	var embedded []api.Market
	for _, e := range events.Events {
		embedded = append(embedded, e.Markets...)
	}

	tests := []struct {
		d    Dataset
		rows int
		id   string
	}{
		{Markets(Loaded(markets.Markets)), len(markets.Markets), "id"},
		{Markets(Loaded(embedded)), len(embedded), "id"},
		{Outcomes(Loaded(markets.Markets)), -1, "token_id"},
		{Events(Loaded(events.Events)), len(events.Events), "id"},
	}
	// The detail of the market the recorder followed.
	var market api.Market
	readFixture(t, "market.json", &market)
	var history struct {
		Data []api.PricePoint `json:"data"`
	}
	readFixture(t, "prices_history.json", &history)
	var trades struct {
		Data []api.Trade `json:"data"`
	}
	readFixture(t, "trades.json", &trades)
	var book api.Book
	readFixture(t, "book.json", &book)
	tests = append(tests, []struct {
		d    Dataset
		rows int
		id   string
	}{
		{History(Loaded([]Series{{Market: &market, Points: history.Data}})), len(history.Data), "token_id"},
		{Trades(Loaded(trades.Data)), len(trades.Data), "transaction_hash"},
		{Book(Loaded([]Depth{{Market: &market, Book: &book}})), len(book.Bids) + len(book.Asks), "price"},
	}...)

	for _, tt := range tests {
		records, sum := export(t, tt.d, Options{})
		if tt.rows >= 0 && sum.Rows != tt.rows {
			t.Errorf("%s: %d rows, want %d", tt.d.Name(), sum.Rows, tt.rows)
		}
		if sum.Rows == 0 || len(records) != sum.Rows+1 {
			t.Errorf("%s: %d records for %d rows", tt.d.Name(), len(records), sum.Rows)
		}
		for _, id := range column(t, records, tt.id) {
			if id == "" {
				t.Errorf("%s: a row has no %s", tt.d.Name(), tt.id)
			}
		}
	}

	// A market in the listing arrives with its event and tags.
	records, _ := export(t, tests[0].d, Options{})
	for _, name := range []string{"event_slug", "tags", "url", "outcome_1", "outcome_2"} {
		for _, cell := range column(t, records, name) {
			if cell == "" {
				t.Errorf("markets: a row has no %s", name)
			}
		}
	}
}

func readFixture(t *testing.T, name string, v any) {
	t.Helper()
	b, err := os.ReadFile(testdata(name))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatal(err)
	}
}

// numbered is a dataset of n one-cell rows, in pages of two, that counts how
// many pages were asked for.
func numbered(n int, fetched *int) Dataset {
	pages := func(yield func([]int, error) bool) {
		for start := 0; start < n; start += 2 {
			*fetched++
			page := []int{start}
			if start+1 < n {
				page = append(page, start+1)
			}
			if !yield(page, nil) {
				return
			}
		}
	}
	return &table{
		name:    "numbered",
		columns: []Column{{"n", Number}},
		rows: flatten(pages, func(i *int) [][]string {
			return [][]string{{string(rune('a' + *i))}}
		}, nil),
	}
}

func TestLimit(t *testing.T) {
	tests := []struct {
		name       string
		rows       int
		limit      int
		wantRows   int
		wantCapped bool
		wantPages  int
	}{
		{"no limit writes everything", 5, 0, 5, false, 3},
		{"a limit above the rows changes nothing", 5, 9, 5, false, 3},
		{"a limit below the rows caps them", 5, 3, 3, true, 2},
		{"reaching the limit exactly is not a cap", 4, 4, 4, false, 2},
		{"a limit at a page boundary looks one page ahead", 6, 4, 4, true, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fetched := 0
			records, sum := export(t, numbered(tt.rows, &fetched), Options{Limit: tt.limit})
			if sum.Rows != tt.wantRows || len(records) != tt.wantRows+1 {
				t.Errorf("%d rows in %d records, want %d rows", sum.Rows, len(records), tt.wantRows)
			}
			if sum.Capped != tt.wantCapped {
				t.Errorf("capped = %v, want %v", sum.Capped, tt.wantCapped)
			}
			if fetched != tt.wantPages {
				t.Errorf("%d pages fetched, want %d", fetched, tt.wantPages)
			}
		})
	}
}

// TestLimitCountsOnlyWrittenMarkets checks the note about markets with more
// than two outcomes against one cut off by the limit.
func TestLimitCountsOnlyWrittenMarkets(t *testing.T) {
	markets := decode[api.Market](t, goldenMarkets)
	// The market with three outcomes is the third.
	_, sum := export(t, Markets(Loaded(markets)), Options{Limit: 2})
	if !sum.Capped || len(sum.Notes) != 0 {
		t.Errorf("summary = %+v, want it capped with no notes: the wide market was not written", sum)
	}
}

func TestProgress(t *testing.T) {
	var seen []int
	fetched := 0
	export(t, numbered(3, &fetched), Options{Progress: func(rows int) { seen = append(seen, rows) }})
	if len(seen) != 3 || seen[0] != 1 || seen[2] != 3 {
		t.Errorf("progress saw %v, want 1 2 3", seen)
	}
}

// failing is a dataset that yields some rows and then an error.
func failing(rows int, err error) Dataset {
	pages := func(yield func([]int, error) bool) {
		if !yield(make([]int, rows), nil) {
			return
		}
		yield(nil, err)
	}
	return &table{
		name:    "failing",
		columns: []Column{{"n", Number}},
		rows:    flatten(pages, func(*int) [][]string { return [][]string{{"1"}} }, nil),
	}
}

func TestRunReportsAListingError(t *testing.T) {
	boom := errors.New("boom")
	var buf bytes.Buffer
	sum, err := Run(context.Background(), failing(2, boom), &buf, Options{})
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want the listing's", err)
	}
	if sum.Rows != 2 {
		t.Errorf("%d rows before the error, want 2", sum.Rows)
	}
}

func TestRunRejectsARaggedRow(t *testing.T) {
	d := &table{
		name:    "ragged",
		columns: []Column{{"a", Text}, {"b", Text}},
		rows: iter.Seq2[[]string, error](func(yield func([]string, error) bool) {
			yield([]string{"only one"}, nil)
		}),
	}
	if _, err := Run(context.Background(), d, &bytes.Buffer{}, Options{}); err == nil {
		t.Error("a row with too few cells was written, want an error")
	}
}

func TestRunStopsWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	fetched := 0
	o := Options{Progress: func(rows int) {
		if rows == 3 {
			cancel()
		}
	}}
	sum, err := Run(ctx, numbered(100, &fetched), &bytes.Buffer{}, o)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if sum.Rows != 3 {
		t.Errorf("%d rows written, want it to stop at 3", sum.Rows)
	}
}

func TestFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.csv")
	if err := os.WriteFile(path, []byte("an earlier export\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	fetched := 0
	sum, err := File(context.Background(), numbered(3, &fetched), path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Rows != 3 {
		t.Errorf("%d rows, want 3", sum.Rows)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := "n\na\nb\nc\n"; string(got) != want {
		t.Errorf("file holds %q, want %q", got, want)
	}
	wantFiles(t, filepath.Dir(path), "out.csv")
}

func TestFileLeavesNothingBehindOnFailure(t *testing.T) {
	fetched := 0
	tests := []struct {
		name      string
		cancelled bool
		d         Dataset
	}{
		{"a listing error", false, failing(2, errors.New("boom"))},
		{"a cancelled export", true, numbered(3, &fetched)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.cancelled {
				cancel()
			}
			dir := t.TempDir()
			if _, err := File(ctx, tt.d, filepath.Join(dir, "out.csv"), Options{}); err == nil {
				t.Fatal("no error, want the export to fail")
			}
			wantFiles(t, dir)
		})
	}

	t.Run("an earlier file survives", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "out.csv")
		if err := os.WriteFile(path, []byte("an earlier export\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := File(context.Background(), failing(2, errors.New("boom")), path, Options{}); err == nil {
			t.Fatal("no error, want the export to fail")
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "an earlier export\n" {
			t.Errorf("the earlier file now holds %q", got)
		}
		wantFiles(t, filepath.Dir(path), "out.csv")
	})
}

func TestFileInAMissingDirectory(t *testing.T) {
	fetched := 0
	path := filepath.Join(t.TempDir(), "nowhere", "out.csv")
	if _, err := File(context.Background(), numbered(1, &fetched), path, Options{}); err == nil {
		t.Error("no error for a directory that does not exist")
	}
	if fetched != 0 {
		t.Errorf("%d pages fetched for a file that could not be created, want 0", fetched)
	}
}

// wantFiles checks that dir holds exactly the named files.
func wantFiles(t *testing.T, dir string, want ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("directory holds %q, want %q", got, want)
	}
}
