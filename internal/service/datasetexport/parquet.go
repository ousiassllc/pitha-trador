package datasetexport

import (
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/service/datasetexport/parquetenc"
)

// ParquetColumns is the Parquet schema of every data file: the columns of Columns, in order,
// with their types.
func ParquetColumns() []parquetenc.Column {
	cols := make([]parquetenc.Column, len(Columns))
	for i, c := range Columns {
		cols[i] = parquetenc.Column{Name: c.Name, Type: c.typ}
	}
	return cols
}

// encodeParquet renders the same rows as the CSV file csv (already encoded from them) as one
// Parquet file named name: the same columns, the same rows in the same order, an empty cell is
// null. The same rows give the same bytes (parquetenc pins every writer setting).
func encodeParquet(name string, csv encoded, rows []*row) (encoded, error) {
	cells := make([][]string, len(rows))
	for i, r := range rows {
		cells[i] = r.cells
	}
	data, err := parquetenc.Encode(ParquetColumns(), cells, SchemaVersion)
	if err != nil {
		return encoded{}, fmt.Errorf("datasetexport: encode %s: %w", name, err)
	}
	return encoded{name: name, data: data, rows: csv.rows, first: csv.first, last: csv.last}, nil
}

// encodeSide renders one side of the dataset in both formats: the CSV file and the Parquet file
// named csvName / parquetName.
func encodeSide(csvName, parquetName string, rows []*row) ([]encoded, error) {
	c, err := encodeCSV(csvName, rows)
	if err != nil {
		return nil, err
	}
	p, err := encodeParquet(parquetName, c, rows)
	if err != nil {
		return nil, err
	}
	return []encoded{c, p}, nil
}
