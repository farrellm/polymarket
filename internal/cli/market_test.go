package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/farrellm/polymarket/internal/api"
)

// cells are one column of a CSV read back, the header aside.
func cells(t *testing.T, records [][]string, name string) []string {
	t.Helper()
	at := slices.Index(records[0], name)
	if at < 0 {
		t.Fatalf("no column %s in %q", name, records[0])
	}
	var out []string
	for _, r := range records[1:] {
		out = append(out, r[at])
	}
	return out
}

func wantCells(t *testing.T, records [][]string, name string, want ...string) {
	t.Helper()
	if got := cells(t, records, name); !slices.Equal(got, want) {
		t.Errorf("%s = %q, want %q", name, got, want)
	}
}

func TestExportHistory(t *testing.T) {
	s := newService(t)
	stdout, stderr, err := s.run(t, "export", "history", "--market", "first", "--interval", "1w")
	if err != nil {
		t.Fatal(err)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing beside the data when it goes to stdout", stderr)
	}
	records := readCSV(t, stdout)
	// Both outcomes, each paged to its end before the next is begun.
	wantCells(t, records, "outcome", "Yes", "Yes", "Yes", "No", "No", "No")
	wantCells(t, records, "outcome_index", "0", "0", "0", "1", "1", "1")
	wantCells(t, records, "token_id", "11", "11", "11", "12", "12", "12")
	wantCells(t, records, "price", "0.5", "0.6", "0.61", "0.5", "0.6", "0.61")
	wantCells(t, records, "resolution_seconds", "3600", "3600", "0", "3600", "3600", "0")
	if got := cells(t, records, "timestamp")[0]; got != "2026-10-01T13:00:00Z" {
		t.Errorf("timestamp = %q", got)
	}
	if got := cells(t, records, "market_slug")[0]; got != "first" {
		t.Errorf("market_slug = %q", got)
	}

	if n := len(s.asked("/gamma/markets/slug/first")); n != 1 {
		t.Errorf("%d lookups of the market by its slug, want 1", n)
	}
	asked := s.asked("/data/prices-history")
	if len(asked) != 4 {
		t.Fatalf("%d history requests, want two pages for each of two outcomes", len(asked))
	}
	wantParams(t, asked[0], map[string]string{"token_id": "11", "interval": "1w", "limit": "10000", "cursor": ""})
	wantParams(t, asked[1], map[string]string{"token_id": "11", "interval": "1w", "cursor": "page2"})
	wantParams(t, asked[2], map[string]string{"token_id": "12", "cursor": ""})
}

func TestExportHistoryOfOneOutcome(t *testing.T) {
	for _, outcome := range []string{"no", "1"} {
		s := newService(t)
		// A number names the market by its ID.
		stdout, _, err := s.run(t, "export", "history", "--market", "1", "--outcome", outcome, "--limit", "1")
		if err != nil {
			t.Fatal(err)
		}
		records := readCSV(t, stdout)
		wantCells(t, records, "outcome", "No")
		if n := len(s.asked("/gamma/markets/1")); n != 1 {
			t.Errorf("%d lookups of the market by its ID, want 1", n)
		}
		asked := s.asked("/data/prices-history")
		if len(asked) != 1 {
			t.Fatalf("--outcome %s: %d history requests, want only the first page of the one outcome", outcome, len(asked))
		}
		// All of it unless told otherwise, and one row more than the limit.
		wantParams(t, asked[0], map[string]string{"token_id": "12", "interval": "max", "limit": "2"})
	}
}

func TestExportTrades(t *testing.T) {
	s := newService(t)
	path := filepath.Join(t.TempDir(), "trades.csv")
	_, stderr, err := s.run(t, "export", "trades", "--market", "first",
		"--since", "2026-09-01", "--until", "2026-10-03T12:00:00Z", "-o", path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr, "wrote 3 rows of trades to "+path) {
		t.Errorf("stderr = %q", stderr)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	records := readCSV(t, string(b))
	wantCells(t, records, "side", "BUY", "SELL", "SELL")
	wantCells(t, records, "transaction_hash", "0xa", "0xb", "0xc")
	wantCells(t, records, "timestamp", "2026-10-02T13:17:45Z", "2026-10-02T13:17:45Z", "2026-10-02T13:17:45Z")

	asked := s.asked("/data/trades")
	if len(asked) != 2 {
		t.Fatalf("%d trades requests, want two pages", len(asked))
	}
	wantParams(t, asked[0], map[string]string{
		"condition": "0x1", "limit": "1000", "start": "1788220800", "end": "1791028800", "cursor": "",
	})
	wantParams(t, asked[1], map[string]string{"condition": "0x1", "cursor": "page2"})
}

// The service takes the bounds of the trades and ignores them, so they are
// applied to what it sends: all the trades it has are of 2026-10-02 13:17:45.
func TestExportTradesBounds(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		pages int
	}{
		{"until before them: none, and every page read", []string{"--until", "2026-10-02T13:17:45Z"}, 2},
		{"since after them: none, and no page past the first", []string{"--since", "2026-10-03"}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newService(t)
			stdout, _, err := s.run(t, append([]string{"export", "trades", "--market", "first"}, tt.args...)...)
			if err != nil {
				t.Fatal(err)
			}
			if records := readCSV(t, stdout); len(records) != 1 {
				t.Errorf("%d records, want the header alone", len(records))
			}
			if n := len(s.asked("/data/trades")); n != tt.pages {
				t.Errorf("%d trades requests, want %d", n, tt.pages)
			}
		})
	}
}

func TestExtendTrades(t *testing.T) {
	s := newService(t)
	path := filepath.Join(t.TempDir(), "trades.csv")
	if _, _, err := s.run(t, "export", "trades", "--market", "first", "-o", path); err != nil {
		t.Fatal(err)
	}
	_, stderr, err := s.run(t, "export", "trades", "--market", "first", "--extend", "-o", path)
	if err != nil {
		t.Fatal(err)
	}
	if want := "extended trades in " + path + ": 0 new rows, 3 in all"; !strings.Contains(stderr, want) {
		t.Errorf("stderr = %q, want it to say %q", stderr, want)
	}
	asked := s.asked("/data/trades")
	// The newest trade the file holds is where the extension starts.
	wantParams(t, asked[len(asked)-2], map[string]string{"start": "1790947065", "cursor": ""})

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	wantCells(t, readCSV(t, string(b)), "transaction_hash", "0xa", "0xb", "0xc")
}

func TestExportBook(t *testing.T) {
	s := newService(t)
	stdout, _, err := s.run(t, "export", "book", "--market", "first", "--outcome", "Yes")
	if err != nil {
		t.Fatal(err)
	}
	records := readCSV(t, stdout)
	// Each side best first, whatever order the service sent it in.
	wantCells(t, records, "side", "bid", "bid", "ask", "ask")
	wantCells(t, records, "level", "1", "2", "1", "2")
	wantCells(t, records, "price", "0.59", "0.58", "0.61", "0.62")
	wantCells(t, records, "size", "20", "10", "40", "30")
	wantCells(t, records, "outcome", "Yes", "Yes", "Yes", "Yes")
	wantParams(t, s.asked("/clob/book")[0], map[string]string{"token_id": "11"})
}

// The second outcome of the stand-in has no book, as no outcome of a market
// that has stopped trading does.
func TestExportBookOfAMarketNotTrading(t *testing.T) {
	s := newService(t)
	path := filepath.Join(t.TempDir(), "book.csv")
	_, _, err := s.run(t, "export", "book", "--market", "first", "-o", path)
	if err == nil || !strings.Contains(err.Error(), "has no order book") {
		t.Fatalf("error = %v, want one that says there is no order book", err)
	}
	if _, statErr := os.Stat(path); statErr == nil {
		t.Error("a file was written for a book that could not be read whole")
	}
}

func TestExportOfAnUnknownMarketOrOutcome(t *testing.T) {
	s := newService(t)
	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"export", "trades", "--market", "nope"}, `no market with the slug or ID "nope"`},
		{[]string{"export", "history", "--market", "99"}, `no market with the slug or ID "99"`},
		{[]string{"export", "book", "--market", "999999999999"}, `no market with the slug or ID "999999999999"`},
		{[]string{"export", "history", "--market", "first", "--outcome", "maybe"}, `no outcome "maybe": it has Yes, No`},
		{[]string{"export", "book", "--market", "first", "--outcome", "2"}, `no outcome "2"`},
	} {
		stdout, _, err := s.run(t, tt.args...)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%v: error = %v, want it to say %q", tt.args, err, tt.want)
		}
		if stdout != "" {
			t.Errorf("%v: stdout = %q, want nothing", tt.args, stdout)
		}
	}
	if n := len(s.asked("/data/prices-history")) + len(s.asked("/clob/book")) + len(s.asked("/data/trades")); n != 0 {
		t.Errorf("%d requests for data that was never going to be written", n)
	}
}

func TestOutcomesOf(t *testing.T) {
	m := &api.Market{Slug: "m", Outcomes: []api.Outcome{{Label: "Yes", TokenID: "1"}, {Label: "No", TokenID: "2"}}}
	for name, want := range map[string][]int{"": {0, 1}, "YES": {0}, "no": {1}, "0": {0}} {
		got, err := outcomesOf(m, name)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("outcomesOf(%q) = %v, %v; want %v", name, got, err, want)
		}
	}
	// A market that has not opened is listed with outcomes but no tokens.
	unopened := &api.Market{Slug: "m", Outcomes: []api.Outcome{{Label: "Yes"}, {Label: "No"}}}
	if _, err := outcomesOf(unopened, ""); err == nil || !strings.Contains(err.Error(), "not opened") {
		t.Errorf("error = %v, want one that says the market has not opened", err)
	}
}
