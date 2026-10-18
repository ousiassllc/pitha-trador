package replayflow_test

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/policy/policytest"
)

// metaRamp is a rising 8-bar series with one LONG decision at minute 1.
func metaRamp(confidence float64) ([]domain.Snapshot, []domain.JevDecision) {
	bars := minuteBars(1, base, []float64{1000, 1000, 1004, 1008, 1008, 1008, 1008, 1008}, nil)
	d := longDecision(1, at(1))
	d.Confidence = ptr(confidence)
	return bars, []domain.JevDecision{d}
}

func skippingModels() backtest.ModelSource {
	m := policytest.SkippingModel()
	return fixedModels{&m}
}

func TestDefaultReplay_EntersOnTheModelNotOnJevConfidence(t *testing.T) {
	// 0.50 is below policy.long.min_probability (0.68): the pre-#765 rule would
	// not enter, but the live rule is the model's verdict.
	bars, decisions := metaRamp(0.50)
	cfg := replayConfig(1, "7203", bars, decisions, liveExit())
	if got := replayTrades(t, cfg); len(got) != 1 {
		t.Fatalf("taking model, confidence 0.50: %d trades, want 1 (the model decides, not min_probability)", len(got))
	}

	// ... and a skipping model blocks a confidence of 0.95, which the legacy rule takes.
	bars, decisions = metaRamp(0.95)
	cfg = replayConfig(1, "7203", bars, decisions, liveExit())
	cfg.Models = skippingModels()
	if got := replayTrades(t, cfg); len(got) != 0 {
		t.Fatalf("skipping model, confidence 0.95: %d trades, want 0", len(got))
	}
}

// TestDefaultReplay_DoesNotUseTheLegacyConfidenceRule guarantees the zero-value
// RunConfig.Entry is not the legacy rule: the same history that the legacy mode
// trades (confidence over min_probability) is skipped by the default when the
// model skips, and the legacy rule is reached only by naming it.
func TestDefaultReplay_DoesNotUseTheLegacyConfidenceRule(t *testing.T) {
	if backtest.EntryMeta == backtest.EntryLegacyConfidence {
		t.Fatal("the default entry mode is the legacy rule")
	}
	var zero backtest.RunConfig
	if zero.Entry != backtest.EntryMeta {
		t.Fatalf("zero RunConfig.Entry = %q, want EntryMeta", zero.Entry)
	}

	bars, decisions := metaRamp(0.95)
	cfg := replayConfig(1, "7203", bars, decisions, liveExit())
	cfg.Models = skippingModels()
	if got := replayTrades(t, cfg); len(got) != 0 {
		t.Fatalf("default mode traded a model-skipped candidate (%d trades): the confidence rule leaked in", len(got))
	}

	cfg.Entry = backtest.EntryLegacyConfidence
	if got := replayTrades(t, cfg); len(got) != 1 {
		t.Fatalf("legacy mode: %d trades, want 1 (confidence 0.95 >= 0.68, the model is not consulted)", len(got))
	}

	// The legacy mode also ignores a model that does not exist.
	cfg.Models = nil
	if got := replayTrades(t, cfg); len(got) != 1 {
		t.Fatalf("legacy mode without models: %d trades, want 1", len(got))
	}
}

func TestDefaultReplay_WithoutAnyModelNeverEnters(t *testing.T) {
	bars, decisions := metaRamp(0.95)
	cfg := replayConfig(1, "7203", bars, decisions, liveExit())
	cfg.Models = nil
	if got := replayTrades(t, cfg); len(got) != 0 {
		t.Fatalf("no model: %d trades, want 0 (as live: no active model skips the entry)", len(got))
	}
}

// modelClock is a backtest.ModelSource with no model before activeFrom and the
// taking model from then on; it records the instants it was asked about.
type modelClock struct {
	activeFrom time.Time
	asked      *[]time.Time
}

func (m modelClock) ModelAt(at time.Time) (*metalabel.Model, error) {
	*m.asked = append(*m.asked, at)
	if at.Before(m.activeFrom) {
		return nil, nil
	}
	model := policytest.TakingModel()
	return &model, nil
}

func TestDefaultReplay_UsesTheModelInForceAtTheDecisionTime(t *testing.T) {
	bars := minuteBars(1, base, []float64{1000, 1000, 1004, 1008, 1008, 1008, 1008, 1008, 1008, 1008, 1008, 1008}, nil)
	early, late := longDecision(1, at(1)), longDecision(1, at(8))
	// The model becomes active at minute 5: the decision at minute 1 had none (a
	// model trained later must not score it), the one at minute 8 has.
	var asked []time.Time
	cfg := replayConfig(1, "7203", bars, []domain.JevDecision{early, late}, liveExit())
	cfg.Models = modelClock{activeFrom: at(5), asked: &asked}

	trades := replayTrades(t, cfg)
	if len(trades) != 1 || !trades[0].EntryTimestamp.Equal(at(8)) {
		t.Fatalf("trades = %+v, want one entry at minute 8 only", trades)
	}
	for _, instant := range asked {
		if !instant.Equal(at(1)) && !instant.Equal(at(8)) {
			t.Errorf("ModelAt asked about %v, want only the decisions' own times (minute 1 and 8)", instant)
		}
	}
}

func TestDefaultReplay_UnusableModelSkipsTheEntry(t *testing.T) {
	bars, decisions := metaRamp(0.95)
	cfg := replayConfig(1, "7203", bars, decisions, liveExit())
	cfg.Models = failingModels{}
	if got := replayTrades(t, cfg); len(got) != 0 {
		t.Fatalf("unusable model: %d trades, want 0 (meta_model_unavailable)", len(got))
	}
}

type failingModels struct{}

func (failingModels) ModelAt(time.Time) (*metalabel.Model, error) { return nil, errors.New("damaged") }

func TestDefaultReplay_MinimumEVRaisesTheBar(t *testing.T) {
	bars, decisions := metaRamp(0.95)
	cfg := replayConfig(1, "7203", bars, decisions, liveExit())
	cfg.Meta = metalabel.DefaultDecideConfig()
	cfg.Meta.MinEVBps = 5 // the taking model's EV is +24 bps
	if got := replayTrades(t, cfg); len(got) != 1 {
		t.Fatalf("min EV 5 bps: %d trades, want 1", len(got))
	}
	cfg.Meta.MinEVBps = 50
	if got := replayTrades(t, cfg); len(got) != 0 {
		t.Fatalf("min EV 50 bps: %d trades, want 0 (EV 24 bps does not clear it)", len(got))
	}
}

// The trade carries the model's EV at the entry decision (issue #779), so the
// cost sensitivity table can tell "predicted worth taking" from "realized net
// above zero". The legacy confidence rule consults no model: no EV.
func TestDefaultReplay_TradeCarriesThePredictedEV(t *testing.T) {
	bars, decisions := metaRamp(0.95)
	cfg := replayConfig(1, "7203", bars, decisions, liveExit())
	got := replayTrades(t, cfg)
	// The taking model scores p = sigmoid(2); a win pays 30 bps and a loss costs 20.
	wantEV := 50/(1+math.Exp(-2)) - 20
	if len(got) != 1 || got[0].PredictedEVBps == nil || math.Abs(*got[0].PredictedEVBps-wantEV) > 1e-9 {
		t.Fatalf("trades = %+v, want one trade with predicted EV %v bps", got, wantEV)
	}

	cfg.Entry = backtest.EntryLegacyConfidence
	got = replayTrades(t, cfg)
	if len(got) != 1 || got[0].PredictedEVBps != nil {
		t.Fatalf("legacy trades = %+v, want one trade without a predicted EV", got)
	}
}

func TestDefaultReplay_MissingModelInputSkipsTheEntry(t *testing.T) {
	// No order-flow features on the bars: the model is missing inputs and skips,
	// as live does, rather than scoring a guessed value.
	bars, decisions := metaRamp(0.95)
	for i := range bars {
		bars[i].Feature.OrderFlowFeatures = domain.OrderFlowFeatures{}
	}
	if got := replayTrades(t, replayConfig(1, "7203", bars, decisions, liveExit())); len(got) != 0 {
		t.Fatalf("%d trades, want 0 without the model's inputs", len(got))
	}
}

// TestCompareEntryRules shows the old and the new rule evaluating the same
// synthetic history differently: a falling market where Jev keeps saying LONG
// with high confidence. The legacy rule takes every such candidate and loses;
// a model that learned to skip them avoids it.
func TestCompareEntryRules_NewRuleDiffersFromOldOnSyntheticData(t *testing.T) {
	prices := []float64{1000, 1000, 995, 990, 985, 980, 1000, 1000, 995, 990, 985, 980, 975, 970}
	bars := minuteBars(1, base, prices, nil)
	decisions := []domain.JevDecision{longDecision(1, at(1)), longDecision(1, at(7))}
	cfg := replayConfig(1, "7203", bars, decisions, liveExit())
	cfg.Models = skippingModels()

	cmp, err := backtest.CompareEntryRules(t.Context(), []backtest.RunConfig{cfg}, backtest.Period{Start: at(0), End: at(len(prices))})
	if err != nil {
		t.Fatalf("CompareEntryRules: %v", err)
	}
	if cmp.Legacy.TradeCount != 2 || cmp.Legacy.Expectancy >= 0 {
		t.Fatalf("legacy = %+v, want two losing trades", cmp.Legacy)
	}
	if cmp.Meta.TradeCount != 0 || cmp.Meta.Expectancy != 0 {
		t.Fatalf("meta = %+v, want no trades (the model skips)", cmp.Meta)
	}
	if cmp.ExpectancyDiff() <= 0 {
		t.Fatalf("ExpectancyDiff = %v, want positive: the live rule avoided the legacy rule's losses", cmp.ExpectancyDiff())
	}
}
