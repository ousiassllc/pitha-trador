package researchlabel

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// tableDump is every row of the table rendered as text, in rowid order.
func tableDump(t *testing.T, db *sql.DB, table string) string {
	t.Helper()
	rows, err := db.Query(`SELECT * FROM ` + table + ` ORDER BY rowid`)
	if err != nil {
		t.Fatalf("dump %s: %v", table, err)
	}
	defer func() { _ = rows.Close() }()
	cols, _ := rows.Columns()
	var b strings.Builder
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&b, "%v\n", vals)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestRecorder_ChangesNoExistingTable(t *testing.T) {
	db := openDB(t)
	seedDay(t, db, "2026-10-13")
	seedDay(t, db, "2026-10-14")
	// One decision on a snapshot, with a calibration outcome, to prove those are untouched too.
	if _, err := db.Exec(`INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash, state_json, question_version, response_json, direction, confidence, latency_ms, model_id)
		VALUES (1, 1, '7201', ?, 'trader', 'h', '{}', 'q', '{}', 'LONG', 0.7, 5, 'm')`, sqlutil.FormatTime(time.Date(2026, 10, 13, 5, 0, 0, 0, time.UTC))); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO calibration_outcomes (jev_decision_id, horizon_minutes, future_return, max_adverse_excursion, max_favorable_excursion, label_version)
		VALUES (1, 10, 0.1, 0, 0.2, 1)`); err != nil {
		t.Fatal(err)
	}
	existing := []string{"market_snapshots", "jev_decisions", "calibration_outcomes", "counterfactual_labels", "calibration_label_skips", "instruments", "runtime_settings", "metalabel_models", "metalabel_predictions", "trade_signals"}
	before := map[string]string{}
	for _, tbl := range existing {
		before[tbl] = tableDump(t, db, tbl)
	}
	schemaBefore := tableDump(t, db, "sqlite_master")

	if _, err := newRecorder(db, "2026-10-15 16:30").Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, tbl := range existing {
		if got := tableDump(t, db, tbl); got != before[tbl] {
			t.Errorf("table %s changed by the recorder", tbl)
		}
	}
	// No trigger or other object was added by running (only rows of the two new tables).
	if got := tableDump(t, db, "sqlite_master"); got != schemaBefore {
		t.Error("the schema changed while recording")
	}
	if before["market_snapshots"] == "" || before["jev_decisions"] == "" || before["calibration_outcomes"] == "" {
		t.Fatal("the comparison fixture is empty")
	}
}
