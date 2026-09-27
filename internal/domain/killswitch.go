package domain

import "time"

// Kill Switch trigger reasons (docs/requirements/functional.md FR-RISK-2,
// FR-RISK-6; docs/architecture/er.md §kill_switch_events.reason CHECK
// constraint). These nine values are the only ones the database schema
// accepts. A manually-triggered Kill Switch (FR-RISK-4's
// `POST /system/kill`) does not fit this automatic-detection list and is
// tracked separately (internal/service/risk.Engine's `system.killed`
// runtime_settings flag), not as a kill_switch_events row.
const (
	KillReasonDailyLossLimit           = "daily_loss_limit"
	KillReasonConsecutiveLosses        = "consecutive_losses"
	KillReasonMarketDataDown           = "market_data_down"
	KillReasonJevAPIDown               = "jev_api_down"
	KillReasonBrokerAPIError           = "broker_api_error"
	KillReasonUnexpectedPosition       = "unexpected_position"
	KillReasonFillDiscrepancy          = "fill_discrepancy"
	KillReasonDBWriteFailure           = "db_write_failure"
	KillReasonOperatorHeartbeatTimeout = "operator_heartbeat_timeout"
)

// resolved_by values (er.md §kill_switch_events).
const (
	ResolvedByAuto   = "auto"
	ResolvedByManual = "manual"
)

// KillSwitchEvent mirrors one kill_switch_events row: a Risk Engine Kill
// Switch activation and (once resolved) its resolution
// (docs/requirements/functional.md FR-RISK-2〜3, FR-RISK-5〜7).
type KillSwitchEvent struct {
	ID          int64
	TriggeredAt time.Time
	Reason      string
	DetailJSON  string

	// ResolvedAt/ResolvedBy are both nil while the Kill Switch remains
	// active for this event's Reason.
	ResolvedAt *time.Time
	ResolvedBy *string

	CreatedAt time.Time
}

// SystemState is the overall Running/Paused/Killed state
// docs/api/endpoints.md §4's action routes report and transition
// (functional.md FR-RISK-4).
type SystemState string

const (
	SystemStateRunning SystemState = "running"
	SystemStatePaused  SystemState = "paused"
	SystemStateKilled  SystemState = "killed"
)
