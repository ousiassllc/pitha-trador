package exporttests

import (
	"bytes"
	"encoding/csv"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// table is a parsed dataset file.
type table struct {
	header []string
	rows   [][]string
	index  map[string]int
}

func parseCSV(t *testing.T, data []byte) table {
	t.Helper()
	recs, err := csv.NewReader(bytes.NewReader(data)).ReadAll()
	if err != nil || len(recs) == 0 {
		t.Fatalf("csv: %v (%d records)", err, len(recs))
	}
	tb := table{header: recs[0], rows: recs[1:], index: map[string]int{}}
	for i, h := range tb.header {
		tb.index[h] = i
	}
	return tb
}

func (tb table) cell(t *testing.T, row int, col string) string {
	t.Helper()
	i, ok := tb.index[col]
	if !ok {
		t.Fatalf("no column %q", col)
	}
	return tb.rows[row][i]
}

// rowOf finds the row of decision id.
func (tb table) rowOf(t *testing.T, id int) int {
	t.Helper()
	for i := range tb.rows {
		if tb.cell(t, i, "decision_id") == strconv.Itoa(id) {
			return i
		}
	}
	t.Fatalf("decision %d is not in the file", id)
	return -1
}

func (tb table) ids(t *testing.T) []int {
	t.Helper()
	ids := make([]int, len(tb.rows))
	for i := range tb.rows {
		ids[i], _ = strconv.Atoi(tb.cell(t, i, "decision_id"))
	}
	return ids
}

func (tb table) wantNum(t *testing.T, id int, col string, want float64) {
	t.Helper()
	got, err := strconv.ParseFloat(tb.cell(t, tb.rowOf(t, id), col), 64)
	if err != nil || math.Abs(got-want) > eps {
		t.Errorf("decision %d %s = %q, want %v", id, col, tb.cell(t, tb.rowOf(t, id), col), want)
	}
}

func (tb table) wantText(t *testing.T, id int, col, want string) {
	t.Helper()
	if got := tb.cell(t, tb.rowOf(t, id), col); got != want {
		t.Errorf("decision %d %s = %q, want %q", id, col, got, want)
	}
}

// datasetFiles reads every file of the dataset folder the manifest names (data files and manifest.json).
func datasetFiles(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	for _, e := range entries {
		out[e.Name()] = readFile(t, filepath.Join(dir, e.Name()))
	}
	return out
}

// only the one dataset folder lives in the exports folder, and no temporary one is left behind.
func singleDataset(t *testing.T, s *testServer, id string) string {
	t.Helper()
	entries, err := os.ReadDir(s.exportsDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != id {
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("exports folder holds %v, want only %s", names, id)
	}
	return filepath.Join(s.exportsDir(), id)
}
