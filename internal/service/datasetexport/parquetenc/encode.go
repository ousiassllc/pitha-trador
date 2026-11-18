// Package parquetenc writes the rows of the training dataset (issue #1124) as one Parquet file,
// in memory and byte for byte reproducible: the same columns and cells always give the same
// bytes. It knows nothing of the dataset; datasetexport hands it the column names and types and
// the cells it also writes as CSV.
//
// The file holds no clock, host name, random value or library version: the writer settings that
// shape the bytes (codec, data page version, row group size, created_by) are pinned below, so a
// library upgrade that changes the layout shows up as a test failure, not as a silent new hash.
package parquetenc

import (
	"bytes"
	"fmt"
	"math"
	"reflect"
	"strconv"

	"github.com/parquet-go/parquet-go"
)

// Type is the Parquet type of a column. A cell that is "" is a null of any type.
type Type int

const (
	Int64  Type = iota // a whole number (also the 0 / 1 flags: see below)
	Double             // a decimal number (finite)
	String             // UTF-8 text
)

// There is deliberately no Bool type. parquet-go bit-packs a BOOLEAN page into a pooled buffer and
// leaves the unused bits of its last byte as they were, so two encodes of the same rows can differ in
// that byte (and the page CRC). A 0 / 1 flag is an Int64 column instead, which has no padding.

// Column is the name and the type of one column, in file order.
type Column struct {
	Name string
	Type Type
}

// Pinned writer settings (see the package comment).
const (
	createdByApp   = "pitha-trador training dataset"
	rowGroupRows   = 10000
	dataPageFormat = 2
	// SchemaKey is the key-value metadata entry that carries the dataset's schema_version.
	SchemaKey = "pitha_dataset_schema_version"
)

// Encode renders rows (each one cell per column, in the order of cols) as a Parquet file. Every
// column is optional: "" is null. A cell that does not parse as its column's type is an error,
// never a silently different value. schemaVersion is stored in the file's key-value metadata.
func Encode(cols []Column, rows [][]string, schemaVersion string) ([]byte, error) {
	rowType, err := structOf(cols)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	w := parquet.NewWriter(&buf,
		parquet.SchemaOf(reflect.New(rowType).Interface()),
		parquet.Compression(&parquet.Uncompressed),
		parquet.CreatedBy(createdByApp, schemaVersion, ""),
		parquet.DataPageVersion(dataPageFormat),
		parquet.DataPageStatistics(true),
		parquet.ColumnPageBuffers(parquet.NewBufferPool()), // never the shared pool: no bytes left over from another encode
		parquet.MaxRowsPerRowGroup(rowGroupRows),
		parquet.KeyValueMetadata(SchemaKey, schemaVersion),
	)
	rec := reflect.New(rowType)
	for n, cells := range rows {
		if len(cells) != len(cols) {
			return nil, fmt.Errorf("parquetenc: row %d has %d cells, want %d", n, len(cells), len(cols))
		}
		for i, c := range cols {
			v, err := value(c.Type, cells[i])
			if err != nil {
				return nil, fmt.Errorf("parquetenc: row %d column %s: %w", n, c.Name, err)
			}
			rec.Elem().Field(i).Set(v)
		}
		if err := w.Write(rec.Interface()); err != nil {
			return nil, fmt.Errorf("parquetenc: write row %d: %w", n, err)
		}
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("parquetenc: close: %w", err)
	}
	return buf.Bytes(), nil
}

// structOf builds a struct type with one optional pointer field per column, in column order
// (a parquet.Group would sort the columns by name). The Go field names are Col0, Col1, ...; the
// Parquet column names come from the tags.
func structOf(cols []Column) (reflect.Type, error) {
	fields := make([]reflect.StructField, len(cols))
	for i, c := range cols {
		var t reflect.Type
		switch c.Type {
		case Int64:
			t = reflect.TypeFor[*int64]()
		case Double:
			t = reflect.TypeFor[*float64]()
		case String:
			t = reflect.TypeFor[*string]()
		default:
			return nil, fmt.Errorf("parquetenc: column %s has an unknown type %d", c.Name, c.Type)
		}
		fields[i] = reflect.StructField{
			Name: "Col" + strconv.Itoa(i), Type: t,
			Tag: reflect.StructTag(`parquet:"` + c.Name + `,optional"`),
		}
	}
	return reflect.StructOf(fields), nil
}

// value parses a cell as the pointer type of its column ("" is the nil pointer).
func value(t Type, cell string) (reflect.Value, error) {
	switch t {
	case Int64:
		if cell == "" {
			return reflect.Zero(reflect.TypeFor[*int64]()), nil
		}
		n, err := strconv.ParseInt(cell, 10, 64)
		return reflect.ValueOf(&n), err
	case Double:
		if cell == "" {
			return reflect.Zero(reflect.TypeFor[*float64]()), nil
		}
		f, err := strconv.ParseFloat(cell, 64)
		if err == nil && (math.IsNaN(f) || math.IsInf(f, 0)) {
			err = fmt.Errorf("%q is not a finite number", cell)
		}
		return reflect.ValueOf(&f), err
	default:
		if cell == "" {
			return reflect.Zero(reflect.TypeFor[*string]()), nil
		}
		return reflect.ValueOf(&cell), nil
	}
}
