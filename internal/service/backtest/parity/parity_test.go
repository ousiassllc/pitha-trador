package parity_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest/parity"
)

var t0 = time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)

func trade(symbol string, entryMin int, reason string) backtest.Trade {
	return backtest.Trade{
		Symbol: symbol, Direction: domain.JevDirectionLong, EntryTimestamp: t0.Add(time.Duration(entryMin) * time.Minute),
		ExitTimestamp: t0.Add(time.Duration(entryMin+5) * time.Minute), ExitReason: reason, NetReturnPct: 0.2,
	}
}

func th() parity.Thresholds {
	return parity.Thresholds{MinEntryMatchRate: 0.8, MinExitReasonMatchRate: 0.6, Tolerance: time.Minute}
}

func TestCompare_NoPaperEntriesIsNoDataNotAFailure(t *testing.T) {
	report := parity.Compare(nil, []backtest.Trade{trade("7203", 0, domain.ExitReasonTakeProfit)}, th())
	if report.Verdict != parity.VerdictNoData {
		t.Fatalf("Verdict = %q, want %q", report.Verdict, parity.VerdictNoData)
	}
	if err := report.Err(); err != nil {
		t.Fatalf("Err = %v, want nil: no paper trades is not a failure", err)
	}
	if !strings.Contains(report.String(), "データ無し") {
		t.Errorf("report text %q does not say データ無し", report.String())
	}
}

func TestCompare_IdenticalTradesPass(t *testing.T) {
	trades := []backtest.Trade{
		trade("7203", 0, domain.ExitReasonTakeProfit), trade("6758", 3, domain.ExitReasonStopLoss), trade("7203", 20, domain.ExitReasonMaxHolding),
	}
	report := parity.Compare(trades, trades, th())
	if report.Verdict != parity.VerdictPass || report.Err() != nil {
		t.Fatalf("Verdict = %q, err %v; want a pass", report.Verdict, report.Err())
	}
	if report.EntryMatchRate != 1 || report.ExitReasonMatchRate != 1 || report.MatchedEntries != 3 || report.DirectionMatched != 3 {
		t.Errorf("report = %+v, want every entry and exit reason matched", report)
	}
}

func TestCompare_LowEntryMatchRateFails(t *testing.T) {
	actual := []backtest.Trade{
		trade("7203", 0, domain.ExitReasonTakeProfit), trade("6758", 3, domain.ExitReasonTakeProfit),
		trade("9984", 6, domain.ExitReasonTakeProfit), trade("8306", 9, domain.ExitReasonTakeProfit),
	}
	replay := []backtest.Trade{trade("7203", 0, domain.ExitReasonTakeProfit), trade("6758", 3, domain.ExitReasonTakeProfit)}

	report := parity.Compare(actual, replay, th())
	if report.EntryMatchRate != 0.5 || report.Recall != 0.5 || report.Precision != 1 {
		t.Fatalf("rates = entry %v recall %v precision %v, want 0.5/0.5/1", report.EntryMatchRate, report.Recall, report.Precision)
	}
	if report.Verdict != parity.VerdictFail {
		t.Fatalf("Verdict = %q, want %q (0.5 < 0.8)", report.Verdict, parity.VerdictFail)
	}
	if err := report.Err(); err == nil || !strings.Contains(err.Error(), "entry match rate 50.0%") {
		t.Fatalf("Err = %v, want an error naming the entry match rate", err)
	}
}

func TestCompare_InventedEntriesAlsoLowerTheMatchRate(t *testing.T) {
	actual := []backtest.Trade{trade("7203", 0, domain.ExitReasonTakeProfit)}
	replay := []backtest.Trade{
		trade("7203", 0, domain.ExitReasonTakeProfit), trade("6758", 3, domain.ExitReasonTakeProfit),
		trade("9984", 6, domain.ExitReasonTakeProfit), trade("8306", 9, domain.ExitReasonTakeProfit),
	}
	report := parity.Compare(actual, replay, th())
	if report.EntryMatchRate != 0.25 || report.Recall != 1 || report.Verdict != parity.VerdictFail {
		t.Fatalf("report = entry %v recall %v verdict %q, want 0.25 / 1 / fail", report.EntryMatchRate, report.Recall, report.Verdict)
	}
}

func TestCompare_ExitReasonMismatchFails(t *testing.T) {
	actual := []backtest.Trade{trade("7203", 0, domain.ExitReasonTakeProfit), trade("6758", 3, domain.ExitReasonStopLoss), trade("9984", 6, domain.ExitReasonStopLoss)}
	replay := []backtest.Trade{trade("7203", 0, domain.ExitReasonTakeProfit), trade("6758", 3, domain.ExitReasonMaxHolding), trade("9984", 6, domain.ExitReasonMaxHolding)}
	report := parity.Compare(actual, replay, th())
	if report.EntryMatchRate != 1 || report.ExitReasonMatched != 1 || report.ExitReasonCompared != 3 {
		t.Fatalf("report = %+v, want all entries matched but 1 of 3 exit reasons", report)
	}
	if report.Verdict != parity.VerdictFail {
		t.Fatalf("Verdict = %q, want fail (exit_reason rate 33.3%% < 60%%)", report.Verdict)
	}
	if report.ActualExitReasons[domain.ExitReasonStopLoss] != 2 || report.ReplayExitReasons[domain.ExitReasonMaxHolding] != 2 {
		t.Errorf("distributions = %v / %v", report.ActualExitReasons, report.ReplayExitReasons)
	}
}

func TestCompare_ToleranceAndOneToOnePairing(t *testing.T) {
	actual := []backtest.Trade{trade("7203", 0, domain.ExitReasonTakeProfit), trade("7203", 1, domain.ExitReasonTakeProfit)}
	// One replayed entry between two actual ones can be claimed only once.
	one := []backtest.Trade{trade("7203", 0, domain.ExitReasonTakeProfit)}
	if got := parity.Compare(actual, one, th()).MatchedEntries; got != 1 {
		t.Fatalf("MatchedEntries = %d, want 1: a replayed entry is claimed once", got)
	}
	// Outside the tolerance there is no match.
	far := []backtest.Trade{trade("7203", 30, domain.ExitReasonTakeProfit)}
	if got := parity.Compare(actual, far, th()).MatchedEntries; got != 0 {
		t.Fatalf("MatchedEntries = %d, want 0 outside the tolerance", got)
	}
	// Another symbol never matches.
	other := []backtest.Trade{trade("6758", 0, domain.ExitReasonTakeProfit)}
	if got := parity.Compare(actual, other, th()).MatchedEntries; got != 0 {
		t.Fatalf("MatchedEntries = %d, want 0 for another symbol", got)
	}
}

func TestCompare_DirectionMismatchIsCountedSeparately(t *testing.T) {
	actual := trade("7203", 0, domain.ExitReasonTakeProfit)
	replayed := actual
	replayed.Direction = domain.JevDirectionShort
	report := parity.Compare([]backtest.Trade{actual}, []backtest.Trade{replayed}, th())
	if report.MatchedEntries != 1 || report.DirectionMatched != 0 {
		t.Fatalf("matched %d, direction matched %d; want 1 and 0", report.MatchedEntries, report.DirectionMatched)
	}
}

func TestCompare_OpenPaperPositionsHaveNoExitReasonToCompare(t *testing.T) {
	open := backtest.Trade{Symbol: "7203", Direction: domain.JevDirectionLong, EntryTimestamp: t0}
	report := parity.Compare([]backtest.Trade{open}, []backtest.Trade{trade("7203", 0, domain.ExitReasonTakeProfit)}, th())
	if report.ExitReasonCompared != 0 || report.Verdict != parity.VerdictPass {
		t.Fatalf("report = %+v, want no exit reasons compared and a pass on the entry rate", report)
	}
}

func TestTradesFromPositions(t *testing.T) {
	closedAt := t0.Add(5 * time.Minute)
	pnl, reason := -1000.0, domain.ExitReasonStopLoss
	trades := parity.TradesFromPositions([]domain.Position{
		{InstrumentID: 1, Symbol: "7203", Side: domain.PositionSideShort, Quantity: 100, EntryPrice: 2000, OpenedAt: t0, ClosedAt: &closedAt, RealizedPnL: &pnl, ExitReason: &reason},
		{InstrumentID: 2, Symbol: "6758", Side: domain.PositionSideLong, Quantity: 100, EntryPrice: 3000, OpenedAt: t0},
	})
	if len(trades) != 2 {
		t.Fatalf("len = %d, want 2", len(trades))
	}
	if got := trades[0]; got.Direction != domain.JevDirectionShort || got.ExitReason != reason || got.NetReturnPct != -0.5 || !got.ExitTimestamp.Equal(closedAt) {
		t.Errorf("closed trade = %+v, want SHORT, stop_loss, -0.5%% (1000 / 200000)", got)
	}
	if got := trades[1]; !got.ExitTimestamp.IsZero() || got.ExitReason != "" {
		t.Errorf("open trade = %+v, want no exit", got)
	}
}

// provisionalFixture is total Paper entries (symbols S000.. all closed by
// take_profit) of which the replay reproduces the first entryMatched, closing
// the first exitMatched of those for the same reason and the rest by stop_loss.
func provisionalFixture(total, entryMatched, exitMatched int) (actual, replay []backtest.Trade) {
	for i := range total {
		symbol := fmt.Sprintf("S%03d", i)
		actual = append(actual, trade(symbol, 0, domain.ExitReasonTakeProfit))
		if i >= entryMatched {
			continue
		}
		reason := domain.ExitReasonStopLoss
		if i < exitMatched {
			reason = domain.ExitReasonTakeProfit
		}
		replay = append(replay, trade(symbol, 0, reason))
	}
	return actual, replay
}

// The verdict under Provisional splits exactly at its bounds: a rate equal to
// the lower bound passes and one just below fails.
func TestCompare_ProvisionalSplitsAtItsBounds(t *testing.T) {
	tests := []struct {
		name                                  string
		entryMatched, exitMatched             int
		wantVerdict                           parity.Verdict
		wantEntryMatchRate, wantExitMatchRate float64
	}{
		{"entry rate at the bound passes", 50, 50, parity.VerdictPass, 0.5, 1},
		{"entry rate just below the bound fails", 49, 49, parity.VerdictFail, 0.49, 1},
		{"exit_reason rate at the bound passes", 100, 50, parity.VerdictPass, 1, 0.5},
		{"exit_reason rate just below the bound fails", 100, 49, parity.VerdictFail, 1, 0.49},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual, replay := provisionalFixture(100, tc.entryMatched, tc.exitMatched)
			report := parity.Compare(actual, replay, parity.Provisional)
			if report.Verdict != tc.wantVerdict {
				t.Errorf("Verdict = %q, want %q (entry %v, recall %v, exit_reason %v)", report.Verdict, tc.wantVerdict, report.EntryMatchRate, report.Recall, report.ExitReasonMatchRate)
			}
			if report.EntryMatchRate != tc.wantEntryMatchRate || report.Recall != tc.wantEntryMatchRate || report.ExitReasonMatchRate != tc.wantExitMatchRate {
				t.Errorf("rates = entry %v recall %v exit_reason %v, want %v / %v / %v", report.EntryMatchRate, report.Recall, report.ExitReasonMatchRate, tc.wantEntryMatchRate, tc.wantEntryMatchRate, tc.wantExitMatchRate)
			}
			if (report.Err() != nil) != (tc.wantVerdict == parity.VerdictFail) {
				t.Errorf("Err = %v, want it set exactly for a failing verdict", report.Err())
			}
		})
	}
}

// Provisional's time tolerance is inclusive: an entry exactly Tolerance apart
// is the same entry, one nanosecond further is not.
func TestCompare_ProvisionalToleranceIsInclusive(t *testing.T) {
	actual := []backtest.Trade{trade("7203", 0, domain.ExitReasonTakeProfit)}
	for _, tc := range []struct {
		name        string
		offset      time.Duration
		wantMatched int
	}{
		{"at the tolerance", parity.Provisional.Tolerance, 1},
		{"just past the tolerance", parity.Provisional.Tolerance + time.Nanosecond, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			replayed := trade("7203", 0, domain.ExitReasonTakeProfit)
			replayed.EntryTimestamp = replayed.EntryTimestamp.Add(tc.offset)
			if got := parity.Compare(actual, []backtest.Trade{replayed}, parity.Provisional).MatchedEntries; got != tc.wantMatched {
				t.Errorf("MatchedEntries = %d, want %d", got, tc.wantMatched)
			}
		})
	}
}
