package assist

import (
	"encoding/json"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// Sol thresholds (FR-SELFIMPROVE-1): a direction's currently-active
// confidence bucket is "weak" - and Sol proposes tightening
// min_probability - when its FR-CAL-3 bucket accuracy falls below
// targetDirectionAccuracy or its average direction-adjusted future
// return is negative, provided the bucket has at least
// minBucketSampleCount labeled samples (a smaller sample is too noisy to
// act on). Overall miscalibration (ExpectedCalibrationError above
// targetExpectedCalibrationError) additionally proposes tightening
// min_entry_quality by one rank. Every proposed step uses
// maxConfidenceThresholdStep/1 rank - FR-SELFIMPROVE-3's own caps - since
// tightening by less than the allowed maximum leaves a known weakness
// only partially addressed.
const (
	targetDirectionAccuracy        = 0.55
	minBucketSampleCount           = 20
	targetExpectedCalibrationError = 0.10
	maxConfidenceThresholdStep     = 0.05
	maxMinProbability              = 0.99
)

// DirectionCalibration bundles one direction's (LONG or SHORT)
// currently-active Policy Engine thresholds with the Calibration metrics
// computed over that same direction's labeled trader decisions
// (internal/service/calibration.Metrics over samples pre-filtered by
// Direction) - Sol's FR-SELFIMPROVE-1 input.
type DirectionCalibration struct {
	Thresholds  config.PolicyDirectionThresholds
	Calibration domain.CalibrationMetrics
}

// SolAnalysisInput is Sol's full daily analysis input: both directions'
// DirectionCalibration.
type SolAnalysisInput struct {
	Long  DirectionCalibration
	Short DirectionCalibration
}

// SolProposal is Sol's daily output when it finds an actionable weakness
// (FR-SELFIMPROVE-1): RationaleJSON/ProposedChangesJSON are stored
// verbatim as policy_proposals.rationale_json/proposed_changes_json.
type SolProposal struct {
	RationaleJSON       string
	ProposedChangesJSON string
}

// directionFinding is one direction's rationale entry (marshaled into
// SolProposal.RationaleJSON's "long"/"short" fields): the bucket
// statistics Sol evaluated and what action, if any, they triggered.
type directionFinding struct {
	Direction                string   `json:"direction"`
	ConfidenceBucketRange    string   `json:"confidence_bucket_range,omitempty"`
	BucketDirectionAccuracy  float64  `json:"bucket_direction_accuracy,omitempty"`
	BucketAvgFutureReturnPct float64  `json:"bucket_avg_future_return_pct,omitempty"`
	BucketSampleCount        int      `json:"bucket_sample_count,omitempty"`
	BrierScore               float64  `json:"brier_score"`
	ExpectedCalibrationError float64  `json:"expected_calibration_error"`
	SampleCount              int      `json:"sample_count"`
	Actions                  []string `json:"actions,omitempty"`
}

type solRationale struct {
	Long  directionFinding `json:"long"`
	Short directionFinding `json:"short"`
}

// Sol is the Think adapter: it analyzes recent Calibration metrics and
// proposes Policy Engine threshold tightenings when it finds a weak
// confidence bucket or poor overall calibration (FR-SELFIMPROVE-1).
type Sol struct{}

// NewSol returns a Sol adapter.
func NewSol() *Sol {
	return &Sol{}
}

// Analyze runs Sol's daily analysis (FR-SELFIMPROVE-1). It returns
// (proposal, true, nil) when at least one direction's thresholds warrant
// a change, or (SolProposal{}, false, nil) when nothing does - a
// perfectly calibrated/healthy day produces no proposal, which is a
// valid outcome the caller (internal/service/selfimprove.Governor) does
// not record as a policy_proposals row.
func (s *Sol) Analyze(in SolAnalysisInput) (SolProposal, bool, error) {
	longFinding, longChanges := analyzeDirection(domain.JevDirectionLong, domain.PolicyKeyLongMinProbability, domain.PolicyKeyLongMinEntryQuality, in.Long)
	shortFinding, shortChanges := analyzeDirection(domain.JevDirectionShort, domain.PolicyKeyShortMinProbability, domain.PolicyKeyShortMinEntryQuality, in.Short)

	changes := append(longChanges, shortChanges...)
	if len(changes) == 0 {
		return SolProposal{}, false, nil
	}

	rationale := solRationale{Long: longFinding, Short: shortFinding}
	rationaleJSON, err := json.Marshal(rationale)
	if err != nil {
		return SolProposal{}, false, fmt.Errorf("assist: sol: encode rationale: %w", err)
	}
	changesJSON, err := domain.EncodePolicyChanges(changes)
	if err != nil {
		return SolProposal{}, false, fmt.Errorf("assist: sol: encode proposed changes: %w", err)
	}
	return SolProposal{RationaleJSON: string(rationaleJSON), ProposedChangesJSON: changesJSON}, true, nil
}

// analyzeDirection applies Sol's heuristic (this file's doc comment) to
// one direction, returning its rationale entry and zero, one, or two
// domain.PolicyChange (min_probability, min_entry_quality).
func analyzeDirection(direction, minProbabilityKey, minEntryQualityKey string, dc DirectionCalibration) (directionFinding, []domain.PolicyChange) {
	finding := directionFinding{
		Direction:                direction,
		BrierScore:               dc.Calibration.BrierScore,
		ExpectedCalibrationError: dc.Calibration.ExpectedCalibrationError,
		SampleCount:              dc.Calibration.SampleCount,
	}

	var changes []domain.PolicyChange

	if bucket, ok := findActiveBucket(dc.Calibration.Buckets, dc.Thresholds.MinProbability); ok {
		finding.ConfidenceBucketRange = bucket.Range
		finding.BucketDirectionAccuracy = bucket.DirectionAccuracy
		finding.BucketAvgFutureReturnPct = bucket.AvgFutureReturnPct
		finding.BucketSampleCount = bucket.SampleCount

		weak := bucket.SampleCount >= minBucketSampleCount &&
			(bucket.DirectionAccuracy < targetDirectionAccuracy || bucket.AvgFutureReturnPct < 0)
		if weak {
			newMinProbability := dc.Thresholds.MinProbability + maxConfidenceThresholdStep
			if newMinProbability > maxMinProbability {
				newMinProbability = maxMinProbability
			}
			if newMinProbability > dc.Thresholds.MinProbability {
				changes = append(changes, mustFloatChange(minProbabilityKey, dc.Thresholds.MinProbability, newMinProbability))
				finding.Actions = append(finding.Actions, "raise_min_probability")
			}
		}
	}

	if dc.Calibration.SampleCount >= minBucketSampleCount && dc.Calibration.ExpectedCalibrationError > targetExpectedCalibrationError {
		if newQuality, ok := stepEntryQualityUp(dc.Thresholds.MinEntryQuality); ok {
			changes = append(changes, mustStringChange(minEntryQualityKey, dc.Thresholds.MinEntryQuality, newQuality))
			finding.Actions = append(finding.Actions, "raise_min_entry_quality")
		}
	}

	return finding, changes
}

// findActiveBucket returns the ConfidenceBucket covering probability
// (domain.DefaultConfidenceBucketRanges' [Low, High) ranges, last range
// inclusive of High), or (zero value, false) if buckets has no entry for
// that range (no labeled samples fell in it yet).
func findActiveBucket(buckets []domain.ConfidenceBucket, probability float64) (domain.ConfidenceBucket, bool) {
	for _, r := range domain.DefaultConfidenceBucketRanges {
		if probability < r.Low || (probability >= r.High && r.High < 1.0) {
			continue
		}
		for _, b := range buckets {
			if b.Range == r.Range {
				return b, true
			}
		}
		return domain.ConfidenceBucket{}, false
	}
	return domain.ConfidenceBucket{}, false
}

// stepEntryQualityUp returns the entry_quality one rank above current,
// or (current, false) if current is already the best rank
// (JevEntryQualityExceptional) or unrecognized.
func stepEntryQualityUp(current string) (string, bool) {
	currentRank, ok := domain.EntryQualityRank(current)
	if !ok || currentRank+1 >= len(entryQualityByRank) {
		return current, false
	}
	return entryQualityByRank[currentRank+1], true
}

// entryQualityByRank is domain.EntryQualityRank's inverse (index ==
// rank), for stepEntryQualityUp.
var entryQualityByRank = []string{
	domain.JevEntryQualityPoor,
	domain.JevEntryQualityFair,
	domain.JevEntryQualityGood,
	domain.JevEntryQualityStrong,
	domain.JevEntryQualityExceptional,
}

func mustFloatChange(key string, oldValue, newValue float64) domain.PolicyChange {
	old, err := json.Marshal(oldValue)
	if err != nil {
		panic(fmt.Sprintf("assist: sol: encode float %v: %v", oldValue, err))
	}
	next, err := json.Marshal(newValue)
	if err != nil {
		panic(fmt.Sprintf("assist: sol: encode float %v: %v", newValue, err))
	}
	return domain.PolicyChange{Key: key, OldValue: string(old), NewValue: string(next)}
}

func mustStringChange(key, oldValue, newValue string) domain.PolicyChange {
	old, err := json.Marshal(oldValue)
	if err != nil {
		panic(fmt.Sprintf("assist: sol: encode string %q: %v", oldValue, err))
	}
	next, err := json.Marshal(newValue)
	if err != nil {
		panic(fmt.Sprintf("assist: sol: encode string %q: %v", newValue, err))
	}
	return domain.PolicyChange{Key: key, OldValue: string(old), NewValue: string(next)}
}
