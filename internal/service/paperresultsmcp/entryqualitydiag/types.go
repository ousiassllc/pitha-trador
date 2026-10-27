// Package entryqualitydiag is the read-only analysis behind the Paper results
// MCP's entry_quality_whatif tool (issue #911): what the Policy Engine's
// entry guards would do with the Jev trader decisions of a period if
// min_entry_quality were another value. It re-runs the live rule
// (policy.ThresholdReject) on the stored answers, so the counts are the
// engine's own verdicts under other thresholds, and it writes nothing: no
// setting, no signal.
package entryqualitydiag

import "github.com/ousiassllc/pitha-trador/internal/config"

// MaxRows is the most LONG/SHORT trader decisions one call reads. A period with
// more is query.ErrLimitExceeded rather than a partial count.
const MaxRows = 50000

// MaxScenarios is the most min_entry_quality values one call evaluates (every
// level of the scale).
const MaxScenarios = 5

// DefaultScenarios are the min_entry_quality values evaluated when none is
// asked for.
var DefaultScenarios = []string{"fair", "good", "strong"}

// PromptScope is the Trader prompt versions the counts cover: Only when one
// version was asked for, otherwise every version but Excluded (the versions of
// another cohort than the live one, the rows the Calibration and the
// meta-model training leave out as well, issue #881).
type PromptScope struct {
	Only     string   `json:"only,omitempty"`
	Excluded []string `json:"excluded"`
}

// DirectionThresholds are the thresholds of one side that apply after
// entry_quality: the same for every scenario.
type DirectionThresholds struct {
	MinContinuationProbability float64 `json:"min_continuation_probability"`
	MaxToxicFlow               float64 `json:"max_toxic_flow"`
	MaxLiquidityStressed       float64 `json:"max_liquidity_stressed"`
}

// Thresholds are the policy.* values every scenario is judged against besides
// its own min_entry_quality: config/strategy.yaml (or the build's default)
// with the runtime_settings overrides in force now applied. OverriddenKeys
// are the runtime_settings keys that changed a value. They are today's values:
// a decision made under other thresholds is judged by today's.
type Thresholds struct {
	Long           DirectionThresholds `json:"long"`
	Short          DirectionThresholds `json:"short"`
	OverriddenKeys []string            `json:"overridden_keys"`
}

// Bucket is a count under a fixed reason code.
type Bucket struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

// Population classifies the period's LONG/SHORT trader decisions by where the
// Policy Engine's tree took them. ReachedGuards is the population the
// scenarios are about; LongShort is the sum of the five classes after it.
type Population struct {
	// TraderDecisions are all trader decisions of the scope, NONE included.
	TraderDecisions int `json:"trader_decisions"`
	LongShort       int `json:"long_short"`
	// VWAPReversion decisions took the VWAP reversion rule (regime arm in a
	// mean-reverting regime): entry_quality does not apply to them.
	VWAPReversion int `json:"vwap_reversion"`
	// NotLendableShort decisions were turned away for a non-lendable stock
	// before entry_quality was looked at (reject_reason not_lendable).
	NotLendableShort int `json:"not_lendable_short"`
	// RejectedBeforeGuards decisions failed an earlier gate (the reasons are in
	// RejectedBeforeGuardsByReason; "no_signal" is a decision without a
	// trade_signals row).
	RejectedBeforeGuards         int      `json:"rejected_before_guards"`
	RejectedBeforeGuardsByReason []Bucket `json:"rejected_before_guards_by_reason"`
	// IncompleteAnswer decisions lack an answer field the guards read.
	IncompleteAnswer int `json:"incomplete_answer"`
	// ReachedGuards decisions got as far as the entry guards.
	ReachedGuards int `json:"reached_guards"`
}

// Distribution is how many LONG/SHORT decisions of one prompt version answered
// one entry_quality ("unknown" for a missing or unrecognised answer).
type Distribution struct {
	PromptVersion string `json:"prompt_version"`
	EntryQuality  string `json:"entry_quality"`
	Count         int    `json:"count"`
}

// Counts is what one scenario does to the decisions that reached the guards.
// Candidates = BelowEntryQuality + PassEntryQuality, and PassEntryQuality =
// BlockedContinuation + BlockedToxicFlow + BlockedLiquidityStressed +
// PassAllGuards (the first guard to fail is the one counted, in the engine's
// order).
type Counts struct {
	Candidates               int `json:"candidates"`
	BelowEntryQuality        int `json:"below_entry_quality"`
	PassEntryQuality         int `json:"pass_entry_quality"`
	BlockedContinuation      int `json:"blocked_continuation_probability"`
	BlockedToxicFlow         int `json:"blocked_toxic_flow"`
	BlockedLiquidityStressed int `json:"blocked_liquidity_stressed"`
	PassAllGuards            int `json:"pass_all_guards"`
}

// Scenario is one min_entry_quality value, for both sides together and each.
// Outcomes says how the candidates that passed every guard did, per side.
type Scenario struct {
	MinEntryQuality string           `json:"min_entry_quality"`
	Total           Counts           `json:"total"`
	Long            Counts           `json:"long"`
	Short           Counts           `json:"short"`
	Outcomes        ScenarioOutcomes `json:"outcomes"`
}

// Data is entry_quality_whatif's data.
type Data struct {
	RowsFrom         string           `json:"rows_from"`
	RowsTo           string           `json:"rows_to"`
	PromptVersions   PromptScope      `json:"prompt_versions"`
	Thresholds       Thresholds       `json:"thresholds"`
	Population       Population       `json:"population"`
	Distribution     []Distribution   `json:"entry_quality_distribution"`
	Scenarios        []Scenario       `json:"scenarios"`
	LabelAssumptions LabelAssumptions `json:"label_assumptions"`
}

// Args are the tool's own arguments.
type Args struct {
	// MinEntryQuality are the values to evaluate (DefaultScenarios when empty).
	MinEntryQuality []string
	// PromptVersion restricts the counts to one Trader prompt version; empty is
	// the live version's cohort.
	PromptVersion string
	// Foreign are the prompt versions left out when PromptVersion is empty
	// (jev.ForeignVersionsOfLive).
	Foreign []string
	// Baseline is config/strategy.yaml's policy section; nil is the build's
	// default.
	Baseline *config.PolicyConfig
}
