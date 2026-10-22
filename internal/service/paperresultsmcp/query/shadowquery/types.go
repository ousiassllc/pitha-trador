// Package shadowquery holds the read-only SQL behind the Jev shadow tools of the
// Paper results MCP (issue #872): jev_shadow_compare and jev_shadow_cost. It is a
// subpackage of query (which would otherwise outgrow its size budget) and uses
// query's period, querier and error helpers; like them it selects named columns
// only and never returns a response body.
package shadowquery

import (
	"database/sql"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// MaxShadowVersions is the most candidate versions jev_shadow_compare returns
// (more is query.ErrLimitExceeded, like the horizons of jev_calibration).
const MaxShadowVersions = 10

// MaxShadowCostDays is the most JST days of daily cost jev_shadow_cost lists.
const MaxShadowCostDays = 31

// RecentPromotions is how many promotions jev_shadow_cost lists.
const RecentPromotions = 10

// entryQualityOrder is the fixed order of the entry_quality rows, poor to
// exceptional: every level is always present, with zeros.
var entryQualityOrder = []string{
	domain.JevEntryQualityPoor, domain.JevEntryQualityFair, domain.JevEntryQualityGood,
	domain.JevEntryQualityStrong, domain.JevEntryQualityExceptional,
}

// Count is a number of decisions and its share of the pairs (nil without pairs).
type Count struct {
	Count int      `json:"count"`
	Share *float64 `json:"share"`
}

// VersionShare is a live prompt version and how many of the pairs it served.
type VersionShare struct {
	Version string `json:"version"`
	Pairs   int    `json:"pairs"`
}

// ShadowCost is the shadow's cost for the pairs of one version.
type ShadowCost struct {
	Calls          int      `json:"calls"`
	RequestCostUSD float64  `json:"request_cost_usd"`
	AvgLatencyMs   *float64 `json:"avg_latency_ms"`
}

// DirectionAgreement is how often the shadow's direction equals the live one.
type DirectionAgreement struct {
	Agree int      `json:"agree"`
	Rate  *float64 `json:"rate"`
}

// OutcomeNote is the fixed caveat jev_shadow_compare returns with its outcomes.
const OutcomeNote = "calibration_outcomes is labeled for the live direction and future_return stops at the live direction's barrier. " +
	"A shadow decision whose direction differs from a directional live decision cannot be scored from it (the live outcome would decide the shadow's), " +
	"so those rows are counted in excluded_disagreement and left out of hit_rate and avg_directional_return_pct (computed over comparable rows only; gross of cost). " +
	"This compares which entries each prompt takes, not profit and loss."

// OutcomeStats is one side's result for one entry_quality level at one
// horizon and label version, over the pairs that have a calibration_outcomes
// row. Samples are the outcomes, Directional those whose side direction is not
// NONE, Comparable the directional ones that can be scored from future_return
// (the side's direction equals the live one, or the live one is NONE),
// ExcludedDisagreement = Directional - Comparable (always 0 for the live side).
// HitRate is the share of Comparable rows whose direction-signed future_return
// is above 0 and AvgDirectionalReturnPct its mean (percent), computed the same
// way for both sides (gross, no cost). The Recorded* fields use the label as
// the labeler recorded it (was_direction_correct, net of cost; barrier_hit;
// net_return_bps; label version 2 only) and so exist only where the side's
// direction equals the live direction the label was made for.
type OutcomeStats struct {
	Samples                 int                 `json:"samples"`
	Directional             int                 `json:"directional"`
	Comparable              int                 `json:"comparable"`
	ExcludedDisagreement    int                 `json:"excluded_disagreement"`
	HitRate                 *float64            `json:"hit_rate"`
	AvgDirectionalReturnPct *float64            `json:"avg_directional_return_pct"`
	RecordedSamples         int                 `json:"recorded_samples"`
	RecordedHitRate         *float64            `json:"recorded_hit_rate"`
	RecordedBarrierCounts   query.BarrierCounts `json:"recorded_barrier_counts"`
	RecordedAvgNetReturnBps *float64            `json:"recorded_avg_net_return_bps"`
}

// QualityOutcome is one entry_quality level's live and shadow outcome stats.
type QualityOutcome struct {
	EntryQuality string       `json:"entry_quality"`
	Live         OutcomeStats `json:"live"`
	Shadow       OutcomeStats `json:"shadow"`
}

// HorizonOutcomes is the outcomes of one horizon and label version. Labels of
// different versions define "correct" differently (FR-CAL-4) and are never
// mixed.
type HorizonOutcomes struct {
	HorizonMinutes int              `json:"horizon_minutes"`
	LabelVersion   int              `json:"label_version"`
	ByEntryQuality []QualityOutcome `json:"by_entry_quality"`
}

// QualityCount is one entry_quality level's live and shadow counts.
type QualityCount struct {
	EntryQuality string `json:"entry_quality"`
	Live         Count  `json:"live"`
	Shadow       Count  `json:"shadow"`
}

// ShadowVersionComparison is the live-vs-shadow comparison of one candidate
// prompt version over the same inputs.
type ShadowVersionComparison struct {
	PromptVersion      string             `json:"prompt_version"`
	Pairs              int                `json:"pairs"`
	LiveVersions       []VersionShare     `json:"live_versions"`
	EntryQuality       []QualityCount     `json:"entry_quality"`
	DirectionAgreement DirectionAgreement `json:"direction_agreement"`
	Cost               ShadowCost         `json:"cost"`
	Outcomes           []HorizonOutcomes  `json:"outcomes"`
}

// ShadowComparison is jev_shadow_compare's data.
type ShadowComparison struct {
	Versions []ShadowVersionComparison `json:"versions"`
	// OutcomeNote is OutcomeNote: how to read the outcomes.
	OutcomeNote string `json:"outcome_note"`
}

// pairsCTE selects the (live trader decision, shadow decision) pairs of a
// period and, when ? is non-empty, one candidate version. It binds from, to,
// version, version. jev_decisions has no entry_quality column: the live
// decision's answer is the "entry_quality" member of its response_json, read

func floatPtr(n sql.NullFloat64) *float64 {
	if !n.Valid {
		return nil
	}
	return &n.Float64
}

// ratio is num/den, nil without a denominator.
func ratio(num, den int) *float64 {
	if den == 0 {
		return nil
	}
	v := float64(num) / float64(den)
	return &v
}
