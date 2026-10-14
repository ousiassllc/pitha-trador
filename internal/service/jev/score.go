package jev

import (
	"errors"
	"fmt"
	"math"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// ErrInvalidScore is wrapped by NewScore's error when Jev's answer cannot be
// turned into a Score.
var ErrInvalidScore = errors.New("jev: invalid answer for score")

// maxEntryQualityRank is domain.JevEntryQualityExceptional's rank
// (domain.EntryQualityRank: poor 0 .. exceptional 4).
const maxEntryQualityRank = 4

// Score is Jev Trader's answer reduced to take/skip material (issue #761,
// parent #752 メタラベリング). Jev's direction is the primary signal (the
// candidate side); everything else it says is evidence for whether to take
// that candidate, never a verified probability of a price move
// (FR-TRADER-2): a secondary model (internal/service/metalabel) learns what
// the values are worth from realized outcomes.
//
// Every component is in [0, 1]; larger is more favourable to taking the
// candidate except ToxicFlow and LiquidityStressed, where larger is worse.
type Score struct {
	// EntryQuality is domain.JevEntryQuality*'s rank scaled to [0, 1]
	// (poor 0, fair 0.25, good 0.5, strong 0.75, exceptional 1).
	EntryQuality float64
	// ToxicFlow, LiquidityStressed and ContinuationProbability are Jev's
	// yes-probabilities of the same name.
	ToxicFlow               float64
	LiquidityStressed       float64
	ContinuationProbability float64
	// Confidence is Jev's self-reported confidence in its direction.
	Confidence float64
}

// NewScore validates Jev's answer and returns it as a Score. It fails when
// entryQuality is not a domain.JevEntryQuality* value or a probability is not
// a number in [0, 1].
func NewScore(entryQuality string, toxicFlow, liquidityStressed, continuationProbability, confidence float64) (Score, error) {
	rank, ok := domain.EntryQualityRank(entryQuality)
	if !ok {
		return Score{}, fmt.Errorf("%w: entry_quality %q is not recognized", ErrInvalidScore, entryQuality)
	}
	for _, p := range []struct {
		name  string
		value float64
	}{
		{"toxic_flow", toxicFlow}, {"liquidity_stressed", liquidityStressed},
		{"continuation_probability", continuationProbability}, {"confidence", confidence},
	} {
		if math.IsNaN(p.value) || p.value < 0 || p.value > 1 {
			return Score{}, fmt.Errorf("%w: %s %v is outside [0, 1]", ErrInvalidScore, p.name, p.value)
		}
	}
	return Score{
		EntryQuality:            float64(rank) / maxEntryQualityRank,
		ToxicFlow:               toxicFlow,
		LiquidityStressed:       liquidityStressed,
		ContinuationProbability: continuationProbability,
		Confidence:              confidence,
	}, nil
}

// Value is the single Jev score feature in [0, 1]: the unweighted mean of the
// five components, with ToxicFlow and LiquidityStressed inverted so that a
// larger value always favours taking the candidate. The weights are
// deliberately equal; a model that needs different weights reads the
// components.
func (s Score) Value() float64 {
	return (s.EntryQuality + (1 - s.ToxicFlow) + (1 - s.LiquidityStressed) + s.ContinuationProbability + s.Confidence) / 5
}

// ScoreOf returns the Score of a persisted Trader decision. ok is false for a
// decision that carries no complete Trader answer (a Scout decision, or a row
// with a missing or out-of-range field): the caller must treat that as "no
// Jev score", not as zero.
func ScoreOf(d domain.JevDecision) (score Score, ok bool) {
	if d.EntryQuality == nil || d.ToxicFlow == nil || d.LiquidityStressed == nil ||
		d.ContinuationProbability == nil || d.Confidence == nil {
		return Score{}, false
	}
	score, err := NewScore(*d.EntryQuality, *d.ToxicFlow, *d.LiquidityStressed, *d.ContinuationProbability, *d.Confidence)
	return score, err == nil
}
