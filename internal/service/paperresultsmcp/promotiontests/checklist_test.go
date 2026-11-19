package promotiontests

import (
	"math"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/stats"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/promotiondiag"
)

var itemNames = []string{
	"same_rows_and_period", "common_rows", "common_days", "diff_ci_lower", "multiple_comparison", "positive_days",
	"not_one_day_wins", "max_drawdown", "after_ai_cost", "after_training_cutoff", "model_version_stable",
}

// both are the conversion arguments: 0.001 USD x 150 / 1e6 x 1e4 is 0.0015 bps per row.
var both = map[string]any{"usd_jpy": 150.0, "notional_jpy": 1_000_000.0}

func status(res promotiondiag.Result) map[string]string {
	out := map[string]string{}
	for _, it := range res.Items {
		out[it.Name] = it.Status
	}
	return out
}

// A candidate that follows the market on every row (+30 bps) against a live Jev that always answers LONG
// (+30 / -30) passes all eleven items, in the fixed order, once the cutoff is confirmed and the cost
// conversion is given; the key numbers are hand-checkable (+15 bps mean difference on 312 rows, 12 days).
func TestStrongCandidatePassesEveryItem(t *testing.T) {
	t.Parallel()
	srv, w := newServer(t, "")
	decs := seed(t, w)
	shadowAnswers(t, w, "trader-v9", "strong-model", 0.001, decs, follow)
	confirmCutoff(t, w)
	res := srv.call(t, "trader-v9", both).result(t)

	if len(res.Items) != 11 {
		t.Fatalf("%d items", len(res.Items))
	}
	for i, it := range res.Items {
		if it.Name != itemNames[i] || it.Status != promotiondiag.StatusPass {
			t.Errorf("item %d = %+v, want %s pass", i, it, itemNames[i])
		}
	}
	if res.FirstUnmet != nil || !res.EligibleAsEvidence || res.Applied {
		t.Errorf("first_unmet=%v eligible=%v applied=%v", res.FirstUnmet, res.EligibleAsEvidence, res.Applied)
	}
	kn := res.KeyNumbers
	if kn.CommonRows != 312 || kn.ReferenceRows != 312 || kn.Days != 12 || kn.Coverage == nil || *kn.Coverage != 1 {
		t.Errorf("key numbers = %+v", kn)
	}
	// the strong candidate earns +30 on every row; the live +30 on up rows and -30 on down rows
	if kn.MeanNetBps == nil || math.Abs(*kn.MeanNetBps-30) > eps || kn.DiffMeanBps == nil || math.Abs(*kn.DiffMeanBps-(30-*kn.CurrentMeanBps)) > eps || *kn.DiffMeanBps < 5 {
		t.Errorf("means = %v current %v diff %v", kn.MeanNetBps, kn.CurrentMeanBps, kn.DiffMeanBps)
	}
	if kn.DiffCI == nil || kn.DiffCI.Lower <= 0 || kn.HolmAdjustedP == nil || *kn.HolmAdjustedP > 0.05 || kn.PositiveDayPct == nil || *kn.PositiveDayPct != 100 {
		t.Errorf("ci/p = %+v", kn)
	}
	if d := item(t, res, "after_ai_cost"); d.Actual.(float64) <= 0 { // the cheaper candidate: its difference after the cost difference is a bit above the raw one
		t.Errorf("after_ai_cost = %+v", d)
	}
	if res.Criteria.Effective != res.Criteria.Floor || res.Criteria.Floor.MinCommonRows != 300 || res.Criteria.Floor.MinDays != 10 || res.Criteria.Floor.MinDiffCILowerBps != 0 ||
		res.Criteria.Floor.MinPositiveDayPct != 50 || res.Criteria.Floor.MaxAdjustedP != 0.05 || res.Criteria.MinCoverage != 0.8 {
		t.Errorf("criteria = %+v", res.Criteria)
	}
	for k, s := range res.Criteria.Source {
		if s != "floor" {
			t.Errorf("source of %s = %s, want floor", k, s)
		}
	}
}

// Without usd_jpy and notional_jpy the cost item is unknown (not pass, not fail), and while the LLM cutoff
// is the placeholder so is the cutoff item; first_unmet is the first of them in the fixed order and the
// candidate is not eligible as evidence.
func TestUnknownCostAndPlaceholderCutoffAreNotPass(t *testing.T) {
	t.Parallel()
	srv, w := newServer(t, "")
	decs := seed(t, w)
	shadowAnswers(t, w, "trader-v9", "strong-model", 0.001, decs, follow)
	res := srv.call(t, "trader-v9", nil).result(t)
	got := status(res)
	for _, name := range itemNames {
		want := promotiondiag.StatusPass
		if name == "after_ai_cost" || name == "after_training_cutoff" {
			want = promotiondiag.StatusUnknown
		}
		if got[name] != want {
			t.Errorf("%s = %s, want %s", name, got[name], want)
		}
	}
	if d := item(t, res, "after_ai_cost"); d.Detail != "usd_jpy_and_notional_jpy_required" || d.Actual != nil {
		t.Errorf("after_ai_cost = %+v", d)
	}
	if d := item(t, res, "after_training_cutoff"); d.Detail != "llm_cutoff_is_placeholder" {
		t.Errorf("after_training_cutoff = %+v", d)
	}
	if res.FirstUnmet == nil || *res.FirstUnmet != "after_ai_cost" || res.EligibleAsEvidence || res.Applied {
		t.Errorf("first_unmet=%v eligible=%v applied=%v", res.FirstUnmet, res.EligibleAsEvidence, res.Applied)
	}
	// with the conversion but a live cost that is unknown on one row, the cost item stays unknown
	testkitNullCost(t, w)
	res = srv.call(t, "trader-v9", both).result(t)
	if d := item(t, res, "after_ai_cost"); d.Status != promotiondiag.StatusUnknown || d.Detail != "reference_cost_unknown" {
		t.Errorf("after_ai_cost with an unknown live cost = %+v", d)
	}
}

// A candidate that mirrors the live Jev on some rows and its opposite on others has no edge: the checklist
// reports the first unmet item (the difference's interval lower end) with the actual and the required number,
// and the later items are still evaluated.
func TestWeakCandidateReportsTheFirstUnmetItemWithNumbers(t *testing.T) {
	t.Parallel()
	srv, w := newServer(t, "")
	decs := seed(t, w)
	shadowAnswers(t, w, "trader-v8", "weak-model", 0.001, decs, func(d decision) string {
		if d.id%2 == 0 {
			return follow(d)
		}
		if d.up { // the side that loses
			return "SHORT"
		}
		return "LONG"
	})
	confirmCutoff(t, w)
	res := srv.call(t, "trader-v8", both).result(t)
	if res.FirstUnmet == nil || *res.FirstUnmet != "diff_ci_lower" || res.EligibleAsEvidence {
		t.Fatalf("first_unmet = %v eligible %v; items %+v", res.FirstUnmet, res.EligibleAsEvidence, res.Items)
	}
	d := item(t, res, "diff_ci_lower")
	if d.Status != promotiondiag.StatusFail || d.Required.(float64) != 0 || d.Actual.(float64) >= 0 || d.Comparator != ">" {
		t.Errorf("diff_ci_lower = %+v", d)
	}
	if got := status(res); got["same_rows_and_period"] != "pass" || got["common_rows"] != "pass" || got["common_days"] != "pass" {
		t.Errorf("earlier items = %v", got)
	}
	if m := item(t, res, "multiple_comparison"); m.Status != promotiondiag.StatusFail || m.Actual.(float64) <= 0.05 || m.Required.(float64) != 0.05 {
		t.Errorf("multiple_comparison = %+v", m)
	}
}

// A candidate that answered only 60% (188 of 312) of the live rows is not on the same rows: the coverage item fails first,
// with the share as the actual value, and a stricter per-call minimum fails the row count with its number.
func TestCoverageAndRowCountFailWithTheirNumbers(t *testing.T) {
	t.Parallel()
	srv, w := newServer(t, "")
	decs := seed(t, w)
	shadowAnswers(t, w, "trader-part", "strong-model", 0.001, decs, func(d decision) string {
		if d.id%5 < 3 {
			return follow(d)
		}
		return ""
	})
	shadowAnswers(t, w, "trader-most", "strong-model", 0.001, decs, func(d decision) string {
		if d.id%25 != 0 { // 300 of 312 rows
			return follow(d)
		}
		return ""
	})
	part := srv.call(t, "trader-part", nil).result(t)
	if part.FirstUnmet == nil || *part.FirstUnmet != "same_rows_and_period" {
		t.Fatalf("first_unmet = %v", part.FirstUnmet)
	}
	if c := item(t, part, "same_rows_and_period"); c.Status != "fail" || math.Abs(c.Actual.(float64)-188.0/312) > eps || c.Required.(float64) != 0.8 {
		t.Errorf("coverage = %+v", c)
	}
	if c := item(t, part, "common_rows"); c.Status != "fail" || c.Actual.(float64) != 188 || c.Required.(float64) != 300 {
		t.Errorf("common_rows = %+v", c)
	}
	strict := srv.call(t, "trader-most", map[string]any{"min_common_rows": 400, "min_days": 13}).result(t)
	if c := item(t, strict, "common_rows"); c.Status != "fail" || c.Actual.(float64) != 300 || c.Required.(float64) != 400 {
		t.Errorf("tightened common_rows = %+v", c)
	}
	if c := item(t, strict, "common_days"); c.Status != "fail" || c.Actual.(float64) != 12 || c.Required.(float64) != 13 {
		t.Errorf("tightened common_days = %+v", c)
	}
	if strict.Criteria.Source["research.promotion.min_days"] != "call" || strict.Criteria.Effective.MinDays != 13 || strict.Criteria.Floor.MinDays != 10 {
		t.Errorf("criteria = %+v", strict.Criteria)
	}
}

// The Holm-adjusted p of the checklist is the candidate ledger's own over the same family (every candidate
// of the period), widened by family_size_extra: the same numbers a person reads in candidate_ledger.
func TestAdjustedPIsTheLedgersAndWidensWithTheFamily(t *testing.T) {
	t.Parallel()
	srv, w := newServer(t, "")
	decs := seed(t, w)
	shadowAnswers(t, w, "trader-v9", "strong-model", 0.001, decs, follow)
	shadowAnswers(t, w, "trader-v7", "strong-model", 0.001, decs, func(d decision) string {
		if d.id%3 == 0 {
			return follow(d)
		}
		return "LONG"
	})
	ledger := srv.post(t, "candidate_ledger", withPeriod(map[string]any{"alignment": "pairwise"})).data(t)
	var ps []float64
	ids := map[string]int{}
	for _, c := range ledger["candidates"].([]any) {
		m := c.(map[string]any)
		if d, ok := m["diff_vs_live"].(map[string]any); ok {
			ids[m["id"].(string)] = len(ps)
			if d["p_value"] != nil {
				ps = append(ps, d["p_value"].(float64))
			} else {
				ps = append(ps, 1) // an undefined p still counts in the family, as p = 1
			}
		}
	}
	if len(ps) < 3 {
		t.Fatalf("the family has %d members", len(ps))
	}
	var adjusted [2]float64
	for k, extra := range []int{0, 5} {
		family := append(append([]float64{}, ps...), make([]float64, extra)...)
		for i := len(ps); i < len(family); i++ {
			family[i] = 1
		}
		want := stats.Holm(family)[ids["shadow:trader-v9"]]
		res := srv.call(t, "trader-v9", map[string]any{"family_size_extra": extra}).result(t)
		got := res.KeyNumbers.HolmAdjustedP
		if got != nil {
			adjusted[k] = *got
		}
		if got == nil || math.Abs(*got-want) > 1e-12 || res.KeyNumbers.FamilySize != len(ps)+extra || res.KeyNumbers.FamilySizeExtra != extra {
			t.Errorf("extra %d: adjusted p = %v family %d, want %v family %d", extra, got, res.KeyNumbers.FamilySize, want, len(ps)+extra)
		}
	}
	if adjusted[1] <= adjusted[0] { // a wider family raises the adjusted p
		t.Errorf("a wider family did not raise the adjusted p: %v vs %v", adjusted[1], adjusted[0])
	}
}

// Two model ids on the live side in the period (or on the candidate's) fail model_version_stable with the counts.
func TestModelVersionStableFailsWithCounts(t *testing.T) {
	t.Parallel()
	srv, w := newServer(t, "")
	decs := seed(t, w)
	shadowAnswers(t, w, "trader-v9", "strong-model", 0.001, decs, follow)
	if res := srv.call(t, "trader-v9", nil).result(t); item(t, res, "model_version_stable").Status != "pass" {
		t.Fatalf("model_version_stable = %+v", item(t, res, "model_version_stable"))
	}
	testkitExec(t, w, `UPDATE jev_decisions SET model_id = 'live-model-2' WHERE id = 1`)
	testkitExec(t, w, `UPDATE jev_shadow_decisions SET model_id = 'strong-model-2' WHERE jev_decision_id IN (2, 3)`)
	m := item(t, srv.call(t, "trader-v9", nil).result(t), "model_version_stable")
	counts := m.Actual.(map[string]any)
	if m.Status != "fail" || m.Detail != "more_than_one_model_id" || counts["candidate_model_ids"].(float64) != 2 || counts["current_model_ids"].(float64) != 2 {
		t.Errorf("model_version_stable = %+v", m)
	}
}
