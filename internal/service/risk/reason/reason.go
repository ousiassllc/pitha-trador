// Package reason holds the FR-RISK-1 rejection codes of the Risk Engine that
// no other leaf package of internal/service/risk owns (correlation, sizing and
// bucket own theirs). It imports nothing, so the backtest's simrisk can reject
// with the live codes without depending on package risk and, through it, on
// repository/* and database/sql (issue #842). package risk re-exports every
// code as risk.Reason*.
package reason

const (
	SystemPaused         = "system_paused"
	KillSwitchActive     = "kill_switch_active"
	CooldownAfterLoss    = "cooldown_after_loss"
	MaxOpenPositions     = "max_open_positions"
	MaxDailyLossPct      = "max_daily_loss_pct"
	MaxConsecutiveLosses = "max_consecutive_losses"
	MaxSpreadBps         = "max_spread_bps"
	// RiskEngineError is the fail-closed rejection when Check cannot read the
	// state a limit needs (DB error, missing snapshot/spread).
	RiskEngineError = "risk_engine_error"
	// SnapshotsUnavailable rejects a new entry when the Engine has no snapshot
	// source to price and size it. NewEngine refuses such a Config, so this
	// only guards an Engine that lost its source after construction (issue
	// #841); exits are never judged by Check and stay open.
	SnapshotsUnavailable = "snapshots_unavailable"
)
