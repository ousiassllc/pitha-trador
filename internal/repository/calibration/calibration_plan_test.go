package calibration

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
)

// The pending-labels scan is a timestamp range scan of
// jev_decisions_type_timestamp_idx, not a walk over every decision
// (issue #484).
func TestPendingLabelsQuery_UsesTypeTimestampIndexRange(t *testing.T) {
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "plan.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	rows, err := db.Query("EXPLAIN QUERY PLAN "+pendingLabelsQuery,
		"2026-09-27T09:00:00Z", "2026-09-28T09:00:00Z", 5, 5)
	if err != nil {
		t.Fatalf("EXPLAIN QUERY PLAN: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var plan []string
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		plan = append(plan, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("plan rows: %v", err)
	}
	joined := strings.Join(plan, "\n")
	if !strings.Contains(joined, "jev_decisions_type_timestamp_idx (decision_type=? AND timestamp>? AND timestamp<?)") {
		t.Fatalf("want a timestamp range search of jev_decisions_type_timestamp_idx; plan:\n%s", joined)
	}
}
