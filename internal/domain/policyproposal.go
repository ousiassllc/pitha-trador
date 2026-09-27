package domain

import (
	"encoding/json"
	"fmt"
	"math"
	"time"
)

// PolicyProposal status values (docs/architecture/er.md §policy_proposals
// CHECK constraint, functional.md §4.14).
const (
	PolicyProposalStatusPending    = "pending"
	PolicyProposalStatusApproved   = "approved"
	PolicyProposalStatusRejected   = "rejected"
	PolicyProposalStatusApplied    = "applied"
	PolicyProposalStatusRolledBack = "rolled_back"
)

// PolicyProposal mirrors one policy_proposals row: a single Sol-generated
// Policy Engine threshold improvement proposal, together with Opus's
// shadow-backtest review and its eventual apply/rollback outcome
// (docs/architecture/er.md §policy_proposals, functional.md §4.14).
type PolicyProposal struct {
	ID         int64
	ProposedAt time.Time
	ProposedBy string
	// RationaleJSON is Sol's analysis behind ProposedChangesJSON (losing
	// trade / Calibration bucket statistics that triggered it,
	// FR-SELFIMPROVE-1), stored verbatim as the JSON text Sol produced.
	RationaleJSON string
	// ProposedChangesJSON is the JSON encoding of a []PolicyChange
	// (ParsePolicyChanges parses it back); FR-SELFIMPROVE-2 restricts
	// every PolicyChange.Key to a PolicyProposalKeys entry.
	ProposedChangesJSON string
	Status              string
	// BacktestResultJSON is Opus's shadow-backtest Expectancy/MaxDrawdown
	// comparison (FR-SELFIMPROVE-4), nil until the Governor runs it.
	BacktestResultJSON *string
	ReviewedBy         *string
	// ReviewJSON is Opus's approve/reject rationale (FR-SELFIMPROVE-5).
	ReviewJSON *string
	// AppliedPolicyVersion is the identifier the Governor mints when this
	// proposal is applied (FR-SELFIMPROVE-5), nil until then.
	AppliedPolicyVersion *string
	AppliedAt            *time.Time
	// RolledBackAt/RolledBackReason are set only if FR-SELFIMPROVE-6's
	// post-apply Expectancy tracking later reverts this proposal.
	RolledBackAt     *time.Time
	RolledBackReason *string
	CreatedAt        time.Time
}

// PolicyChange is one runtime_settings policy.* key/value change a
// PolicyProposal proposes. OldValue/NewValue are JSON-encoded scalars,
// exactly runtime_settings.value's own storage convention (internal/
// service/risk/settings.go's setBoolSetting/setStringSetting precedent):
// applying an approved change is therefore a direct
// RuntimeSettingsRepository.Set(ctx, Key, NewValue, now) call, no
// re-encoding needed.
type PolicyChange struct {
	Key      string `json:"key"`
	OldValue string `json:"old_value"`
	NewValue string `json:"new_value"`
}

// Policy Engine LONG/SHORT threshold runtime_settings keys
// (config.PolicyDirectionThresholds's own yaml tags under the
// "policy.long."/"policy.short." namespace): the exhaustive set
// FR-SELFIMPROVE-2 allows Sol/Opus to propose changes to. Every other
// runtime_settings key - in particular every risk.* key (Risk Engine
// limits) and Jev's prompt_version - is rejected by
// ValidatePolicyChanges, and no selfimprove/assist API accepts a
// runtime_settings key/value pair directly (only a PolicyChange whose Key
// is checked against this set), so risk.*/prompt_version cannot be
// written through this loop at all (overview.md §1 "AI自己改善ループの
// 境界").
const (
	PolicyKeyLongMinProbability             = "policy.long.min_probability"
	PolicyKeyLongMinEntryQuality            = "policy.long.min_entry_quality"
	PolicyKeyLongMinContinuationProbability = "policy.long.min_continuation_probability"
	PolicyKeyLongMaxToxicFlow               = "policy.long.max_toxic_flow"
	PolicyKeyLongMaxLiquidityStressed       = "policy.long.max_liquidity_stressed"

	PolicyKeyShortMinProbability             = "policy.short.min_probability"
	PolicyKeyShortMinEntryQuality            = "policy.short.min_entry_quality"
	PolicyKeyShortMinContinuationProbability = "policy.short.min_continuation_probability"
	PolicyKeyShortMaxToxicFlow               = "policy.short.max_toxic_flow"
	PolicyKeyShortMaxLiquidityStressed       = "policy.short.max_liquidity_stressed"
)

// entryQualityPolicyKeys is the subset of PolicyProposalKeys whose value
// is a domain.JevEntryQuality* string (step-type, FR-SELFIMPROVE-3's "1
// 段階まで") rather than a float64 confidence-type threshold.
var entryQualityPolicyKeys = map[string]bool{
	PolicyKeyLongMinEntryQuality:  true,
	PolicyKeyShortMinEntryQuality: true,
}

// PolicyProposalKeys is every runtime_settings key a PolicyChange.Key may
// name (FR-SELFIMPROVE-2).
var PolicyProposalKeys = map[string]bool{
	PolicyKeyLongMinProbability:             true,
	PolicyKeyLongMinEntryQuality:            true,
	PolicyKeyLongMinContinuationProbability: true,
	PolicyKeyLongMaxToxicFlow:               true,
	PolicyKeyLongMaxLiquidityStressed:       true,

	PolicyKeyShortMinProbability:             true,
	PolicyKeyShortMinEntryQuality:            true,
	PolicyKeyShortMinContinuationProbability: true,
	PolicyKeyShortMaxToxicFlow:               true,
	PolicyKeyShortMaxLiquidityStressed:       true,
}

// maxConfidenceThresholdStep is FR-SELFIMPROVE-3's per-proposal cap for
// every confidence-type policy.* key (min_probability,
// min_continuation_probability, max_toxic_flow, max_liquidity_stressed):
// "confidence系しきい値で±0.05".
const maxConfidenceThresholdStep = 0.05

// maxEntryQualityStepRanks is FR-SELFIMPROVE-3's per-proposal cap for
// policy.{long,short}.min_entry_quality: "段階型しきい値で1段階まで".
const maxEntryQualityStepRanks = 1

// floatMagnitudeEpsilon absorbs float64 round-trip noise
// (json.Marshal/Unmarshal of e.g. 0.05) so a change exactly at
// maxConfidenceThresholdStep is never spuriously rejected.
const floatMagnitudeEpsilon = 1e-9

// entryQualityRank orders JevEntryQuality* from worst (0) to best,
// mirroring internal/service/policy.Engine's own private ranking (same
// FR-POLICY-1/2 "entry_quality >= X" ordering) so
// FR-SELFIMPROVE-3's step-count check and the policy package's threshold
// comparison never disagree. An unrecognized value ranks as -1 (invalid):
// unlike the Policy Engine's live-decision fallback of 0 ("never let an
// unknown value silently clear a threshold"), a proposal naming an
// unrecognized entry_quality value is a validation error here, not a
// silently-degraded rank.
var entryQualityRank = map[string]int{
	JevEntryQualityPoor:        0,
	JevEntryQualityFair:        1,
	JevEntryQualityGood:        2,
	JevEntryQualityStrong:      3,
	JevEntryQualityExceptional: 4,
}

// EntryQualityRank returns quality's rank (0=poor .. 4=exceptional), or
// (-1, false) if quality is not a recognized JevEntryQuality* value.
func EntryQualityRank(quality string) (int, bool) {
	rank, ok := entryQualityRank[quality]
	return rank, ok
}

// ParsePolicyChanges decodes changesJSON (a PolicyProposal's
// ProposedChangesJSON) into a []PolicyChange.
func ParsePolicyChanges(changesJSON string) ([]PolicyChange, error) {
	var changes []PolicyChange
	if err := json.Unmarshal([]byte(changesJSON), &changes); err != nil {
		return nil, fmt.Errorf("domain: parse policy changes: %w", err)
	}
	return changes, nil
}

// EncodePolicyChanges is ParsePolicyChanges's inverse, building the
// ProposedChangesJSON a PolicyProposal stores.
func EncodePolicyChanges(changes []PolicyChange) (string, error) {
	data, err := json.Marshal(changes)
	if err != nil {
		return "", fmt.Errorf("domain: encode policy changes: %w", err)
	}
	return string(data), nil
}

// ValidatePolicyChanges enforces FR-SELFIMPROVE-2 (every Key must be a
// PolicyProposalKeys entry - risk.* keys and Jev's prompt_version are
// rejected, whatever their spelling) and FR-SELFIMPROVE-3 (per-key change
// magnitude caps) against every change in changes. It returns the first
// violation found, or nil if changes is non-empty and every change
// passes both checks.
func ValidatePolicyChanges(changes []PolicyChange) error {
	if len(changes) == 0 {
		return fmt.Errorf("domain: policy proposal has no changes")
	}
	for _, c := range changes {
		if !PolicyProposalKeys[c.Key] {
			return fmt.Errorf("domain: policy change key %q is not a policy.* threshold key Sol/Opus may change (FR-SELFIMPROVE-2)", c.Key)
		}
		if entryQualityPolicyKeys[c.Key] {
			if err := validateEntryQualityStep(c); err != nil {
				return err
			}
			continue
		}
		if err := validateConfidenceStep(c); err != nil {
			return err
		}
	}
	return nil
}

func validateEntryQualityStep(c PolicyChange) error {
	oldQuality, err := decodeJSONString(c.OldValue)
	if err != nil {
		return fmt.Errorf("domain: policy change %q old_value: %w", c.Key, err)
	}
	newQuality, err := decodeJSONString(c.NewValue)
	if err != nil {
		return fmt.Errorf("domain: policy change %q new_value: %w", c.Key, err)
	}
	oldRank, ok := EntryQualityRank(oldQuality)
	if !ok {
		return fmt.Errorf("domain: policy change %q old_value %q is not a recognized entry_quality", c.Key, oldQuality)
	}
	newRank, ok := EntryQualityRank(newQuality)
	if !ok {
		return fmt.Errorf("domain: policy change %q new_value %q is not a recognized entry_quality", c.Key, newQuality)
	}
	if step := newRank - oldRank; step > maxEntryQualityStepRanks || step < -maxEntryQualityStepRanks {
		return fmt.Errorf("domain: policy change %q steps entry_quality by %d rank(s), exceeding FR-SELFIMPROVE-3's %d-step cap", c.Key, step, maxEntryQualityStepRanks)
	}
	return nil
}

func validateConfidenceStep(c PolicyChange) error {
	oldValue, err := decodeJSONFloat(c.OldValue)
	if err != nil {
		return fmt.Errorf("domain: policy change %q old_value: %w", c.Key, err)
	}
	newValue, err := decodeJSONFloat(c.NewValue)
	if err != nil {
		return fmt.Errorf("domain: policy change %q new_value: %w", c.Key, err)
	}
	if magnitude := math.Abs(newValue - oldValue); magnitude > maxConfidenceThresholdStep+floatMagnitudeEpsilon {
		return fmt.Errorf("domain: policy change %q moves value by %.4f, exceeding FR-SELFIMPROVE-3's ±%.2f cap", c.Key, magnitude, maxConfidenceThresholdStep)
	}
	return nil
}

func decodeJSONFloat(raw string) (float64, error) {
	var v float64
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return 0, fmt.Errorf("decode %q as number: %w", raw, err)
	}
	return v, nil
}

func decodeJSONString(raw string) (string, error) {
	var v string
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return "", fmt.Errorf("decode %q as string: %w", raw, err)
	}
	return v, nil
}
