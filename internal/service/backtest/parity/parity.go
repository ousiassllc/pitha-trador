// Package parity is the backtest's reproducibility check (issue #775,
// FR-BT-4): it compares the entries a replay of a period produced with the
// entries Paper Trading actually made in it - count, symbol and time - and
// their exit_reason distributions, and reports the match rates numerically
// against a lower bound. It reads only trades and positions the system already
// stores; nothing is persisted.
package parity

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
)

// Thresholds are the lower bounds a replay must reach to be called
// reproducing Paper Trading, and the pairing tolerance.
type Thresholds struct {
	// MinEntryMatchRate bounds EntryMatchRate from below.
	MinEntryMatchRate float64
	// MinExitReasonMatchRate bounds ExitReasonMatchRate from below.
	MinExitReasonMatchRate float64
	// Tolerance is how far apart in time a replayed and an actual entry of the
	// same symbol may be to count as the same entry (bars are minutes apart,
	// and an entry fills at its decision's bar).
	Tolerance time.Duration
}

// Provisional is the lower bound in force until real Paper results exist
// (docs/requirements/functional/components-platform.md FR-BT-4): half of the
// entries, and half of the matched entries' exit reasons, must agree. It is a
// deliberately loose placeholder - no Paper trade existed to calibrate a
// tighter one - and is to be raised once measured rates are known.
var Provisional = Thresholds{MinEntryMatchRate: 0.5, MinExitReasonMatchRate: 0.5, Tolerance: 2 * time.Minute}

// Verdict is the outcome of a comparison.
type Verdict string

const (
	// VerdictNoData: Paper made no entry in the period, so there is nothing to
	// reproduce. It is not a failure.
	VerdictNoData Verdict = "データ無し"
	VerdictPass   Verdict = "合格"
	VerdictFail   Verdict = "不合格"
)

// Report is the comparison of a replay with the actual Paper trades of one
// period.
type Report struct {
	Thresholds Thresholds
	Verdict    Verdict

	ActualEntries, ReplayEntries, MatchedEntries int
	// EntryMatchRate is MatchedEntries / max(ActualEntries, ReplayEntries):
	// entries the replay missed and entries it invented both lower it.
	EntryMatchRate float64
	// Recall is the share of actual entries the replay reproduced; Precision
	// the share of replayed entries that were actual.
	Recall, Precision float64
	// DirectionMatched counts matched entries whose direction also agrees.
	DirectionMatched int

	// ExitReasonCompared is the matched entries whose actual position is
	// closed; ExitReasonMatched of them closed for the same reason in the
	// replay. With none compared the exit_reason bound does not apply.
	ExitReasonCompared, ExitReasonMatched int
	ExitReasonMatchRate                   float64
	// ActualExitReasons/ReplayExitReasons are the exit_reason distributions of
	// all closed trades of each side.
	ActualExitReasons, ReplayExitReasons map[string]int

	// ActualMetrics/ReplayMetrics are FR-BT-1's metrics over each side's
	// closed trades, for the side-by-side comparison.
	ActualMetrics, ReplayMetrics backtest.Metrics
}

// TradesFromPositions converts Paper positions to the replay's trade shape so
// both sides compare and aggregate alike. NetReturnPct is the realized P&L
// (net of fees) over the entry notional; an open position has no exit time,
// reason or return.
func TradesFromPositions(positions []domain.Position) []backtest.Trade {
	trades := make([]backtest.Trade, 0, len(positions))
	for _, p := range positions {
		t := backtest.Trade{
			InstrumentID: p.InstrumentID, Symbol: p.Symbol, Direction: p.Side,
			EntryTimestamp: p.OpenedAt, EntryPrice: p.EntryPrice, Quantity: p.Quantity,
		}
		if p.ClosedAt != nil && p.RealizedPnL != nil && p.Quantity > 0 && p.EntryPrice > 0 {
			t.ExitTimestamp = *p.ClosedAt
			t.NetReturnPct = *p.RealizedPnL / (float64(p.Quantity) * p.EntryPrice) * 100
			if p.ExitReason != nil {
				t.ExitReason = *p.ExitReason
			}
		}
		trades = append(trades, t)
	}
	return trades
}

// Compare matches replay's entries to actual's (same symbol, entry times within
// th.Tolerance, each trade used once, nearest first) and judges the match
// rates against th.
func Compare(actual, replay []backtest.Trade, th Thresholds) Report {
	r := Report{
		Thresholds: th, ActualEntries: len(actual), ReplayEntries: len(replay),
		ActualExitReasons: exitReasons(actual), ReplayExitReasons: exitReasons(replay),
		ActualMetrics: backtest.Aggregate(closedOnly(actual)), ReplayMetrics: backtest.Aggregate(closedOnly(replay)),
	}
	for _, pair := range match(actual, replay, th.Tolerance) {
		r.MatchedEntries++
		if pair.actual.Direction == pair.replay.Direction {
			r.DirectionMatched++
		}
		if pair.actual.ExitReason != "" {
			r.ExitReasonCompared++
			if pair.actual.ExitReason == pair.replay.ExitReason {
				r.ExitReasonMatched++
			}
		}
	}
	r.EntryMatchRate = ratio(r.MatchedEntries, max(r.ActualEntries, r.ReplayEntries))
	r.Recall = ratio(r.MatchedEntries, r.ActualEntries)
	r.Precision = ratio(r.MatchedEntries, r.ReplayEntries)
	r.ExitReasonMatchRate = ratio(r.ExitReasonMatched, r.ExitReasonCompared)
	switch {
	case r.ActualEntries == 0:
		r.Verdict = VerdictNoData
	case r.EntryMatchRate < th.MinEntryMatchRate || (r.ExitReasonCompared > 0 && r.ExitReasonMatchRate < th.MinExitReasonMatchRate):
		r.Verdict = VerdictFail
	default:
		r.Verdict = VerdictPass
	}
	return r
}

// Err is nil unless the verdict is a failure: データ無し is not one.
func (r Report) Err() error {
	if r.Verdict != VerdictFail {
		return nil
	}
	return fmt.Errorf("parity: backtest does not reproduce Paper trading: entry match rate %.1f%% (lower bound %.1f%%), exit_reason match rate %.1f%% (lower bound %.1f%%)",
		r.EntryMatchRate*100, r.Thresholds.MinEntryMatchRate*100, r.ExitReasonMatchRate*100, r.Thresholds.MinExitReasonMatchRate*100)
}

// String is the operator-facing report (Japanese): the numbers and the verdict.
func (r Report) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "バックテスト再現性検証: %s\n", r.Verdict)
	if r.Verdict == VerdictNoData {
		b.WriteString("  期間内にPaperのエントリーが無いため、突き合わせる対象がありません。\n")
		return b.String()
	}
	fmt.Fprintf(&b, "  エントリー: Paper %d件 / 再生 %d件 / 一致 %d件（方向も一致 %d件）\n", r.ActualEntries, r.ReplayEntries, r.MatchedEntries, r.DirectionMatched)
	fmt.Fprintf(&b, "  エントリー一致率 %.1f%%（下限 %.1f%%）、再現率 %.1f%%、適合率 %.1f%%\n", r.EntryMatchRate*100, r.Thresholds.MinEntryMatchRate*100, r.Recall*100, r.Precision*100)
	fmt.Fprintf(&b, "  exit_reason一致率 %.1f%%（%d/%d件、下限 %.1f%%）\n", r.ExitReasonMatchRate*100, r.ExitReasonMatched, r.ExitReasonCompared, r.Thresholds.MinExitReasonMatchRate*100)
	fmt.Fprintf(&b, "  exit_reason分布 Paper: %s / 再生: %s\n", formatCounts(r.ActualExitReasons), formatCounts(r.ReplayExitReasons))
	fmt.Fprintf(&b, "  Paper: 件数 %d 勝率 %.1f%% Expectancy %.3f%% MaxDD %.3f%%\n", r.ActualMetrics.TradeCount, r.ActualMetrics.WinRate*100, r.ActualMetrics.Expectancy, r.ActualMetrics.MaxDrawdownPct)
	fmt.Fprintf(&b, "  再生:  件数 %d 勝率 %.1f%% Expectancy %.3f%% MaxDD %.3f%%\n", r.ReplayMetrics.TradeCount, r.ReplayMetrics.WinRate*100, r.ReplayMetrics.Expectancy, r.ReplayMetrics.MaxDrawdownPct)
	return b.String()
}

type pair struct{ actual, replay backtest.Trade }

// match pairs trades of the same symbol whose entry times are within tol,
// the closest pairs first so one replayed entry is never claimed twice.
func match(actual, replay []backtest.Trade, tol time.Duration) []pair {
	type cand struct {
		a, r int
		gap  time.Duration
	}
	var cands []cand
	for i, a := range actual {
		for j, rp := range replay {
			if a.Symbol != rp.Symbol {
				continue
			}
			if gap := a.EntryTimestamp.Sub(rp.EntryTimestamp).Abs(); gap <= tol {
				cands = append(cands, cand{a: i, r: j, gap: gap})
			}
		}
	}
	sort.SliceStable(cands, func(x, y int) bool { return cands[x].gap < cands[y].gap })
	usedA, usedR := map[int]bool{}, map[int]bool{}
	var pairs []pair
	for _, c := range cands {
		if usedA[c.a] || usedR[c.r] {
			continue
		}
		usedA[c.a], usedR[c.r] = true, true
		pairs = append(pairs, pair{actual: actual[c.a], replay: replay[c.r]})
	}
	return pairs
}

func closedOnly(trades []backtest.Trade) []backtest.Trade {
	var out []backtest.Trade
	for _, t := range trades {
		if !t.ExitTimestamp.IsZero() {
			out = append(out, t)
		}
	}
	return out
}

func exitReasons(trades []backtest.Trade) map[string]int {
	counts := map[string]int{}
	for _, t := range closedOnly(trades) {
		reason := t.ExitReason
		if reason == "" {
			reason = "(unknown)"
		}
		counts[reason]++
	}
	return counts
}

func formatCounts(counts map[string]int) string {
	if len(counts) == 0 {
		return "なし"
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s=%d", k, counts[k])
	}
	return strings.Join(parts, " ")
}

func ratio(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) / float64(d)
}
