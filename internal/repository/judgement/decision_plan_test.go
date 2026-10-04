package judgement

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
)

// Both ListRecent statements must read the newest rows in index order and
// stop at LIMIT, never scanning or sorting the whole jev_decisions table
// (issue #419).
func TestDecisionListRecentQueries_UseTimestampIndexesWithoutSort(t *testing.T) {
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "plan.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cases := []struct {
		name, query, index string
		args               []any
	}{
		{"all types", listRecentAllQuery, "jev_decisions_timestamp_idx", []any{50}},
		{"one type", listRecentByTypeQuery, "jev_decisions_type_timestamp_idx", []any{"scout", 50}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := db.Query("EXPLAIN QUERY PLAN "+tc.query, tc.args...)
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
			if strings.Contains(joined, "TEMP B-TREE") || !strings.Contains(joined, tc.index) {
				t.Fatalf("want an ordered scan of %s without a sort; plan:\n%s", tc.index, joined)
			}
		})
	}
}
