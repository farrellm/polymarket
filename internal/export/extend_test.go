package export

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/farrellm/polymarket/internal/api"
)

// columnsOf are the columns of the dataset of this name.
var columnsOf = map[string][]Column{
	"events":   eventColumns,
	"markets":  marketColumns,
	"outcomes": outcomeColumns,
	"history":  historyColumns,
	"trades":   tradeColumns,
	"book":     bookColumns,
}

// rowsOf is a dataset of the given name whose rows set only the cells named,
// each a map from column to cell, the rest empty. It counts the rows read.
func rowsOf(name string, rows ...map[string]string) Dataset {
	columns := columnsOf[name]
	return &table{
		name:    name,
		columns: columns,
		rows: func(yield func([]string, error) bool) {
			for _, r := range rows {
				row := make([]string, len(columns))
				for i, c := range columns {
					row[i] = r[c.Name]
				}
				if !yield(row, nil) {
					return
				}
			}
		},
	}
}

// extend extends the file at path with d and reads the file back.
func extend(t *testing.T, d Dataset, path string) ([][]string, Summary) {
	t.Helper()
	sum, err := Extend(context.Background(), d, path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return readBack(t, path), sum
}

// readBack reads an export back, Parquet or CSV by its name, header first.
func readBack(t *testing.T, path string) [][]string {
	t.Helper()
	head, rows, err := readFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return append([][]string{head}, rows...)
}

// formats are the extensions of the formats a file can be extended in.
var formats = []string{".csv", ".parquet"}

func TestMergesNameColumnsOfTheirDatasets(t *testing.T) {
	for name, spec := range merges {
		columns, ok := columnsOf[name]
		if !ok {
			t.Errorf("%s: no such dataset", name)
			continue
		}
		names := make([]string, len(columns))
		for i, c := range columns {
			names[i] = c.Name
		}
		for _, k := range spec.key {
			if !slices.Contains(names, k) {
				t.Errorf("%s: key column %s is not one of its columns", name, k)
			}
		}
		for _, s := range spec.order {
			if !slices.Contains(names, s.column) {
				t.Errorf("%s: sort column %s is not one of its columns", name, s.column)
			}
		}
	}
}

func TestExtendCreatesAMissingFile(t *testing.T) {
	for _, ext := range formats {
		t.Run(ext, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "events"+ext)
			records, sum := extend(t, rowsOf("events", map[string]string{"id": "1"}), path)
			wantCells(t, records, "id", "1")
			if sum.Added != 1 || sum.Total != 1 || sum.Rows != 1 {
				t.Errorf("summary %+v, want 1 row added, 1 in all", sum)
			}
			wantFiles(t, filepath.Dir(path), "events"+ext)
		})
	}
}

func TestExtendUpsertsByKey(t *testing.T) {
	for _, ext := range formats {
		t.Run(ext, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "markets"+ext)
			extend(t, rowsOf("markets",
				map[string]string{"id": "1", "question": "One?", "volume": "10"},
				map[string]string{"id": "2", "question": "Two?", "volume": "20"},
			), path)

			// The second is fetched again with a new figure, a third is new, and the
			// first is no longer listed (closed, say) and stays as it was.
			records, sum := extend(t, rowsOf("markets",
				map[string]string{"id": "3", "question": "Three?", "volume": "30"},
				map[string]string{"id": "2", "question": "Two?", "volume": "25"},
			), path)
			wantCells(t, records, "id", "1", "2", "3")
			wantCells(t, records, "volume", "10", "25", "30")
			if sum.Rows != 2 || sum.Added != 1 || sum.Total != 3 {
				t.Errorf("summary %+v, want 2 fetched, 1 added, 3 in all", sum)
			}
		})
	}
}

func TestExtendTradesKeepsThemNewestFirst(t *testing.T) {
	for _, ext := range formats {
		t.Run(ext, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "trades"+ext)
			fill := func(ts, hash, size string) map[string]string {
				return map[string]string{"timestamp": ts, "transaction_hash": hash, "size": size}
			}
			extend(t, rowsOf("trades",
				fill("2026-10-02T12:00:00Z", "0xb", "5"),
				fill("2026-10-02T12:00:00Z", "0xb", "6"),
				fill("2026-10-01T12:00:00Z", "0xa", "1"),
			), path)

			// What was fetched overlaps the newest second of the file: two fills of
			// one transaction, the same two as before, and nothing else from then.
			records, sum := extend(t, rowsOf("trades",
				fill("2026-10-03T08:00:00Z", "0xd", "9"),
				fill("2026-10-02T18:00:00Z", "0xc", "7"),
				fill("2026-10-02T12:00:00Z", "0xb", "5"),
				fill("2026-10-02T12:00:00Z", "0xb", "6"),
			), path)
			wantCells(t, records, "transaction_hash", "0xd", "0xc", "0xb", "0xb", "0xa")
			wantCells(t, records, "size", "9", "7", "5", "6", "1")
			if sum.Added != 2 || sum.Total != 5 {
				t.Errorf("summary %+v, want 2 added, 5 in all", sum)
			}
		})
	}
}

func TestExtendHistoryKeepsEachOutcomeOldestFirst(t *testing.T) {
	for _, ext := range formats {
		t.Run(ext, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "history"+ext)
			point := func(outcome, ts, res string) map[string]string {
				return map[string]string{"market_id": "7", "outcome_index": outcome, "token_id": "t" + outcome,
					"timestamp": ts, "resolution_seconds": res, "price": "0.5"}
			}
			extend(t, rowsOf("history",
				point("0", "2026-09-01T00:00:00Z", "43200"),
				point("0", "2026-10-01T00:00:00Z", "0"),
				point("1", "2026-09-01T00:00:00Z", "43200"),
				point("10", "2026-09-01T00:00:00Z", "43200"),
			), path)

			// A finer interval later: a point at a moment the file has, but of
			// another width, is another point.
			records, sum := extend(t, rowsOf("history",
				point("0", "2026-09-01T00:00:00Z", "300"),
				point("0", "2026-10-02T00:00:00Z", "300"),
				point("1", "2026-10-02T00:00:00Z", "300"),
			), path)
			wantCells(t, records, "outcome_index", "0", "0", "0", "0", "1", "1", "10")
			wantCells(t, records, "timestamp",
				"2026-09-01T00:00:00Z", "2026-09-01T00:00:00Z", "2026-10-01T00:00:00Z", "2026-10-02T00:00:00Z",
				"2026-09-01T00:00:00Z", "2026-10-02T00:00:00Z", "2026-09-01T00:00:00Z")
			// The sort is stable: the old point of a moment stays before the new.
			wantCells(t, records, "resolution_seconds", "43200", "300", "0", "300", "43200", "300", "43200")
			if sum.Added != 3 {
				t.Errorf("summary %+v, want 3 added", sum)
			}
		})
	}
}

func TestExtendBookAddsASnapshot(t *testing.T) {
	for _, ext := range formats {
		t.Run(ext, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "book"+ext)
			level := func(ts, side, n string) map[string]string {
				return map[string]string{"token_id": "t", "timestamp": ts, "side": side, "level": n}
			}
			extend(t, rowsOf("book", level("2026-10-01T00:00:00Z", "bid", "1"), level("2026-10-01T00:00:00Z", "ask", "1")), path)
			records, _ := extend(t, rowsOf("book", level("2026-10-02T00:00:00Z", "bid", "1"), level("2026-10-02T00:00:00Z", "ask", "1")), path)
			wantCells(t, records, "timestamp",
				"2026-10-01T00:00:00Z", "2026-10-01T00:00:00Z", "2026-10-02T00:00:00Z", "2026-10-02T00:00:00Z")
			wantCells(t, records, "side", "bid", "ask", "bid", "ask")
		})
	}
}

func TestExtendComparesGuardedCells(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.csv")
	extend(t, rowsOf("events", map[string]string{"id": "=1", "title": "-x"}), path)
	records, sum := extend(t, rowsOf("events", map[string]string{"id": "=1", "title": "-y"}), path)
	wantCells(t, records, "id", "'=1")
	wantCells(t, records, "title", "'-y")
	if sum.Added != 0 {
		t.Errorf("summary %+v, want the row matched, not added", sum)
	}
}

func TestExtendRefuses(t *testing.T) {
	dir := t.TempDir()
	other := filepath.Join(dir, "events.csv")
	extend(t, rowsOf("events", map[string]string{"id": "1"}), other)
	before := readBack(t, other)

	fetched := 0
	tests := []struct {
		name string
		d    Dataset
		want string
	}{
		{"a file of another dataset", numberedAs("markets", &fetched), "not an export of markets"},
		{"a dataset that cannot be extended", Tags(Loaded([]TagStat{{Tag: api.Tag{ID: "1"}}})), "cannot be extended"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Extend(context.Background(), tt.d, other, Options{})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want one saying %q", err, tt.want)
			}
		})
	}
	if fetched != 0 {
		t.Errorf("%d pages fetched for a file that cannot be extended", fetched)
	}
	if got := readBack(t, other); !slices.EqualFunc(got, before, slices.Equal) {
		t.Errorf("the file now holds %q, want %q", got, before)
	}
	wantFiles(t, dir, "events.csv")
}

// numberedAs is numbered under another name, with that dataset's columns.
func numberedAs(name string, fetched *int) Dataset {
	d := numbered(3, fetched).(*table)
	d.name = name
	d.columns = columnsOf[name]
	return d
}

func TestExtendLeavesTheFileOnFailure(t *testing.T) {
	for _, ext := range formats {
		t.Run(ext, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "events"+ext)
			extend(t, rowsOf("events", map[string]string{"id": "1"}), path)
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}

			broken := rowsOf("events", map[string]string{"id": "2"}).(*table)
			rows := broken.rows
			broken.rows = func(yield func([]string, error) bool) {
				for row := range rows {
					if !yield(row, nil) {
						return
					}
				}
				yield(nil, errors.New("boom"))
			}
			if _, err := Extend(context.Background(), broken, path, Options{}); err == nil {
				t.Fatal("no error, want the extension to fail")
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(want) {
				t.Errorf("the file now holds %q, want %q", got, want)
			}
			wantFiles(t, filepath.Dir(path), "events"+ext)
		})
	}
}

func TestNewest(t *testing.T) {
	for _, ext := range formats {
		t.Run(ext, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "trades"+ext)
			if got, err := Newest(path, "timestamp"); err != nil || !got.IsZero() {
				t.Errorf("a missing file: %v, %v, want the zero time", got, err)
			}
			extend(t, rowsOf("trades",
				map[string]string{"timestamp": "2026-10-01T00:00:00Z"},
				map[string]string{"timestamp": "2026-10-03T00:00:00Z"},
				map[string]string{"timestamp": ""},
				map[string]string{"timestamp": "2026-10-02T00:00:00Z"},
			), path)
			got, err := Newest(path, "timestamp")
			if err != nil {
				t.Fatal(err)
			}
			if want := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
				t.Errorf("newest = %v, want %v", got, want)
			}
			if _, err := Newest(path, "when"); err == nil {
				t.Error("no error for a column the file does not have")
			}
		})
	}
}

// trades is a source of three pages of trades, newest first, a day apart,
// that counts the pages asked for.
type trades struct{ asked int }

func (s *trades) Trades(_ context.Context, q api.TradesQuery) ([]api.Trade, string, error) {
	s.asked++
	day := func(d int) api.Trade {
		return api.Trade{Time: api.Time{Time: time.Date(2026, 10, d, 12, 0, 0, 0, time.UTC)}, TransactionHash: "0x" + string(rune('0'+d))}
	}
	switch q.Cursor {
	case "":
		return []api.Trade{day(6), day(5)}, "2", nil
	case "2":
		return []api.Trade{day(4), day(3)}, "3", nil
	default:
		return []api.Trade{day(2), day(1)}, "", nil
	}
}

func TestTradePages(t *testing.T) {
	at := func(d int) time.Time { return time.Date(2026, 10, d, 12, 0, 0, 0, time.UTC) }
	tests := []struct {
		name       string
		start, end time.Time
		want       string
		asked      int
	}{
		{"no bounds: every trade", time.Time{}, time.Time{}, "654321", 3},
		{"from the 4th: no page past it", at(4), time.Time{}, "654", 2},
		{"before the 5th: the end is not included", time.Time{}, at(5), "4321", 3},
		{"both", at(3), at(5), "43", 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := &trades{}
			var got []string
			for page, err := range TradePages(context.Background(), src, api.TradesQuery{Start: tt.start, End: tt.end}) {
				if err != nil {
					t.Fatal(err)
				}
				for _, tr := range page {
					got = append(got, strings.TrimPrefix(tr.TransactionHash, "0x"))
				}
			}
			if strings.Join(got, "") != tt.want {
				t.Errorf("trades %q, want %s", got, tt.want)
			}
			if src.asked != tt.asked {
				t.Errorf("%d pages asked for, want %d", src.asked, tt.asked)
			}
		})
	}
}
