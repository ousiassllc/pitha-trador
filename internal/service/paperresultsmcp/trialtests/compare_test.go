package trialtests

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query/trialquery"
)

func near(t *testing.T, name string, got *float64, want float64) {
	t.Helper()
	if got == nil || math.Abs(*got-want) > 1e-9 {
		var g any
		if got != nil {
			g = *got
		}
		t.Errorf("%s = %v, want %v", name, g, want)
	}
}

func armReport(t *testing.T, c trialquery.Comparison, arm string) trialquery.ArmReport {
	t.Helper()
	for _, a := range c.PerArm {
		if a.ArmID == arm {
			return a
		}
	}
	t.Fatalf("no arm %s in %+v", arm, c.Arms)
	return trialquery.ArmReport{}
}

func armOutcome(t *testing.T, c trialquery.Comparison, horizon, labelVersion int, arm string) trialquery.ArmOutcome {
	t.Helper()
	for _, s := range c.Outcomes {
		if s.HorizonMinutes != horizon || s.LabelVersion != labelVersion {
			continue
		}
		for _, a := range s.Arms {
			if a.ArmID == arm {
				return a
			}
		}
	}
	t.Fatalf("no outcome slice %d/v%d for %s", horizon, labelVersion, arm)
	return trialquery.ArmOutcome{}
}

func compare(t *testing.T, srv *testServer, args map[string]any) trialquery.Comparison {
	t.Helper()
	var c trialquery.Comparison
	srv.call(t, "jev_trial_compare", args).data(t, "jev_trial_compare", &c)
	return c
}

func withArms(arms ...string) map[string]any {
	return map[string]any{"from": period["from"], "to": period["to"], "arms": arms}
}

// The missing rate's denominator holds failed, refused and partial rows, and the
// common-success count is the decisions every selected arm answered with ok.
func TestCompareMissingRateAndCommonSuccess(t *testing.T) {
	srv, w := newServer(t)
	seedMain(t, w)
	c := compare(t, srv, period)

	if got := strings.Join(c.Arms, ","); got != "arm-a,arm-b,arm-c" {
		t.Fatalf("arms = %s", got)
	}
	if c.Cohort.Size != 6 || c.Cohort.CommonSuccessCount != 1 {
		t.Fatalf("cohort size %d / common_success_count %d, want 6 / 1 (decision 7 was asked of arm-a only; 8 is outside the period)", c.Cohort.Size, c.Cohort.CommonSuccessCount)
	}
	near(t, "common_success_rate", c.Cohort.CommonSuccessRate, 1.0/6)
	if c.Cohort.Definition == "" || c.Cohort.CommonSuccessDefinition == "" {
		t.Error("the cohort definition is not stated")
	}

	a, b, cc := armReport(t, c, armA), armReport(t, c, armB), armReport(t, c, armC)
	if a.PeriodRows.Total != 7 || a.PeriodRows.OK != 7 || a.PeriodRows.MissingRate == nil || *a.PeriodRows.MissingRate != 0 {
		t.Errorf("arm-a period rows = %+v", a.PeriodRows)
	}
	if b.PeriodRows.Total != 6 || b.PeriodRows.OK != 3 || b.PeriodRows.Failed != 1 || b.PeriodRows.Refused != 1 || b.PeriodRows.Partial != 1 {
		t.Errorf("arm-b period rows = %+v", b.PeriodRows)
	}
	near(t, "arm-b missing_rate", b.PeriodRows.MissingRate, 3.0/6)
	near(t, "arm-c missing_rate", cc.PeriodRows.MissingRate, 5.0/6)
	if cc.PeriodRows.FailureCode["timeout"] != 1 || cc.PeriodRows.FailureCode["http_402"] != 1 || cc.PeriodRows.FailureCode["http_529"] != 1 || b.PeriodRows.FailureCode["http_429"] != 1 {
		t.Errorf("failure codes: b=%v c=%v", b.PeriodRows.FailureCode, cc.PeriodRows.FailureCode)
	}
	// The cohort rows of arm-a leave out decision 7.
	if a.CohortRows.Total != 6 || c.Cohort.Rows.Total != 18 {
		t.Errorf("cohort rows: arm-a %d, all %d, want 6 / 18", a.CohortRows.Total, c.Cohort.Rows.Total)
	}
	near(t, "cohort missing_rate", c.Cohort.Rows.MissingRate, 8.0/18)
	if c.Cohort.Rows.OK != 10 || c.Cohort.Rows.Failed != 6 || c.Cohort.Rows.Refused != 1 || c.Cohort.Rows.Partial != 1 {
		t.Errorf("cohort rows = %+v", c.Cohort.Rows)
	}
}

// Selecting arms changes the cohort and the period filter drops other days.
func TestCompareArmAndPeriodFilters(t *testing.T) {
	srv, w := newServer(t)
	seedMain(t, w)
	c := compare(t, srv, withArms(armA, armB))
	if len(c.Arms) != 2 || c.Cohort.Size != 6 || c.Cohort.CommonSuccessCount != 3 {
		t.Fatalf("arms %v cohort %d common %d, want 2 arms / 6 / 3 (decisions 1, 5, 6)", c.Arms, c.Cohort.Size, c.Cohort.CommonSuccessCount)
	}
	if one := compare(t, srv, withArms(armC)); len(one.Arms) != 1 || one.Cohort.Size != 6 || one.Cohort.CommonSuccessCount != 1 {
		t.Errorf("one arm: %+v", one.Cohort)
	}
	// An arm without rows has an empty cohort, not an error.
	empty := compare(t, srv, withArms(armA, "arm-nope"))
	if empty.Cohort.Size != 0 || empty.Cohort.CommonSuccessRate != nil || len(empty.Outcomes) != 0 {
		t.Errorf("unknown arm cohort = %+v outcomes %v", empty.Cohort, empty.Outcomes)
	}
	// A period holding only decision 8 sees its three failed rows alone.
	aug := compare(t, srv, map[string]any{"from": "2026-07-25", "to": "2026-08-05"})
	if aug.Cohort.Size != 1 || aug.Cohort.CommonSuccessCount != 0 || aug.Cohort.Rows.Failed != 3 {
		t.Errorf("august = %+v", aug.Cohort)
	}
}

// Truncation, RAG and cutoff are counted, never excluded.
func TestCompareCountsFairnessFlagsWithoutExcludingRows(t *testing.T) {
	srv, w := newServer(t)
	seedMain(t, w)
	c := compare(t, srv, period)
	r := c.Cohort.Rows
	if r.TruncationSuspected != 1 || r.RAGIncludesLiveModelAnswers != 2 || r.LLMCutoffStatus["unverified"] != 17 || r.LLMCutoffStatus["confirmed"] != 1 {
		t.Errorf("cohort flags = %+v", r)
	}
	if b := armReport(t, c, armB); b.CohortRows.TruncationSuspected != 1 || b.CohortRows.Total != 6 {
		t.Errorf("arm-b = %+v", b.CohortRows)
	}
	if a := armReport(t, c, armA); a.CohortRows.RAGIncludesLiveModelAnswers != 1 || a.CohortRows.LLMCutoffStatus["confirmed"] != 1 {
		t.Errorf("arm-a = %+v", a.CohortRows)
	}
	if c.Fairness.CohortSize != 6 || strings.Join(c.Fairness.Caveats, ",") != "truncation_suspected,rag_includes_live_model_answers,llm_cutoff_unverified" {
		t.Errorf("fairness = %+v", c.Fairness)
	}
	// The flagged rows still take part in the metrics: decision 1 (flagged on arm-b and arm-c) is a hit of every arm.
	if o := armOutcome(t, c, 5, 2, armB); o.Cohort.Hits != 2 || o.Cohort.Records != 6 {
		t.Errorf("arm-b outcome = %+v", o.Cohort)
	}
	if !strings.Contains(c.Notice, "公平な比較ではない") || !strings.Contains(c.Notice, "推奨ではない") {
		t.Errorf("notice = %s", c.Notice)
	}
}

// The directional metrics: NONE is its own bucket (not a hit, not a miss, in the cohort denominator),
// a non-ok row is "no answer" in the cohort denominator, and over_answered is labelled apart.
func TestCompareOutcomesNoneIsNotImputedAndMissesStayInTheDenominator(t *testing.T) {
	srv, w := newServer(t)
	seedMain(t, w)
	c := compare(t, srv, period)

	a := armOutcome(t, c, 5, 2, armA).Cohort
	if a.Records != 6 || a.NoneCount != 1 || a.Scorable != 5 || a.Hits != 4 || a.NoAnswer != 0 {
		t.Errorf("arm-a = %+v", a)
	}
	near(t, "arm-a over_cohort", a.OverCohort.Rate, 4.0/6) // the NONE stays in the denominator
	near(t, "arm-a over_answered", a.OverAnswered.Rate, 4.0/5)
	near(t, "arm-a coverage", a.Coverage.Rate, 5.0/6)
	byEQ := map[string][2]int{}
	for _, q := range a.ByEntryQuality {
		byEQ[q.EntryQuality] = [2]int{q.Scorable, q.Hits}
	}
	if byEQ["good"] != [2]int{2, 2} || byEQ["fair"] != [2]int{1, 0} || byEQ["poor"] != [2]int{1, 1} || byEQ["strong"] != [2]int{1, 1} || len(a.ByEntryQuality) != 5 {
		t.Errorf("arm-a by entry_quality = %v", byEQ)
	}

	b := armOutcome(t, c, 5, 2, armB).Cohort
	if b.Records != 6 || b.NoAnswer != 3 || b.Scorable != 3 || b.Hits != 2 {
		t.Errorf("arm-b = %+v", b) // 3 = decision 2 failed, 3 refused, 4 partial
	}
	near(t, "arm-b over_cohort", b.OverCohort.Rate, 2.0/6)
	near(t, "arm-b over_answered", b.OverAnswered.Rate, 2.0/3)

	cc := armOutcome(t, c, 5, 2, armC).Cohort
	near(t, "arm-c over_cohort", cc.OverCohort.Rate, 1.0/6)
	near(t, "arm-c over_answered", cc.OverAnswered.Rate, 1) // 1 of 1 answered looks perfect; the cohort rate does not

	// Direction counts and agreement keep the same two denominators.
	ad := armReport(t, c, armA)
	if ad.Direction.NoneCount != 1 || ad.Direction.Cohort != 6 || ad.Direction.Answered != 6 {
		t.Errorf("arm-a direction = %+v", ad.Direction)
	}
	near(t, "arm-a agreement over_cohort", ad.Agreement.OverCohort, 4.0/6)
	cd := armReport(t, c, armC)
	near(t, "arm-c agreement over_cohort", cd.Agreement.OverCohort, 1.0/6)
	near(t, "arm-c agreement over_answered", cd.Agreement.OverAnswered, 1)
	if cd.Direction.NoAnswer != 5 {
		t.Errorf("arm-c direction = %+v", cd.Direction)
	}
	for _, q := range cd.EntryQuality {
		if q.EntryQuality == "poor" {
			near(t, "arm-c poor share over_cohort", q.OverCohort, 1.0/6)
		}
	}

	// The common-success subset is decision 1 only, labelled informational. Label versions are not mixed.
	cs := armOutcome(t, c, 5, 2, armA).CommonSuccess
	if !cs.Informational || cs.Records != 1 || cs.Hits != 1 {
		t.Errorf("arm-a common success = %+v", cs)
	}
	if v1 := armOutcome(t, c, 10, 1, armA).Cohort; v1.Records != 1 {
		t.Errorf("label version 1 slice = %+v", v1)
	}
}

// A decision the arm answered NONE (or did not answer) that has an existing counterfactual label is
// reported under counterfactual_proxy (proxy: true) only, never in the realised metrics.
func TestCompareCounterfactualProxyIsSeparateAndLabelled(t *testing.T) {
	srv, w := newServer(t)
	seedMain(t, w)
	c := compare(t, srv, period)
	p := c.CounterfactualProxy
	if !p.Proxy || p.Note == "" || len(p.Arms) != 3 {
		t.Fatalf("proxy block = %+v", p)
	}
	hor := func(arm string) (trialquery.ProxyArm, trialquery.ProxyHorizon) {
		for _, a := range p.Arms {
			if a.ArmID == arm {
				if len(a.Horizons) != 1 || a.Horizons[0].HorizonMinutes != 5 {
					t.Fatalf("%s proxy horizons = %+v", arm, a.Horizons)
				}
				return a, a.Horizons[0]
			}
		}
		t.Fatalf("no proxy arm %s", arm)
		return trialquery.ProxyArm{}, trialquery.ProxyHorizon{}
	}
	a, ah := hor(armA)
	if a.NoDirectionDecision != 1 || a.NoneAnswers != 1 || ah.Labelled != 1 || ah.BarrierHit["sl"] != 1 || ah.BarrierHit["tp"] != 0 {
		t.Errorf("arm-a proxy = %+v %+v (decision 1's label belongs to an answered LONG and is not counted)", a, ah)
	}
	near(t, "arm-a proxy avg", ah.AvgNetReturnBps, -12.5)
	b, bh := hor(armB)
	if b.NoDirectionDecision != 3 || b.NoAnswer != 3 || bh.Labelled != 1 || bh.BarrierHit["tp"] != 1 {
		t.Errorf("arm-b proxy = %+v %+v", b, bh)
	}
	near(t, "arm-b proxy coverage", bh.Coverage, 1.0/3)
	cArm, ch := hor(armC)
	if cArm.NoDirectionDecision != 5 || ch.Labelled != 2 {
		t.Errorf("arm-c proxy = %+v %+v", cArm, ch)
	}
	near(t, "arm-c proxy avg", ch.AvgNetReturnBps, 8.75)
	near(t, "arm-c proxy coverage", ch.Coverage, 0.4)
	// The realised arm-a metrics do not move with the proxy: its NONE stays a none_count, not a hit or a miss.
	if o := armOutcome(t, c, 5, 2, armA).Cohort; o.NoneCount != 1 || o.Hits != 4 || o.Scorable != 5 {
		t.Errorf("arm-a realised = %+v", o)
	}
	var raw map[string]json.RawMessage
	srv.call(t, "jev_trial_compare", period).data(t, "jev_trial_compare", &raw)
	if !strings.Contains(string(raw["counterfactual_proxy"]), `"proxy":true`) {
		t.Errorf("counterfactual_proxy is not flagged proxy: %s", raw["counterfactual_proxy"])
	}
}

// The rank is over the cohort: an arm that answers few decisions (all hits) but fails the rest must not
// outrank an arm that answered every decision, even though it wins on the answered ones alone.
func TestCompareRankIsNotBuiltFromSuccessesOnly(t *testing.T) {
	srv, w := newServer(t)
	seedRank(t, w)
	c := compare(t, srv, period)
	if c.Cohort.Size != 20 || c.Cohort.CommonSuccessCount != 3 {
		t.Fatalf("cohort %d common %d, want 20 / 3", c.Cohort.Size, c.Cohort.CommonSuccessCount)
	}
	a, cc := armOutcome(t, c, 5, 2, armA), armOutcome(t, c, 5, 2, armC)
	near(t, "arm-a over_cohort", a.Cohort.OverCohort.Rate, 12.0/20)
	near(t, "arm-c over_cohort", cc.Cohort.OverCohort.Rate, 3.0/20)
	near(t, "arm-c over_answered", cc.Cohort.OverAnswered.Rate, 1)
	if a.Cohort.OverAnswered.Rate == nil || *a.Cohort.OverAnswered.Rate >= *cc.Cohort.OverAnswered.Rate {
		t.Fatal("fixture: arm-c must look better on the answered rows alone")
	}
	if a.Rank == nil || cc.Rank == nil || *a.Rank != 1 || *cc.Rank != 2 {
		t.Errorf("ranks = %v / %v, want 1 / 2 (over the cohort)", a.Rank, cc.Rank)
	}
	// Few outcome records give no rank at all.
	srv2, w2 := newServer(t)
	seedMain(t, w2)
	for _, o := range compare(t, srv2, period).Outcomes {
		for _, arm := range o.Arms {
			if arm.Rank != nil {
				t.Errorf("rank %d with only %d outcome records", *arm.Rank, arm.Cohort.Records)
			}
		}
	}
}
