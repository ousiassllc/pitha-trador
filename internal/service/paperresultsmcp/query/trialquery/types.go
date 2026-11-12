// Package trialquery holds the read-only SQL and the aggregation behind the
// model-comparison trial tools of the Paper results MCP (issue #1060):
// jev_trial_compare and jev_trial_cost. It reads jev_trial_decisions (what the
// trial's arms answered, or why they did not), the live decision each row was
// asked alongside, and the outcome records that already exist
// (calibration_outcomes, counterfactual_labels). It computes no new label, and
// it holds no winner, vote, consensus or promotion: a rank is an ordering of
// one metric, never a recommendation.
//
// Like the other query subpackages it selects named columns only and never
// returns response_json, error_excerpt or any error text: counts and codes.
package trialquery

import (
	"regexp"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/domain/jevtrial"
)

// Limits of one call (more is query.ErrLimitExceeded, never a partial answer).
const (
	// MaxArms is the most arms one comparison covers (the trial's own limit).
	MaxArms = jevtrialsettings.MaxArms
	// MaxRows is the most jev_trial_decisions rows one comparison reads.
	MaxRows = 100_000
	// MaxOutcomeRows is the most outcome records (calibration_outcomes and
	// counterfactual_labels each) one comparison reads.
	MaxOutcomeRows = 200_000
	// MaxSlices is the most (horizon, label version) pairs reported.
	MaxSlices = 16
)

// Evaluation-side thresholds (the trial rows carry no threshold; these are the
// reader's choices, fixed here so every call applies the same ones).
const (
	// HitThresholdPct is the signed future_return (percent) above which a
	// scored directional answer counts as a hit: 0, i.e. gross of cost, the
	// same rule as jev_shadow_compare's hit_rate.
	HitThresholdPct = 0.0
	// MinOutcomeRecordsForRank is the fewest cohort decisions with an outcome
	// record a slice needs before its arms are given a rank. Below it rank is
	// null: an ordering of a handful of outcomes is noise, not a comparison.
	MinOutcomeRecordsForRank = 20
)

// armIDPattern is jevtrialsettings's arm id rule (it ends up in settings keys).
var armIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,47}$`)

// entryQualityOrder is the fixed order of the entry_quality rows, poor to
// exceptional: every level is always present, with zeros.
var entryQualityOrder = []string{
	domain.JevEntryQualityPoor, domain.JevEntryQualityFair, domain.JevEntryQualityGood,
	domain.JevEntryQualityStrong, domain.JevEntryQualityExceptional,
}

// Notice is the fixed caveat jev_trial_compare returns with every answer.
const Notice = "rank は並べ替えにすぎず、昇格・採用の推奨ではない（勝者・推奨・多数決・合議の項目は無い）。" +
	"truncation_suspected や rag_includes_live_model_answers が付いた行、llm_cutoff_status が unverified の行が残る間は、同じ入力での公平な比較ではない" +
	"（これらの行は除外せず、件数を fairness に出す）。失敗・拒否・一部拒否の行は「答えなし」として分母に入れ、成功した行だけで順位を作らない。" +
	"NONE の答えには方向の結果が無いので当たり外れに数えず、方向を補って勝たせない。見送りの反実仮想ラベルは counterfactual_proxy に proxy として別に出す"

// RowCounts counts jev_trial_decisions rows. The denominator of MissingRate is
// Total: failed, refused and partial rows are in it.
type RowCounts struct {
	Total       int            `json:"total"`
	OK          int            `json:"ok"`
	Failed      int            `json:"failed"`
	Refused     int            `json:"refused"`
	Partial     int            `json:"partial"`
	MissingRate *float64       `json:"missing_rate"`
	FailureCode map[string]int `json:"failure_codes"`
	// TruncationSuspected and RAGIncludesLiveModelAnswers count the rows that
	// carry the flag. They are counted, never excluded.
	TruncationSuspected         int            `json:"truncation_suspected"`
	RAGIncludesLiveModelAnswers int            `json:"rag_includes_live_model_answers"`
	LLMCutoffStatus             map[string]int `json:"llm_cutoff_status"`
}

func newRowCounts() RowCounts {
	return RowCounts{FailureCode: map[string]int{}, LLMCutoffStatus: map[string]int{}}
}

func (c *RowCounts) add(r *armRow) {
	c.Total++
	switch r.status {
	case jevtrial.StatusOK:
		c.OK++
	case jevtrial.StatusFailed:
		c.Failed++
		c.FailureCode[r.failureCode]++
	case jevtrial.StatusRefused:
		c.Refused++
	case jevtrial.StatusPartial:
		c.Partial++
	}
	if r.truncation {
		c.TruncationSuspected++
	}
	if r.rag {
		c.RAGIncludesLiveModelAnswers++
	}
	c.LLMCutoffStatus[r.cutoff]++
}

func (c *RowCounts) finish() { c.MissingRate = ratio(c.Failed+c.Refused+c.Partial, c.Total) }

// Rate is a numerator over a denominator, nil without a denominator.
type Rate struct {
	Numerator   int      `json:"numerator"`
	Denominator int      `json:"denominator"`
	Rate        *float64 `json:"rate"`
}

func rate(num, den int) Rate { return Rate{Numerator: num, Denominator: den, Rate: ratio(num, den)} }

func ratio(num, den int) *float64 {
	if den == 0 {
		return nil
	}
	v := float64(num) / float64(den)
	return &v
}
