package export

import (
	"bytes"
	"context"
	"encoding/csv"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/parquet"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"

	"github.com/farrellm/polymarket/internal/api"
)

// A Parquet export holds the cells of a raw CSV export of the same rows,
// empty cells included.
func TestParquetHoldsTheCellsOfTheCSV(t *testing.T) {
	dir := t.TempDir()
	csvs := goldenDatasets(t)
	for i, d := range goldenDatasets(t) {
		t.Run(d.Name(), func(t *testing.T) {
			want, _ := export(t, csvs[i], Options{Raw: true})
			path := filepath.Join(dir, d.Name()+".parquet")
			sum, err := File(context.Background(), d, path, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if sum.Rows != len(want)-1 {
				t.Errorf("summary says %d rows, want %d", sum.Rows, len(want)-1)
			}
			if got := readBack(t, path); !slices.EqualFunc(got, want, slices.Equal) {
				t.Errorf("read back\n%q\nwant\n%q", got, want)
			}
		})
	}
}

func TestParquetIsNeverGuarded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "markets.parquet")
	markets := decode[api.Market](t, goldenMarkets)[3:]
	if _, err := File(context.Background(), Markets(Loaded(markets)), path, Options{}); err != nil {
		t.Fatal(err)
	}
	records := readBack(t, path)
	wantCells(t, records, "slug", "-odd")
	wantCells(t, records, "event_title", "@odd: the event")
}

func TestParquetColumnsAreTyped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.parquet")
	if _, err := File(context.Background(), goldenDatasets(t)[3], path, Options{}); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	tbl, err := pqarrow.ReadTable(context.Background(), f, parquet.NewReaderProperties(memory.DefaultAllocator),
		pqarrow.ArrowReadProperties{}, memory.DefaultAllocator)
	if err != nil {
		t.Fatal(err)
	}
	defer tbl.Release()

	var got []string
	for _, f := range tbl.Schema().Fields() {
		got = append(got, f.Name+" "+f.Type.String())
	}
	want := []string{
		"market_id utf8", "market_slug utf8", "question utf8", "condition_id utf8",
		"outcome_index int64", "outcome utf8", "token_id utf8",
		"timestamp " + (&arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}).String(),
		"price float64", "resolution_seconds int64",
	}
	if !slices.Equal(got, want) {
		t.Errorf("schema\n%q\nwant\n%q", got, want)
	}
}

func TestExtendRefusesACSVNamedParquet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.parquet")
	var buf bytes.Buffer
	cw := csv.NewWriter(&buf)
	_ = cw.Write(header(eventColumns))
	cw.Flush()
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Extend(context.Background(), rowsOf("events", map[string]string{"id": "1"}), path, Options{})
	if err == nil || !strings.Contains(err.Error(), "does not read as Parquet") {
		t.Errorf("err = %v, want one saying it is not Parquet", err)
	}
}

func TestParquetRefusesACellNotOfItsKind(t *testing.T) {
	path := filepath.Join(t.TempDir(), "markets.parquet")
	_, err := File(context.Background(), rowsOf("markets", map[string]string{"id": "1", "volume": "lots"}), path, Options{})
	if err == nil || !strings.Contains(err.Error(), "volume") {
		t.Errorf("err = %v, want one naming the column", err)
	}
	wantFiles(t, filepath.Dir(path))
}
