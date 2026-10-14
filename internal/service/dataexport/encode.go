package dataexport

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"reflect"
	"strconv"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/logging"
)

// Format is the file format of the tables in the archive.
type Format string

const (
	FormatCSV   Format = "csv"
	FormatJSONL Format = "jsonl"
)

func (f Format) valid() bool { return f == FormatCSV || f == FormatJSONL }

// rowEncoder writes the rows of one table to one archive file.
type rowEncoder interface {
	// write encodes a row already passed through prepareRow.
	write(row []any) error
	flush() error
}

func newEncoder(f Format, w io.Writer, columns []Column) (rowEncoder, error) {
	switch f {
	case FormatCSV:
		e := &csvEncoder{w: csv.NewWriter(w), record: make([]string, len(columns))}
		names := make([]string, len(columns))
		for i, c := range columns {
			names[i] = c.Name
		}
		if err := e.w.Write(names); err != nil {
			return nil, fmt.Errorf("dataexport: write csv header: %w", err)
		}
		return e, nil
	case FormatJSONL:
		return &jsonlEncoder{w: w, columns: columns}, nil
	default:
		return nil, fmt.Errorf("dataexport: unknown format %q", f)
	}
}

// prepareRow validates row against columns and returns its values masked and
// normalized to nil, string, bool, int64 or float64 (JSON columns stay
// strings). Masking happens here, in the one path every row takes, so no
// collector can bypass it: a value under a sensitive column name is replaced
// whole, every string is masked for embedded secrets, and a JSON column is
// masked as a JSON document.
func prepareRow(columns []Column, row Row) ([]any, error) {
	if len(row) != len(columns) {
		return nil, fmt.Errorf("dataexport: row has %d values for %d columns", len(row), len(columns))
	}
	out := make([]any, len(row))
	for i, v := range row {
		n, err := normalize(v)
		if err != nil {
			return nil, fmt.Errorf("dataexport: column %q: %w", columns[i].Name, err)
		}
		switch s, isString := n.(string); {
		case n == nil:
		case logging.IsSensitiveKey(columns[i].Name):
			n = logging.RedactedValue
		case isString && columns[i].JSON:
			n = logging.MaskJSONText(s)
		case isString:
			n = logging.MaskString(s)
		}
		out[i] = n
	}
	return out, nil
}

func normalize(v any) (any, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case string, bool, int64:
		return x, nil
	case float64:
		return finite(x), nil
	case time.Time:
		return x.UTC().Format(time.RFC3339Nano), nil
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.String:
		return rv.String(), nil
	case reflect.Bool:
		return rv.Bool(), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int(), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		u := rv.Uint()
		if u > math.MaxInt64 {
			return nil, fmt.Errorf("unsigned value %d overflows int64", u)
		}
		return int64(u), nil
	case reflect.Float32, reflect.Float64:
		return finite(rv.Float()), nil
	}
	return nil, fmt.Errorf("unsupported value type %T", v)
}

// finite maps NaN and ±Inf, which neither JSON nor a downstream analysis
// tool can represent, to NULL.
func finite(f float64) any {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return nil
	}
	return f
}

type csvEncoder struct {
	w      *csv.Writer
	record []string
}

func (e *csvEncoder) write(row []any) error {
	for i, v := range row {
		switch x := v.(type) {
		case nil:
			e.record[i] = ""
		case string:
			e.record[i] = x
		case bool:
			e.record[i] = strconv.FormatBool(x)
		case int64:
			e.record[i] = strconv.FormatInt(x, 10)
		case float64:
			e.record[i] = strconv.FormatFloat(x, 'g', -1, 64)
		}
	}
	return e.w.Write(e.record)
}

func (e *csvEncoder) flush() error {
	e.w.Flush()
	return e.w.Error()
}

type jsonlEncoder struct {
	w       io.Writer
	columns []Column
	line    bytes.Buffer
}

func (e *jsonlEncoder) write(row []any) error {
	e.line.Reset()
	e.line.WriteByte('{')
	for i, v := range row {
		if i > 0 {
			e.line.WriteByte(',')
		}
		if err := e.writeJSON(e.columns[i].Name); err != nil {
			return err
		}
		e.line.WriteByte(':')
		if s, ok := v.(string); ok && e.columns[i].JSON && json.Valid([]byte(s)) {
			// A JSON document column is nested, not double-encoded.
			if err := json.Compact(&e.line, []byte(s)); err != nil {
				return fmt.Errorf("dataexport: compact json column %q: %w", e.columns[i].Name, err)
			}
			continue
		}
		if err := e.writeJSON(v); err != nil {
			return err
		}
	}
	e.line.WriteString("}\n")
	_, err := e.w.Write(e.line.Bytes())
	return err
}

func (e *jsonlEncoder) writeJSON(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("dataexport: encode json value: %w", err)
	}
	e.line.Write(b)
	return nil
}

func (e *jsonlEncoder) flush() error { return nil }
