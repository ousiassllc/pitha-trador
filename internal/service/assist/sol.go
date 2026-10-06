package assist

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// SolAnalyzePath is the Sol API endpoint the daily analysis request is
// POSTed to.
const SolAnalyzePath = "/v1/analyze"

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

// SolChange is one threshold change the Sol API proposes. NewValue is the
// raw JSON scalar exactly as the LLM produced it: Sol does not validate it
// (the LLM output is untrusted). internal/service/selfimprove.Governor
// turns it into a domain.PolicyChange and machine-checks the key and change
// width before anything is recorded as a real proposal
// (FR-SELFIMPROVE-8).
type SolChange struct {
	Key      string          `json:"key"`
	NewValue json.RawMessage `json:"new_value"`
}

// SolResponse is the JSON body the Sol API answers with. An empty
// ProposedChanges means "no actionable weakness today".
type SolResponse struct {
	Rationale       json.RawMessage `json:"rationale"`
	ProposedChanges []SolChange     `json:"proposed_changes"`
}

// SolProposal is Sol's daily output when the LLM proposes a change
// (FR-SELFIMPROVE-1): RationaleJSON is stored verbatim as
// policy_proposals.rationale_json; Changes are validated by the caller
// before they become proposed_changes_json.
type SolProposal struct {
	RationaleJSON string
	Changes       []SolChange
}

// Sol is the Think adapter: it turns the recent Calibration metrics into
// the threshold changes it proposes (FR-SELFIMPROVE-1, FR-SELFIMPROVE-8).
// By default the code generates the candidate changes and Jev picks one
// (sol_jev.go); when SOL_BASE_URL is set the external Sol LLM API proposes
// them instead.
type Sol struct {
	client *Client
	jev    Asker
}

// NewSol returns a Sol adapter that calls client (SOL_API_KEY/SOL_BASE_URL),
// or Jev (WithJev) when client is not configured.
func NewSol(client *Client, opts ...Option) *Sol {
	return &Sol{client: client, jev: applyOptions(opts).jev}
}

type solRequest struct {
	Task        string         `json:"task"`
	Long        solDirection   `json:"long"`
	Short       solDirection   `json:"short"`
	Constraints solConstraints `json:"constraints"`
}

type solDirection struct {
	Thresholds  solThresholds  `json:"thresholds"`
	Calibration solCalibration `json:"calibration"`
}

type solThresholds struct {
	MinProbability             float64 `json:"min_probability"`
	MinEntryQuality            string  `json:"min_entry_quality"`
	MinContinuationProbability float64 `json:"min_continuation_probability"`
	MaxToxicFlow               float64 `json:"max_toxic_flow"`
	MaxLiquidityStressed       float64 `json:"max_liquidity_stressed"`
}

type solCalibration struct {
	Buckets                  []solBucket `json:"buckets"`
	BrierScore               float64     `json:"brier_score"`
	LogLoss                  float64     `json:"log_loss"`
	ExpectedCalibrationError float64     `json:"expected_calibration_error"`
	SampleCount              int         `json:"sample_count"`
}

type solBucket struct {
	Range              string  `json:"range"`
	DirectionAccuracy  float64 `json:"direction_accuracy"`
	AvgFutureReturnPct float64 `json:"avg_future_return_pct"`
	SampleCount        int     `json:"sample_count"`
}

// solConstraints tells the LLM the rules its output will be machine-checked
// against (FR-SELFIMPROVE-2/3). They are guidance only: enforcement is
// selfimprove.Governor's.
type solConstraints struct {
	AllowedKeys          []string `json:"allowed_keys"`
	MaxConfidenceStep    float64  `json:"max_confidence_step"`
	MaxEntryQualityRanks int      `json:"max_entry_quality_ranks"`
	EntryQualityOrder    []string `json:"entry_quality_order"`
}

func newSolRequest(in SolAnalysisInput) solRequest {
	keys := make([]string, 0, len(domain.PolicyProposalKeys))
	for key := range domain.PolicyProposalKeys {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	return solRequest{
		Task:  "propose_policy_threshold_changes",
		Long:  newSolDirection(in.Long),
		Short: newSolDirection(in.Short),
		Constraints: solConstraints{
			AllowedKeys:          keys,
			MaxConfidenceStep:    domain.MaxConfidenceThresholdStep,
			MaxEntryQualityRanks: domain.MaxEntryQualityStepRanks,
			EntryQualityOrder: []string{
				domain.JevEntryQualityPoor, domain.JevEntryQualityFair, domain.JevEntryQualityGood,
				domain.JevEntryQualityStrong, domain.JevEntryQualityExceptional,
			},
		},
	}
}

func newSolDirection(dc DirectionCalibration) solDirection {
	buckets := make([]solBucket, 0, len(dc.Calibration.Buckets))
	for _, b := range dc.Calibration.Buckets {
		buckets = append(buckets, solBucket{
			Range: b.Range, DirectionAccuracy: b.DirectionAccuracy,
			AvgFutureReturnPct: b.AvgFutureReturnPct, SampleCount: b.SampleCount,
		})
	}
	return solDirection{
		Thresholds: solThresholds{
			MinProbability:             dc.Thresholds.MinProbability,
			MinEntryQuality:            dc.Thresholds.MinEntryQuality,
			MinContinuationProbability: dc.Thresholds.MinContinuationProbability,
			MaxToxicFlow:               dc.Thresholds.MaxToxicFlow,
			MaxLiquidityStressed:       dc.Thresholds.MaxLiquidityStressed,
		},
		Calibration: solCalibration{
			Buckets:                  buckets,
			BrierScore:               dc.Calibration.BrierScore,
			LogLoss:                  dc.Calibration.LogLoss,
			ExpectedCalibrationError: dc.Calibration.ExpectedCalibrationError,
			SampleCount:              dc.Calibration.SampleCount,
		},
	}
}

// Analyze runs Sol's daily analysis (FR-SELFIMPROVE-1) by calling the Sol
// API. It returns (proposal, true, nil) when the LLM proposes at least one
// change, or (SolProposal{}, false, nil) when it proposes none - a valid
// outcome the caller does not record as a policy_proposals row. An API
// failure (including ErrNotConfigured) is returned as an error: the daily
// analysis is skipped and retried the next business day (overview.md §8).
func (s *Sol) Analyze(ctx context.Context, in SolAnalysisInput) (SolProposal, bool, error) {
	viaJev, err := useJev(s.client, s.jev)
	if err != nil {
		return SolProposal{}, false, fmt.Errorf("assist: sol analyze: %w", err)
	}
	if viaJev {
		proposal, ok, err := s.analyzeWithJev(ctx, in)
		if err != nil {
			return SolProposal{}, false, fmt.Errorf("assist: sol analyze: %w", err)
		}
		return proposal, ok, nil
	}
	var resp SolResponse
	if err := s.client.PostJSON(ctx, SolAnalyzePath, newSolRequest(in), &resp); err != nil {
		return SolProposal{}, false, fmt.Errorf("assist: sol analyze: %w", err)
	}
	if len(resp.ProposedChanges) == 0 {
		return SolProposal{}, false, nil
	}

	rationale := bytes.TrimSpace(resp.Rationale)
	if len(rationale) == 0 || bytes.Equal(rationale, []byte("null")) {
		rationale = []byte("{}")
	}
	return SolProposal{RationaleJSON: string(rationale), Changes: resp.ProposedChanges}, true, nil
}
