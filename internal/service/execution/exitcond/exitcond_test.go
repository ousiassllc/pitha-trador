package exitcond_test

import (
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/execution/exitcond"
	"github.com/ousiassllc/pitha-trador/internal/service/execution/vwapcross"
)

func ptr[T any](v T) *T { return &v }

var (
	opened = time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	long   = domain.Position{Side: domain.PositionSideLong, EntryPrice: 1000, OpenedAt: opened}
	short  = domain.Position{Side: domain.PositionSideShort, EntryPrice: 1000, OpenedAt: opened}
)

func allOn() exitcond.Params {
	return exitcond.Params{StopLossPct: 1, TakeProfitPct: 2, TrailingStopPct: 0.5, MaxHolding: 20 * time.Minute, ForceFlatBeforeClose: 10 * time.Minute}
}

func at(min int) time.Time { return opened.Add(time.Duration(min) * time.Minute) }

func evaluate(t *testing.T, p exitcond.Params, pos domain.Position, mkt exitcond.Market, lk exitcond.Lookups) (string, bool) {
	t.Helper()
	reason, exit, err := exitcond.Evaluate(p, pos, mkt, lk)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	return reason, exit
}

func best(v float64) exitcond.Lookups {
	return exitcond.Lookups{BestPrice: func() (float64, error) { return v, nil }}
}

func TestEvaluate_EachCondition(t *testing.T) {
	jevDown := &domain.JevDecision{Direction: ptr(domain.JevDirectionShort)}
	lowContinuation := &domain.JevDecision{Direction: ptr(domain.JevDirectionLong), ContinuationProbability: ptr(0.2)}
	tests := []struct {
		name string
		pos  domain.Position
		mkt  exitcond.Market
		lk   exitcond.Lookups
		want string
	}{
		{"stop loss long", long, exitcond.Market{Price: 989, Now: at(1)}, exitcond.Lookups{}, domain.ExitReasonStopLoss},
		{"stop loss short", short, exitcond.Market{Price: 1011, Now: at(1)}, exitcond.Lookups{}, domain.ExitReasonStopLoss},
		{"take profit long", long, exitcond.Market{Price: 1021, Now: at(1)}, exitcond.Lookups{}, domain.ExitReasonTakeProfit},
		{"take profit short", short, exitcond.Market{Price: 979, Now: at(1)}, exitcond.Lookups{}, domain.ExitReasonTakeProfit},
		{"trailing stop long", long, exitcond.Market{Price: 1010, Now: at(1)}, best(1016), domain.ExitReasonTrailingStop},
		{"trailing stop short", short, exitcond.Market{Price: 990, Now: at(1)}, best(984), domain.ExitReasonTrailingStop},
		{"jev direction reversed", long, exitcond.Market{Price: 1001, Now: at(1), Decision: jevDown}, exitcond.Lookups{}, domain.ExitReasonJevDirectionReversed},
		{"continuation drop", long, exitcond.Market{Price: 1001, Now: at(1), Decision: lowContinuation},
			exitcond.Lookups{ContinuationDropped: func(p float64) bool { return p < 0.6 }}, domain.ExitReasonContinuationProbDrop},
		{"vwap cross long", long, exitcond.Market{Price: 999, VWAP: ptr(1000.0), PrevVWAP: &vwapcross.Observation{Price: 1001, VWAP: 1000}, Now: at(1)}, exitcond.Lookups{}, domain.ExitReasonVWAPCross},
		{"max holding", long, exitcond.Market{Price: 1001, Now: at(20)}, exitcond.Lookups{}, domain.ExitReasonMaxHolding},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got, exit := evaluate(t, allOn(), tc.pos, tc.mkt, tc.lk); !exit || got != tc.want {
				t.Fatalf("Evaluate = (%q, %v), want %q", got, exit, tc.want)
			}
		})
	}

	t.Run("force flat before close", func(t *testing.T) {
		p := allOn()
		p.MaxHolding = 0
		closeAt := at(30)
		if _, exit := evaluate(t, p, long, exitcond.Market{Price: 1001, Now: at(19), MarketCloseAt: &closeAt}, exitcond.Lookups{}); exit {
			t.Fatal("exited 11 minutes before the close with a 10-minute force-flat window")
		}
		if got, exit := evaluate(t, p, long, exitcond.Market{Price: 1001, Now: at(20), MarketCloseAt: &closeAt}, exitcond.Lookups{}); !exit || got != domain.ExitReasonForceFlatBeforeClose {
			t.Fatalf("Evaluate = (%q, %v), want force_flat_before_close", got, exit)
		}
	})
}

func TestEvaluate_PriorityOrder(t *testing.T) {
	closeAt := at(20)
	// Everything triggers at once: the order is SL, TP, trailing, Jev reversal,
	// continuation, VWAP, max holding, force flat.
	mkt := exitcond.Market{
		Price: 999, Now: at(25), MarketCloseAt: &closeAt, VWAP: ptr(1000.0),
		PrevVWAP: &vwapcross.Observation{Price: 1005, VWAP: 1000},
		Decision: &domain.JevDecision{Direction: ptr(domain.JevDirectionShort), ContinuationProbability: ptr(0.1)},
	}
	lk := exitcond.Lookups{BestPrice: func() (float64, error) { return 1010, nil }, ContinuationDropped: func(float64) bool { return true }}
	want := []string{
		domain.ExitReasonTrailingStop, domain.ExitReasonJevDirectionReversed, domain.ExitReasonContinuationProbDrop,
		domain.ExitReasonVWAPCross, domain.ExitReasonMaxHolding, domain.ExitReasonForceFlatBeforeClose,
	}
	p := allOn()
	for i, reason := range want {
		got, exit := evaluate(t, p, long, mkt, lk)
		if !exit || got != reason {
			t.Fatalf("step %d: Evaluate = (%q, %v), want %q", i, got, exit, reason)
		}
		switch reason { // switch the winning condition off, as the next one must win
		case domain.ExitReasonTrailingStop:
			p.TrailingStopPct = 0
		case domain.ExitReasonJevDirectionReversed:
			mkt.Decision = &domain.JevDecision{Direction: ptr(domain.JevDirectionLong), ContinuationProbability: ptr(0.1)}
		case domain.ExitReasonContinuationProbDrop:
			lk.ContinuationDropped = nil
		case domain.ExitReasonVWAPCross:
			mkt.VWAP = nil
		case domain.ExitReasonMaxHolding:
			p.MaxHolding = 0
		}
	}

	mkt.Price = 989
	if got, _ := evaluate(t, allOn(), long, mkt, lk); got != domain.ExitReasonStopLoss {
		t.Fatalf("a stop-loss price exited as %q, want stop_loss first", got)
	}
}

func TestEvaluate_DisabledConditionsAndMissingInputs(t *testing.T) {
	// A zero Params and no lookups: nothing can exit, whatever the price does.
	if got, exit := evaluate(t, exitcond.Params{}, long, exitcond.Market{Price: 1, Now: at(999)}, exitcond.Lookups{}); exit {
		t.Fatalf("zero Params exited (%q)", got)
	}
	// FR-EXIT-3: no Jev decision disables only the two Jev conditions.
	if got, exit := evaluate(t, allOn(), long, exitcond.Market{Price: 989, Now: at(1)}, exitcond.Lookups{}); !exit || got != domain.ExitReasonStopLoss {
		t.Fatalf("without Jev: (%q, %v), want stop_loss", got, exit)
	}
}

func TestEvaluate_BestPriceErrorIsReturned(t *testing.T) {
	boom := errors.New("history unavailable")
	_, _, err := exitcond.Evaluate(allOn(), long, exitcond.Market{Price: 1001, Now: at(1)}, exitcond.Lookups{BestPrice: func() (float64, error) { return 0, boom }})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the BestPrice error", err)
	}
}

func TestTrailingTriggered(t *testing.T) {
	if !exitcond.TrailingTriggered(0.5, 1, 1010, 1004) { // 0.59% off the peak
		t.Error("LONG retracing 0.59% from the peak did not trigger a 0.5% trailing stop")
	}
	if exitcond.TrailingTriggered(0.5, 1, 1010, 1006) { // 0.40%
		t.Error("LONG retracing 0.40% triggered a 0.5% trailing stop")
	}
	if !exitcond.TrailingTriggered(0.5, -1, 990, 996) { // SHORT: 0.61% off the trough
		t.Error("SHORT retracing from the trough did not trigger")
	}
	if exitcond.TrailingTriggered(0.5, 1, 1000, 1020) { // the current price is the new peak
		t.Error("a new high triggered the trailing stop")
	}
}
