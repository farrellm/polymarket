package export

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strings"
)

// Options adjusts one export. The zero value writes every row.
type Options struct {
	// Limit is the most rows to write; zero means all of them.
	Limit int
	// Raw writes text cells exactly as they are, without the guard against
	// a spreadsheet evaluating them.
	Raw bool
	// Progress, if set, is called after each row with the number written so
	// far. It is called from the goroutine that runs the export.
	Progress func(rows int)
}

// Summary reports what an export wrote.
type Summary struct {
	Dataset string
	// Rows does not count the header.
	Rows int
	// Capped reports that the limit was reached with rows still to come.
	Capped bool
	// Notes are the dataset's own remarks on what it left out.
	Notes []string
}

// Run writes the dataset to w as CSV: RFC 4180, UTF-8, \n line endings and
// always a header row. It stops at the first error, including ctx being
// cancelled, and reports how far it got.
func Run(ctx context.Context, d Dataset, w io.Writer, o Options) (Summary, error) {
	sum := Summary{Dataset: d.Name()}
	columns := d.Columns()
	cw := csv.NewWriter(w)

	cells := make([]string, len(columns))
	for i, c := range columns {
		cells[i] = c.Name
	}
	if err := cw.Write(cells); err != nil {
		return sum, err
	}

	for row, err := range d.Rows() {
		if err != nil {
			return sum, err
		}
		if err := ctx.Err(); err != nil {
			return sum, err
		}
		if o.Limit > 0 && sum.Rows == o.Limit {
			sum.Capped = true
			break
		}
		if len(row) != len(columns) {
			return sum, fmt.Errorf("export %s: a row has %d cells for %d columns", d.Name(), len(row), len(columns))
		}
		for i, cell := range row {
			if !o.Raw && columns[i].Kind == Text {
				cell = guard(cell)
			}
			cells[i] = cell
		}
		if err := cw.Write(cells); err != nil {
			return sum, err
		}
		sum.Rows++
		if o.Progress != nil {
			o.Progress(sum.Rows)
		}
	}

	cw.Flush()
	if err := cw.Error(); err != nil {
		return sum, err
	}
	sum.Notes = d.Notes()
	return sum, nil
}

// guard keeps a spreadsheet from evaluating a text cell as a formula, by
// putting an apostrophe in front of one that starts like a formula.
func guard(cell string) string {
	if cell != "" && strings.IndexByte("=+-@", cell[0]) >= 0 {
		return "'" + cell
	}
	return cell
}

// File writes the dataset to path, replacing any file already there. The
// rows go to path.tmp alongside it and are renamed into place only once all
// of them are written, so an export that fails or is cancelled leaves
// neither a partial file nor a damaged earlier one.
func File(ctx context.Context, d Dataset, path string, o Options) (sum Summary, err error) {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return Summary{Dataset: d.Name()}, err
	}
	defer func() {
		if err != nil {
			_ = f.Close()
			_ = os.Remove(tmp)
		}
	}()

	if sum, err = Run(ctx, d, f, o); err != nil {
		return sum, err
	}
	if err = f.Close(); err != nil {
		return sum, err
	}
	if err = os.Rename(tmp, path); err != nil {
		return sum, err
	}
	return sum, nil
}
