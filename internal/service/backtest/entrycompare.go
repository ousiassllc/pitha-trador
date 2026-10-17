package backtest

import "context"

// EntryComparison is the same history replayed under the live entry rule and
// under the pre-#765 confidence-threshold rule: how differently that rule would
// have evaluated the system (issue #775's "新旧ルールでのExpectancyの差").
type EntryComparison struct {
	// Meta is the replay under EntryMeta (the live rule), Legacy under
	// EntryLegacyConfidence; everything else - Risk Engine, exits, fills - is
	// the same.
	Meta, Legacy Metrics
}

// ExpectancyDiff is Meta's Expectancy minus Legacy's.
func (c EntryComparison) ExpectancyDiff() float64 { return c.Meta.Expectancy - c.Legacy.Expectancy }

// CompareEntryRules replays cfgs over period once per entry rule, as portfolios
// (ShadowBacktestPortfolioTrades). It stores nothing: the comparison is computed
// on read from the same history.
func CompareEntryRules(ctx context.Context, cfgs []RunConfig, period Period) (EntryComparison, error) {
	var out EntryComparison
	for _, mode := range []struct {
		entry EntryMode
		into  *Metrics
	}{{EntryMeta, &out.Meta}, {EntryLegacyConfidence, &out.Legacy}} {
		modeCfgs := make([]RunConfig, len(cfgs))
		for i, cfg := range cfgs {
			cfg.Entry = mode.entry
			modeCfgs[i] = cfg
		}
		trades, err := ShadowBacktestPortfolioTrades(ctx, modeCfgs, period)
		if err != nil {
			return EntryComparison{}, err
		}
		*mode.into = Aggregate(trades)
	}
	return out, nil
}
