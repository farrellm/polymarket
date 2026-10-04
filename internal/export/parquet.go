package export

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/parquet"
	"github.com/apache/arrow-go/v18/parquet/compress"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
)

// rowGroup is how many rows go into one row group of a Parquet file.
const rowGroup = 1 << 16

// isParquet reports whether path names a Parquet file, which File and Extend
// write instead of CSV.
func isParquet(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".parquet")
}

// arrowType is the type of a column of the kind in Parquet. Times are
// microseconds in UTC, which DuckDB reads as TIMESTAMPTZ.
func arrowType(k Kind) arrow.DataType {
	switch k {
	case Number:
		return arrow.PrimitiveTypes.Float64
	case Integer:
		return arrow.PrimitiveTypes.Int64
	case Bool:
		return arrow.FixedWidthTypes.Boolean
	case Time:
		return &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}
	default:
		return arrow.BinaryTypes.String
	}
}

// schemaOf is the Parquet schema of the columns, in their order, every one
// of them nullable: an empty cell is a null.
func schemaOf(columns []Column) *arrow.Schema {
	fields := make([]arrow.Field, len(columns))
	for i, c := range columns {
		fields[i] = arrow.Field{Name: c.Name, Type: arrowType(c.Kind), Nullable: true}
	}
	return arrow.NewSchema(fields, nil)
}

// RunParquet writes the dataset to w as Parquet, compressed with zstd, the
// columns typed by their kinds. Text is never guarded: a typed file is not
// for a spreadsheet. Like Run, it stops at the first error and reports how
// far it got.
func RunParquet(ctx context.Context, d Dataset, w io.Writer, o Options) (Summary, error) {
	sum := Summary{Dataset: d.Name()}
	o.Raw = true
	pw, err := newParquetWriter(d.Columns(), w)
	if err != nil {
		return sum, err
	}
	if err := each(ctx, d, o, &sum, pw.add); err != nil {
		pw.abandon()
		return sum, err
	}
	if err := pw.close(); err != nil {
		return sum, err
	}
	sum.Notes = d.Notes()
	return sum, nil
}

// writeParquet writes rows of cells, as each would hand them over, to w.
func writeParquet(w io.Writer, columns []Column, rows [][]string) error {
	pw, err := newParquetWriter(columns, w)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := pw.add(row); err != nil {
			pw.abandon()
			return err
		}
	}
	return pw.close()
}

// parquetWriter turns rows of cells into row groups of a Parquet file.
type parquetWriter struct {
	columns []Column
	b       *array.RecordBuilder
	fw      *pqarrow.FileWriter
	rows    int
}

func newParquetWriter(columns []Column, w io.Writer) (*parquetWriter, error) {
	schema := schemaOf(columns)
	props := parquet.NewWriterProperties(
		parquet.WithCompression(compress.Codecs.Zstd),
		parquet.WithMaxRowGroupLength(rowGroup),
	)
	// Hidden behind a bare io.Writer, since the file writer closes one that
	// is a Closer, and the file is its caller's to close.
	fw, err := pqarrow.NewFileWriter(schema, struct{ io.Writer }{w}, props, pqarrow.DefaultWriterProps())
	if err != nil {
		return nil, err
	}
	return &parquetWriter{
		columns: columns,
		b:       array.NewRecordBuilder(memory.DefaultAllocator, schema),
		fw:      fw,
	}, nil
}

// add appends one row, writing a row group once there are enough.
func (pw *parquetWriter) add(row []string) error {
	for i, cell := range row {
		if err := appendCell(pw.b.Field(i), pw.columns[i], cell); err != nil {
			return err
		}
	}
	pw.rows++
	if pw.rows == rowGroup {
		return pw.flush()
	}
	return nil
}

func (pw *parquetWriter) flush() error {
	if pw.rows == 0 {
		return nil
	}
	rec := pw.b.NewRecordBatch()
	defer rec.Release()
	pw.rows = 0
	return pw.fw.Write(rec)
}

// close writes what is left and the file's footer.
func (pw *parquetWriter) close() error {
	defer pw.b.Release()
	if err := pw.flush(); err != nil {
		_ = pw.fw.Close()
		return err
	}
	return pw.fw.Close()
}

// abandon releases the writer after an error; what it wrote is to be thrown
// away.
func (pw *parquetWriter) abandon() {
	pw.b.Release()
	_ = pw.fw.Close()
}

// appendCell parses a cell as its column's kind onto the builder. An empty
// cell is a null.
func appendCell(b array.Builder, c Column, cell string) error {
	if cell == "" {
		b.AppendNull()
		return nil
	}
	bad := func(err error) error {
		return fmt.Errorf("column %s: %q is not a %s: %w", c.Name, cell, kindName(c.Kind), err)
	}
	switch b := b.(type) {
	case *array.Float64Builder:
		f, err := strconv.ParseFloat(cell, 64)
		if err != nil {
			return bad(err)
		}
		b.Append(f)
	case *array.Int64Builder:
		n, err := strconv.ParseInt(cell, 10, 64)
		if err != nil {
			return bad(err)
		}
		b.Append(n)
	case *array.BooleanBuilder:
		v, err := strconv.ParseBool(cell)
		if err != nil {
			return bad(err)
		}
		b.Append(v)
	case *array.TimestampBuilder:
		t, err := time.Parse(time.RFC3339, cell)
		if err != nil {
			return bad(err)
		}
		b.AppendTime(t)
	case *array.StringBuilder:
		b.Append(cell)
	default:
		return fmt.Errorf("column %s: no way to write a %s", c.Name, kindName(c.Kind))
	}
	return nil
}

func kindName(k Kind) string {
	switch k {
	case Number:
		return "number"
	case Integer:
		return "whole number"
	case Bool:
		return "boolean"
	case Time:
		return "time"
	default:
		return "text"
	}
}

// readParquet reads a Parquet file back as a header and rows of cells, each
// written as the export would write it, so that they compare with fetched
// rows. It reads what DuckDB writes too: any string or timestamp, and any
// width of integer.
func readParquet(path string) (head []string, rows [][]string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = f.Close() }()
	tbl, err := pqarrow.ReadTable(context.Background(), f, parquet.NewReaderProperties(memory.DefaultAllocator),
		pqarrow.ArrowReadProperties{}, memory.DefaultAllocator)
	if err != nil {
		return nil, nil, fmt.Errorf("%s does not read as Parquet: %w", path, err)
	}
	defer tbl.Release()

	for _, f := range tbl.Schema().Fields() {
		head = append(head, f.Name)
	}
	rows = make([][]string, 0, tbl.NumRows())
	tr := array.NewTableReader(tbl, rowGroup)
	defer tr.Release()
	for tr.Next() {
		rec := tr.RecordBatch()
		start := len(rows)
		for range rec.NumRows() {
			rows = append(rows, make([]string, len(head)))
		}
		for j, col := range rec.Columns() {
			for i := range col.Len() {
				if rows[start+i][j], err = cellOf(col, i); err != nil {
					return nil, nil, fmt.Errorf("%s, column %s: %w", path, head[j], err)
				}
			}
		}
	}
	return head, rows, nil
}

// cellOf writes a value of a column read from Parquet as a cell.
func cellOf(col arrow.Array, i int) (string, error) {
	if col.IsNull(i) {
		return "", nil
	}
	switch a := col.(type) {
	case *array.String:
		return a.Value(i), nil
	case *array.LargeString:
		return a.Value(i), nil
	case *array.StringView:
		return a.Value(i), nil
	case *array.Float64:
		return strconv.FormatFloat(a.Value(i), 'f', -1, 64), nil
	case *array.Int64:
		return strconv.FormatInt(a.Value(i), 10), nil
	case *array.Int32:
		return strconv.FormatInt(int64(a.Value(i)), 10), nil
	case *array.Boolean:
		return strconv.FormatBool(a.Value(i)), nil
	case *array.Timestamp:
		unit := a.DataType().(*arrow.TimestampType).Unit
		return a.Value(i).ToTime(unit).UTC().Format(time.RFC3339), nil
	default:
		return "", fmt.Errorf("a column of %s is not one an export writes", col.DataType())
	}
}
