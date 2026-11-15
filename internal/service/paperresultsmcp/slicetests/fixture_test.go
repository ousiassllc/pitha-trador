package slicetests

import (
	"database/sql"
	"testing"
)

const (
	m1, v3 = "model-1", "trader-v3"
	m2, v4 = "model-2", "trader-v4"
)

// smallSpecs is the hand-computed dataset: cohort model-1/trader-v3 has 7 evaluated decisions on three JST
// days (Mon 10-05, Tue 10-06, Wed 10-07) and one decision (id 9) that cannot be labeled (no bars), so
// the cohort reads 8 and evaluates 7. Cohort model-2/trader-v4 is one decision on Thu 10-08.
//
//	id day time  inst dir   scenario  net  regime       vol conf  spread turnover
//	1  Mon 09:00 1    LONG  up        +30  trend         5  0.65   1     7
//	2  Mon 09:40 1    SHORT up        -30  mean_revert  20  0.95   2     6
//	3  Tue 09:00 1    LONG  down      -30  trend        10  0.55   3     5
//	4  Tue 11:40 2    SHORT down      +30  NULL         NULL NULL  4     4
//	5  Wed 10:00 2    LONG  mild        0  trend        15  0.90   5     3
//	6  Wed 15:00 3    SHORT up        -30  mean_revert  25  0.60   6     2
//	7  Wed 13:40 3    LONG  dip       -20  trend         1  0.70   7     1
//
// Instruments 1 / 2 are in the sectors Auto / Bank, instrument 3 has none.
func smallSpecs(t *testing.T) []spec {
	return []spec{
		{id: 1, inst: 1, at: jst(t, "2026-10-05T09:00:00"), dir: "LONG", scenario: "up", regime: "trend", vol: f(5), conf: f(0.65), spread: f(1), turn: f(7)},
		{id: 2, inst: 1, at: jst(t, "2026-10-05T09:40:00"), dir: "SHORT", scenario: "up", regime: "mean_revert", vol: f(20), conf: f(0.95), spread: f(2), turn: f(6)},
		{id: 3, inst: 1, at: jst(t, "2026-10-06T09:00:00"), dir: "LONG", scenario: "down", regime: "trend", vol: f(10), conf: f(0.55), spread: f(3), turn: f(5)},
		{id: 4, inst: 2, at: jst(t, "2026-10-06T11:40:00"), dir: "SHORT", scenario: "down", spread: f(4), turn: f(4)},
		{id: 5, inst: 2, at: jst(t, "2026-10-07T10:00:00"), dir: "LONG", scenario: "mild", regime: "trend", vol: f(15), conf: f(0.9), spread: f(5), turn: f(3)},
		{id: 6, inst: 3, at: jst(t, "2026-10-07T15:00:00"), dir: "SHORT", scenario: "up", regime: "mean_revert", vol: f(25), conf: f(0.6), spread: f(6), turn: f(2)},
		{id: 7, inst: 3, at: jst(t, "2026-10-07T13:40:00"), dir: "LONG", scenario: "dip", regime: "trend", vol: f(1), conf: f(0.7), spread: f(7), turn: f(1)},
		{id: 9, inst: 3, at: jst(t, "2026-10-07T09:00:00"), dir: "LONG", scenario: "up", noBars: true},
		{id: 20, inst: 4, at: jst(t, "2026-10-08T09:00:00"), dir: "SHORT", scenario: "down", model: m2, version: v4, regime: "trend", conf: f(0.8)},
	}
}

var smallSectors = map[int]string{1: "Auto", 2: "Bank", 3: "", 4: "Bank"}

// seedSmall writes smallSpecs (the cohort model-1/trader-v3 is the default of its specs).
func seedSmall(t *testing.T, w *sql.DB) {
	t.Helper()
	specs := smallSpecs(t)
	for i := range specs {
		if specs[i].model == "" {
			specs[i].model, specs[i].version = m1, v3
		}
	}
	seed(t, w, smallSectors, specs)
}
