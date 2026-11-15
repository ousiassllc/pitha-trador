package parity

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
)

// Mismatch says why an entry on one side has no counterpart on the other. It
// is a classification of what the two entry lists show (symbol and time), not a
// diagnosis of the cause inside the replay; the entry-rate numbers it breaks
// down are Report's.
type Mismatch string

const (
	// MismatchSymbolAbsent: the other side made no entry in the symbol at all
	// in the period (the replay had no decision/bars to enter on, or its model,
	// Risk Engine or guards skipped it; or, for an extra replayed entry, Paper
	// never traded the symbol).
	MismatchSymbolAbsent Mismatch = "symbol_absent"
	// MismatchTimeOffset: the other side entered the symbol, but no entry of it
	// lies within the tolerance (the same trade at a different bar, or a
	// different trade of the symbol).
	MismatchTimeOffset Mismatch = "time_offset"
	// MismatchPairedElsewhere: an entry of the symbol within the tolerance
	// exists on the other side but is already paired with a closer one (one
	// side entered the symbol twice where the other entered once).
	MismatchPairedElsewhere Mismatch = "paired_elsewhere"
)

// mismatchOrder is the report order and the Japanese label of each class.
var mismatchOrder = []struct {
	reason Mismatch
	label  string
}{
	{MismatchSymbolAbsent, "相手側に同銘柄のエントリー無し"},
	{MismatchTimeOffset, "同銘柄はあるが時刻が許容差の外"},
	{MismatchPairedElsewhere, "許容差内の相手は別のエントリーと対応済み"},
}

// classifyUnmatched counts the trades of side that used marks unpaired, by
// why: other is the opposite side's list.
func classifyUnmatched(side, other []backtest.Trade, used []bool, tol time.Duration) map[Mismatch]int {
	counts := map[Mismatch]int{}
	for i, t := range side {
		if used[i] {
			continue
		}
		counts[classify(t, other, tol)]++
	}
	return counts
}

func classify(t backtest.Trade, other []backtest.Trade, tol time.Duration) Mismatch {
	sameSymbol, withinTolerance := false, false
	for _, o := range other {
		if o.Symbol != t.Symbol {
			continue
		}
		sameSymbol = true
		if t.EntryTimestamp.Sub(o.EntryTimestamp).Abs() <= tol {
			withinTolerance = true
		}
	}
	switch {
	case !sameSymbol:
		return MismatchSymbolAbsent
	case withinTolerance:
		return MismatchPairedElsewhere
	default:
		return MismatchTimeOffset
	}
}

// countExitReasonPair records a matched closed pair's exit reasons.
func (r *Report) countExitReasonPair(p pair) {
	row := r.ExitReasonPairs[p.actual.ExitReason]
	if row == nil {
		row = map[string]int{}
		r.ExitReasonPairs[p.actual.ExitReason] = row
	}
	reason := p.replay.ExitReason
	if reason == "" {
		reason = "(unknown)"
	}
	row[reason]++
}

// formatMismatches is the one-line breakdown of a classification ("なし" when
// every entry of the side was paired).
func formatMismatches(counts map[Mismatch]int) string {
	var parts []string
	for _, m := range mismatchOrder {
		if n := counts[m.reason]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d件（%s）", m.reason, n, m.label))
		}
	}
	if len(parts) == 0 {
		return "なし"
	}
	return strings.Join(parts, " / ")
}

// formatExitReasonPairs lists, per Paper exit_reason, what the replay closed
// the matched positions with ("Paper stop_loss → 再生 stop_loss=3 trailing_stop=1").
func formatExitReasonPairs(pairs map[string]map[string]int) string {
	var b strings.Builder
	paper := make([]string, 0, len(pairs))
	for k := range pairs {
		paper = append(paper, k)
	}
	sort.Strings(paper)
	for _, k := range paper {
		fmt.Fprintf(&b, "    Paper %s → 再生 %s\n", k, formatCounts(pairs[k]))
	}
	return b.String()
}
