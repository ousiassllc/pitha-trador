package domain

import "time"

// Position side values (docs/architecture/er.md §positions CHECK
// constraint).
const (
	PositionSideLong  = "LONG"
	PositionSideShort = "SHORT"
)

// Position exit reason values (functional.md FR-EXIT-1's eight exit
// conditions, plus manual close and Kill Switch forced close). Execution
// (internal/service/execution) sets Position.ExitReason to one of these
// when it closes a position.
const (
	ExitReasonStopLoss             = "stop_loss"
	ExitReasonTakeProfit           = "take_profit"
	ExitReasonTrailingStop         = "trailing_stop"
	ExitReasonJevDirectionReversed = "jev_direction_reversed"
	ExitReasonContinuationProbDrop = "continuation_probability_dropped"
	ExitReasonVWAPCross            = "vwap_cross"
	ExitReasonMaxHolding           = "max_holding"
	ExitReasonForceFlatBeforeClose = "force_flat_before_close"
	ExitReasonManual               = "manual"
	// ExitReasonForceClose is FR-RISK-3's "保有ポジション強制クローズ指
	// 示" (architecture/overview.md §10.3, er.md §positions.exit_reason's
	// documented "force_close" example): internal/service/risk.Engine's
	// PositionCloser calls this when a Kill Switch reason requiring it
	// fires (closer.go's forceCloseReasons) - the specific reason is
	// already recorded on the driving kill_switch_events row, so
	// positions.exit_reason only needs to say "closed by Kill Switch",
	// not repeat which one.
	ExitReasonForceClose = "force_close"
)

// Position mirrors one positions row: a held (or closed) Paper/Live
// position, Entry/Exit order-linked (docs/architecture/er.md §positions).
// At most one open (ClosedAt == nil) Position exists per instrument at a
// time (positions_open_instrument_uq, functional.md §4.9's per-symbol
// "position" state).
type Position struct {
	ID           int64
	InstrumentID int64
	EntryOrderID int64
	ExitOrderID  *int64
	Symbol       string
	// Side is PositionSideLong or PositionSideShort.
	Side          string
	Quantity      int64
	EntryPrice    float64
	CurrentPrice  float64
	UnrealizedPnL float64
	RealizedPnL   *float64
	OpenedAt      time.Time
	ClosedAt      *time.Time
	// ExitReason is one of the ExitReason* constants, set only once
	// ClosedAt is non-nil.
	ExitReason *string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// IsOpen reports whether this position has not been closed yet.
func (p Position) IsOpen() bool { return p.ClosedAt == nil }
