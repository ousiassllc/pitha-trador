package ledgertests

import (
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/ledgerdiag"
)

// Every horizon is read from its own label: 30m rows are research_horizon_labels joined on the decision's
// instrument and timestamp; a decision without a label on both sides leaves the ledger for everyone.
func TestHorizonsUseTheirOwnLabelsAndCountExclusions(t *testing.T) {
	t.Parallel()
	srv, w := newServer(t, "")
	decs := seedBase(t, w)
	seedCandidates(t, w, decs)
	// 30m: LONG / SHORT net per decision 1-6; 7 has no label, 8 has no SHORT quote.
	for i, ls := range [][2]float64{{40, -50}, {10, -20}, {-15, 5}, {-30, 25}, {5, -10}, {-2, -8}} {
		horizonLabel(t, w, decs[i], "30m", ls[0], ls[1])
	}
	horizonLabel(t, w, decs[7], "30m", 7.0, nil)
	// 60m: only decisions 1 and 2; close: none.
	horizonLabel(t, w, decs[0], "60m", 10.0, -10.0)
	horizonLabel(t, w, decs[1], "60m", 5.0, -5.0)

	l := srv.call(t, map[string]any{"horizon": "30m", "candidates": []string{idShadow, "baseline:momentum", "baseline:contrarian"}}).ledger(t)
	if l.Horizon != "30m" || l.Exclusions.Decisions != 8 || l.Exclusions.NoLabel != 2 || l.Exclusions.Evaluated != 6 || l.Reference.Rows != 6 {
		t.Fatalf("30m exclusions %+v reference rows %d", l.Exclusions, l.Reference.Rows)
	}
	// llm: 40, -20, -15, +25, 5, -8 = 27 over 6 rows
	if !near(l.Reference.MeanNetBps, 4.5) {
		t.Errorf("30m live mean = %v", deref(l.Reference.MeanNetBps))
	}
	// shadow LONG NONE SHORT SHORT LONG SHORT on the six rows: 40, 0, 5, 25, 5, -8 = 67
	if e := entryOf(t, l, idShadow); *e.NCommon != 6 || !near(e.MeanNetBps, 67.0/6) || !near(e.DiffVsLive.MeanBps, 40.0/6) || !near(e.MissingRate, 0) {
		t.Errorf("shadow at 30m = %+v", e)
	}
	// momentum follows return_5m of the decision's bar (+, +, -, -, NULL, +): 40, 10, 5, 25, 0, -2
	if e := entryOf(t, l, "baseline:momentum"); !near(e.MeanNetBps, 78.0/6) {
		t.Errorf("momentum at 30m = %v", deref(e.MeanNetBps))
	}
	if e := entryOf(t, l, "baseline:contrarian"); !near(e.MeanNetBps, -123.0/6) {
		t.Errorf("contrarian at 30m = %v", deref(e.MeanNetBps))
	}

	l60 := srv.call(t, map[string]any{"horizon": "60m", "candidates": []string{idShadow}}).ledger(t)
	if l60.Reference.Rows != 2 || !near(l60.Reference.MeanNetBps, 2.5) || l60.Exclusions.NoLabel != 6 { // llm: row 1 LONG +10, row 2 SHORT -5
		t.Errorf("60m reference = %+v exclusions %+v", l60.Reference, l60.Exclusions)
	}
	lc := srv.call(t, map[string]any{"horizon": "close", "candidates": []string{idShadow}}).ledger(t)
	e := entryOf(t, lc, idShadow)
	if lc.Reference.Rows != 0 || lc.Reference.MeanNetBps != nil || e.MissingRate != nil || *e.NCommon != 0 || e.MeanNetBps != nil || e.DiffVsLive.PValue != nil {
		t.Errorf("close with no labels: reference %+v shadow %+v", lc.Reference, e)
	}
}

// A candidate id that matches nothing is listed with no answers and does not empty the table of the others.
func TestCandidateWithoutAnswersDoesNotEmptyTheTable(t *testing.T) {
	t.Parallel()
	srv, w := newServer(t, "")
	seedCandidates(t, w, seedBase(t, w))
	l := srv.call(t, map[string]any{"candidates": []string{idShadow, "shadow:never-ran"}}).ledger(t)
	empty, shadow := entryOf(t, l, "shadow:never-ran"), entryOf(t, l, idShadow)
	if !empty.NoAnswers || *empty.NCommon != 0 || !near(empty.MissingRate, 1) || empty.MeanNetBps != nil {
		t.Errorf("empty candidate = %+v", empty)
	}
	if *shadow.NCommon != 8 || shadow.NoAnswers {
		t.Errorf("shadow = %+v", shadow)
	}
}

// An empty period is an answer: the reference has no rows and the baselines are listed with none.
func TestEmptyPeriod(t *testing.T) {
	t.Parallel()
	srv, _ := newServer(t, "")
	l := srv.call(t, nil).ledger(t)
	if l.Reference.Rows != 0 || l.Reference.MeanNetBps != nil || l.Exclusions.Decisions != 0 {
		t.Errorf("reference = %+v", l.Reference)
	}
	for _, e := range l.Candidates {
		if e.Kind != ledgerdiag.KindBaseline || *e.NCommon != 0 || e.MissingRate != nil {
			t.Errorf("candidate %+v", e)
		}
	}
}
