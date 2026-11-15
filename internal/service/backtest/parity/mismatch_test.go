package parity_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest/parity"
)

func TestCompare_ClassifiesTheEntriesThatDoNotPair(t *testing.T) {
	actual := []backtest.Trade{
		trade("7203", 0, domain.ExitReasonTakeProfit),  // paired with the replay's 7203 at 0
		trade("7203", 1, domain.ExitReasonStopLoss),    // 7203 at 0 is closer and taken: paired elsewhere
		trade("6758", 0, domain.ExitReasonStopLoss),    // the replay never entered 6758
		trade("9984", 0, domain.ExitReasonMaxHolding),  // the replay entered 9984 ten minutes later
		trade("8306", 30, domain.ExitReasonTakeProfit), // paired
	}
	replay := []backtest.Trade{
		trade("7203", 0, domain.ExitReasonTakeProfit),
		trade("9984", 10, domain.ExitReasonMaxHolding),
		trade("8306", 30, domain.ExitReasonTrailingStop),
		trade("4063", 40, domain.ExitReasonTakeProfit), // Paper never traded 4063
		trade("8306", 31, domain.ExitReasonTakeProfit), // paired elsewhere (8306 at 30 is closer and taken)
	}
	report := parity.Compare(actual, replay, th())

	if report.MatchedEntries != 2 {
		t.Fatalf("MatchedEntries = %d, want 2", report.MatchedEntries)
	}
	wantMissed := map[parity.Mismatch]int{parity.MismatchPairedElsewhere: 1, parity.MismatchSymbolAbsent: 1, parity.MismatchTimeOffset: 1}
	if !reflect.DeepEqual(report.MissedEntries, wantMissed) {
		t.Errorf("MissedEntries = %v, want %v", report.MissedEntries, wantMissed)
	}
	wantExtra := map[parity.Mismatch]int{parity.MismatchSymbolAbsent: 1, parity.MismatchTimeOffset: 1, parity.MismatchPairedElsewhere: 1}
	if !reflect.DeepEqual(report.ExtraEntries, wantExtra) {
		t.Errorf("ExtraEntries = %v, want %v", report.ExtraEntries, wantExtra)
	}
	if got := sum(report.MissedEntries); got != report.ActualEntries-report.MatchedEntries {
		t.Errorf("missed classes sum to %d, want %d", got, report.ActualEntries-report.MatchedEntries)
	}
	if got := sum(report.ExtraEntries); got != report.ReplayEntries-report.MatchedEntries {
		t.Errorf("extra classes sum to %d, want %d", got, report.ReplayEntries-report.MatchedEntries)
	}
}

func TestCompare_ExitReasonPairsSayWhatTheReplayClosedWith(t *testing.T) {
	actual := []backtest.Trade{
		trade("7203", 0, domain.ExitReasonStopLoss), trade("6758", 20, domain.ExitReasonStopLoss), trade("9984", 40, domain.ExitReasonTakeProfit),
	}
	replay := []backtest.Trade{
		trade("7203", 0, domain.ExitReasonStopLoss), trade("6758", 20, domain.ExitReasonTrailingStop), trade("9984", 40, domain.ExitReasonTakeProfit),
	}
	report := parity.Compare(actual, replay, th())

	want := map[string]map[string]int{
		domain.ExitReasonStopLoss:   {domain.ExitReasonStopLoss: 1, domain.ExitReasonTrailingStop: 1},
		domain.ExitReasonTakeProfit: {domain.ExitReasonTakeProfit: 1},
	}
	if !reflect.DeepEqual(report.ExitReasonPairs, want) {
		t.Errorf("ExitReasonPairs = %v, want %v", report.ExitReasonPairs, want)
	}
	diagonal := 0
	for paper, row := range report.ExitReasonPairs {
		diagonal += row[paper]
	}
	if diagonal != report.ExitReasonMatched {
		t.Errorf("diagonal = %d, want ExitReasonMatched %d", diagonal, report.ExitReasonMatched)
	}
	text := report.String()
	for _, want := range []string{"Paper stop_loss → 再生 stop_loss=1 trailing_stop=1", "Paper take_profit → 再生 take_profit=1"} {
		if !strings.Contains(text, want) {
			t.Errorf("report text lacks %q:\n%s", want, text)
		}
	}
}

func TestReport_StringBreaksDownTheMismatches(t *testing.T) {
	actual := []backtest.Trade{trade("7203", 0, domain.ExitReasonTakeProfit), trade("6758", 0, domain.ExitReasonStopLoss)}
	replay := []backtest.Trade{trade("7203", 0, domain.ExitReasonTakeProfit)}
	text := parity.Compare(actual, replay, th()).String()
	for _, want := range []string{"Paperにあって再生に無いエントリー 1件: symbol_absent=1件", "再生にあってPaperに無いエントリー 0件: なし"} {
		if !strings.Contains(text, want) {
			t.Errorf("report text lacks %q:\n%s", want, text)
		}
	}
}

func sum(m map[parity.Mismatch]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}
