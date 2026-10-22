package testkit

import (
	"database/sql"
	"testing"
	"time"
)

// ProposalRow is an order_proposals row to plant. A nil Stop, Take, MaxLoss,
// Applied, Estimate, EffectiveStop, SignalID, OrderID or PositionID is NULL;
// empty strings take the defaults noted.
type ProposalRow struct {
	ID         int64
	Symbol     string // default 7203
	Bucket     string // default stock
	Action     string // default open_long
	Status     string
	Reason     string
	ReceivedAt string // RFC3339 UTC
	ExpiresAt  string // default 23 hours after ReceivedAt
	Rationale  string // default a plain sentence
	ExitPlan   string // default 引けで手じまう
	// SourcesJSON is sources_json; default one https source.
	SourcesJSON string
	Quantity    any
	Stop        any
	Take        any
	MaxLoss     any
	// Linked records and the application's own numbers (#828).
	Applied       any
	Estimate      any
	EffectiveStop any
	SignalID      any
	OrderID       any
	PositionID    any
	CancelRequest any // RFC3339 UTC
}

// Proposal inserts p and returns its id. open_long rows get the exit plan and
// a source; the idempotency key is derived from the id.
func Proposal(t *testing.T, w *sql.DB, p ProposalRow) int64 {
	t.Helper()
	def := func(s *string, v string) {
		if *s == "" {
			*s = v
		}
	}
	def(&p.Symbol, "7203")
	def(&p.Bucket, "stock")
	def(&p.Action, "open_long")
	def(&p.Rationale, "決算が好調で需給も良い")
	def(&p.ExitPlan, "引けで手じまう")
	def(&p.SourcesJSON, `[{"title":"決算短信","url":"https://example.com/ir","retrieved_at":"2026-10-17T00:00:00Z"}]`)
	received, err := time.Parse(time.RFC3339, p.ReceivedAt)
	if err != nil {
		t.Fatal(err)
	}
	def(&p.ExpiresAt, received.Add(23*time.Hour).Format(time.RFC3339))
	var cancelAt any
	if s, ok := p.CancelRequest.(string); ok {
		cancelAt = Stored(t, s)
	}
	MustExec(t, w, `INSERT INTO order_proposals (id, idempotency_key, request_hash, received_at, symbol, bucket, action,
		quantity, order_type, stop_loss_price, take_profit_price, exit_plan, max_loss_jpy, rationale, sources_json, expires_at,
		status, status_reason, instrument_id, trade_signal_id, paper_order_id, position_id, updated_at, cancel_requested_at,
		applied_quantity, computed_max_loss_jpy, effective_stop_loss_price)
		VALUES (?, 'key-' || ?, 'hash', ?, ?, ?, ?, ?, 'MARKET', ?, ?, ?, ?, ?, ?, ?,
		?, ?, 1, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.ID, Stored(t, p.ReceivedAt), p.Symbol, p.Bucket, p.Action, p.Quantity, p.Stop, p.Take, p.ExitPlan, p.MaxLoss,
		p.Rationale, p.SourcesJSON, Stored(t, p.ExpiresAt), p.Status, p.Reason, p.SignalID, p.OrderID, p.PositionID, Stored(t, p.ReceivedAt), cancelAt,
		p.Applied, p.Estimate, p.EffectiveStop)
	return p.ID
}

// ProposalEvent appends an order_proposal_events row. from "" is NULL (the
// first event); detail "" is no detail.
func ProposalEvent(t *testing.T, w *sql.DB, proposalID int64, at, from, to, reason, actor, detail string) {
	t.Helper()
	var fromStatus any
	if from != "" {
		fromStatus = from
	}
	MustExec(t, w, `INSERT INTO order_proposal_events (proposal_id, at, from_status, to_status, reason_code, actor, detail_json)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, proposalID, Stored(t, at), fromStatus, to, reason, actor, detail)
}

// TradeSignal inserts a trade_signals row with the given policy_version and
// returns its id. rejectReason "" is NULL; riskPassed is what the Policy
// Engine or the proposal processor recorded.
func TradeSignal(t *testing.T, w *sql.DB, id int64, at, policyVersion string, riskPassed bool, rejectReason string) int64 {
	t.Helper()
	var reason any
	if rejectReason != "" {
		reason = rejectReason
	}
	MustExec(t, w, `INSERT INTO trade_signals (id, instrument_id, symbol, direction, timestamp, policy_version, risk_passed, reject_reason)
		VALUES (?, 1, '7203', 'LONG', ?, ?, ?, ?)`, id, Stored(t, at), policyVersion, riskPassed, reason)
	return id
}

// PaperOrder inserts a FILLED paper_orders row (BUY or SELL) linked to a
// trade signal (nil for none) and returns its id.
func PaperOrder(t *testing.T, w *sql.DB, id int64, side string, signalID any, qty int, price, fees float64, filledAt string) int64 {
	t.Helper()
	MustExec(t, w, `INSERT INTO paper_orders (id, instrument_id, trade_signal_id, symbol, side, order_type, quantity, status,
		submitted_at, filled_at, filled_price, fees) VALUES (?, 1, ?, '7203', ?, 'MARKET', ?, 'FILLED', ?, ?, ?, ?)`,
		id, signalID, side, qty, Stored(t, filledAt), Stored(t, filledAt), price, fees)
	return id
}

// ClosedPosition inserts a positions row entered by entryOrder and, when
// closedAt is not empty, closed by exitOrder (nil for none) with the stored
// realized pnl; it returns the id.
func ClosedPosition(t *testing.T, w *sql.DB, id int64, entryOrder, exitOrder any, qty int, entry float64, openedAt, closedAt string, pnl any, exitReason string) int64 {
	t.Helper()
	var closed, reason any
	if closedAt != "" {
		closed, reason = Stored(t, closedAt), exitReason
	}
	MustExec(t, w, `INSERT INTO positions (id, instrument_id, entry_order_id, exit_order_id, symbol, side, quantity, entry_price,
		current_price, unrealized_pnl, realized_pnl, opened_at, closed_at, exit_reason)
		VALUES (?, 1, ?, ?, '7203', 'LONG', ?, ?, 4242.5, 777.7, ?, ?, ?, ?)`,
		id, entryOrder, exitOrder, qty, entry, pnl, Stored(t, openedAt), closed, reason)
	return id
}

// Proposal IDs of SeedProposalOutcomes.
const (
	ProposalExpired   = 1 // expired, nothing linked
	ProposalCancelled = 2 // bond_etf, cancel requested by the MCP, then cancelled
	ProposalRejected  = 3 // Risk Engine rejected (bucket_budget_exceeded)
	ProposalWin       = 4 // open_long filled, closed at the take-profit
	ProposalLoss      = 5 // open_long filled, closed at the stop by ProposalClose
	ProposalClose     = 6 // action=close that closed ProposalLoss's position
)

// SeedProposalOutcomes plants one proposal of each way it can end (see the
// Proposal* constants) with the records the application linked to them, plus
// three closed positions of the other origins: a meta-model entry (position 3),
// a bootstrap entry (4) and one whose entry order has no signal (5). All
// timestamps are within the default period of FixedNow.
//
// Numbers: ProposalWin is 100 shares bought at 2500 (fee 10) and sold at 2710
// (fee 20), realized_pnl 20970, held 300 minutes; ProposalLoss is bought at
// 2500 and sold at 2380, realized_pnl -12030. Both proposed stop 2400, take 2700,
// max loss 20000; the application estimated 15000.
func SeedProposalOutcomes(t *testing.T, w *sql.DB) {
	t.Helper()
	const policy = "mcp-proposal-v1"
	Proposal(t, w, ProposalRow{ID: ProposalExpired, ReceivedAt: "2026-10-15T00:00:00Z", Status: "expired", Reason: "expired", Quantity: 100, Stop: 2400.0, MaxLoss: 20000.0})
	ProposalEvent(t, w, ProposalExpired, "2026-10-15T00:00:00Z", "", "received", "", "mcp", "")
	ProposalEvent(t, w, ProposalExpired, "2026-10-16T00:00:05Z", "received", "expired", "expired", "app", "")

	Proposal(t, w, ProposalRow{ID: ProposalCancelled, Symbol: "1326", Bucket: "bond_etf", ReceivedAt: "2026-10-16T00:00:00Z", Status: "cancelled",
		Reason: "cancel_requested", Quantity: 10, Stop: 2400.0, MaxLoss: 5000.0, CancelRequest: "2026-10-16T00:30:00Z"})
	ProposalEvent(t, w, ProposalCancelled, "2026-10-16T00:00:00Z", "", "received", "", "mcp", "")
	ProposalEvent(t, w, ProposalCancelled, "2026-10-16T00:30:00Z", "received", "received", "cancel_requested", "mcp", "")
	ProposalEvent(t, w, ProposalCancelled, "2026-10-16T00:30:05Z", "received", "cancelled", "cancel_requested", "app", "")

	TradeSignal(t, w, 1, "2026-10-17T00:00:04Z", policy, true, "")
	TradeSignal(t, w, 2, "2026-10-17T01:00:04Z", policy, true, "")
	TradeSignal(t, w, 3, "2026-10-16T01:00:04Z", policy, false, "risk_engine_rejected: bucket_budget_exceeded: used 1.2M of 1.0M")
	TradeSignal(t, w, 4, "2026-10-14T00:00:00Z", "policy-v2+sol-3", true, "")
	TradeSignal(t, w, 5, "2026-10-14T01:00:00Z", "policy-vb+sol-1", true, "")

	Proposal(t, w, ProposalRow{ID: ProposalRejected, ReceivedAt: "2026-10-16T01:00:00Z", Status: "rejected", Reason: "bucket_budget_exceeded",
		Quantity: 500, Stop: 2400.0, MaxLoss: 30000.0, SignalID: 3})
	ProposalEvent(t, w, ProposalRejected, "2026-10-16T01:00:00Z", "", "received", "", "mcp", "")
	ProposalEvent(t, w, ProposalRejected, "2026-10-16T01:00:03Z", "received", "processing", "", "app", "")
	ProposalEvent(t, w, ProposalRejected, "2026-10-16T01:00:04Z", "processing", "rejected", "bucket_budget_exceeded", "app", "")

	PaperOrder(t, w, 1, "BUY", 1, 100, 2500, 10, "2026-10-17T00:00:06Z")
	PaperOrder(t, w, 2, "SELL", nil, 100, 2710, 20, "2026-10-17T05:00:00Z")
	ClosedPosition(t, w, 1, 1, 2, 100, 2500, "2026-10-17T00:00:00Z", "2026-10-17T05:00:00Z", 20970.0, "take_profit")
	Proposal(t, w, ProposalRow{ID: ProposalWin, ReceivedAt: "2026-10-17T00:00:00Z", Status: "filled", Quantity: 200, Stop: 2400.0, Take: 2700.0,
		MaxLoss: 20000.0, Applied: 100, Estimate: 15000.0, EffectiveStop: 2400.0, SignalID: 1, OrderID: 1, PositionID: 1})
	ProposalEvent(t, w, ProposalWin, "2026-10-17T00:00:00Z", "", "received", "", "mcp", "")
	ProposalEvent(t, w, ProposalWin, "2026-10-17T00:00:03Z", "received", "processing", "", "app", "")
	ProposalEvent(t, w, ProposalWin, "2026-10-17T00:00:06Z", "processing", "filled", "quantity_reduced", "app",
		`{"requested_quantity":200,"applied_quantity":100}`)

	PaperOrder(t, w, 3, "BUY", 2, 100, 2500, 10, "2026-10-17T01:00:06Z")
	PaperOrder(t, w, 4, "SELL", nil, 100, 2380, 20, "2026-10-17T06:00:03Z")
	ClosedPosition(t, w, 2, 3, 4, 100, 2500, "2026-10-17T01:00:00Z", "2026-10-17T06:00:03Z", -12030.0, "proposal_close")
	Proposal(t, w, ProposalRow{ID: ProposalLoss, ReceivedAt: "2026-10-17T01:00:00Z", Status: "filled", Quantity: 100, Stop: 2400.0, Take: 2700.0,
		MaxLoss: 20000.0, Applied: 100, Estimate: 15000.0, EffectiveStop: 2400.0, SignalID: 2, OrderID: 3, PositionID: 2})
	ProposalEvent(t, w, ProposalLoss, "2026-10-17T01:00:00Z", "", "received", "", "mcp", "")
	ProposalEvent(t, w, ProposalLoss, "2026-10-17T01:00:06Z", "processing", "filled", "", "app", "")
	Proposal(t, w, ProposalRow{ID: ProposalClose, Action: "close", ReceivedAt: "2026-10-17T06:00:00Z", Status: "closed", PositionID: 2, OrderID: 4})
	ProposalEvent(t, w, ProposalClose, "2026-10-17T06:00:00Z", "", "received", "", "mcp", "")
	ProposalEvent(t, w, ProposalClose, "2026-10-17T06:00:03Z", "processing", "closed", "", "app", "")

	PaperOrder(t, w, 5, "BUY", 4, 100, 3000, 10, "2026-10-14T00:00:06Z")
	PaperOrder(t, w, 6, "BUY", 5, 100, 3000, 10, "2026-10-14T01:00:06Z")
	PaperOrder(t, w, 7, "BUY", nil, 100, 3000, 10, "2026-10-14T02:00:06Z")
	ClosedPosition(t, w, 3, 5, nil, 100, 3000, "2026-10-14T00:00:00Z", "2026-10-14T05:00:00Z", 500.0, "take_profit")
	ClosedPosition(t, w, 4, 6, nil, 100, 3000, "2026-10-14T01:00:00Z", "2026-10-14T05:00:00Z", -300.0, "stop_loss")
	ClosedPosition(t, w, 5, 7, nil, 100, 3000, "2026-10-14T02:00:00Z", "2026-10-14T05:00:00Z", 100.0, "time_exit")
}

// Proposals SeedAfterRisk adds (issue #860): proposals whose signal passed Risk
// and ended another way, and one refused by the Risk Engine.
const (
	ProposalRejectedAfterRisk  = 101
	ProposalRejectedByRisk     = 102
	ProposalExpiredAfterRisk   = 103
	ProposalCancelledAfterRisk = 104
	ProposalWaitingAfterRisk   = 105
)

// SeedAfterRiskRejection adds, above SeedProposalOutcomes' ids: a rejected
// proposal whose signal is risk_passed = 0 with
// 'proposal_rejected_after_risk: size_zero', one whose signal is a Risk Engine
// rejection, and an expired, a cancelled and a waiting_session proposal whose
// signal passed Risk and did not become an order.
func SeedAfterRiskRejection(t *testing.T, w *sql.DB) {
	t.Helper()
	const policy = "mcp-proposal-v1"
	for _, c := range []struct {
		id                int64
		status, reason, r string
	}{
		{101, "rejected", "size_zero", "proposal_rejected_after_risk: size_zero"},
		{102, "rejected", "kill_switch_active", "risk_engine_rejected: kill_switch_active"},
		{103, "expired", "expired", "proposal_rejected_after_risk: expired"},
		{104, "cancelled", "cancel_requested", "proposal_rejected_after_risk: cancel_requested"},
		{105, "waiting_session", "outside_trading_session", "proposal_rejected_after_risk: outside_trading_session"},
	} {
		TradeSignal(t, w, c.id, "2026-10-18T00:00:04Z", policy, false, c.r)
		Proposal(t, w, ProposalRow{ID: c.id, ReceivedAt: "2026-10-18T00:00:00Z", Status: c.status, Reason: c.reason, Quantity: 100, Stop: 2400.0, MaxLoss: 20000.0, SignalID: c.id})
	}
}
