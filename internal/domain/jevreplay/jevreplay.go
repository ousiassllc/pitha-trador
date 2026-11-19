// Package jevreplay holds the domain types of the replay evaluation (issue
// #1085): a run that sends the saved inputs of past live Jev trader decisions
// (jev_decisions.state_json) to the trial's arms afterwards, and one result
// row per (run, live decision, arm). A subpackage of domain like jevtrial, and
// like a trial row a replay result is research data only: it is never an input
// of trading, Policy, Risk, the RAG index, Calibration, the meta-model or any
// live/shadow/trial table.
package jevreplay

import (
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain/jevtrial"
)

// DecisionTypeTrader is the only decision type that can be replayed: the arms
// answer the FR-TRADER-1 question group about a saved trader state.
const DecisionTypeTrader = "trader"

// MaxRows bounds Spec.MaxRows: no run covers more live decisions than this.
const MaxRows = 1000

// Run statuses (jev_replay_runs.status).
const (
	// StatusPending: created, the runner has not started it.
	StatusPending = "pending"
	// StatusRunning: the runner is working on it (also while it waits for a
	// daily cap; Run.BlockedReason says why no call is made then).
	StatusRunning = "running"
	// StatusCompleted: every planned call has a result row.
	StatusCompleted = "completed"
	// StatusStopped: ended before completion (Run.StopReason says why).
	StatusStopped = "stopped"
	// StatusFailed: could not be started (Run.StopReason says why).
	StatusFailed = "failed"
)

// Stop reasons (jev_replay_runs.stop_reason, StatusStopped / StatusFailed).
const (
	// StopRequested: replay_stop (or the app) set the stop flag.
	StopRequested = "requested"
	// StopRunCostCap: the run's counted cost reached its estimate's worst case.
	StopRunCostCap = "run_cost_cap"
	// StopPaymentRequired: the API answered HTTP 402 (the key is out of credit).
	StopPaymentRequired = "payment_required"
	// StopKeyNotSet: no OpenRouter key is configured; nothing was sent.
	StopKeyNotSet = "key_not_set"
	// StopNoArms: none of the run's arms is configured any more (or its
	// prompt version is not registered); nothing was sent.
	StopNoArms = "no_arms"
)

// Blocked reasons (jev_replay_runs.blocked_reason while StatusRunning): why no
// new call is made right now. The cap and stop reasons reuse jevtrial.Skip*.
const (
	// BlockSwitchOff: jev.replay.enabled is off (or unreadable).
	BlockSwitchOff = "switch_off"
)

// IsActive reports whether a run in status still has work to do.
func IsActive(status string) bool { return status == StatusPending || status == StatusRunning }

// Spec is what a run was asked to do. From and To are JST dates (YYYY-MM-DD;
// To exclusive) of the live decisions' timestamps; Arms are arm ids of
// jev.trial.arms.
type Spec struct {
	DecisionType string   `json:"decision_type"`
	From         string   `json:"from"`
	To           string   `json:"to"`
	MaxRows      int      `json:"max_rows"`
	Arms         []string `json:"arms"`
}

// Estimate is the pre-flight estimate a run was started with. MaxCostUSD is the
// reservation-style worst case over every planned call; the run never counts
// more than it. ListPriceUSD is the list-price figure (nil when an arm has no
// list price). Token names the estimate's content.
type Estimate struct {
	Decisions    int
	Calls        int
	MaxCostUSD   float64
	ListPriceUSD *float64
	Token        string
}

// Run mirrors one jev_replay_runs row. ArmsJSON is the arms as configured when
// the run was started (never keys).
type Run struct {
	ID            int64
	Status        string
	StopRequested bool
	StopReason    string
	BlockedReason string
	Spec          Spec
	PeriodFrom    time.Time
	PeriodTo      time.Time
	MaxRows       int
	// MaxDecisionID is the highest jev_decisions.id the run may replay.
	MaxDecisionID  int64
	ArmsJSON       string
	Estimate       Estimate
	CallsDone      int
	CallsFailed    int
	CountedCostUSD float64
	CreatedBy      string
	CreatedAt      time.Time
	StartedAt      *time.Time
	FinishedAt     *time.Time
	UpdatedAt      time.Time
}

// Cutoff statuses of a result (jev_replay_results.llm_cutoff_status).
const (
	// CutoffVerified: the arm names the model's published training cutoff and
	// only decisions after it were sent.
	CutoffVerified = "verified"
	// CutoffUnverified: no published cutoff (shown as 未確認); the result is
	// reference-only.
	CutoffUnverified = jevtrial.CutoffUnverified
)

// CutoffLabel is the label a screen or report shows for a cutoff status.
func CutoffLabel(status string) string {
	if status == CutoffVerified {
		return "確認済み"
	}
	return "未確認"
}

// Result mirrors one jev_replay_results row: the trial row's answer, cost and
// status columns (jevtrial.Decision, with LLMCutoffStatus one of the Cutoff*
// constants and RAGIncludesLiveModelAnswers always false) plus the run and the
// reference-only mark. A result with an unverified cutoff is always reference-only.
type Result struct {
	RunID int64
	jevtrial.Decision
	ReferenceOnly bool
}
