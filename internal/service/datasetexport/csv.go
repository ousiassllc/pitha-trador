package datasetexport

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"fmt"
)

// encoded is one data file of the dataset.
type encoded struct {
	name  string
	data  []byte
	rows  int
	first string // first and last decision day of the rows ("" without rows)
	last  string
}

func (e encoded) sha256() string {
	sum := sha256.Sum256(e.data)
	return hex.EncodeToString(sum[:])
}

// encodeCSV renders rows (already in timestamp, id order) as one CSV file: the header of
// ColumnNames, "\n" line ends, one record per row, cells quoted by encoding/csv only where
// needed. The same rows give the same bytes.
func encodeCSV(name string, rows []*row) (encoded, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write(ColumnNames()); err != nil {
		return encoded{}, fmt.Errorf("datasetexport: write header: %w", err)
	}
	e := encoded{name: name, rows: len(rows)}
	for i, r := range rows {
		if err := w.Write(r.cells); err != nil {
			return encoded{}, fmt.Errorf("datasetexport: write row: %w", err)
		}
		if i == 0 {
			e.first = r.day
		}
		e.last = r.day
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return encoded{}, fmt.Errorf("datasetexport: write csv: %w", err)
	}
	e.data = buf.Bytes()
	return e, nil
}
