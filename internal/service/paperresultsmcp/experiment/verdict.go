package experiment

import (
	"errors"
	"fmt"
)

// MinTradesFloor is the server-side lower bound of min_trades: how many closed
// Paper trades EACH of the two periods needs before an experiment is judged.
// Fewer is verdict insufficient_trades: the figures are returned, the call is
// not. A caller (experiments_list / experiment_compare's min_trades) may RAISE
// the minimum but never lower it; a value below the floor is refused with
// invalid_value, never silently replaced (an answer computed on a different
// minimum than the one asked for would mislead).
//
// PROVISIONAL VALUE (user decision, issue #871): 64 stays as it is, but it is
// not final. The production standard deviation sigma of the net pnl per trade
// is NOT measured yet, so the value is to be reviewed once sigma is measured
// (あいり's approval is still outstanding; nothing treats 64 as approved). It
// is kept in this one place.
//
// Basis (docs/architecture/paper-results-mcp-experiments.md §5): the two-sample
// comparison of the mean net pnl per trade, two-sided alpha 0.05, power 0.8,
// to detect a difference of half a standard deviation of the per-trade pnl
// (d = 0.5): n = 2 * (1.96 + 0.84)^2 / d^2 = 62.8 -> 63 per period, +1 for the
// t distribution = 64. That formula is in units of sigma, so how many yen of
// difference 64 trades can show (d * sigma) is unknown too, until sigma is
// measured on the production database.
const MinTradesFloor = 64

// MinTradesProvisionalText is the fixed wording that says MinTradesFloor is a
// provisional value; the tool descriptions and the Settings note use it.
const MinTradesProvisionalText = "暫定値（本番の 1 取引あたり損益の標準偏差 σ は未確認。σ を測って見直す予定）"

// MaxMinTrades is the largest min_trades a caller may ask for.
const MaxMinTrades = 10000

// Where the min_trades of a response came from.
const (
	// MinTradesSourceFloor: MinTradesFloor, a provisional value (sigma not
	// measured yet; to be reviewed). The string keeps "unapproved": the
	// approval is still outstanding.
	MinTradesSourceFloor = "floor_provisional_unapproved"
	// MinTradesSourceRaised: the caller asked for more than the floor.
	MinTradesSourceRaised = "argument_raised"
)

// ErrMinTradesBelowFloor is a min_trades under MinTradesFloor.
var ErrMinTradesBelowFloor = errors.New("experiment: min_trades is below the floor")

// CheckMinTrades refuses a caller's min_trades outside
// MinTradesFloor..MaxMinTrades. The error is ErrMinTradesBelowFloor, or one
// naming the maximum; its text is safe to show the caller.
func CheckMinTrades(minTrades int) error {
	switch {
	case minTrades < MinTradesFloor:
		return fmt.Errorf("%w: min_trades=%d, the lowest accepted is %d (it can be raised, not lowered)", ErrMinTradesBelowFloor, minTrades, MinTradesFloor)
	case minTrades > MaxMinTrades:
		return fmt.Errorf("min_trades=%d is above the highest accepted %d", minTrades, MaxMinTrades)
	}
	return nil
}

// Verdicts. The test is Welch's t test of the mean net pnl per trade (after
// costs) of the two periods, and the interval is the 95% confidence interval
// of after minus before (welch.go). The order of the rules is aborted ->
// confounded -> insufficient_trades -> not_computable -> the call. Only
// improved is a win, and it is deliberately hard to get; every other outcome
// errs on the side of not claiming one. The win rate and the rest are shown
// for reference and never decide.
const (
	// VerdictAborted: the experiment was given up; it is not judged.
	VerdictAborted = "aborted"
	// VerdictConfounded: something other than the variable changed in the
	// before or the after period (Confounders lists it), so the comparison
	// says nothing.
	VerdictConfounded = "confounded"
	// VerdictInsufficientTrades: a period has fewer than min_trades closed
	// trades (min_trades is never below MinTradesFloor). Judgement withheld.
	VerdictInsufficientTrades = "insufficient_trades"
	// VerdictNotComputable: both periods have enough trades but the interval
	// does not exist (every trade of both periods has the same pnl, so the
	// standard error is 0). Judgement withheld.
	VerdictNotComputable = "not_computable"
	// VerdictImproved: the lower end of the interval is above 0 AND the maximum
	// drawdown is no larger than before. The only win.
	VerdictImproved = "improved"
	// VerdictWorsened: the upper end of the interval is below 0 (the mean pnl
	// per trade fell), whatever the drawdown did.
	VerdictWorsened = "worsened"
	// VerdictMixed: the lower end of the interval is above 0 but the maximum
	// drawdown got larger: the gain per trade came with a worse worst case, so
	// it is not called a win.
	VerdictMixed = "mixed"
	// VerdictInconclusive: the interval contains 0: no difference of the mean
	// pnl per trade can be told from chance. Not "no effect" either.
	VerdictInconclusive = "inconclusive"
)

// verdict applies the rules in order. Aborted and confounded experiments, thin
// periods and a test that cannot be computed are not judged. Otherwise the
// 95% interval of the difference of the mean net pnl per trade decides, and
// only an interval wholly above 0 with a maximum drawdown no larger than
// before is a win.
func verdict(c Comparison) string {
	t := c.PerTradePnl
	switch {
	case c.Experiment.Status == StatusAborted:
		return VerdictAborted
	case c.Confounded:
		return VerdictConfounded
	case c.Before.Performance.ClosedTradeCount < c.MinTrades || c.After.Performance.ClosedTradeCount < c.MinTrades:
		return VerdictInsufficientTrades
	case !t.Computable():
		return VerdictNotComputable
	case *t.CILow > 0 && c.Diff.MaxDrawdownJPY <= 0:
		return VerdictImproved
	case *t.CILow > 0:
		return VerdictMixed
	case *t.CIHigh < 0:
		return VerdictWorsened
	}
	return VerdictInconclusive
}

// confounders lists what changed during the experiment (the period r, the
// experiment's start to its end) besides its variable:
//
//   - a whitelisted setting changed or reset (mcp_settings_audit; the
//     experiment's own change - its audit_id, or a later save of the target
