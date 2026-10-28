package scoreflow_test

import (
	"errors"
	"math"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
)

func TestNewScore_ScalesEntryQuality(t *testing.T) {
	cases := []struct {
		quality string
		want    float64
	}{
		{domain.JevEntryQualityPoor, 0},
		{domain.JevEntryQualityFair, 0.25},
		{domain.JevEntryQualityGood, 0.5},
		{domain.JevEntryQualityStrong, 0.75},
		{domain.JevEntryQualityExceptional, 1},
	}
	for _, tc := range cases {
		s, err := jev.NewScore(tc.quality, 0.2, 0.4, 0.6, 0.8)
		if err != nil {
			t.Fatalf("NewScore(%s): %v", tc.quality, err)
		}
		if s.EntryQuality != tc.want {
			t.Errorf("%s: EntryQuality = %v, want %v", tc.quality, s.EntryQuality, tc.want)
		}
		if s.ToxicFlow != 0.2 || s.LiquidityStressed != 0.4 || s.ContinuationProbability != 0.6 || s.Confidence != 0.8 {
			t.Errorf("%s: components not carried over: %+v", tc.quality, s)
		}
	}
}

func TestScore_ValueInvertsRiskComponents(t *testing.T) {
	best, _ := jev.NewScore(domain.JevEntryQualityExceptional, 0, 0, 1, 1)
	if got := best.Value(); got != 1 {
		t.Errorf("best Value = %v, want 1", got)
	}
	worst, _ := jev.NewScore(domain.JevEntryQualityPoor, 1, 1, 0, 0)
	if got := worst.Value(); got != 0 {
		t.Errorf("worst Value = %v, want 0", got)
	}
	mid, _ := jev.NewScore(domain.JevEntryQualityGood, 0.2, 0.4, 0.6, 0.8)
	// (0.5 + 0.8 + 0.6 + 0.6 + 0.8) / 5
	if got, want := mid.Value(), 0.66; math.Abs(got-want) > 1e-12 {
		t.Errorf("mid Value = %v, want %v", got, want)
	}
	riskier, _ := jev.NewScore(domain.JevEntryQualityGood, 0.9, 0.4, 0.6, 0.8)
	if riskier.Value() >= mid.Value() {
		t.Errorf("higher toxic_flow must lower Value: %v >= %v", riskier.Value(), mid.Value())
	}
}

func TestNewScore_RejectsInvalidAnswers(t *testing.T) {
	cases := map[string]func() (jev.Score, error){
		"unknown quality":  func() (jev.Score, error) { return jev.NewScore("great", 0, 0, 0, 0) },
		"empty quality":    func() (jev.Score, error) { return jev.NewScore("", 0, 0, 0, 0) },
		"toxic above 1":    func() (jev.Score, error) { return jev.NewScore("good", 1.1, 0, 0, 0) },
		"stress negative":  func() (jev.Score, error) { return jev.NewScore("good", 0, -0.1, 0, 0) },
		"continuation NaN": func() (jev.Score, error) { return jev.NewScore("good", 0, 0, math.NaN(), 0) },
		"confidence +Inf":  func() (jev.Score, error) { return jev.NewScore("good", 0, 0, 0, math.Inf(1)) },
	}
	for name, f := range cases {
		if _, err := f(); !errors.Is(err, jev.ErrInvalidScore) {
			t.Errorf("%s: error = %v, want ErrInvalidScore", name, err)
		}
	}
}

func TestScoreOf(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	q := domain.JevEntryQualityStrong
	d := domain.JevDecision{
		DecisionType: domain.JevDecisionTypeTrader, EntryQuality: &q,
		ToxicFlow: f(0.1), LiquidityStressed: f(0.2), ContinuationProbability: f(0.7), Confidence: f(0.9),
	}
	s, ok := jev.ScoreOf(d)
	if !ok || s.EntryQuality != 0.75 || s.Confidence != 0.9 {
		t.Fatalf("ScoreOf() = %+v, %v", s, ok)
	}

	// A missing field means no score, not a zero.
	missing := d
	missing.ToxicFlow = nil
	if _, ok := jev.ScoreOf(missing); ok {
		t.Error("a decision without toxic_flow must have no score")
	}
	if _, ok := jev.ScoreOf(domain.JevDecision{DecisionType: domain.JevDecisionTypeScout}); ok {
		t.Error("a scout decision must have no score")
	}
	bad := d
	bad.Confidence = f(2)
	if _, ok := jev.ScoreOf(bad); ok {
		t.Error("an out-of-range field must have no score")
	}
}

func TestTraderResponse_Score(t *testing.T) {
	resp := jev.TraderResponse{
		Direction: domain.JevDirectionLong, Regime: domain.JevRegimeTrend, EntryQuality: domain.JevEntryQualityExceptional,
		ToxicFlow: 0, LiquidityStressed: 0, ContinuationProbability: 1, Confidence: 1,
	}
	s, err := resp.Score()
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if s.Value() != 1 {
		t.Errorf("best answer Value = %v, want 1", s.Value())
	}
	// Direction is the primary signal, not part of the score.
	resp.Direction = domain.JevDirectionShort
	if s2, _ := resp.Score(); s2 != s {
		t.Errorf("score depends on direction: %+v vs %+v", s2, s)
	}
	if _, err := (jev.TraderResponse{EntryQuality: "good", ToxicFlow: 1.5}).Score(); err == nil {
		t.Fatal("Score accepted toxic_flow 1.5")
	}
}
