package jev

import "github.com/ousiassllc/pitha-trador/internal/service/jev/systemone"

// The question wording below is model-facing and is what Jev actually
// answers. Changing it changes what the recorded probabilities mean, so
// bump ScoutQuestionVersion / TraderQuestionVersion (prompt_version.go)
// whenever any instructions, criteria or option rubric changes.

// Question ids of the FR-SCOUT-1 group. They double as the JSON keys of
// ScoutResponse and of the answers map.
const (
	qInterestingNow   = "interesting_now"
	qMomentumQuality  = "momentum_quality"
	qLiquidityOk      = "liquidity_ok"
	qAbnormalActivity = "abnormal_activity"
)

// stateGuide is appended to every question so each one is self-contained:
// the API evaluates the questions independently and does not share
// context between them. It names the two top-level fields of the state
// (systemOneState) and fixes the units the market fields use.
const stateGuide = "Context: `market` is the current intraday snapshot of one stock listed on the Tokyo Stock Exchange. " +
	"Returns are fractions (0.01 = +1%), `*_bps` fields are basis points, `volume_ratio_*` is volume relative to its recent norm (about 1 is normal). " +
	"A field that is absent from `market` could not be computed this cycle: treat it as unknown, never as zero. " +
	"`market.news_context`, when present, is auxiliary context and never outweighs price, volume and order-book evidence. " +
	"`similar_past_cases.cases` lists past states similar to this one (smaller `distance` = more similar). " +
	"A case carries what Jev said back then (`direction`, `confidence`, `regime`) and, once that call has been graded, its realized outcome: " +
	"`future_return` (percent, 1.0 = +1%, unlike the fractions in `market`) over `horizon_minutes` and `was_direction_correct`. " +
	"A case without `future_return` has no realized outcome yet: its `direction` and `confidence` are only what Jev said back then, not what happened. " +
	"Treat realized outcomes as weak, small-sample context, never as a forecast and never above the current evidence in `market`. " +
	"When `cases` is absent or empty there is no history."

// withStateGuide returns question followed by stateGuide.
func withStateGuide(question string) string { return question + "\n\n" + stateGuide }

var momentumQualityOptions = []systemone.Option{
	{Name: MomentumQualityWeak, Rubric: "No usable directional momentum: choppy or contradictory returns across `return_1m` to `return_15m`, price hugging VWAP, volume at or below normal."},
	{Name: MomentumQualityModerate, Rubric: "Some directional drift that is only partly confirmed: returns agree on direction but are small, or volume, VWAP or order flow do not clearly confirm it."},
	{Name: MomentumQualityStrong, Rubric: "Consistent direction across 1m to 15m returns, price clearly on one side of a sloping VWAP, and above-normal volume (`volume_ratio_5m` well above 1)."},
	{Name: MomentumQualityExceptional, Rubric: "Rare, decisive move: strong on every dimension above, plus clear order-flow dominance and price at or near its 5-minute or session extreme on heavy volume, leading both its sector and the market."},
}

// scoutQuestions is the FR-SCOUT-1 question group (functional.md §4.4).
var scoutQuestions = map[string]systemone.Question{
	qInterestingNow: systemone.NoulQuestion(
		withStateGuide("Using `market`, is this stock showing a tradable intraday momentum move right now that a momentum day-trader should look at more closely? "+
			"Look for a decisive move over the last 1 to 15 minutes (`return_1m`, `return_3m`, `return_5m`, `return_15m`) that is confirmed by VWAP behaviour "+
			"(`price_vs_vwap_bps`, `vwap_slope`), by participation (`volume_ratio_1m`, `volume_ratio_5m`, `turnover_5m`) and, when available, by strength relative to its sector and the market "+
			"(`stock_vs_sector_relative_strength`, `market_return_5m`). A stock that sits still, merely drifts with the market, or has already faded back to VWAP is not interesting."),
		"A clear, still-developing directional move (up or down) with confirming volume and VWAP behaviour; worth evaluating for an entry now.",
		"No meaningful move, a move without volume confirmation, a move that is already exhausted or reversed, or too little data to tell.",
	),
	qMomentumQuality: systemone.ChoiceQuestion(
		withStateGuide("Rate how clean and sustainable the current intraday momentum in `market` is, whether it points up or down. "+
			"Consider the consistency of `return_1m` to `return_15m`, the position relative to VWAP (`price_vs_vwap_bps`, `vwap_slope`), volume expansion (`volume_ratio_5m`), "+
			"order-flow support (`trade_flow_imbalance`, `orderbook_imbalance`) and proximity to the 5-minute and session extremes "+
			"(`high_distance_5m`, `low_distance_5m`, `session_high_distance`, `session_low_distance`). If there is no momentum at all, answer weak."),
		momentumQualityOptions...,
	),
	qLiquidityOk: systemone.NoulQuestion(
		withStateGuide("Can a retail-sized intraday position (a few hundred thousand to a few million yen) in this stock be entered and exited close to the quoted price without significant slippage? "+
			"Judge by `spread_bps` relative to the price level, recent turnover (`turnover_1m`, `turnover_5m`, `volume_5m`) and displayed depth (`bid_depth`, `ask_depth`, `best_bid`, `best_ask`). "+
			"When order-book fields are absent, judge on the remaining evidence and lean towards no if nothing demonstrates liquidity."),
		"Spread is tight for the price level, turnover is steady over the last minutes and displayed depth is large relative to a retail-sized order.",
		"Wide spread, thin or sporadic turnover, a thin book, or no evidence of liquidity at all.",
	),
	qAbnormalActivity: systemone.NoulQuestion(
		withStateGuide("Is trading activity in `market` abnormal for this stock right now: a surge in volume, volatility or order flow that is unusual rather than routine? "+
			"Consider `volume_ratio_1m`, `volume_ratio_5m`, `volatility_expansion_ratio`, `realized_vol_5m` against `realized_vol_15m`, `atr_1m`, `trade_flow_imbalance`, "+
			"and `market.news_context` as a possible explanation. Activity that only mirrors a market-wide move (`market_return_5m`, `market_breadth`) is less abnormal for this stock."),
		"A clear, unusual surge in volume, volatility or order flow that is specific to this stock.",
		"Activity in line with the stock's normal level, or merely mirroring the overall market.",
	),
}
