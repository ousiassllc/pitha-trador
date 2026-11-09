package historytests

import (
	"reflect"
	"testing"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/changeguard"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel/training"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// passing are stored results that clear bar().
func passing() stats {
	return stats{samples: 120, takes: 60, days: 8, ev: &repo.Interval{Low: 1, High: 9}, auc: &repo.Interval{Low: 0.55, High: 0.7}}
}

func rowOf(version, reason string, n int, s stats, a any) row {
	status := repo.ModelRejected
	if reason == "" {
		status = repo.ModelActive
	}
	return row{version: version, status: status, reason: reason, trained: minute(n), samples: s.samples, takes: s.takes, days: s.days,
		evCI: s.ev, aucCI: s.auc, adoption: a}
}

func firstUnmet(m map[string]any) any { return m["first_unmet"] }

// A reject_reason outside the numeric bar (cutoff gate, comparison, change
// rate limit) is returned as stored and first_unmet is NOT aligned to it: it is
// derived from the numeric checks only.
func TestFirstUnmetIsNotAlignedToNonNumericReasons(t *testing.T) {
	path, w := testkit.NewMigratedDB(t)
	c := bar()
	until := "2026-10-09T15:00:00Z"
	low := passing()
	low.takes = 10 // fails the numeric bar too: the cutoff gate overwrote that reason
	cmp := &repo.Comparison{ActiveVersion: "v0", Comparable: true, PairedSamples: 120, CandidateBps: 1, ActiveBps: 2, DiffBps: -1}
	rows := []row{
		rowOf("cutoff", training.RejectLLMModelChanged, 1, passing(), repo.Adoption{Criteria: c}),
		rowOf("cutoff_over_numeric", training.RejectLLMModelChanged, 2, low, repo.Adoption{Criteria: c}),
		rowOf("not_better", training.RejectNotBetter, 3, passing(), repo.Adoption{Criteria: c, Comparison: cmp}),
		rowOf("not_comparable", training.RejectNotComparable, 4, passing(),
			repo.Adoption{Criteria: c, Comparison: &repo.Comparison{ActiveVersion: "v0", Note: "NOTE-SENTINEL"}}),
		rowOf("swap", changeguard.ReasonModelSwapRateLimited, 5, passing(),
			repo.Adoption{Criteria: c, Comparison: cmp, Gate: &repo.Gate{Reason: changeguard.ReasonModelSwapRateLimited, Detail: "DETAIL-SENTINEL", Until: until}}),
		rowOf("freeze", changeguard.ReasonPolicyChangeFreeze, 6, passing(),
			repo.Adoption{Criteria: c, Gate: &repo.Gate{Reason: changeguard.ReasonPolicyChangeFreeze, Detail: "DETAIL-SENTINEL", Until: until}}),
	}
	for _, r := range rows {
		insert(t, w, r)
	}
	models := newServer(t, path).models(t)

	for _, v := range []string{"cutoff", "not_better", "not_comparable", "swap", "freeze"} {
		if got := firstUnmet(models[v]); got != nil {
			t.Errorf("%s: first_unmet = %v, want null (every numeric check is met)", v, got)
		}
		if models[v]["numeric_complete"] != true {
			t.Errorf("%s: numeric_complete = %v", v, models[v]["numeric_complete"])
		}
	}
	if got := models["cutoff_over_numeric"]; got["reject_reason"] != training.RejectLLMModelChanged || firstUnmet(got) != "oos_takes" {
		t.Errorf("overwritten numeric reason: reject_reason %v, first_unmet %v; want the cutoff reason kept and oos_takes", got["reject_reason"], firstUnmet(got))
	}
	for v, r := range map[string]string{"cutoff": training.RejectLLMModelChanged, "not_better": training.RejectNotBetter,
		"not_comparable": training.RejectNotComparable, "swap": changeguard.ReasonModelSwapRateLimited, "freeze": changeguard.ReasonPolicyChangeFreeze} {
		if models[v]["reject_reason"] != r {
			t.Errorf("%s: reject_reason = %v, want %q unchanged", v, models[v]["reject_reason"], r)
		}
	}

	wantStatus := map[string][2]string{ // comparison_status, gate reason ("" = null)
		"cutoff": {"not_performed", ""}, "not_better": {"recorded", ""}, "not_comparable": {"recorded", ""},
		"swap": {"recorded", changeguard.ReasonModelSwapRateLimited}, "freeze": {"not_performed", changeguard.ReasonPolicyChangeFreeze},
	}
	for v, want := range wantStatus {
		m := models[v]
		if m["comparison_status"] != want[0] {
			t.Errorf("%s: comparison_status = %v, want %s", v, m["comparison_status"], want[0])
		}
		if (m["comparison"] == nil) != (want[0] == "not_performed") {
			t.Errorf("%s: comparison = %v with status %s", v, m["comparison"], want[0])
		}
		gate := asMap(m["gate"])
		if want[1] == "" && m["gate"] != nil || want[1] != "" && (gate["reason"] != want[1] || gate["until"] != until) {
			t.Errorf("%s: gate = %v, want reason %q until %s", v, m["gate"], want[1], until)
		}
	}
	got := asMap(models["not_better"]["comparison"])
	want := map[string]any{"active_version": "v0", "comparable": true, "paired_samples": 120.0, "candidate_bps": 1.0, "active_bps": 2.0,
		"diff_bps": -1.0, "diff_ci": nil, "active_ev_ci": nil}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("comparison = %v, want %v", got, want)
	}
}

// A model stored before issue #780 ("{}") or with an unreadable adoption_json
// has no criteria: oos_takes / oos_days are null (the column default 0 is not a
// result) and every check that needs the bar or a missing value is met=null,
// never false or 0.
func TestUnrecordedAndUnreadableModelsAreUnknownNotZero(t *testing.T) {
	path, w := testkit.NewMigratedDB(t)
	s := passing()
	insert(t, w, rowOf("old", training.RejectOOSSamples, 1, stats{samples: 3}, "{}"))
	insert(t, w, rowOf("bad", "", 2, s, "{not json"))
	insert(t, w, rowOf("noci", training.RejectEV, 3, stats{samples: 120, takes: 60, days: 8}, repo.Adoption{Criteria: bar()}))
	models := newServer(t, path).models(t)

	for v, status := range map[string]string{"old": "not_recorded", "bad": "unreadable", "noci": "recorded"} {
		if models[v]["adoption_recorded_status"] != status {
			t.Errorf("%s: adoption_recorded_status = %v, want %s", v, models[v]["adoption_recorded_status"], status)
		}
	}
	for _, v := range []string{"old", "bad"} {
		m := models[v]
		if m["oos_takes"] != nil || m["oos_days"] != nil || m["criteria"] != nil || m["take_rate"] != nil {
			t.Errorf("%s: oos_takes/oos_days/criteria/take_rate = %v/%v/%v/%v, want all null", v, m["oos_takes"], m["oos_days"], m["criteria"], m["take_rate"])
		}
		for _, name := range checkNames {
			got := asMap(asMap(m["numeric_checks"])[name])
			if got["met"] != nil || got["required"] != nil {
				t.Errorf("%s %s: met/required = %v/%v, want null", v, name, got["met"], got["required"])
			}
		}
		if m["numeric_complete"] != false || firstUnmet(m) != nil || m["comparison_status"] != "not_recorded" {
			t.Errorf("%s: numeric_complete %v first_unmet %v comparison_status %v", v, m["numeric_complete"], firstUnmet(m), m["comparison_status"])
		}
		for _, name := range []string{"oos_days", "oos_takes"} {
			if a := asMap(asMap(m["numeric_checks"])[name])["actual"]; a != nil {
				t.Errorf("%s %s actual = %v, want null", v, name, a)
			}
		}
	}
	// oos_ev_ci / oos_auc_ci are their own columns (NULL, not 0, when undefined),
	// so an unreadable adoption_json does not hide them; only the met flag is unknown.
	if got := asMap(models["bad"]["oos_ev_ci"]); got["low"] != 1.0 || got["high"] != 9.0 {
		t.Errorf("bad oos_ev_ci = %v, want the stored interval", got)
	}
	if got := asMap(asMap(models["bad"]["numeric_checks"])["oos_ev_ci_low"]); got["actual"] != 1.0 || got["met"] != nil {
		t.Errorf("bad oos_ev_ci_low = %v, want actual 1, met null", got)
	}
	// oos_samples is a real stored value even for a row without criteria.
	if a := asMap(asMap(models["old"]["numeric_checks"])["oos_samples"])["actual"]; a != 3.0 {
		t.Errorf("old oos_samples actual = %v, want the stored 3", a)
	}

	// Undefined CIs: those checks are unknown, the rest are judged.
	m := models["noci"]
	for _, name := range []string{"oos_ev_ci_low", "oos_auc_ci_low"} {
		got := asMap(asMap(m["numeric_checks"])[name])
		if got["actual"] != nil || got["met"] != nil || got["required"] == nil {
			t.Errorf("noci %s = %v, want actual null, met null, required set", name, got)
		}
	}
	if m["numeric_complete"] != false || firstUnmet(m) != nil || m["oos_ev_ci"] != nil || m["oos_auc_ci"] != nil {
		t.Errorf("noci: numeric_complete %v first_unmet %v oos_ev_ci %v oos_auc_ci %v", m["numeric_complete"], firstUnmet(m), m["oos_ev_ci"], m["oos_auc_ci"])
	}
	if got := asMap(asMap(m["numeric_checks"])["oos_takes"])["met"]; got != true {
		t.Errorf("noci oos_takes met = %v, want true: the other checks are still judged", got)
	}
}

// With a CI undefined, first_unmet is the first KNOWN unmet check, not
// necessarily the real first (numeric_complete=false says so).
func TestFirstUnmetWithUnknownChecksIsTheFirstKnown(t *testing.T) {
	path, w := testkit.NewMigratedDB(t)
	s := passing()
	s.takes, s.ev = 5, nil
	insert(t, w, rowOf("partial", training.RejectTakes, 1, s, repo.Adoption{Criteria: bar()}))
	m := newServer(t, path).models(t)["partial"]
	if firstUnmet(m) != "oos_takes" || m["numeric_complete"] != false {
		t.Errorf("first_unmet %v numeric_complete %v, want oos_takes / false", firstUnmet(m), m["numeric_complete"])
	}
}

// A model really stored with oos_takes = 0 (recorded) is a result: actual 0,
// met false; take_rate 0.
func TestRecordedZeroTakesIsAResult(t *testing.T) {
	path, w := testkit.NewMigratedDB(t)
	s := passing()
	s.takes = 0
	insert(t, w, rowOf("zero", training.RejectTakes, 1, s, repo.Adoption{Criteria: bar()}))
	m := newServer(t, path).models(t)["zero"]
	got := asMap(asMap(m["numeric_checks"])["oos_takes"])
	if got["actual"] != 0.0 || got["met"] != false || got["required"] != 50.0 || m["oos_takes"] != 0.0 || m["take_rate"] != 0.0 {
		t.Errorf("oos_takes check = %v, oos_takes %v, take_rate %v; want actual 0, met false", got, m["oos_takes"], m["take_rate"])
	}
}

// criteria is what the model was judged against, never today's default.
func TestCriteriaAreTheStoredOnesNotTheDefaults(t *testing.T) {
	path, w := testkit.NewMigratedDB(t)
	c := repo.Criteria{MinOOSSamples: 7, MinTakes: 3, MinOOSDays: 2, MinEVLowerBps: -1.5, MinAUCLower: 0.45, Alpha: 0.05, Resamples: 200, Seed: 9}
	if d := training.DefaultAcceptanceConfig(); d.MinOOSSamples == c.MinOOSSamples || d.MinTakes == c.MinTakes {
		t.Fatalf("fixture drifted: criteria equal the defaults %+v", d)
	}
	insert(t, w, rowOf("custom", "", 1, passing(), repo.Adoption{Criteria: c}))
	insert(t, w, rowOf("none", training.RejectOOSSamples, 2, passing(), "{}"))
	models := newServer(t, path).models(t)
	want := map[string]any{"min_oos_samples": 7.0, "min_takes": 3.0, "min_oos_days": 2.0, "min_ev_lower_bps": -1.5,
		"min_auc_lower": 0.45, "alpha": 0.05, "resamples": 200.0}
	if got := asMap(models["custom"]["criteria"]); !reflect.DeepEqual(got, want) {
		t.Errorf("criteria = %v, want %v (no seed, no defaults)", got, want)
	}
	if models["none"]["criteria"] != nil {
		t.Errorf("criteria of a model without a record = %v, want null: the defaults are not filled in", models["none"]["criteria"])
	}
}
