// Package exitplan builds the wording of the trader-v5 candidate prompt (issue
// #925): trader-v3 with its direction and continuation_probability questions
// asked against the exits Paper positions are really closed by (FR-EXIT-2), not
// "over the next several minutes".
//
// Why: Jev was never told what the trade is judged by (the question says only
// "over the next several minutes"), and the real Paper exits of the candidates
// that passed every gate lost on average (docs/architecture/paper-results-mcp-jev-prompt.md).
//
// The numbers come from execution.Config (execution.DefaultConfig(): the
// FR-EXIT-2 initial values), so the wording is that of the code that closes the
// trade. A change of those defaults changes the wording, and so the hash that
// promptflow/prompt_version_test.go pins for trader-v5: the test fails until the
// change is made as a new version, never as a silent edit of trader-v5.
package exitplan

import (
	"fmt"
	"maps"
	"math"
	"strconv"
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/systemone"
)

// Question ids this package rewrites: the keys of jev's trader question group.
const (
	qDirection               = "direction"
	qContinuationProbability = "continuation_probability"
)

// The phrases of trader-v3 that are replaced; the rest of both questions (and
// every other question, option and Rubric) stays the trader-v3 text.
const (
	directionHorizon    = " over the next several minutes (a short intraday horizon, minutes rather than hours), or NONE if there is no clear edge"
	continuationHorizon = " over the next several minutes rather than stall or reverse"
)

// pct renders a percentage as "1.2%" and bps the same value as "120 bps".
func pct(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) + "%" }
func bps(v float64) string {
	return strconv.FormatFloat(math.Round(v*100*1e6)/1e6, 'f', -1, 64) + " bps"
}

// Questions returns base (the trader-v3 question group) with the direction and
// continuation_probability Instructions rewritten for the exits cfg describes.
// withGuide is the shared state guide appended to every question; the
// replacement is applied to the Instructions base already carries, so only the
// horizon phrase changes. It panics if base does not carry the trader-v3
// phrase: trader-v3 is pinned by hash (promptflow/prompt_version_test.go), so
// that is a programming error that every test run of this repository catches,
// not a runtime condition. The wording lists only the numeric exits: Execution
// also closes on a Jev reversal, a continuation_probability drop, a VWAP cross
// and before the close, which the questions leave out on purpose.
func Questions(base map[string]systemone.Question, cfg execution.Config) map[string]systemone.Question {
	exits := fmt.Sprintf("take profit at +%s (%s), stop loss at -%s (%s), a trailing stop %s below the highest price since entry and a forced exit %d minutes after entry",
		pct(cfg.TakeProfitPct), bps(cfg.TakeProfitPct), pct(cfg.StopLossPct), bps(cfg.StopLossPct), pct(cfg.TrailingStopPct), cfg.MaxHoldingMinutes)
	out := maps.Clone(base)
	out[qDirection] = replaced(base[qDirection], directionHorizon,
		" for a position entered at the ask (a LONG; a SHORT is the mirror image) and closed by whichever of these comes first: "+exits+
			", the round-trip cost (the spread, slippage and fees) coming off the result. Answer LONG or SHORT only if the trade is expected to end positive after that cost; "+
			"a move that may continue but not far enough to pay for the round trip is NONE, and so is a case with no clear edge")
	out[qContinuationProbability] = replaced(base[qContinuationProbability], continuationHorizon,
		" far enough for a position taken on it now (a LONG enters at the ask, a SHORT is the mirror image) to reach its take profit (+"+pct(cfg.TakeProfitPct)+") before the stop loss (-"+pct(cfg.StopLossPct)+
			"), the "+pct(cfg.TrailingStopPct)+" trailing stop or the "+strconv.Itoa(cfg.MaxHoldingMinutes)+"-minute limit, and to end positive after the round-trip cost, rather than stall or reverse")
	return out
}

// replaced returns q with the one occurrence of old in its Instructions
// replaced by repl.
func replaced(q systemone.Question, old, repl string) systemone.Question {
	if strings.Count(q.Instructions, old) != 1 {
		panic("exitplan: the trader-v3 question no longer carries the horizon phrase " + strconv.Quote(old))
	}
	q.Instructions = strings.Replace(q.Instructions, old, repl, 1)
	return q
}
