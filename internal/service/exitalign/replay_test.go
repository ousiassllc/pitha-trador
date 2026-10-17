package exitalign_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
	"github.com/ousiassllc/pitha-trador/internal/service/exitalign"
)

var base = time.Date(2026, 9, 28, 1, 0, 0, 0, time.UTC)

type fakeDecisions map[int64]domain.JevDecision

func (f fakeDecisions) Get(_ context.Context, id int64) (domain.JevDecision, error) {
	return f[id], nil
}

// fakeBars serves the bars of one instrument in [from, to), as the repository
// does.
type fakeBars []domain.Snapshot

func (f fakeBars) ListHistoryByInstrumentRange(_ context.Context, _ int64, from, to time.Time) ([]domain.Snapshot, error) {
	var out []domain.Snapshot
	for _, b := range f {
		if !b.Timestamp.Before(from) && b.Timestamp.Before(to) {
			out = append(out, b)
		}
	}
	return out, nil
}

type fakeOutcomes []domain.CalibrationOutcome

func (f fakeOutcomes) EachOutcomeByDecisionRange(_ context.Context, _, _ time.Time, fn func(domain.CalibrationOutcome) error) error {
	for _, o := range f {
		if err := fn(o); err != nil {
			return err
		}
	}
	return nil
}

// minuteBars returns a bar per minute 0..n with the bid and ask at the price.
func minuteBars(n int, price func(minute int) float64) fakeBars {
	var out fakeBars
	for m := 0; m <= n; m++ {
		p := price(m)
		out = append(out, domain.Snapshot{InstrumentID: 1, Timestamp: base.Add(time.Duration(m) * time.Minute), Price: p, Bid: ptr(p), Ask: ptr(p)})
	}
	return out
}

func label(decisionID int64, hit string, net, ref float64) domain.CalibrationOutcome {
	return domain.CalibrationOutcome{
		JevDecisionID: decisionID, HorizonMinutes: 10, LabelVersion: domain.CalibrationLabelVersionTripleBarrier,
		BarrierHit: hit, NetReturnBps: ptr(net), EntryRefPrice: ptr(ref),
	}
}

var longDecision = fakeDecisions{1: {ID: 1, InstrumentID: 1, Timestamp: base, CreatedAt: base, Direction: ptr(domain.JevDirectionLong)}}

// The label stops at its take-profit (+30 bps net) a few minutes in; the
// shipped exit lets the same trade run on, here to the trailing stop. Compare
// reports both net returns per decision and the distribution of the difference
// (issue #774), and the paper exit reasons of the window.
func TestReplayer_Compare_LabelVersusActualExit(t *testing.T) {
	// +45 bps at minute 4 (the label's take-profit), +80 bps at minute 12 (the
	// peak), then a retrace of more than 0.5 % from the peak: the trailing stop
	// exits at +19 bps gross.
	bars := minuteBars(25, func(m int) float64 {
		switch {
		case m < 4:
			return 1000
		case m < 12:
			return 1004.5
		case m < 14:
			return 1008
		default:
			return 1001.9
		}
	})
	positions := &fakePositions{returns: []trading.ClosedReturn{{ExitReason: domain.ExitReasonTrailingStop}, {ExitReason: domain.ExitReasonStopLoss}, {ExitReason: domain.ExitReasonStopLoss}}}
	replayer := exitalign.NewReplayer(longDecision, bars, fakeOutcomes{label(1, domain.BarrierHitTakeProfit, 30, 1000)}, settingsStore{}, positions)

	got, err := replayer.Compare(context.Background(), base.Add(-time.Hour), base.Add(time.Hour), shippedExit())
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if got.ReplayedCount != 1 || got.SkippedCount != 0 || got.HorizonMinutes != 10 {
		t.Fatalf("counts = replayed %d skipped %d horizon %d, want 1/0/10", got.ReplayedCount, got.SkippedCount, got.HorizonMinutes)
	}
	if got.LabelTPCount != 1 || got.LabelSLCount != 0 || got.LabelTimeoutCount != 0 {
		t.Errorf("label hits tp/sl/timeout = %d/%d/%d, want 1/0/0", got.LabelTPCount, got.LabelSLCount, got.LabelTimeoutCount)
	}
	// Replay: the trailing stop at 1001.9 (peak 1008): +19 bps gross, less the 10 bps cost.
	if !near(got.LabelNetBps.Mean, 30) || !near(got.ReplayNetBps.Mean, 9) || !near(got.DiffBps.Mean, -21) {
		t.Errorf("label/replay/diff mean = %v/%v/%v, want 30/9/-21", got.LabelNetBps.Mean, got.ReplayNetBps.Mean, got.DiffBps.Mean)
	}
	if len(got.ReplayExits) != 1 || got.ReplayExits[0] != (exitalign.ReasonCount{Reason: domain.ExitReasonTrailingStop, Count: 1}) {
		t.Errorf("replay exits = %v, want one trailing_stop", got.ReplayExits)
	}
	if len(got.PaperExits) != 2 || got.PaperExits[0] != (exitalign.ReasonCount{Reason: domain.ExitReasonStopLoss, Count: 2}) {
		t.Errorf("paper exits = %v, want stop_loss x2 then trailing_stop x1", got.PaperExits)
	}
	if len(got.NotEvaluatedExits) != len(exitalign.NotEvaluatedExits) || got.CostLongBps != 10 || got.CostShortBps != 10 {
		t.Errorf("not evaluated = %v, costs = %v/%v; want the listed exits and 10/10 at the defaults", got.NotEvaluatedExits, got.CostLongBps, got.CostShortBps)
	}
}

// The replay deducts the label's cost, which follows the cost settings: the
// same decision replays lower when the cost rises.
func TestReplayer_Compare_ReplayCostFollowsTheCostSettings(t *testing.T) {
	bars := minuteBars(25, func(m int) float64 { return 1000 + float64(m) })
	replayNet := func(store settingsStore) float64 {
		replayer := exitalign.NewReplayer(longDecision, bars, fakeOutcomes{label(1, domain.BarrierHitTimeout, 0, 1000)}, store, nil)
		got, err := replayer.Compare(context.Background(), base.Add(-time.Hour), base.Add(time.Hour), exitalign.ExitConfig{MaxHoldingMinutes: 20})
		if err != nil || got.ReplayedCount != 1 {
			t.Fatalf("Compare = %+v, %v", got, err)
		}
		return got.ReplayNetBps.Mean
	}
	cheap := replayNet(settingsStore{})
	costly := replayNet(settingsStore{"cost.fee_bps": "2"}) // +4 bps round trip
	if !near(cheap-costly, 4) {
		t.Fatalf("replay net %v (defaults) vs %v (fee 2 bps): want a 4 bps lower replay", cheap, costly)
	}
}

// A label whose bars stop before the maximum holding time cannot be replayed
// and is counted as skipped; labels of other horizons or without barriers are
// ignored; a window with no labels is empty.
func TestReplayer_Compare_SkipsAndFilters(t *testing.T) {
	short := minuteBars(10, func(int) float64 { return 1000 }) // the label's 10 minutes only
	other := label(1, domain.BarrierHitTimeout, 0, 1000)
	other.HorizonMinutes = 5 // not the Settings timeout
	legacy := label(1, "", 0, 1000)
	legacy.LabelVersion = domain.CalibrationLabelVersionClose

	replayer := exitalign.NewReplayer(longDecision, short, fakeOutcomes{label(1, domain.BarrierHitTimeout, 0, 1000), other, legacy}, settingsStore{}, nil)
	got, err := replayer.Compare(context.Background(), base.Add(-time.Hour), base.Add(time.Hour), shippedExit())
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if got.ReplayedCount != 0 || got.SkippedCount != 1 || got.DiffBps.Count != 0 {
		t.Fatalf("replayed/skipped/diff count = %d/%d/%d, want 0/1/0", got.ReplayedCount, got.SkippedCount, got.DiffBps.Count)
	}

	empty, err := exitalign.NewReplayer(longDecision, short, fakeOutcomes{}, settingsStore{}, nil).Compare(context.Background(), base, base.Add(time.Hour), shippedExit())
	if err != nil || empty.ReplayedCount != 0 || empty.SkippedCount != 0 || empty.PaperExits != nil {
		t.Fatalf("Compare of an empty window = %+v, %v; want nothing", empty, err)
	}
}

func near(a, b float64) bool { return a-b < 1e-9 && b-a < 1e-9 }
