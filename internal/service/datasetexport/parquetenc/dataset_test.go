package parquetenc_test

import (
	"bytes"
	"strconv"
	"testing"

	"github.com/parquet-go/parquet-go"

	"github.com/ousiassllc/pitha-trador/internal/service/datasetexport"
	"github.com/ousiassllc/pitha-trador/internal/service/datasetexport/parquetenc"
)

// datasetRows fills every column of the dataset schema with a value of its type (some empty), as a real row looks.
func datasetRows(cols []parquetenc.Column, n, salt int) [][]string {
	rows := make([][]string, n)
	for i := range rows {
		rows[i] = make([]string, len(cols))
		for j, c := range cols {
			switch {
			case (i+j+salt)%4 == 0: // empty: null
			case c.Type == parquetenc.Int64:
				rows[i][j] = strconv.Itoa(1 + (i*7+j+salt)%2)
			case c.Type == parquetenc.Double:
				rows[i][j] = strconv.FormatFloat(float64(i+j+salt)/8, 'f', -1, 64)
			default:
				rows[i][j] = "t" + strconv.Itoa((i+j+salt)%5)
			}
		}
	}
	return rows
}

// The dataset schema (every column of the export) gives the same bytes for the same rows however many
// other encodes ran in between, also over several row groups, and the file has the export's columns in order.
func TestDatasetSchemaIsDeterministicAndKeepsColumnOrder(t *testing.T) {
	cols := datasetexport.ParquetColumns()
	rows := datasetRows(cols, 5, 0)
	first, err := parquetenc.Encode(cols, rows, datasetexport.SchemaVersion)
	if err != nil {
		t.Fatal(err)
	}
	f, err := parquet.OpenFile(bytes.NewReader(first), int64(len(first)))
	if err != nil || f.NumRows() != int64(len(rows)) || len(f.Schema().Fields()) != len(datasetexport.Columns) {
		t.Fatalf("%v rows %d columns %d", err, f.NumRows(), len(f.Schema().Fields()))
	}
	for i, field := range f.Schema().Fields() {
		if field.Name() != datasetexport.Columns[i].Name {
			t.Fatalf("column %d is %s, want %s", i, field.Name(), datasetexport.Columns[i].Name)
		}
	}
	for range 100 {
		for salt := 1; salt <= 3; salt++ {
			if _, err := parquetenc.Encode(cols, datasetRows(cols, 3+salt, salt), datasetexport.SchemaVersion); err != nil {
				t.Fatal(err)
			}
		}
		again, err := parquetenc.Encode(cols, rows, datasetexport.SchemaVersion)
		if err != nil || !bytes.Equal(again, first) {
			t.Fatalf("the same rows gave other bytes (%v)", err)
		}
	}
	big := datasetRows(cols, 25000, 0)
	a, err1 := parquetenc.Encode(cols, big, datasetexport.SchemaVersion)
	b, err2 := parquetenc.Encode(cols, big, datasetexport.SchemaVersion)
	if err1 != nil || err2 != nil || !bytes.Equal(a, b) {
		t.Fatalf("several row groups gave other bytes (%v %v)", err1, err2)
	}
}
