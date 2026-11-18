package researchquotediag

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

func seedDay(t *testing.T, db *sql.DB, date string, n int) {
	t.Helper()
	start, err := time.Parse(time.RFC3339, date+"T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	for i := range n {
		at := sqlutil.FormatTime(start.Add(time.Duration(i) * time.Minute))
		if _, err := db.Exec(`INSERT INTO research_quote_samples (timestamp, source, symbol, price, via, session_phase) VALUES (?, 'tachibana', '7203', 100, 'event', 'morning')`, at); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO market_snapshots (instrument_id, symbol, timestamp, price, volume, turnover, vwap, price_vs_vwap_bps, raw_data_json)
			VALUES (1, '7203', ?, 100, 1, 1, 1, 1, '{}')`, at); err != nil {
			t.Fatal(err)
		}
	}
}

// A period that does not fit in the row cap is answered for the days that do,
// never with half a day, and says where it stopped.
func TestRowCapStopsAtAWholeDayAndSaysSo(t *testing.T) {
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`INSERT INTO instruments (id, symbol, name, market, is_active, created_at, updated_at) VALUES (1, '7203', 'x', 'TSE Prime', 1, 't', 't')`); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"2026-10-13", "2026-10-14", "2026-10-15"} {
		seedDay(t, db, d, 5) // 5 samples + 5 snapshots = 10 rows a day
	}
	old := rowCap
	rowCap = 25 // two whole days (20 rows) fit, the third (10 more) does not
	t.Cleanup(func() { rowCap = old })

	jst := query.JST
	r := query.Range{From: time.Date(2026, 10, 13, 0, 0, 0, 0, jst), To: time.Date(2026, 10, 17, 0, 0, 0, 0, jst)}
	d, rows, err := Analyze(context.Background(), db, r, Request{})
	if err != nil {
		t.Fatal(err)
	}
	if d.Scan.Complete || d.Scan.Note == "" || d.Scan.RowCap != 25 {
		t.Errorf("scan = %+v, want incomplete with a note", d.Scan)
	}
	if len(d.Days) != 2 || d.Scan.CoveredThrough == nil || *d.Scan.CoveredThrough != "2026-10-14" || rows != 20 || d.Scan.RowsRead != 20 {
		t.Errorf("days = %d, covered through %v, rows %d: want the two whole days up to 2026-10-14 and 20 rows", len(d.Days), d.Scan.CoveredThrough, rows)
	}
	if d.Tachibana.Rows != 10 || d.Comparison.MinutesBoth != 10 || d.Comparison.OnlyKabu != 0 {
		t.Errorf("totals = %+v / %+v, want only the complete days counted", d.Tachibana, d.Comparison)
	}
}
