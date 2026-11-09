package historytests

import (
	"fmt"
	"testing"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel/training"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// checkNames are the five numeric checks in training.AcceptanceConfig.Judge's order.
var checkNames = []string{"oos_samples", "oos_days", "oos_takes", "oos_ev_ci_low", "oos_auc_ci_low"}

// rejectOf is the reject_reason Judge records when the check is the first unmet.
var rejectOf = map[string]string{
	"oos_samples": training.RejectOOSSamples, "oos_days": training.RejectOOSDays, "oos_takes": training.RejectTakes,
	"oos_ev_ci_low": training.RejectEV, "oos_auc_ci_low": training.RejectAUC,
}

func acceptance(c repo.Criteria) training.AcceptanceConfig {
	return training.AcceptanceConfig{MinOOSSamples: c.MinOOSSamples, MinTakes: c.MinTakes, MinOOSDays: c.MinOOSDays,
		MinAUCLower: c.MinAUCLower, MinEVLowerBps: c.MinEVLowerBps, Alpha: c.Alpha, Resamples: c.Resamples}
}

// stats are the stored results of one model.
type stats struct {
	samples, takes, days int
	ev, auc              *repo.Interval
}

// judged returns Judge's verdict on s under c: what the Trainer would store.
func judged(c repo.Criteria, s stats) (status, reason string) {
	return acceptance(c).Judge(training.Evaluation{Samples: s.samples, Takes: s.takes}, training.Intervals{Days: s.days, EV: s.ev, AUC: s.auc})
}

// variant builds stats where bit i of unmet breaks check i. The other checks
// sit exactly on the count minimum or just above the CI bar; a broken one is
// just under the count minimum / equal to the CI bar (boundary: where >= and >
// differ) or well under it.
func variant(c repo.Criteria, unmet int, boundary bool) stats {
	s := stats{samples: c.MinOOSSamples, days: c.MinOOSDays, takes: c.MinTakes, // exactly the minimum: >= is met
		ev: &repo.Interval{Low: c.MinEVLowerBps + 0.0001, High: c.MinEVLowerBps + 10}, auc: &repo.Interval{Low: c.MinAUCLower + 0.0001, High: 0.9}}
	step := 10
	if boundary {
		step = 0
	}
	if unmet&1 != 0 {
		s.samples = c.MinOOSSamples - max(step, 1)
	}
	if unmet&2 != 0 {
		s.days = c.MinOOSDays - max(step/5, 1)
	}
	if unmet&4 != 0 {
		s.takes = c.MinTakes - max(step, 1)
	}
	if unmet&8 != 0 { // oos_ev_ci_low > min: equal to the minimum is unmet
		s.ev = &repo.Interval{Low: c.MinEVLowerBps - float64(step), High: c.MinEVLowerBps + 10}
	}
	if unmet&16 != 0 {
		s.auc = &repo.Interval{Low: c.MinAUCLower - float64(step)/100, High: 0.9}
	}
	return s
}

// Every combination of unmet numeric checks, at the boundary (>= vs >; a CI
// lower limit equal to the bar is unmet) and well under it: each check's met
// flag, first_unmet and the reject_reason Judge records agree with Judge.
func TestNumericChecksAgreeWithJudge(t *testing.T) {
	for _, boundary := range []bool{true, false} {
		t.Run(fmt.Sprintf("boundary=%v", boundary), func(t *testing.T) {
			path, w := testkit.NewMigratedDB(t)
			c := bar()
			for mask := range 32 {
				s := variant(c, mask, boundary)
				status, reason := judged(c, s)
				insert(t, w, row{version: fmt.Sprintf("m%02d", mask), status: status, reason: reason, trained: minute(mask),
					samples: s.samples, takes: s.takes, days: s.days, evCI: s.ev, aucCI: s.auc, adoption: repo.Adoption{Criteria: c}})
			}
			models := newServer(t, path).models(t)
			if len(models) != 32 {
				t.Fatalf("got %d models, want 32", len(models))
			}
			for mask := range 32 {
				s := variant(c, mask, boundary)
				_, reason := judged(c, s)
				m := models[fmt.Sprintf("m%02d", mask)]
				checks := asMap(m["numeric_checks"])
				first := any(nil)
				for i, name := range checkNames {
					met := mask&(1<<i) == 0
					got := asMap(checks[name])
					if got["met"] != met {
						t.Errorf("mask %05b %s: met = %v, want %v", mask, name, got["met"], met)
					}
					if first == nil && !met {
						first = name
					}
				}
				if m["first_unmet"] != first {
					t.Errorf("mask %05b: first_unmet = %v, want %v", mask, m["first_unmet"], first)
				}
				if first == nil && reason != "" || first != nil && reason != rejectOf[first.(string)] {
					t.Errorf("mask %05b: Judge reason %q does not match first unmet %v", mask, reason, first)
				}
				if m["reject_reason"] != reason {
					t.Errorf("mask %05b: reject_reason = %v, want the stored %q", mask, m["reject_reason"], reason)
				}
				if m["numeric_complete"] != true {
					t.Errorf("mask %05b: numeric_complete = %v, want true", mask, m["numeric_complete"])
				}
			}
		})
	}
}

// The exact values behind the boundary: actual and required are what was
// stored and what the model was judged against, and a CI lower limit equal to
// the bar is met=false.
func TestNumericChecksReportActualAndRequired(t *testing.T) {
	path, w := testkit.NewMigratedDB(t)
	c := bar()
	s := stats{samples: 100, days: 7, takes: 50, ev: &repo.Interval{Low: 0, High: 9}, auc: &repo.Interval{Low: 0.5, High: 0.7}}
	status, reason := judged(c, s)
	if status != repo.ModelRejected || reason != training.RejectEV {
		t.Fatalf("fixture drifted: Judge = %s %s, want the EV bar (lower limit equal to it)", status, reason)
	}
	insert(t, w, row{version: "b", status: status, reason: reason, trained: minute(1), samples: s.samples, takes: s.takes, days: s.days,
		evCI: s.ev, aucCI: s.auc, adoption: repo.Adoption{Criteria: c}})
	m := newServer(t, path).models(t)["b"]
	want := map[string][3]any{
		"oos_samples": {100.0, 100.0, true}, "oos_days": {7.0, 7.0, true}, "oos_takes": {50.0, 50.0, true},
		"oos_ev_ci_low": {0.0, 0.0, false}, "oos_auc_ci_low": {0.5, 0.5, false},
	}
	for name, w := range want {
		got := asMap(asMap(m["numeric_checks"])[name])
		if got["actual"] != w[0] || got["required"] != w[1] || got["met"] != w[2] {
			t.Errorf("%s = %v, want actual %v required %v met %v", name, got, w[0], w[1], w[2])
		}
	}
	if m["take_rate"] != 0.5 || m["oos_takes"] != 50.0 || m["oos_days"] != 7.0 {
		t.Errorf("take_rate/oos_takes/oos_days = %v/%v/%v, want 0.5/50/7", m["take_rate"], m["oos_takes"], m["oos_days"])
	}
	if got := asMap(m["oos_ev_ci"]); got["low"] != 0.0 || got["high"] != 9.0 {
		t.Errorf("oos_ev_ci = %v", got)
	}
}
