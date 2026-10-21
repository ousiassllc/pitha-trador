// Package orderproposal is the vocabulary of the Paper results MCP's order
// proposals shared by the MCP (internal/service/paperresultsmcp) and the
// application that processes them (internal/bootstrap/proposalexec): the
// order_proposals / order_proposal_events values and the Proposal the
// processor reads (issues #827, #828). It lives beside domain, not in it, to
// keep that directory within linterly's size limit.
package orderproposal

import "time"

// PolicyVersion is trade_signals.policy_version of the signal the
// application writes for one open_long order proposal of the Paper results MCP
// (issues #827, #828). Such a signal has no Jev decision behind it
// (jev_decision_id is NULL). Readers that only see the table (the Paper
// results MCP's risk_rejections) tell proposal rejections from the Policy
// Engine's by this value.
const PolicyVersion = "mcp-proposal-v1"

// Values of order_proposals.action.
const (
	ActionOpenLong = "open_long"
	ActionClose    = "close"
)

// Values of order_proposals.status (and of order_proposal_events.from_status /
// to_status). The MCP writes only StatusReceived; the rest are the
// application's (internal/bootstrap/proposalexec).
const (
	StatusReceived       = "received"
	StatusProcessing     = "processing"
	StatusWaitingSession = "waiting_session"
	StatusRejected       = "rejected"
	StatusSubmitted      = "submitted"
	StatusFilled         = "filled"
	StatusExpired        = "expired"
	StatusCancelled      = "cancelled"
	StatusClosed         = "closed"
	StatusFailed         = "failed"
)

// Statuses lists every status in the order the read tools report them: the
// course of a proposal, then how it ended.
func Statuses() []string {
	return []string{
		StatusReceived, StatusProcessing, StatusWaitingSession, StatusSubmitted, StatusFilled,
		StatusClosed, StatusRejected, StatusExpired, StatusCancelled, StatusFailed,
	}
}

// Values of order_proposal_events.actor.
const (
	ActorMCP = "mcp"
	ActorApp = "app"
	ActorGUI = "gui"
)

// Proposal mirrors the order_proposals columns the application's
// processor reads (internal/bootstrap/proposalexec). The proposal's content
// (rationale, sources, exit plan) is not carried: the processor never needs
// it, and order_proposal_get reads it straight from the table.
type Proposal struct {
	ID         int64
	Symbol     string
	Bucket     string
	Action     string
	Quantity   *int64
	BudgetJPY  *float64
	OrderType  string
	LimitPrice *float64
	// StopLossPrice and MaxLossJPY are the proposal's own (open_long only).
	StopLossPrice *float64
	MaxLossJPY    *float64
	ReceivedAt    time.Time
	ExpiresAt     time.Time
	Status        string
	StatusReason  string

	// Links the application filled in while processing; nil until then.
	InstrumentID  *int64
	TradeSignalID *int64
	PaperOrderID  *int64
	PositionID    *int64

	// CancelRequestedAt is when order_proposal_cancel asked for a
	// cancellation; nil when nobody did.
	CancelRequestedAt *time.Time
}

// CancelRequested reports whether a cancellation was asked for.
func (p Proposal) CancelRequested() bool { return p.CancelRequestedAt != nil }
