// Package jevshadow holds the domain types of the Jev shadow comparison (issue
// #872): a candidate prompt's answer (Decision) and the audit of a promotion
// (Promotion). It is a subpackage of domain, like orderproposal.
package jevshadow

import "time"

// Decision mirrors one jev_shadow_decisions row (issue #872): what a
// candidate Trader prompt version answered for the same input as a live
// jev_decisions row. It is never an input of trading: Policy, the meta-model,
// Risk, order placement, the RAG index, Calibration and the meta-model
// training read jev_decisions only.
type Decision struct {
	ID int64
	// JevDecisionID is the live decision (decision_type=trader) it shadows.
	JevDecisionID int64
	// PromptVersion is the candidate version that produced this answer.
	PromptVersion string
	ModelID       string
	// ResponseJSON is the candidate's response as the Trader recorded it.
	ResponseJSON string

	Direction               string
	Regime                  string
	EntryQuality            string
	Confidence              float64
	ToxicFlow               float64
	LiquidityStressed       float64
	ContinuationProbability float64

	LatencyMs int
	// InputTokens is the usage.input_tokens the API reported.
	InputTokens int
	// RequestCost is the estimated cost in USD (input tokens x the published
	// per-token price).
	RequestCost float64
	CreatedAt   time.Time
}

// Promotion actors (jev_prompt_promotions.actor).
const (
	ActorMCP = "mcp"
	ActorGUI = "gui"
)

// Promotion mirrors one jev_prompt_promotions row: the audit of a switch of
// the live Trader prompt version. Only an explicit GUI or MCP operation writes
// one; the self-improvement loop (Sol/Opus) cannot.
type Promotion struct {
	ID          int64
	At          time.Time
	Actor       string
	Client      string
	FromVersion string
	ToVersion   string
}
