package actuals_test

import (
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/repository/actuals/actualstest"
)

// Q11 slices Jev's figures like the screen does, against the hand-computed
// values of actualstest.Outcomes (the week is strftime %W here, not the ISO
// week).
func TestVerifyQueries_JevSlicesQ11(t *testing.T) {
	db := openDB(t)
	actualstest.Seed(t, db)
	_, queries := verifyQueries(t)
	q11 := run(t, db, "Q11", queries["Q11"])
	at := func(version int, dim, key string) row {
		return find(q11, map[string]any{"label_version": float64(version), "dim": dim, "key": key})
	}
	expect(t, "Q11 v2 12th", at(2, "day", "2026-10-12"), map[string]any{"n": 2.0, "hit_rate": 0.5, "brier": 0.2})
	expect(t, "Q11 v2 13th", at(2, "day", "2026-10-13"), map[string]any{"n": 1.0, "hit_rate": 1.0, "brier": 0.01})
	expect(t, "Q11 v2 14th", at(2, "day", "2026-10-14"), map[string]any{"n": 1.0, "hit_rate": 0.0, "brier": 0.25})
	expect(t, "Q11 v2 LONG", at(2, "direction", "LONG"), map[string]any{"n": 3.0, "hit_rate": 0.6667, "brier": 0.1})
	expect(t, "Q11 v2 SHORT", at(2, "direction", "SHORT"), map[string]any{"n": 1.0, "hit_rate": 0.0, "brier": 0.36})
	expect(t, "Q11 v1 13th", at(1, "day", "2026-10-13"), map[string]any{"n": 1.0, "hit_rate": 0.0, "brier": 0.49})
	expect(t, "Q11 v1 LONG", at(1, "direction", "LONG"), map[string]any{"n": 2.0, "hit_rate": 0.5, "brier": 0.265})
	expect(t, "Q11 v2 week", find(q11, map[string]any{"label_version": 2.0, "dim": "week"}), map[string]any{"n": 4.0, "hit_rate": 0.5, "brier": 0.165})
}
