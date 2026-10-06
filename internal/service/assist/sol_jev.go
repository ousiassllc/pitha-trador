package assist

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/systemone"
)

const (
	solQBestChange = "best_change"
	// solOptionNone is the "propose nothing today" answer.
	solOptionNone = "none"
)

// entryQualityOrder lists domain.JevEntryQuality* from worst to best, the
// order FR-SELFIMPROVE-3's "1段階まで" is counted in.
var entryQualityOrder = []string{
	domain.JevEntryQualityPoor, domain.JevEntryQualityFair, domain.JevEntryQualityGood,
	domain.JevEntryQualityStrong, domain.JevEntryQualityExceptional,
}

// solCandidate is one single-key change the code generated for Jev to judge.
type solCandidate struct {
	// ID is the option name Jev answers with: "<key>=<new value>".
	ID     string
	Change SolChange
	Rubric string
}

// solCandidates generates, per direction that has Calibration samples, one
// candidate for every FR-SELFIMPROVE-2 key moved one FR-SELFIMPROVE-3 step up
// and down (clamped to the valid range; unchanged values are dropped). The
// candidates are within the Governor's machine checks by construction; Jev
// only chooses among them, it never writes a value.
func solCandidates(in SolAnalysisInput) []solCandidate {
	var out []solCandidate
	for _, dir := range []struct {
		prefix string
		dc     DirectionCalibration
	}{{"policy.long.", in.Long}, {"policy.short.", in.Short}} {
		if dir.dc.Calibration.SampleCount == 0 {
			continue // no labeled samples: nothing to learn from
		}
		th := dir.dc.Thresholds
		for _, f := range []struct {
			key    string
			value  float64
			strict int // +1: raising the value is stricter, -1: lowering is
		}{
			{"min_probability", th.MinProbability, +1},
			{"min_continuation_probability", th.MinContinuationProbability, +1},
			{"max_toxic_flow", th.MaxToxicFlow, -1},
			{"max_liquidity_stressed", th.MaxLiquidityStressed, -1},
		} {
			for _, step := range []float64{domain.MaxConfidenceThresholdStep, -domain.MaxConfidenceThresholdStep} {
				if c, ok := floatCandidate(dir.prefix+f.key, f.value, step, f.strict); ok {
					out = append(out, c)
				}
			}
		}
		for _, step := range []int{1, -1} {
			if c, ok := entryQualityCandidate(dir.prefix+"min_entry_quality", th, step); ok {
				out = append(out, c)
			}
		}
	}
	return out
}

func floatCandidate(key string, current, step float64, strict int) (solCandidate, bool) {
	next := math.Round(math.Min(1, math.Max(0, current+step))*1e4) / 1e4
	if math.Abs(next-current) < 1e-9 {
		return solCandidate{}, false
	}
	oldValue, newValue := strconv.FormatFloat(current, 'f', -1, 64), strconv.FormatFloat(next, 'f', -1, 64)
	tone := "Loosen (more trades)"
	if (step > 0) == (strict > 0) {
		tone = "Tighten (fewer, higher-quality trades)"
	}
	return newCandidate(key, oldValue, newValue, newValue, tone), true
}

func entryQualityCandidate(key string, th config.PolicyDirectionThresholds, step int) (solCandidate, bool) {
	rank, ok := domain.EntryQualityRank(th.MinEntryQuality)
	next := rank + step
	if !ok || next < 0 || next >= len(entryQualityOrder) {
		return solCandidate{}, false
	}
	tone := "Loosen (more trades)"
	if step > 0 {
		tone = "Tighten (fewer, higher-quality trades)"
	}
	quoted := strconv.Quote(entryQualityOrder[next])
	return newCandidate(key, th.MinEntryQuality, entryQualityOrder[next], quoted, tone), true
}

func newCandidate(key, oldValue, newValue, rawValue, tone string) solCandidate {
	return solCandidate{
		ID:     key + "=" + newValue,
		Change: SolChange{Key: key, NewValue: json.RawMessage(rawValue)},
		Rubric: fmt.Sprintf("%s: change `%s` from %s to %s.", tone, key, oldValue, newValue),
	}
}

// solJevRationale is what Sol stores as policy_proposals.rationale_json when
// Jev made the choice: the code's candidate set and Jev's pick.
type solJevRationale struct {
	Source          string   `json:"source"`
	Model           string   `json:"model"`
	Selected        string   `json:"selected"`
	Confidence      float64  `json:"confidence"`
	CandidateIDs    []string `json:"candidates"`
	BrierScoreLong  float64  `json:"brier_score_long"`
	BrierScoreShort float64  `json:"brier_score_short"`
}

// analyzeWithJev asks Jev one choice question over the generated candidates
// (plus "none") and proposes the chosen change. Jev is never asked to write a
// proposal: the candidates, their values and the rationale come from code.
func (s *Sol) analyzeWithJev(ctx context.Context, in SolAnalysisInput) (SolProposal, bool, error) {
	candidates := solCandidates(in)
	if len(candidates) == 0 {
		return SolProposal{}, false, nil
	}
	options := make([]systemone.Option, 0, len(candidates)+1)
	ids := make([]string, 0, len(candidates))
	for _, c := range candidates {
		options = append(options, systemone.Option{Name: c.ID, Rubric: c.Rubric})
		ids = append(ids, c.ID)
	}
	options = append(options, systemone.Option{Name: solOptionNone, Rubric: "No change: the calibration shows no actionable weakness, or the evidence is too thin to justify any candidate."})

	questions := map[string]systemone.Question{
		solQBestChange: systemone.ChoiceQuestion(
			"Using `long` and `short` (each: the current Policy Engine `thresholds` and the `calibration` of past Jev decisions in that direction: bucket accuracy, average future return, Brier score, ECE), "+
				"which single threshold change, if any, would most likely improve expectancy without hurting drawdown? "+
				"Answer `none` unless the calibration shows a clear, evidence-backed weakness. "+
				"A bucket whose direction accuracy is low or whose average future return is negative suggests tightening; consistently strong buckets suggest room to loosen.",
			options...),
	}
	result, err := s.jev.Ask(ctx, "sol", newSolRequest(in), questions)
	if err != nil {
		return SolProposal{}, false, err
	}
	choice := result.Choice(solQBestChange)
	if choice.Value == solOptionNone {
		return SolProposal{}, false, nil
	}
	for _, c := range candidates {
		if c.ID != choice.Value {
			continue
		}
		rationale, err := json.Marshal(solJevRationale{
			Source: "jev", Model: result.Model, Selected: c.ID, Confidence: choice.Confidence, CandidateIDs: ids,
			BrierScoreLong: in.Long.Calibration.BrierScore, BrierScoreShort: in.Short.Calibration.BrierScore,
		})
		if err != nil {
			return SolProposal{}, false, fmt.Errorf("encode rationale: %w", err)
		}
		return SolProposal{RationaleJSON: string(rationale), Changes: []SolChange{c.Change}}, true, nil
	}
	return SolProposal{}, false, fmt.Errorf("%w: choice %q is not a generated candidate", systemone.ErrInvalidResponse, choice.Value)
}
