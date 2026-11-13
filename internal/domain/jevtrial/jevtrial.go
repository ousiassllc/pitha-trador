// Package jevtrial holds the domain types of the model-comparison trial (issue
// #1059): one jev_trial_decisions row, the answer of a non-live decision model
// (an "arm") to the same input as a live Jev decision. It is a subpackage of
// domain, like jevshadow, and like the shadow's answers a trial row is never an
// input of trading, the RAG index, Calibration or the meta-model.
package jevtrial

import "time"

// Statuses of a trial row (jev_trial_decisions.status).
const (
	// StatusOK: every question was answered.
	StatusOK = "ok"
	// StatusFailed: no usable response (transport error, HTTP error, timeout,
	// an invalid answer set); FailureCode says which.
	StatusFailed = "failed"
	// StatusRefused: the provider refused every question.
	StatusRefused = "refused"
	// StatusPartial: the provider refused some questions and answered the others.
	StatusPartial = "partial"
)

// Cost sources of a trial row (jev_trial_decisions.cost_source).
const (
	// CostReported: CostUSD is the response's usage.cost.
	CostReported = "reported"
	// CostListPriceEstimate: CostUSD is the list price (per million input
	// tokens) times the input tokens; the response reported no cost.
	CostListPriceEstimate = "list_price_estimate"
	// CostUnknown: neither a reported cost nor a list price. CostUSD is nil
	// (never 0 USD) and CountedCostUSD is a worst-case figure.
	CostUnknown = "unknown"
)

// CutoffUnverified is the llm_cutoff_status of every row until a model's
// training-data cutoff is published: no date is guessed.
const CutoffUnverified = "unverified"

// Failure codes (jev_trial_decisions.failure_code, StatusFailed rows only).
const (
	FailureHTTP402         = "http_402"
	FailureHTTP403         = "http_403"
	FailureHTTP429         = "http_429"
	FailureHTTP529         = "http_529"
	FailureHTTP413         = "http_413"
	FailureHTTP4xx         = "http_4xx"
	FailureHTTP5xx         = "http_5xx"
	FailureTimeout         = "timeout"
	FailureTransport       = "transport"
	FailureInvalidResponse = "invalid_response"
)

// MaxErrorExcerptBytes bounds Decision.ErrorExcerpt: a trial row never holds an
// error body in full.
const MaxErrorExcerptBytes = 200

// Decision mirrors one jev_trial_decisions row. Pointer fields are NULL when
// the model refused or did not report that value; they are never filled with 0
// or NONE.
type Decision struct {
	ID int64
	// JevDecisionID is the live decision (decision_type=trader) it was asked
	// alongside; ArmID is the arm (jev.trial.arms id).
	JevDecisionID int64
	ArmID         string
	PromptVersion string
	// Provider is the arm's provider ("openrouter"); RequestedModel the model
	// id sent; ServedModel, ServedProvider and ResponseID are what the
	// response reported ("" without a response).
	Provider       string
	RequestedModel string
	ServedModel    string
	ServedProvider string
	ResponseID     string

	Status       string
	FailureCode  string
	HTTPStatus   *int
	ErrorExcerpt string

	Direction               *string
	Regime                  *string
	EntryQuality            *string
	Confidence              *float64
	ToxicFlow               *float64
	LiquidityStressed       *float64
	ContinuationProbability *float64
	// ResponseJSON is the response body as received (nil without a response).
	ResponseJSON *string

	LatencyMs    int
	InputTokens  *int
	OutputTokens *int
	// CostUSD is the reported or list-price-estimated cost, nil for unknown.
	CostUSD    *float64
	CostSource string
	// CountedCostUSD is what the daily caps counted for this call.
	CountedCostUSD float64

	RequestBytes        int
	StateBytes          int
	TruncationSuspected bool
	LLMCutoffStatus     string
	// RAGIncludesLiveModelAnswers is true when the RAG context sent with the
	// request carried the live Jev's past answers (the trial is then not a
	// fully independent condition).
	RAGIncludesLiveModelAnswers bool
	CreatedAt                   time.Time
}
