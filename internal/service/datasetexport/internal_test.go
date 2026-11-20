package datasetexport

import (
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

func TestRejectCodeIsAFixedCodeNeverTheText(t *testing.T) {
	for reason, want := range map[string]string{
		"risk_engine_rejected":                            "risk_engine_rejected",
		"risk_engine_rejected: SECRET detail 12 bps":      "risk_engine_rejected",
		"entry_quality_below_threshold: 0.31 < 0.50":      "entry_quality_below_threshold",
		"spread_too_wide":                                 "spread_too_wide",
		"jev_none: confidence=0.81 regime=RANGE":          "jev_none",
		"meta_skip: ev_not_above_minimum 0.2":             "meta_skip:ev_not_above_minimum",
		"meta_skip: something-new":                        "meta_skip:other",
		"api_error: timeout while calling":                "api_error:timeout",
		"api_error: SENTINEL secret in the message":       "api_error",
		"a brand new reason with a SENTINEL_KEY in it: x": "other",
		"": "unknown",
	} {
		if got := rejectCode(reason); got != want {
			t.Errorf("rejectCode(%q) = %q, want %q", reason, got, want)
		}
	}
}

func TestOriginFollowsTheOriginViewRule(t *testing.T) {
	for _, c := range []struct {
		dir  string
		g    gate
		want string
	}{
		{"NONE", gate{count: 1, passed: true}, "no_direction"},
		{"", gate{}, "no_direction"},
		{"LONG", gate{count: 2, passed: true, lastWhy: "x"}, "entered"},
		{"SHORT", gate{}, "no_signal"},
		{"LONG", gate{count: 1, lastWhy: "risk_engine_rejected"}, "risk_rejected"},
		{"LONG", gate{count: 1, lastWhy: "risk_engine_rejected: stop"}, "risk_rejected"},
		{"LONG", gate{count: 1, lastWhy: "risk_engine_rejectedX"}, "guard_skipped"},
		{"LONG", gate{count: 1, lastWhy: "thin_liquidity"}, "guard_skipped"},
	} {
		if got := originOf(c.dir, c.g); got != c.want {
			t.Errorf("originOf(%q, %+v) = %q, want %q", c.dir, c.g, got, c.want)
		}
	}
}

func TestCheckSplit(t *testing.T) {
	from := time.Date(2026, 10, 5, 0, 0, 0, 0, query.JST)
	to := from.AddDate(0, 0, 7)
	for _, c := range []struct {
		split   string
		embargo int
		ok      bool
	}{
		{"", 0, true}, {"", 1, false},
		{"2026-10-06", 0, true}, {"2026-10-11", 0, true},
		{"2026-10-05", 0, false}, {"2026-10-12", 0, false}, {"2026-10-04", 0, false}, {"2026-10-13", 0, false},
		{"2026-10-08", 30, false}, {"2026-10-08", 3, true}, {"2026-10-08", 4, false}, {"2026-10-08", -1, false}, {"2026-10-08", 31, false},
		{"2026-10-8", 0, false}, {"x", 0, false},
	} {
		if err := CheckSplit(from, to, c.split, c.embargo); (err == nil) != c.ok {
			t.Errorf("CheckSplit(%q, %d) = %v, want ok=%v", c.split, c.embargo, err, c.ok)
		}
	}
}

// splitRows sends every row to exactly one place, whatever the days, windows and embargo: the four
// counts add up, no train day reaches split_at, no eval day precedes split_at + embargo, no kept train
// window reaches the eval start. (Synthetic rows, a deterministic sweep over days, hours and windows.)
func TestSplitRowsAccountsForEveryRowAndKeepsTheSidesApart(t *testing.T) {
	const splitAt = "2026-10-08"
	for embargo := 0; embargo <= 3; embargo++ {
		var rows []*row
		for day := 5; day <= 12; day++ {
			for _, hour := range []int{9, 15, 23} {
				for _, windowMin := range []int{0, 30, 60, 90, 300} {
					at := time.Date(2026, 10, day, hour, 50, 0, 0, query.JST)
					r := &row{id: int64(len(rows) + 1), at: at, day: at.Format(time.DateOnly), windowEnd: at.Add(time.Duration(windowMin) * time.Minute), cells: make([]string, off.total)}
					rows = append(rows, r)
				}
			}
		}
		train, eval, inEmbargo, purged := splitRows(rows, splitAt, embargo)
		if len(train)+len(eval)+inEmbargo+purged != len(rows) {
			t.Fatalf("embargo %d: %d+%d+%d+%d != %d", embargo, len(train), len(eval), inEmbargo, purged, len(rows))
		}
		evalStart := time.Date(2026, 10, 8+embargo, 0, 0, 0, 0, query.JST)
		for _, r := range train {
			if r.day >= splitAt || !r.windowEnd.Before(evalStart) || r.cells[cSplit] != SplitTrain {
				t.Errorf("embargo %d: bad train row %s (window to %v) %s", embargo, r.day, r.windowEnd, r.cells[cSplit])
			}
		}
		for _, r := range eval {
			if r.day < evalStart.Format(time.DateOnly) || r.cells[cSplit] != SplitEval {
				t.Errorf("embargo %d: bad eval row %s", embargo, r.day)
			}
		}
		// A row of a train day that was left out is purged exactly when its window reaches the eval start.
		var wantPurged int
		for _, r := range rows {
			if r.day < splitAt && !r.windowEnd.Before(evalStart) {
				wantPurged++
			}
		}
		if purged != wantPurged {
			t.Errorf("embargo %d: purged %d, want %d", embargo, purged, wantPurged)
		}
	}
}
