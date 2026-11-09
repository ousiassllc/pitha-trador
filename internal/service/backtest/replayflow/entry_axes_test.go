package replayflow_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
)

// OnEntryEvaluation observes without changing the replay: the trades are
// identical with and without it, and it reports each fresh decision once with
// its outcome and reject reason.
func TestOnEntryEvaluation_ObservesWithoutChangingTheReplay(t *testing.T) {
	bars := minuteBars(1, base, []float64{1000, 1000, 1004, 1008, 1008, 1008, 1008, 1008, 1008, 1008}, nil)
	weak := longDecision(1, at(1))
	weak.EntryQuality = ptr(domain.JevEntryQualityFair)
	decisions := []domain.JevDecision{weak, longDecision(1, at(4))}

	plain := replayConfig(1, "7203", bars, decisions, liveExit())
	want := replayTrades(t, plain)

	var seen []backtest.EntryEvaluation
	observed := replayConfig(1, "7203", bars, decisions, liveExit())
	observed.OnEntryEvaluation = func(e backtest.EntryEvaluation) { seen = append(seen, e) }
	if got := replayTrades(t, observed); !reflect.DeepEqual(got, want) {
		t.Fatalf("trades differ with the observer:\n got %+v\nwant %+v", got, want)
	}

	if len(seen) != 2 {
		t.Fatalf("%d evaluations, want 2 (one per fresh decision): %+v", len(seen), seen)
	}
	if first := seen[0]; first.Outcome != backtest.OutcomeRejected || first.Signal.RejectReason == nil ||
		!strings.HasPrefix(*first.Signal.RejectReason, policy.ReasonEntryQualityBelowThreshold) || !first.BarTime.Equal(at(1)) {
		t.Fatalf("first evaluation = %+v, want an entry_quality rejection at minute 1", first)
	}
	if second := seen[1]; second.Outcome != backtest.OutcomeOpened || second.PredictedEVBps == nil || !second.BarTime.Equal(at(4)) {
		t.Fatalf("second evaluation = %+v, want an opened entry at minute 4 with the model's EV", second)
	}
}
