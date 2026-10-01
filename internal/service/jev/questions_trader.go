package jev

import (
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/systemone"
)

// Question ids of the FR-TRADER-1 group. They double as the JSON keys of
// TraderResponse and of the answers map. See questions.go for the
// wording/versioning rules.
const (
	qDirection               = "direction"
	qRegime                  = "regime"
	qEntryQuality            = "entry_quality"
	qToxicFlow               = "toxic_flow"
	qLiquidityStressed       = "liquidity_stressed"
	qContinuationProbability = "continuation_probability"
)

var directionOptions = []systemone.Option{
	{Name: domain.JevDirectionLong, Rubric: "Evidence favors the price continuing upward: above a rising VWAP, positive returns across timeframes, buying flow or bid support, and the move is not yet exhausted."},
	{Name: domain.JevDirectionShort, Rubric: "Evidence favors the price continuing downward: below a falling VWAP, negative returns across timeframes, selling flow or ask pressure, and the move is not yet exhausted."},
	{Name: domain.JevDirectionNone, Rubric: "No clear edge: conflicting signals, no momentum, a move that already looks exhausted, or thin or missing data. Choose this whenever in doubt."},
}

var regimeOptions = []systemone.Option{
	{Name: domain.JevRegimeTrend, Rubric: "Sustained one-way movement: returns agree across 1m to 30m, price holds one side of a sloping VWAP, pullbacks are shallow and orderly."},
	{Name: domain.JevRegimeRange, Rubric: "Price oscillates around VWAP without net direction: small mixed returns, flat `vwap_slope`, no volume expansion."},
	{Name: domain.JevRegimeBreakout, Rubric: "Price is leaving a prior range or setting a fresh 5-minute or session high or low (`high_distance_5m`, `low_distance_5m`, `session_high_distance`, `session_low_distance` near 0, or a new VWAP cross) with volume and volatility expansion."},
	{Name: domain.JevRegimeChaotic, Rubric: "Erratic, direction-less swings: volatility expansion with conflicting returns, an unstable book or a wide, fluctuating spread; no regime is reliable."},
}

var entryQualityOptions = []systemone.Option{
	{Name: domain.JevEntryQualityPoor, Rubric: "Chasing an extended or exhausted move, entering against flow or VWAP, a wide spread, or no momentum to follow."},
	{Name: domain.JevEntryQualityFair, Rubric: "Marginal: some support, but price is stretched from VWAP, volume confirmation is weak or the spread cost is noticeable; reward is not clearly above risk."},
	{Name: domain.JevEntryQualityGood, Rubric: "Reasonable timing with VWAP and volume confirmation, a modest stretch from VWAP and an acceptable spread."},
	{Name: domain.JevEntryQualityStrong, Rubric: "Early or well-timed (for example a break and retest on volume) with solid order-flow support, a small stretch from VWAP and a tight spread."},
	{Name: domain.JevEntryQualityExceptional, Rubric: "Rare textbook entry: just triggered on heavy volume with aligned flow and relative strength, close to VWAP so a stop is tight, a tight spread and no conflicting signal."},
}

// traderQuestions is the FR-TRADER-1 question group (functional.md §4.5).
var traderQuestions = map[string]systemone.Question{
	qDirection: systemone.ChoiceQuestion(
		withStateGuide("Decide which direction a disciplined intraday momentum trader should take in `market` over the next several minutes (a short intraday horizon, minutes rather than hours), or NONE if there is no clear edge. "+
			"Weigh price against VWAP (`price_vs_vwap_bps`, `vwap_slope`, `vwap_cross_direction`), returns across timeframes (`return_1m` to `return_30m`), order flow (`trade_flow_imbalance`, `orderbook_imbalance`, `microprice` against `price`), "+
			"relative strength (`stock_vs_sector_relative_strength`, `market_breadth`) and distance from recent highs and lows. "+
			"Not trading is the default: prefer NONE when signals conflict, data is missing or the move looks exhausted."),
		directionOptions...,
	),
	qRegime: systemone.ChoiceQuestion(
		withStateGuide("Classify the current intraday price regime of `market`, using returns across timeframes, `price_vs_vwap_bps`, `vwap_slope`, the distances from 5-minute and session highs and lows, "+
			"`volume_ratio_5m`, `volatility_expansion_ratio` and the stability of `spread_bps`."),
		regimeOptions...,
	),
	qEntryQuality: systemone.ChoiceQuestion(
		withStateGuide("Rate the quality of entering a position with the current momentum of `market` right now: the entry point itself (timing, stretch and risk/reward), not just how strong the momentum is. "+
			"Consider how far price is extended from VWAP (`price_vs_vwap_bps`) and from the 5-minute extremes, whether the move is just starting or already stretched (`return_5m` against `return_15m` and `return_30m`), "+
			"the spread cost (`spread_bps`), volume confirmation (`volume_ratio_5m`) and order-flow support."),
		entryQualityOptions...,
	),
	qToxicFlow: systemone.NoulQuestion(
		withStateGuide("Does the order flow in `market` look toxic for a trader who follows the current move: one-sided, aggressive or possibly informed trading, or a spike that the order book does not support and that tends to reverse or gap against followers? "+
			"Consider `trade_flow_imbalance`, `buy_trade_ratio`, `sell_trade_ratio`, `orderbook_imbalance`, `microprice` against `price`, `spread_bps`, `volatility_expansion_ratio` and `market.news_context`."),
		"Flow is highly one-sided or erratic so that following the move risks adverse selection: price spikes without book support, imbalance flipping, a widening spread or a news-driven gap.",
		"Balanced, orderly flow; trades and book are consistent with the price move and the spread is stable.",
	),
	qLiquidityStressed: systemone.NoulQuestion(
		withStateGuide("Is liquidity in `market` stressed right now, such that exiting a position quickly would be costly or risky? "+
			"Consider `spread_bps` (wide or widening), `bid_depth` and `ask_depth` (thin or lopsided), `best_bid` and `best_ask` against `price`, and drying-up `turnover_1m` and `volume_1m`."),
		"Wide or unstable spread, a thin or one-sided book, or turnover drying up; an exit could slip noticeably.",
		"Tight, stable spread, a deep two-sided book and steady turnover.",
	),
	qContinuationProbability: systemone.NoulQuestion(
		withStateGuide("Will the current short-term price move in `market` continue in the same direction over the next several minutes rather than stall or reverse? "+
			"The direction is that of the most recent net move (the sign of `return_5m`, as confirmed by `price_vs_vwap_bps`). If there is no discernible move, answer no. "+
			"Consider volume (`volume_ratio_1m`, `volume_ratio_5m`), VWAP behaviour (`vwap_slope`, `vwap_cross_direction`), order flow and how stretched the move already is (`return_15m`, `return_30m`, distance from highs and lows)."),
		"The move is likely to extend: confirmed by volume, VWAP and order flow, and not yet exhausted.",
		"The move is likely to stall or reverse: exhausted, volume fading, price snapping back towards VWAP, flow turning against it, or there is no move at all.",
	),
}
