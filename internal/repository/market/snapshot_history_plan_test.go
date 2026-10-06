package market

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
)

// The batched history query must reach market_snapshots only through
// index searches ((instrument_id, timestamp)), never a table scan, and must
// not select raw_data_json.
func TestListHistoryByInstrumentsSQL_UsesIndexSearchAndSkipsRawData(t *testing.T) {
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	query := fmt.Sprintf(listHistoryByInstrumentsSQL, "?,?,?", snapshotHistoryColumns)
	if strings.Contains(query, "raw_data_json") {
		t.Errorf("history query selects raw_data_json: %s", query)
	}
	rows, err := db.Query("EXPLAIN QUERY PLAN "+query, 3, 1, 2, 3)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer func() { _ = rows.Close() }()
	searched := false
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(detail, "SCAN market_snapshots") {
			t.Errorf("full scan of market_snapshots: %s", detail)
		}
		if strings.Contains(detail, "SEARCH market_snapshots") {
			searched = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !searched {
		t.Error("plan never searches market_snapshots by index")
	}
}
