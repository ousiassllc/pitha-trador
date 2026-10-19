package actuals

// Criteria are the documented thresholds of the evaluation (issue #777
// 要件2/4; docs/requirements/functional/performance-evaluation.md §4
// 合格ライン（FR-EVAL-4）). They are code defaults, not runtime settings: nothing in the
// evaluation is editable from the Settings screen, and nothing gates Live
// trading on it (#55 stays a human decision).
type Criteria struct {
	// MinSample is the smallest number of observations from which a metric is
	// shown; below it the metric is 判定不能.
	MinSample int `json:"min_sample"`
	// MinClusters is the smallest number of distinct JST days a metric must
	// span. Trades of one day share the market's direction, so they are not
	// independent: the confidence intervals treat each day as one cluster and
	// cannot be trusted with only a few.
	MinClusters int `json:"min_clusters"`

	// The 合格ライン ("is it winning"). All four must hold.
	//
	// MinDays: distinct JST days with at least one closed position.
	MinDays int `json:"min_days"`
	// MinTrades: closed positions.
	MinTrades int `json:"min_trades"`
	// MinEVLowerBps: the lower end of EV (bps per trade)'s 95% interval must
	// be strictly above this.
	MinEVLowerBps float64 `json:"min_ev_lower_bps"`
	// MaxDrawdownPct: the max drawdown as a percentage of initial_capital
	// must not exceed this.
	MaxDrawdownPct float64 `json:"max_drawdown_pct"`
}

// DefaultCriteria returns the documented defaults.
//
//   - MinSample 30 / MinClusters 5: the usual floor for a normal-theory
//     interval of a mean (30 observations) and a t interval with at least 4
//     degrees of freedom over the daily clusters.
//   - MinDays 20 ≈ one month of trading days; MinTrades 100: a per-trade EV
//     of a few bps cannot be told from zero with fewer, given the per-trade
//     standard deviation of tens of bps.
//   - MinEVLowerBps 0: the interval must exclude "no edge after costs".
//   - MaxDrawdownPct 5: five times Paper's max_daily_loss_pct (1%), i.e. the
//     account may not lose more than five worst-allowed days in a row.
func DefaultCriteria() Criteria {
	return Criteria{
		MinSample:      30,
		MinClusters:    5,
		MinDays:        20,
		MinTrades:      100,
		MinEVLowerBps:  0,
		MaxDrawdownPct: 5,
	}
}
