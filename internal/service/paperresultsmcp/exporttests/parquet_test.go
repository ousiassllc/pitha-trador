package exporttests

import (
	"bytes"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/parquet-go/parquet-go"

	"github.com/ousiassllc/pitha-trador/internal/service/datasetexport"
)

// readParquet reads a dataset file back: its column names and every row, in file order.
func readParquet(t *testing.T, data []byte) ([]string, []parquet.Row) {
	t.Helper()
	f, err := parquet.OpenFile(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("parquet: %v", err)
	}
	var names []string
	for _, field := range f.Schema().Fields() {
		names = append(names, field.Name())
	}
	reader := parquet.NewReader(f)
	defer func() { _ = reader.Close() }()
	rows := make([]parquet.Row, f.NumRows())
	n, _ := reader.ReadRows(rows)
	if int64(n) != f.NumRows() {
		t.Fatalf("read %d of %d rows", n, f.NumRows())
	}
	return names, rows
}

// sameAsCSV: the Parquet file holds the CSV's columns and rows, value for value (an empty CSV cell is null,
// a number is the same number, text is the same text), and its hash and size are the manifest's.
func sameAsCSV(t *testing.T, dir string, csvFile, pqFile datasetexport.File) {
	t.Helper()
	csvBytes, pqBytes := readFile(t, filepath.Join(dir, csvFile.Name)), readFile(t, filepath.Join(dir, pqFile.Name))
	if sha(pqBytes) != pqFile.SHA256 || len(pqBytes) != pqFile.Bytes || pqFile.Format != "parquet" || pqFile.Rows != csvFile.Rows {
		t.Errorf("%s does not match its manifest entry %+v", pqFile.Name, pqFile)
	}
	tb := parseCSV(t, csvBytes)
	names, rows := readParquet(t, pqBytes)
	if len(rows) != len(tb.rows) || len(rows) != pqFile.Rows {
		t.Fatalf("%s has %d rows, the CSV %d, the manifest %d", pqFile.Name, len(rows), len(tb.rows), pqFile.Rows)
	}
	if strings.Join(names, ",") != strings.Join(tb.header, ",") {
		t.Fatalf("%s columns %v, the CSV %v", pqFile.Name, names, tb.header)
	}
	for r, row := range rows {
		for c, v := range row {
			want := tb.rows[r][c]
			switch {
			case want == "":
				if !v.IsNull() {
					t.Errorf("%s row %d %s = %v, want null", pqFile.Name, r, names[c], v)
				}
			case v.IsNull():
				t.Errorf("%s row %d %s is null, want %q", pqFile.Name, r, names[c], want)
			case v.Kind() == parquet.Int64:
				if n, err := strconv.ParseInt(want, 10, 64); err != nil || n != v.Int64() {
					t.Errorf("%s row %d %s = %d, want %q", pqFile.Name, r, names[c], v.Int64(), want)
				}
			case v.Kind() == parquet.Double:
				if f, err := strconv.ParseFloat(want, 64); err != nil || math.Abs(f-v.Double()) > 0 {
					t.Errorf("%s row %d %s = %v, want %q", pqFile.Name, r, names[c], v.Double(), want)
				}
			case string(v.ByteArray()) != want:
				t.Errorf("%s row %d %s = %q, want %q", pqFile.Name, r, names[c], v.ByteArray(), want)
			}
		}
	}
}

// Without a split: a Parquet file beside the CSV with the same row count and values; the manifest lists both
// with their own hashes, and the dataset hash covers the Parquet file (another Parquet byte is another id).
func TestParquetBesideTheCSVHasTheSameRows(t *testing.T) {
	srv, w := newServer(t, "")
	seed(t, w, fixture())
	enableExport(t, w)
	m := srv.call(t, nil).export(t).Manifest
	if len(m.Files) != 2 || m.Files[0].Name != "dataset.csv" || m.Files[1].Name != "dataset.parquet" {
		t.Fatalf("files %+v", m.Files)
	}
	sameAsCSV(t, m.SaveDir, m.Files[0], m.Files[1])
	if m.Files[1].Rows != m.Rows || m.Rows != m.Decisions {
		t.Errorf("rows: parquet %d, manifest %d, decisions %d", m.Files[1].Rows, m.Rows, m.Decisions)
	}
	assertModes(t, srv.exportsDir(), m.SaveDir)
}

// With a split: each side has its Parquet file, the same rows as its CSV, and the train and eval periods do
// not overlap in the Parquet files either (the rule is the CSV's: the Parquet rows are the same rows).
func TestParquetSidesMatchTheCSVAndNeverOverlap(t *testing.T) {
	for _, embargo := range []int{0, 1, 3} {
		srv, w := newServer(t, "")
		seed(t, w, fixture())
		enableExport(t, w)
		m := srv.call(t, map[string]any{"split_at": "2026-10-08", "embargo_days": embargo}).export(t).Manifest
		if len(m.Files) != 4 {
			t.Fatalf("embargo %d: files %+v", embargo, m.Files)
		}
		sameAsCSV(t, m.SaveDir, m.Files[0], m.Files[1])
		sameAsCSV(t, m.SaveDir, m.Files[2], m.Files[3])

		lastTrain, firstEval := "", "9999"
		for side, f := range map[string]datasetexport.File{"train": m.Files[1], "eval": m.Files[3]} {
			names, rows := readParquet(t, readFile(t, filepath.Join(m.SaveDir, f.Name)))
			dayCol, splitCol := indexOf(t, names, "decision_day"), indexOf(t, names, "split")
			for _, row := range rows {
				day := string(row[dayCol].ByteArray())
				if string(row[splitCol].ByteArray()) != side {
					t.Errorf("embargo %d: a row of %s says split=%s", embargo, f.Name, row[splitCol].ByteArray())
				}
				if side == "train" && day > lastTrain {
					lastTrain = day
				}
				if side == "eval" && day < firstEval {
					firstEval = day
				}
			}
		}
		if lastTrain == "" || firstEval == "9999" || lastTrain >= "2026-10-08" || firstEval < "2026-10-08" || lastTrain >= firstEval {
			t.Errorf("embargo %d: the Parquet train ends %s, eval starts %s", embargo, lastTrain, firstEval)
		}
	}
}

func indexOf(t *testing.T, names []string, name string) int {
	t.Helper()
	for i, n := range names {
		if n == name {
			return i
		}
	}
	t.Fatalf("no column %s", name)
	return -1
}

// The same input gives the same Parquet bytes, id and manifest hashes in two separate databases (two servers,
// two processes' worth of buffers), not only in a re-run over the same folder.
func TestParquetBytesAreReproducibleAcrossDatabases(t *testing.T) {
	var ids, hashes []string
	for range 3 {
		srv, w := newServer(t, "")
		seed(t, w, fixture())
		enableExport(t, w)
		m := srv.call(t, map[string]any{"split_at": "2026-10-09", "embargo_days": 1}).export(t).Manifest
		ids = append(ids, m.DatasetID)
		var h string
		for _, f := range m.Files {
			h += f.Name + ":" + f.SHA256 + ";"
		}
		hashes = append(hashes, h)
	}
	if ids[0] != ids[1] || ids[1] != ids[2] || hashes[0] != hashes[1] || hashes[1] != hashes[2] {
		t.Errorf("ids %v, file hashes %v", ids, hashes)
	}
}
