package shadowtests

import (
	"math"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query/shadowquery"
)

func approx(t *testing.T, name string, got *float64, want float64) {
	t.Helper()
	if got == nil || math.Abs(*got-want) > 1e-9 {
		t.Errorf("%s = %v, want %v", name, deref(got), want)
	}
}

func deref(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

type compareData struct {
	Versions []shadowquery.ShadowVersionComparison `json:"versions"`
}

func level(v shadowquery.ShadowVersionComparison, eq string) shadowquery.QualityCount {
	for _, q := range v.EntryQuality {
		if q.EntryQuality == eq {
			return q
		}
	}
	return shadowquery.QualityCount{}
}

func outcomeOf(v shadowquery.ShadowVersionComparison, horizon, lv int, eq string) shadowquery.QualityOutcome {
	for _, h := range v.Outcomes {
		if h.HorizonMinutes == horizon && h.LabelVersion == lv {
			for _, q := range h.ByEntryQuality {
				if q.EntryQuality == eq {
					return q
				}
			}
		}
	}
	return shadowquery.QualityOutcome{}
}

// Four live trader-v3 decisions (poor, poor, fair, poor) with the shadow trader-v4
// answering good, fair, good, poor. Directions: live LONG on 1-3 and SHORT on 4;
// shadow LONG, SHORT, LONG, SHORT. One pair (5) lies outside the period.
func TestCompareSetsLiveAndShadowEntryQualityDistributionsSideBySide(t *testing.T) {
	srv, w := shadowServer(t)
	liveDecision(t, w, 1, "2026-10-01T01:00:00Z", "trader-v3", "LONG", "poor")
	liveDecision(t, w, 2, "2026-10-01T02:00:00Z", "trader-v3", "LONG", "poor")
	liveDecision(t, w, 3, "2026-10-01T03:00:00Z", "trader-v3", "LONG", "fair")
	liveDecision(t, w, 4, "2026-10-01T04:00:00Z", "trader-v3", "SHORT", "poor")
	liveDecision(t, w, 5, "2026-08-01T00:00:00Z", "trader-v3", "LONG", "poor") // outside the period
	shadowDecision(t, w, 1, "trader-v4", "LONG", "good", 0.0001, "2026-10-01T01:00:05Z")
	shadowDecision(t, w, 2, "trader-v4", "SHORT", "fair", 0.0002, "2026-10-01T02:00:05Z")
	shadowDecision(t, w, 3, "trader-v4", "LONG", "good", 0.0003, "2026-10-01T03:00:05Z")
	shadowDecision(t, w, 4, "trader-v4", "SHORT", "poor", 0.0004, "2026-10-01T04:00:05Z")
	shadowDecision(t, w, 5, "trader-v4", "LONG", "good", 0.5, "2026-08-01T00:00:05Z")

	var d compareData
	srv.call(t, "jev_shadow_compare", period).data(t, "jev_shadow_compare", &d)
	if len(d.Versions) != 1 {
		t.Fatalf("versions = %+v, want one (trader-v4)", d.Versions)
	}
	v := d.Versions[0]
	if v.PromptVersion != "trader-v4" || v.Pairs != 4 {
		t.Fatalf("version %q pairs %d, want trader-v4 / 4 (the out-of-period pair excluded)", v.PromptVersion, v.Pairs)
	}
	if len(v.LiveVersions) != 1 || v.LiveVersions[0].Version != "trader-v3" || v.LiveVersions[0].Pairs != 4 {
		t.Errorf("live versions = %+v", v.LiveVersions)
	}
	// Distribution: every level is present, in poor..exceptional order, with counts and shares.
	if len(v.EntryQuality) != 5 || v.EntryQuality[0].EntryQuality != "poor" || v.EntryQuality[4].EntryQuality != "exceptional" {
		t.Fatalf("entry_quality rows = %+v", v.EntryQuality)
	}
	for eq, want := range map[string][2]int{"poor": {3, 1}, "fair": {1, 1}, "good": {0, 2}, "strong": {0, 0}, "exceptional": {0, 0}} {
		got := level(v, eq)
		if got.Live.Count != want[0] || got.Shadow.Count != want[1] {
			t.Errorf("%s: live %d shadow %d, want %d / %d", eq, got.Live.Count, got.Shadow.Count, want[0], want[1])
		}
	}
	approx(t, "poor live share", level(v, "poor").Live.Share, 0.75)
	approx(t, "good shadow share", level(v, "good").Shadow.Share, 0.5)
	// Direction agreement: pairs 1, 3, 4 agree (LONG/LONG, LONG/LONG, SHORT/SHORT), pair 2 does not.
	if v.DirectionAgreement.Agree != 3 {
		t.Errorf("agree = %d, want 3", v.DirectionAgreement.Agree)
	}
	approx(t, "agreement rate", v.DirectionAgreement.Rate, 0.75)
	// Cost: the sum of the period's request_cost only.
	if v.Cost.Calls != 4 {
		t.Errorf("cost calls = %d, want 4", v.Cost.Calls)
	}
	if math.Abs(v.Cost.RequestCostUSD-0.0010) > 1e-12 {
		t.Errorf("request_cost_usd = %v, want 0.001", v.Cost.RequestCostUSD)
	}

	// The version filter.
	var f compareData
	srv.call(t, "jev_shadow_compare", map[string]any{"from": "2026-10-01", "to": "2026-10-10", "prompt_version": "trader-v9"}).data(t, "jev_shadow_compare", &f)
	if len(f.Versions) != 0 {
		t.Errorf("an unknown version returned %+v", f.Versions)
	}
	var one compareData
	srv.call(t, "jev_shadow_compare", map[string]any{"from": "2026-10-01", "to": "2026-10-10", "prompt_version": "trader-v4"}).data(t, "jev_shadow_compare", &one)
	if len(one.Versions) != 1 || one.Versions[0].Pairs != 4 {
		t.Errorf("filtered = %+v", one.Versions)
	}
}

// The outcome comparison reuses calibration_outcomes (joined on jev_decision_id,
// like jev_calibration) and recomputes the SHADOW side's result from
// future_return and the shadow's own direction (docs §5.1).
func TestCompareOutcomesRecomputeTheShadowResultFromItsOwnDirection(t *testing.T) {
	srv, w := shadowServer(t)
	// Live LONG decision 1 (poor): future_return +1.0%. Shadow SHORT (fair) -> directional return -1.0, a miss.
	liveDecision(t, w, 1, "2026-10-01T01:00:00Z", "trader-v3", "LONG", "poor")
	shadowDecision(t, w, 1, "trader-v4", "SHORT", "fair", 0.0001, "2026-10-01T01:00:05Z")
	outcome(t, w, 1, 5, 2, 1.0, 1, "tp", 12.0)
	// Live LONG decision 2 (poor): future_return -0.5%. Shadow LONG (good) agrees -> directional -0.5, a miss; the recorded label says 0.
	liveDecision(t, w, 2, "2026-10-01T02:00:00Z", "trader-v3", "LONG", "poor")
	shadowDecision(t, w, 2, "trader-v4", "LONG", "good", 0.0001, "2026-10-01T02:00:05Z")
	outcome(t, w, 2, 5, 2, -0.5, 0, "sl", -8.0)
	// Live SHORT decision 3 (fair): future_return -2.0% (the price fell: the SHORT was right). Shadow NONE (poor): no trade.
	liveDecision(t, w, 3, "2026-10-01T03:00:00Z", "trader-v3", "SHORT", "fair")
	shadowDecision(t, w, 3, "trader-v4", "NONE", "poor", 0.0001, "2026-10-01T03:00:05Z")
	outcome(t, w, 3, 5, 2, -2.0, 1, "tp", 30.0)
	// Live LONG decision 4 (poor): future_return +3.0%. Shadow LONG (good) agrees -> directional +3.0, a hit.
	liveDecision(t, w, 4, "2026-10-01T04:00:00Z", "trader-v3", "LONG", "poor")
	shadowDecision(t, w, 4, "trader-v4", "LONG", "good", 0.0001, "2026-10-01T04:00:05Z")
	outcome(t, w, 4, 5, 2, 3.0, 1, "tp", 20.0)
	// A decision without an outcome yet contributes to the distribution only.
	liveDecision(t, w, 6, "2026-10-01T06:00:00Z", "trader-v3", "LONG", "poor")
	shadowDecision(t, w, 6, "trader-v4", "LONG", "good", 0.0001, "2026-10-01T06:00:05Z")

	var d compareData
	srv.call(t, "jev_shadow_compare", period).data(t, "jev_shadow_compare", &d)
	v := d.Versions[0]
	if v.Pairs != 5 {
		t.Fatalf("pairs = %d, want 5", v.Pairs)
	}
	if len(v.Outcomes) != 1 || v.Outcomes[0].HorizonMinutes != 5 || v.Outcomes[0].LabelVersion != 2 {
		t.Fatalf("outcomes = %+v, want one horizon-5 / label-2 group", v.Outcomes)
	}

	// Live poor: decisions 1, 2, 4 (outcomes) - all LONG: returns +1.0, -0.5, +3.0.
	livePoor := outcomeOf(v, 5, 2, "poor").Live
	if livePoor.Samples != 3 || livePoor.Directional != 3 {
		t.Errorf("live poor samples/directional = %d/%d, want 3/3", livePoor.Samples, livePoor.Directional)
	}
	approx(t, "live poor hit rate", livePoor.HitRate, 2.0/3.0)
	approx(t, "live poor avg directional return", livePoor.AvgDirectionalReturnPct, (1.0-0.5+3.0)/3)
	if livePoor.RecordedSamples != 3 {
		t.Errorf("live poor recorded samples = %d, want 3", livePoor.RecordedSamples)
	}
	approx(t, "live poor recorded hit rate (was_direction_correct)", livePoor.RecordedHitRate, 2.0/3.0)
	approx(t, "live poor recorded avg net bps", livePoor.RecordedAvgNetReturnBps, (12.0-8.0+20.0)/3)
	if livePoor.RecordedBarrierCounts.TP != 2 || livePoor.RecordedBarrierCounts.SL != 1 {
		t.Errorf("live poor barrier counts = %+v", livePoor.RecordedBarrierCounts)
	}

	// Shadow good: decisions 2 (LONG, -0.5) and 4 (LONG, +3.0): both agree with the live direction.
	shadowGood := outcomeOf(v, 5, 2, "good").Shadow
	if shadowGood.Samples != 2 || shadowGood.Directional != 2 {
		t.Errorf("shadow good samples/directional = %d/%d, want 2/2", shadowGood.Samples, shadowGood.Directional)
	}
	approx(t, "shadow good hit rate", shadowGood.HitRate, 0.5)
	approx(t, "shadow good avg directional return", shadowGood.AvgDirectionalReturnPct, (-0.5+3.0)/2)
	if shadowGood.RecordedSamples != 2 {
		t.Errorf("shadow good recorded samples = %d, want 2 (both agree with the live direction)", shadowGood.RecordedSamples)
	}

	// Shadow fair: decision 1, the shadow said SHORT while the live was LONG and the price rose 1.0%:
	// directional return -1.0, a miss - although the live label says the (LONG) direction was correct.
	shadowFair := outcomeOf(v, 5, 2, "fair").Shadow
	if shadowFair.Samples != 1 || shadowFair.Directional != 1 {
		t.Errorf("shadow fair samples/directional = %d/%d, want 1/1", shadowFair.Samples, shadowFair.Directional)
	}
	approx(t, "shadow fair hit rate (recomputed)", shadowFair.HitRate, 0)
	approx(t, "shadow fair avg directional return", shadowFair.AvgDirectionalReturnPct, -1.0)
	if shadowFair.RecordedSamples != 0 || shadowFair.RecordedHitRate != nil || shadowFair.RecordedAvgNetReturnBps != nil {
		t.Errorf("shadow fair recorded fields = %+v, want none (its direction differs from the label's)", shadowFair)
	}

	// Shadow NONE (decision 3): counted as a sample, not as a directional decision.
	shadowPoor := outcomeOf(v, 5, 2, "poor").Shadow
	if shadowPoor.Samples != 1 || shadowPoor.Directional != 0 || shadowPoor.HitRate != nil || shadowPoor.AvgDirectionalReturnPct != nil {
		t.Errorf("shadow poor (NONE) = %+v, want 1 sample and no directional result", shadowPoor)
	}
	// Live fair (decision 3, SHORT, future_return -2.0): directional return +2.0, a hit.
	liveFair := outcomeOf(v, 5, 2, "fair").Live
	approx(t, "live fair hit rate (SHORT sign flip)", liveFair.HitRate, 1)
	approx(t, "live fair avg directional return", liveFair.AvgDirectionalReturnPct, 2.0)
}

// The comparison's live side reproduces jev_calibration's counts for the same
// population: the same join (jev_decision_id) and the same label versions.
func TestCompareNeverMixesLabelVersions(t *testing.T) {
	srv, w := shadowServer(t)
	liveDecision(t, w, 1, "2026-10-01T01:00:00Z", "trader-v3", "LONG", "poor")
	shadowDecision(t, w, 1, "trader-v4", "LONG", "good", 0.0001, "2026-10-01T01:00:05Z")
	outcome(t, w, 1, 10, 1, 1.0, 1, nil, nil)
	outcome(t, w, 1, 5, 2, 1.0, 1, "tp", 5.0)
	var d compareData
	srv.call(t, "jev_shadow_compare", period).data(t, "jev_shadow_compare", &d)
	if got := len(d.Versions[0].Outcomes); got != 2 {
		t.Fatalf("outcome groups = %d, want 2 (label version 1 at 10 min, label version 2 at 5 min)", got)
	}
	lv1 := outcomeOf(d.Versions[0], 10, 1, "poor").Live
	if lv1.Samples != 1 || lv1.RecordedAvgNetReturnBps != nil || lv1.RecordedBarrierCounts != (query.BarrierCounts{}) {
		t.Errorf("label version 1 group = %+v, want the net/barrier fields empty (label version 2 only)", lv1)
	}
	lv2 := outcomeOf(d.Versions[0], 5, 2, "poor").Live
	approx(t, "label version 2 recorded net bps", lv2.RecordedAvgNetReturnBps, 5.0)
}
