// Package policy is the Policy Engine (docs/architecture/overview.md §4,
// functional.md §4.6): it turns one Jev Trader decision into a LONG/SHORT/
// NONE trade_signals row (engine.go, FR-POLICY-1〜3), reading its
// LONG/SHORT thresholds from config.PolicyConfig (FR-POLICY-4), and
// persists every decision - not only the ones that pass - via
// trading.SignalRepository (FR-POLICY-5).
//
// The live entry rule (Engine.Evaluate; meta.go, FR-POLICY-6, issue #765) does
// not threshold Jev's confidence: Jev's direction is the primary signal and the
// meta-labeling model (internal/service/metalabel) enters it only when its
// calibrated expected value is above zero. No model means NONE. The backtest replays
// the same rule (DecideReplay, issue #775); Decide, the legacy
// confidence-threshold rule (FR-POLICY-1/2), remains only as the backtest's
// explicitly named comparison mode.
//
// Risk Engine (functional.md §4.7, internal/service/risk.Engine) now
// exists and satisfies RiskChecker; AlwaysPassRiskChecker remains this
// package's zero-value default until a later sub-scope's cmd/ wiring
// constructs a real risk.Engine and passes it to NewEngine.
//
// handler.go connects Jev Trader and this Engine to the jev-trader queue
// (jobqueue.JobQueueJevTrader): Handler.HandleJob matches
// internal/service/scheduler.Handler's signature, so it can be
// registered directly once the Scheduler wiring itself is built (a later
// sub-scope, mirroring internal/service/jev.Scout.HandleJob's own
// connection point).
package policy
