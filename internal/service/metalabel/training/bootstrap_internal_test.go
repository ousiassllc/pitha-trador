package training

import (
	"fmt"
	"math/rand/v2"
	"testing"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
)

// nullSample is one OOS set without an edge: every decision is taken, wins or
// loses 30 bps by a fair coin shifted by a shared day effect (sd dayShift), over
// days days of 8 symbols. With naive true each row is its own cluster, the iid
// bootstrap that ignores the correlation.
func nullSample(rng *rand.Rand, days, perDay int, dayShift float64, naive bool) []scoredRow {
	var rows []scoredRow
	for d := range days {
		shift := dayShift * rng.NormFloat64()
		for i := range perDay {
			ret := -30.0
			if rng.Float64() < 0.5+shift {
				ret = 30
			}
			r := scoredRow{index: len(rows), day: fmt.Sprint(d), symbol: fmt.Sprint(i % 8), win: ret > 0, raw: rng.Float64(), take: true, ret: ret}
			if naive {
				r.day, r.symbol = fmt.Sprint(len(rows)), "-"
			}
			rows = append(rows, r)
		}
	}
	return rows
}

func evLowerAboveZero(rows []scoredRow, c AcceptanceConfig, seed uint64) bool {
	iv := Evaluation{scored: rows}.evInterval(newClusters(rows), c, seed)
	return iv != nil && iv.Low > 0
}

// With the strongly correlated outcomes of the documented noise model (a day
// effect of 9 bps sd, an intraclass correlation of about 0.09) the lower
// confidence limit of the EV is above 0 in at most 5% of the edge-free OOS sets
// at the minimum of 5 days; an iid bootstrap of the same data is far above that,
// which is why the resampling units are the days and the (day, symbol) clusters.
func TestEVInterval_CoversTheNullUnderSameDayCorrelation(t *testing.T) {
	cfg := DefaultAcceptanceConfig()
	rng := rand.New(rand.NewPCG(2026, 780))
	const sims, dayShift = 700, 0.15
	var clustered, naive int
	for range sims {
		sample := nullSample(rng, cfg.MinOOSDays, 60, dayShift, false)
		seed := rng.Uint64()
		if evLowerAboveZero(sample, cfg, seed) {
			clustered++
		}
		iid := make([]scoredRow, len(sample))
		for i, r := range sample {
			r.day, r.symbol = fmt.Sprint(i), "-"
			iid[i] = r
		}
		if evLowerAboveZero(iid, cfg, seed) {
			naive++
		}
	}
	if rate := float64(clustered) / sims; rate > 0.05 {
		t.Errorf("cluster bootstrap: EV lower limit above 0 in %.1f%% of edge-free sets, want <= 5%%", 100*rate)
	}
	if rate := float64(naive) / sims; rate < 0.15 {
		t.Errorf("iid bootstrap false-positive rate %.1f%%: the correlation is too weak to show why the clustering matters", 100*rate)
	}
	t.Logf("false-positive rate of the EV lower limit: cluster %.1f%%, iid %.1f%%", 100*float64(clustered)/sims, 100*float64(naive)/sims)
}

// The AUC interval brackets the true AUC of a scored sample and is reproducible.
func TestAUCInterval_BracketsTheAUCAndIsReproducible(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 7))
	var rows []scoredRow
	for d := range 6 {
		for i := range 150 {
			win := rng.Float64() < 0.5
			score := rng.NormFloat64()
			if win {
				score += 0.5 // true AUC = Phi(0.5/sqrt2) = 0.638
			}
			rows = append(rows, scoredRow{index: len(rows), day: fmt.Sprint(d), symbol: fmt.Sprint(i % 5), win: win, raw: score})
		}
	}
	cl := newClusters(rows)
	iv := aucInterval(cl, rows, 2000, 0.01, rand.New(rand.NewPCG(1, 2)))
	if iv == nil {
		t.Fatal("no interval")
	}
	if !(iv.Low < 0.638 && 0.638 < iv.High) || iv.Low <= 0.5 {
		t.Errorf("AUC interval = %+v, want it to contain 0.638 and exclude 0.5", iv)
	}
	again := aucInterval(cl, rows, 2000, 0.01, rand.New(rand.NewPCG(1, 2)))
	if *again != *iv {
		t.Errorf("same seed gave %+v then %+v", iv, again)
	}
}

func TestIntervals_UndefinedWithoutEnoughDaysOrDraws(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 3))
	oneDay := nullSample(rng, 1, 300, 0, false)
	if iv := (Evaluation{scored: oneDay}).Intervals(DefaultAcceptanceConfig(), 1); iv.EV != nil || iv.AUC != nil || iv.Days != 1 {
		t.Errorf("one day: %+v, want no intervals", iv)
	}
	if iv := percentileInterval([]float64{1, 2, 3}, 0.01); iv != nil {
		t.Errorf("3 draws gave %+v, want nil", iv)
	}
	var none *repo.Interval
	if iv := ratioInterval(newClusters(nil), nil, nil, 100, 0.1, rng); iv != none {
		t.Errorf("no clusters gave %+v", iv)
	}
}

// scoredPair builds the same OOS rows scored by two models: the candidate takes
// every row with returns shifted by candEdge relative to the active one.
func pairedRows(rng *rand.Rand, days, perDay int, activeEdge, candEdge float64) (cand, active []scoredRow) {
	idx := 0
	for d := range days {
		for i := range perDay {
			base := 30 * rng.NormFloat64()
			mk := func(edge float64) scoredRow {
				ret := base + edge + 15*rng.NormFloat64() // each model's own noise around the shared outcome
				return scoredRow{index: idx, day: fmt.Sprint(d), symbol: fmt.Sprint(i % 6), take: true, ret: ret, raw: rng.Float64(), win: ret > 0}
			}
			cand = append(cand, mk(candEdge))
			active = append(active, mk(activeEdge))
			idx++
		}
	}
	return cand, active
}

func TestCompare_WorseOrUndistinguishableCandidateIsNotAdopted(t *testing.T) {
	cfg := DefaultAcceptanceConfig()
	cfg.MinOOSSamples = 200
	rng := rand.New(rand.NewPCG(11, 11))
	for name, tc := range map[string]struct {
		activeEdge, candEdge float64
		adopt                bool
		reason               string
	}{
		// The active model still earns a significantly positive EV: the candidate must beat it.
		"clearly worse":             {activeEdge: 12, candEdge: 6, adopt: false, reason: RejectNotBetter},
		"no significant difference": {activeEdge: 12, candEdge: 12.2, adopt: false, reason: RejectNotBetter},
		"significantly better":      {activeEdge: 12, candEdge: 40, adopt: true},
		// The active model's own EV is not distinguishable from 0 any more: no worse is enough.
		"active decayed, candidate no worse": {activeEdge: 0, candEdge: 6, adopt: true},
		"active decayed, candidate worse":    {activeEdge: 0, candEdge: -6, adopt: false, reason: RejectNotBetter},
	} {
		cand, active := pairedRows(rng, 6, 80, tc.activeEdge, tc.candEdge)
		cmp := Compare(Evaluation{scored: cand}, Evaluation{scored: active}, cfg, 1, "v-active")
		if !cmp.Comparable || cmp.PairedSamples != len(cand) {
			t.Fatalf("%s: comparison = %+v", name, cmp)
		}
		adopt, reason := cfg.judgeComparison(cmp)
		if adopt != tc.adopt || reason != tc.reason {
			t.Errorf("%s: adopt %v (%s), want %v (%s); diff %.2f ci %+v active ev ci %+v", name, adopt, reason, tc.adopt, tc.reason, cmp.DiffBps, cmp.DiffCI, cmp.ActiveEVCI)
		}
	}
}

func TestCompare_TooFewSharedRowsIsNotComparable(t *testing.T) {
	cfg := DefaultAcceptanceConfig()
	rng := rand.New(rand.NewPCG(12, 12))
	cand, _ := pairedRows(rng, 6, 80, 0, 0)
	cmp := Compare(Evaluation{scored: cand}, Evaluation{scored: cand[:50]}, cfg, 1, "v-active")
	if cmp.Comparable || cmp.Note == "" || cmp.PairedSamples != 50 {
		t.Errorf("comparison = %+v, want not comparable with a note", cmp)
	}
	if adopt, reason := cfg.judgeComparison(cmp); adopt || reason != RejectNotComparable {
		t.Errorf("judgeComparison = %v, %q, want the active model kept (%s)", adopt, reason, RejectNotComparable)
	}
}
