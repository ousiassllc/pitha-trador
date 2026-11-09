// Package replaylayer is the replay layer (table 2, population iii) of the
// offline 2x2 comparison of the meta-labeling model (M axis) and the
// entry_quality cut-off (Q axis), issue #1046. It replays the four cells of
// metaablation.Cells() through the existing backtest engine (portfolio replay:
// simrisk Risk Engine, fillmodel costs, every FR-EXIT-1 exit) over ONE window
// [From, To), with the same decisions, the same snapshots, the same Risk
// configuration, the same initial capital and the same cost, exit and
// flow-guard values. The cells differ ONLY in their axes:
//
//   - Q on: entry_quality >= metaablation.QualityCut ("good", fixed). Q off:
//     the entry_quality cut is not applied (continuation, toxic_flow and
//     liquidity_stressed are). VWAP reversion candidates are never subject to
//     entry_quality, so Q cannot change them; they are reported separately
//     (Counts, Stages, daily streams) and the Q comparisons are based on the
//     momentum stream.
//   - M on: the given, already stored and re-scored model gates the entry
//     through metalabel.Decide with the pre-registered minimum EV. The model is
//     never trained or calibrated here (this package imports no training code;
//     a test pins that). M off: the M stage is skipped; the candidate is NOT
//     ordered unconditionally - guards, Risk, capital and exits still apply.
//
// The old confidence threshold (policy.*.min_probability) is used by no cell:
// it is 0 in all of them. M off is expressed with the engine's existing public
// non-meta rule and that threshold at 0 (see EntryMOff), Q off with the lowest
// entry_quality floor (QualityOffFloor); no policy code is changed.
//
// # Continuous state
//
// The main evaluation replays each cell from the same initial capital
// continuously through time. Capital, loss streaks and cooldowns are carried
// from day to day; only the daily resets the engine itself performs (simrisk:
// the day's realized P&L restarts and a latched Kill Switch is lifted on a new
// trading day) happen. Daily P&L (yen and as a ratio of the initial capital),
// the daily equity curve and its maximum drawdown come from that one path, and
// a comparison is on DAILY differences (B - A) of those series - never on
// decision-ID differences, because the set of entries is path dependent.
//
// # Missing market data
//
// Input.Unavailable lists the periods where market_snapshots are missing. They
// are expanded to whole JST days, reported with their reasons, and excluded
// from all four cells identically. Whether the engine state could be carried
// over such a gap is unknowable, so every gap splits the window into COMMON
// continuous segments: the same boundaries for all cells, each starting from
// the same pre-fixed initial conditions (initial capital, no open position, no
// streak). Results, equity curves and drawdowns are reported per segment and
// are never stitched into one curve. The optional reference experiment
// (Input.ReferenceDailyReset) replays every day independently from the initial
// state; it is labelled as reference, carries no equity curve or drawdown, and
// is kept apart from the continuous results.
//
// # Independence
//
// This package does not decide which decisions are independent evaluation
// data (after the model's train, calibration and original OOS ends, LabelEnd
// and embargo). The caller passes ONLY independent decisions in
// Input.Decisions, and a window and snapshots restricted accordingly.
//
// # Output
//
// Result holds plain structs: aggregated counts, yen and bps, no prices, order
// books, state_json or response_json. Counts of the stages follow the live
// order eligibility -> Q -> flow guards -> M -> Risk -> fill, each decision
// counted at the FIRST stage that rejects it.
package replaylayer
