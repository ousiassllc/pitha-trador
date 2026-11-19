package ledgertests

import (
	"slices"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/ledgerdiag"
)

const (
	idTree60    = "research_model:gbt:60m"
	idTreeClose = "research_model:gbt:close"
	idRidge60   = "research_model:ridge:60m"
)

// The tree model and the 60m / close horizons of the research model are candidates of
// their own: one per algorithm and horizon (research_model:<algorithm>:<horizon>), read
// from research_model_predictions through the run's algorithm and label horizon, and
// lined up with the live Jev on the very same decisions. Chosen at the 60m horizon, the
// ledger scores every candidate's direction on the 60m labels, at close on the close
// labels. Hand-computed on decisions 1-4 (live LONG SHORT LONG SHORT):
//
//	60m   LONG/SHORT net  1: +10/-10  2: +5/-5   3: -8/+8   4: -6/+6    live: 10 -5 -8 6 = +3 / 4
//	      gbt:60m   LONG NONE SHORT SHORT  -> 10 0 8 6    = +24 / 4
//	      ridge:60m SHORT LONG NONE LONG   -> -10 5 0 -6  = -11 / 4
//	close LONG/SHORT net  1: +20/-20  2: +15/-15  3: -12/+12  4: -4/+4  live: 20 -15 -12 4 = -3 / 4
//	      gbt:close LONG LONG SHORT NONE   -> 20 15 12 0  = +47 / 4
func TestResearchModelTreeAndHorizonsAreCandidatesOnTheSameRows(t *testing.T) {
	t.Parallel()
	srv, w := newServer(t, "")
	decs := seedBase(t, w)
	for i, ls := range [][2]float64{{10, -10}, {5, -5}, {-8, 8}, {-6, 6}} {
		horizonLabel(t, w, decs[i], "60m", ls[0], ls[1])
	}
	for i, ls := range [][2]float64{{20, -20}, {15, -15}, {-12, 12}, {-4, 4}} {
		horizonLabel(t, w, decs[i], "close", ls[0], ls[1])
	}
	for _, r := range []struct {
		version, algorithm, horizon string
		dirs                        []string
	}{
		{"gbt-60m", "gbt", "60m", []string{"LONG", "NONE", "SHORT", "SHORT"}},
		{"ridge-60m", "ridge", "60m", []string{"SHORT", "LONG", "NONE", "LONG"}},
		{"gbt-close", "gbt", "close", []string{"LONG", "LONG", "SHORT", "NONE"}},
	} {
		run := researchRun(t, w, r.version, r.algorithm, r.horizon)
		for i, dir := range r.dirs {
			prediction(t, w, run, decs[i], dir)
		}
	}

	l := srv.call(t, map[string]any{"horizon": "60m", "candidates": []string{idTree60, idRidge60}}).ledger(t)
	if l.Horizon != "60m" || l.Exclusions.Evaluated != 4 || l.Exclusions.NoLabel != 4 || l.Reference.Rows != 4 || !near(l.Reference.MeanNetBps, 3.0/4) {
		t.Fatalf("60m: horizon %s exclusions %+v reference rows %d mean %v", l.Horizon, l.Exclusions, l.Reference.Rows, deref(l.Reference.MeanNetBps))
	}
	tree, ridge := entryOf(t, l, idTree60), entryOf(t, l, idRidge60)
	if tree.Kind != ledgerdiag.KindResearchModel || *tree.NCommon != 4 || !near(tree.MeanNetBps, 24.0/4) || !near(tree.DiffVsLive.MeanBps, 21.0/4) || !near(tree.MissingRate, 0) {
		t.Errorf("gbt:60m = %+v", tree)
	}
	if *ridge.NCommon != 4 || !near(ridge.MeanNetBps, -11.0/4) || !near(ridge.DiffVsLive.MeanBps, -14.0/4) {
		t.Errorf("ridge:60m = %+v", ridge)
	}
	if !slices.Equal(tree.DistinctModelIDs, []string{"gbt:60m"}) || !slices.Equal(ridge.DistinctModelIDs, []string{"ridge:60m"}) {
		t.Errorf("model ids %v and %v, want the algorithm and horizon", tree.DistinctModelIDs, ridge.DistinctModelIDs)
	}

	lc := srv.call(t, map[string]any{"horizon": "close", "candidates": []string{idTreeClose}}).ledger(t)
	if lc.Horizon != "close" || lc.Reference.Rows != 4 || !near(lc.Reference.MeanNetBps, -3.0/4) {
		t.Fatalf("close: horizon %s reference rows %d mean %v", lc.Horizon, lc.Reference.Rows, deref(lc.Reference.MeanNetBps))
	}
	if e := entryOf(t, lc, idTreeClose); *e.NCommon != 4 || !near(e.MeanNetBps, 47.0/4) || !near(e.DiffVsLive.MeanBps, 50.0/4) {
		t.Errorf("gbt:close = %+v", e)
	}

	// A bad horizon in the candidate id is refused; the tree model's id is accepted.
	for _, id := range []string{"research_model:gbt:10m", "research_model:gbt:2h", "research_model:gbt"} {
		if _, _, err := ledgerdiag.ParseID(id); err == nil {
			t.Errorf("ParseID(%q) accepted a horizon the model is not trained on", id)
		}
	}
	if kind, key, err := ledgerdiag.ParseID(idTreeClose); err != nil || kind != ledgerdiag.KindResearchModel || key != "gbt:close" {
		t.Errorf("ParseID(%q) = %q, %q, %v", idTreeClose, kind, key, err)
	}
}
