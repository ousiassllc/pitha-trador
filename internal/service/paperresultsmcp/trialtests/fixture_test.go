package trialtests

import (
	"database/sql"
	"fmt"
	"testing"
)

const (
	armA = "arm-a"
	armB = "arm-b"
	armC = "arm-c"
)

// liveDirs are the live directions of decisions 1..7 (inside the period); decision 8 is outside it.
var liveDirs = []string{"", "LONG", "LONG", "SHORT", "LONG", "NONE", "LONG", "LONG"}

// seedMain plants three arms over the live decisions 1..8 of 2026-10-01 (decision 8: August, outside the period).
//
// Cohort of all three arms = decisions 1..6 (7 was asked of arm-a only). Per decision (arm-a / arm-b / arm-c):
//
//	1 ok LONG / ok LONG (truncation) / ok LONG (RAG flag)  - the only decision all three answered
//	2 ok LONG (cutoff confirmed) / failed http_429 / failed timeout
//	3 ok SHORT / refused / failed http_402
//	4 ok LONG / partial / failed transport
//	5 ok LONG / ok SHORT / failed http_429
//	6 ok NONE / ok LONG / failed http_529
//
// Outcomes (calibration_outcomes, horizon 5): future_return +1, +1, -1, -1, +0.5, +0.5, +1 for decisions 1..7
// (label version 2) and +1 for decision 1 at horizon 10 (label version 1). Counterfactual labels (existing records) exist for
// decisions 1 (tp), 2 (tp) and 6 (sl).
func seedMain(t *testing.T, w *sql.DB) {
	t.Helper()
	for id := 1; id <= 7; id++ {
		liveDecision(t, w, id, fmt.Sprintf("2026-10-01T%02d:00:00Z", id), liveDirs[id])
	}
	liveDecision(t, w, 8, "2026-08-01T00:00:00Z", "LONG")
	rows := []row{
		{decision: 1, arm: armA, dir: "LONG", eq: "poor", rag: true},
		{decision: 2, arm: armA, dir: "LONG", eq: "good", cutoff: "confirmed"},
		{decision: 3, arm: armA, dir: "SHORT", eq: "good"},
		{decision: 4, arm: armA, dir: "LONG", eq: "fair"},
		{decision: 5, arm: armA, dir: "LONG", eq: "strong"},
		{decision: 6, arm: armA, dir: "NONE", eq: "fair"},
		{decision: 7, arm: armA, dir: "LONG", eq: "fair"},
		{decision: 1, arm: armB, dir: "LONG", eq: "poor", trunc: true},
		{decision: 2, arm: armB, status: "failed", failure: "http_429"},
		{decision: 3, arm: armB, status: "refused"},
		{decision: 4, arm: armB, status: "partial", dir: "LONG"},
		{decision: 5, arm: armB, dir: "SHORT", eq: "fair"},
		{decision: 6, arm: armB, dir: "LONG", eq: "fair"},
		{decision: 1, arm: armC, dir: "LONG", eq: "poor", rag: true},
		{decision: 2, arm: armC, status: "failed", failure: "timeout"},
		{decision: 3, arm: armC, status: "failed", failure: "http_402"},
		{decision: 4, arm: armC, status: "failed", failure: "transport"},
		{decision: 5, arm: armC, status: "failed", failure: "http_429"},
		{decision: 6, arm: armC, status: "failed", failure: "http_529"},
		{decision: 8, arm: armA, status: "failed", failure: "timeout"}, // outside the period
		{decision: 8, arm: armB, status: "failed", failure: "timeout"},
		{decision: 8, arm: armC, status: "failed", failure: "timeout"},
	}
	for _, r := range rows {
		insertRow(t, w, r)
	}
	for id, fr := range map[int]float64{1: 1, 2: 1, 3: -1, 4: -1, 5: 0.5, 6: 0.5, 7: 1, 8: 1} {
		outcome(t, w, id, 5, 2, fr)
	}
	outcome(t, w, 1, 10, 1, 1)
	counterfactual(t, w, 1, 5, "tp", 99)
	counterfactual(t, w, 2, 5, "tp", 30)
	counterfactual(t, w, 6, 5, "sl", -12.5)
}

// seedRank plants 20 live decisions (all LONG) in the period. arm-a answers every one; arm-c answers only the
// first three and fails the rest. Returns: decisions 1-3 +1, then even ids +1 and odd ids -1.
func seedRank(t *testing.T, w *sql.DB) {
	t.Helper()
	for id := 1; id <= 20; id++ {
		liveDecision(t, w, id, fmt.Sprintf("2026-10-02T%02d:00:00Z", id), "LONG")
		insertRow(t, w, row{decision: id, arm: armA})
		if id <= 3 {
			insertRow(t, w, row{decision: id, arm: armC})
		} else {
			insertRow(t, w, row{decision: id, arm: armC, status: "failed", failure: "timeout"})
		}
		fr := 1.0
		if id > 3 && id%2 == 1 {
			fr = -1
		}
		outcome(t, w, id, 5, 2, fr)
	}
}
