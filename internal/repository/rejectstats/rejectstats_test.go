package rejectstats_test

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/rejectstats"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`INSERT INTO instruments (id, symbol, name, market) VALUES (1, '7203', 'test', 'TSE')`); err != nil {
		t.Fatalf("seed instrument: %v", err)
	}
	return db
}

func insert(t *testing.T, db *sql.DB, at time.Time, direction string, reason any) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO trade_signals (instrument_id, symbol, timestamp, direction, policy_version, risk_passed, reject_reason) VALUES (1, '7203', ?, ?, 'p1', ?, ?)`,
		sqlutil.FormatTime(at), direction, direction != "NONE", reason); err != nil {
		t.Fatalf("insert: %v", err)
	}
}

// Only today's NONE signals are counted, grouped by the head of reject_reason
// (the text before the first ':'), most frequent first; the detail after ':'
// never reaches the result and a missing reason is "unknown".
func TestTodayCounts_GroupsNoneSignalsByReasonHead(t *testing.T) {
	db := openDB(t)
	from := time.Date(2026, 10, 22, 0, 0, 0, 0, time.FixedZone("JST", 9*3600))
	to := from.AddDate(0, 0, 1)
	noon := from.Add(12 * time.Hour)

	insert(t, db, noon, "NONE", `entry_quality_below_threshold: entry_quality="poor" < min="good"`)
	insert(t, db, noon, "NONE", `entry_quality_below_threshold: entry_quality="fair" < min="good"`)
	insert(t, db, noon, "NONE", `entry_quality_below_threshold`)
	insert(t, db, noon, "NONE", "risk_engine_rejected: daily_loss_limit count=3")
	insert(t, db, noon, "NONE", "probability_below_threshold: p=0.51")
	insert(t, db, noon, "NONE", "  spread_too_wide  : 31bps")
	insert(t, db, noon, "NONE", nil)
	insert(t, db, noon, "NONE", "")
	insert(t, db, noon, "LONG", nil)                          // an entry is not a reason
	insert(t, db, from.Add(-time.Second), "NONE", "jev_none") // yesterday
	insert(t, db, to, "NONE", "jev_none")                     // tomorrow (to is exclusive)
	insert(t, db, from, "NONE", "jev_none")                   // from is inclusive

	got, err := rejectstats.TodayCounts(t.Context(), db, from, to)
	if err != nil {
		t.Fatalf("TodayCounts: %v", err)
	}
	want := []rejectstats.Count{
		{Reason: "entry_quality_below_threshold", Count: 3},
		{Reason: "unknown", Count: 2},
		{Reason: "jev_none", Count: 1},
		{Reason: "probability_below_threshold", Count: 1},
		{Reason: "risk_engine_rejected", Count: 1},
		{Reason: "spread_too_wide", Count: 1},
	}
	if len(got) != len(want) {
		t.Fatalf("counts = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("counts[%d] = %+v, want %+v (all: %+v)", i, got[i], want[i], got)
		}
	}
}

func TestTodayCounts_NoSignalsIsEmptyNotNil(t *testing.T) {
	db := openDB(t)
	got, err := rejectstats.TodayCounts(t.Context(), db, time.Now().Add(-time.Hour), time.Now())
	if err != nil {
		t.Fatalf("TodayCounts: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("counts = %#v, want an empty non-nil slice", got)
	}
}
