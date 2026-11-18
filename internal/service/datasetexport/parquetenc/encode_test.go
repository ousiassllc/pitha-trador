package parquetenc_test

import (
	"bytes"
	"testing"

	"github.com/parquet-go/parquet-go"

	"github.com/ousiassllc/pitha-trador/internal/service/datasetexport/parquetenc"
)

var cols = []parquetenc.Column{
	{Name: "id", Type: parquetenc.Int64},
	{Name: "score", Type: parquetenc.Double},
	{Name: "flag", Type: parquetenc.Int64},
	{Name: "label", Type: parquetenc.String},
}

var rows = [][]string{
	{"1", "0.5", "1", "a"},
	{"2", "", "0", ""},
	{"3", "-12.25", "", "日本語,\"quoted\""},
}

// The same input gives the same bytes, however often and in whatever order the process runs it.
func TestEncodeIsDeterministic(t *testing.T) {
	first, err := parquetenc.Encode(cols, rows, "2")
	if err != nil {
		t.Fatal(err)
	}
	for range 5 {
		again, err := parquetenc.Encode(cols, rows, "2")
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first, again) {
			t.Fatal("two encodes of the same rows differ")
		}
	}
}

// The file reads back with the columns in order, the values as typed, and "" as null.
func TestEncodeRoundTrip(t *testing.T) {
	data, err := parquetenc.Encode(cols, rows, "2")
	if err != nil {
		t.Fatal(err)
	}
	f, err := parquet.OpenFile(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if f.NumRows() != int64(len(rows)) {
		t.Fatalf("rows %d, want %d", f.NumRows(), len(rows))
	}
	for i, c := range f.Schema().Fields() {
		if c.Name() != cols[i].Name || !c.Optional() {
			t.Errorf("column %d is %s optional=%v, want %s optional", i, c.Name(), c.Optional(), cols[i].Name)
		}
	}
	if v, ok := f.Lookup(parquetenc.SchemaKey); !ok || v != "2" {
		t.Errorf("schema version metadata = %q %v", v, ok)
	}
	reader := parquet.NewReader(f)
	defer func() { _ = reader.Close() }()
	buf := make([]parquet.Row, len(rows))
	n, _ := reader.ReadRows(buf)
	if n != len(rows) {
		t.Fatalf("read %d rows", n)
	}
	// row 2: score and label are null; row 3: flag is null.
	if !buf[1][1].IsNull() || !buf[1][3].IsNull() || !buf[2][2].IsNull() {
		t.Errorf("empty cells are not null: %v", buf[1:])
	}
	if buf[0][0].Int64() != 1 || buf[0][1].Double() != 0.5 || buf[0][2].Int64() != 1 || string(buf[0][3].ByteArray()) != "a" {
		t.Errorf("row 1 = %v", buf[0])
	}
	if string(buf[2][3].ByteArray()) != "日本語,\"quoted\"" || buf[2][1].Double() != -12.25 {
		t.Errorf("row 3 = %v", buf[2])
	}
}

// A cell that does not parse as its column's type is an error, never a different value.
func TestEncodeRejectsAMalformedCell(t *testing.T) {
	for _, bad := range [][]string{
		{"x", "1", "1", "a"}, {"1", "abc", "1", "a"}, {"1", "NaN", "1", "a"}, {"1", "+Inf", "1", "a"}, {"1", "1", "1"},
	} {
		if _, err := parquetenc.Encode(cols, [][]string{bad}, "2"); err == nil {
			t.Errorf("row %v was accepted", bad)
		}
	}
}

// No rows is still a valid file with the columns.
func TestEncodeWithoutRows(t *testing.T) {
	data, err := parquetenc.Encode(cols, nil, "2")
	if err != nil {
		t.Fatal(err)
	}
	f, err := parquet.OpenFile(bytes.NewReader(data), int64(len(data)))
	if err != nil || f.NumRows() != 0 || len(f.Schema().Fields()) != len(cols) {
		t.Fatalf("empty file: %v rows %v", err, f)
	}
}
