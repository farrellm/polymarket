package export

import (
	"cmp"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

// merging says how the rows of a dataset are merged into a file of it: what
// makes two rows the same row, and what order the file is kept in.
type merging struct {
	// key are the columns that identify a row; none means all of them.
	key []string
	// order is what the merged rows are sorted by, stably; none leaves the
	// rows of the file where they were and puts new ones at the end.
	order []sortKey
}

type sortKey struct {
	column string
	desc   bool
}

// merges are the datasets that can be extended. The tags are not among
// them: their figures are of one sample of events, and mean nothing added up
// across two.
var merges = map[string]merging{
	"events":   {key: []string{"id"}},
	"markets":  {key: []string{"id"}},
	"outcomes": {key: []string{"market_id", "outcome_index"}},
	// Points of different intervals may fall on the same moment: the width
	// of the bucket tells them apart.
	"history": {
		key:   []string{"token_id", "timestamp", "resolution_seconds"},
		order: []sortKey{{"market_id", false}, {"outcome_index", false}, {"timestamp", false}},
	},
	// One transaction may make several fills, so only a trade's every cell
	// identifies it.
	"trades": {order: []sortKey{{"timestamp", true}}},
	"book":   {key: []string{"token_id", "timestamp", "side", "level"}},
}

// Extend merges the rows of the dataset into the file at path, which an
// earlier export of the same dataset wrote, and writes the result back in its
// place. A fetched row replaces the row of the file it has the key of, where
// that row was; one the file does not hold is added. Rows of the file that
// were not fetched again stay. With no file at path it is File.
//
// The file is read before anything is fetched, so one of another dataset is
// refused without a request. The cells are compared as written, so a file is
// best extended with the same Raw it was written with.
func Extend(ctx context.Context, d Dataset, path string, o Options) (Summary, error) {
	sum := Summary{Dataset: d.Name()}
	spec, ok := merges[d.Name()]
	if !ok {
		return sum, fmt.Errorf("the %s dataset cannot be extended", d.Name())
	}
	old, err := readExport(path, d)
	if errors.Is(err, fs.ErrNotExist) {
		sum, err = File(ctx, d, path, o)
		sum.Added, sum.Total = sum.Rows, sum.Rows
		return sum, err
	}
	if err != nil {
		return sum, err
	}

	if isParquet(path) {
		// Parquet is written raw, so it is compared raw.
		o.Raw = true
	}
	var fetched [][]string
	if err := each(ctx, d, o, &sum, func(row []string) error {
		fetched = append(fetched, slices.Clone(row))
		return nil
	}); err != nil {
		return sum, err
	}
	merged, added := merge(d.Columns(), spec, old, fetched)
	sum.Added, sum.Total = added, len(merged)

	err = replace(path, func(w io.Writer) error {
		if isParquet(path) {
			return writeParquet(w, d.Columns(), merged)
		}
		cw := csv.NewWriter(w)
		if err := cw.Write(header(d.Columns())); err != nil {
			return err
		}
		if err := cw.WriteAll(merged); err != nil {
			return err
		}
		return cw.Error()
	})
	if err != nil {
		return sum, err
	}
	sum.Notes = d.Notes()
	return sum, nil
}

// readExport reads the rows of an export of d, checking that its columns are
// the dataset's.
func readExport(path string, d Dataset) ([][]string, error) {
	head, rows, err := readFile(path)
	if err != nil {
		return nil, err
	}
	if !slices.Equal(head, header(d.Columns())) {
		return nil, fmt.Errorf("%s is not an export of %s: its columns are not that dataset's", path, d.Name())
	}
	return rows, nil
}

// readFile reads an export, Parquet or CSV by its name, as its header and
// rows of cells. An empty CSV file has no header.
func readFile(path string) (head []string, rows [][]string, err error) {
	if isParquet(path) {
		return readParquet(path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = f.Close() }()
	records, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return nil, nil, fmt.Errorf("%s does not read as CSV: %w", path, err)
	}
	if len(records) == 0 {
		return nil, nil, nil
	}
	return records[0], records[1:], nil
}

// merge puts the fetched rows into the old ones, as Extend describes, and
// counts those that are new.
func merge(columns []Column, spec merging, old, fetched [][]string) (merged [][]string, added int) {
	key := indexes(columns, spec.key)
	keyOf := func(row []string) string {
		parts := make([]string, len(key))
		for i, c := range key {
			parts[i] = row[c]
		}
		return strings.Join(parts, "\x00")
	}

	merged = slices.Clone(old)
	at := make(map[string]int, len(old)+len(fetched))
	for i, row := range old {
		at[keyOf(row)] = i
	}
	for _, row := range fetched {
		k := keyOf(row)
		if i, ok := at[k]; ok {
			merged[i] = row
			continue
		}
		at[k] = len(merged)
		merged = append(merged, row)
		added++
	}

	if len(spec.order) > 0 {
		by := make([]int, len(spec.order))
		for i, s := range spec.order {
			by[i] = slices.IndexFunc(columns, func(c Column) bool { return c.Name == s.column })
		}
		slices.SortStableFunc(merged, func(a, b []string) int {
			for j, s := range spec.order {
				i := by[j]
				c := compareCells(columns[i].Kind, a[i], b[i])
				if s.desc {
					c = -c
				}
				if c != 0 {
					return c
				}
			}
			return 0
		})
	}
	return merged, added
}

// indexes are the positions of the named columns; no names is every column.
func indexes(columns []Column, names []string) []int {
	var out []int
	for i, c := range columns {
		if len(names) == 0 || slices.Contains(names, c.Name) {
			out = append(out, i)
		}
	}
	return out
}

// compareCells orders two cells of a column: numbers by value, an empty one
// first, and anything else as text, which is right for RFC 3339 times in UTC.
func compareCells(kind Kind, a, b string) int {
	if kind == Number || kind == Integer {
		x, xerr := strconv.ParseFloat(a, 64)
		y, yerr := strconv.ParseFloat(b, 64)
		if xerr == nil && yerr == nil {
			return cmp.Compare(x, y)
		}
	}
	return strings.Compare(a, b)
}

// Newest is the latest time in the named column of the export at path,
// Parquet or CSV by its name, or the zero time if there is no such file or
// the column has no times in it.
func Newest(path, column string) (time.Time, error) {
	head, rows, err := readFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return time.Time{}, nil
	}
	if err != nil || head == nil {
		return time.Time{}, err
	}
	i := slices.Index(head, column)
	if i < 0 {
		return time.Time{}, fmt.Errorf("%s has no %s column", path, column)
	}
	var newest time.Time
	for _, row := range rows {
		if t, err := time.Parse(time.RFC3339, row[i]); err == nil && t.After(newest) {
			newest = t
		}
	}
	return newest, nil
}
