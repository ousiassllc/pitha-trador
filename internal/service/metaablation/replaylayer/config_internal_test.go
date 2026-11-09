package replaylayer

import (
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest/simrisk"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
)

func configInput() Input {
	g := config.PolicyDirectionThresholds{MinProbability: 0.9, MinEntryQuality: domain.JevEntryQualityExceptional, MinContinuationProbability: 0.6, MaxToxicFlow: 0.35, MaxLiquidityStressed: 0.25}
	bars := []domain.Snapshot{{InstrumentID: 1, Timestamp: time.Date(2026, 3, 2, 0, 1, 0, 0, time.UTC), Price: 1000}}
	return Input{
		From: time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 3, 3, 0, 0, 0, 0, time.UTC),
		Instruments: []Instrument{{InstrumentID: 1, Symbol: "A", Snapshots: bars}, {InstrumentID: 2, Symbol: "B", Snapshots: bars}},
		Thresholds:  policy.Thresholds{Policy: config.PolicyConfig{Long: g, Short: g}, MaxSpreadBps: 50, MinTurnover5mJPY: 1e6},
		Risk:        simrisk.Config{StopLossPct: 1, Limits: config.RiskLimits{InitialCapital: 1e7, MaxOpenPositions: 2}},
		Exit:        backtest.ExitRule{StopLossPct: 1, TakeProfitPct: 2, MaxHolding: time.Hour},
		Model:       &metalabel.Model{}, Meta: metalabel.DefaultDecideConfig(),
	}
}

func cellConfigs(t *testing.T, in Input) [][]backtest.RunConfig {
	t.Helper()
	seg := SegmentSpec{From: in.From, To: in.To}
	var out [][]backtest.RunConfig
	for _, c := range metaablation.Cells() {
		out = append(out, in.runConfigs(c, seg, backtest.NewSliceDecisionSource(nil), nil))
	}
	return out
}

// The four cells share the identical eligibility / guard / Risk / capital / exit
// / fill / cost configuration; only the entry rule, the model and the
// entry_quality floor differ, and the old confidence threshold is 0 everywhere.
func TestCells_ShareIdenticalConfigurationAndDifferOnlyInTheAxes(t *testing.T) {
	cells := cellConfigs(t, configInput())
	if err := assertCommon(cells); err != nil {
		t.Fatalf("assertCommon: %v", err)
	}
	for i, c := range metaablation.Cells() {
		for _, cfg := range cells[i] {
			p := cfg.Thresholds
			wantQ := QualityOffFloor
			if c.QOn {
				wantQ = metaablation.QualityCut
			}
			if p.Policy.Long.MinEntryQuality != wantQ || p.Policy.Short.MinEntryQuality != wantQ {
				t.Errorf("%s: entry_quality floor %q/%q, want %q", c.Name(), p.Policy.Long.MinEntryQuality, p.Policy.Short.MinEntryQuality, wantQ)
			}
			if p.Policy.Long.MinProbability != 0 || p.Policy.Short.MinProbability != 0 {
				t.Errorf("%s: min_probability %v/%v, want 0 (the old confidence threshold unused)", c.Name(), p.Policy.Long.MinProbability, p.Policy.Short.MinProbability)
			}
			if c.MOn != (cfg.Entry == backtest.EntryMeta) || c.MOn != (cfg.Models != nil) {
				t.Errorf("%s: entry %q models %v", c.Name(), cfg.Entry, cfg.Models)
			}
			if p.MaxSpreadBps != 50 || p.MinTurnover5mJPY != 1e6 || p.Policy.Long.MinContinuationProbability != 0.6 {
				t.Errorf("%s: shared guard values changed: %+v", c.Name(), p)
			}
		}
	}
}

// An axis-unrelated difference between cells is caught.
func TestAssertCommon_RejectsCellsThatDifferBeyondTheAxes(t *testing.T) {
	for name, mutate := range map[string]func(*backtest.RunConfig){
		"risk":        func(c *backtest.RunConfig) { r := *c.Risk; r.Limits.MaxOpenPositions++; c.Risk = &r },
		"exit":        func(c *backtest.RunConfig) { c.Exit.StopLossPct++ },
		"cost":        func(c *backtest.RunConfig) { c.Cost.Long.SlippageBps = 5 },
		"guard":       func(c *backtest.RunConfig) { c.Thresholds.Policy.Long.MaxToxicFlow = 0.9 },
		"eligibility": func(c *backtest.RunConfig) { c.Thresholds.MaxSpreadBps = 5 },
		"min ev":      func(c *backtest.RunConfig) { c.Meta.MinEVBps = 9 },
		"min prob":    func(c *backtest.RunConfig) { c.Thresholds.Policy.Long.MinProbability = 0.5 },
	} {
		cells := cellConfigs(t, configInput())
		mutate(&cells[3][0])
		if assertCommon(cells) == nil {
			t.Errorf("%s: a differing configuration was not rejected", name)
		}
	}
}

func TestQAxis_OnIsGoodFixed(t *testing.T) {
	if metaablation.QualityCut != domain.JevEntryQualityGood {
		t.Fatalf("QualityCut = %q, want good", metaablation.QualityCut)
	}
	if QualityOffFloor != domain.JevEntryQualityPoor {
		t.Fatalf("Q off floor %q, want the lowest quality", QualityOffFloor)
	}
}

func TestNormalizeGaps_ExpandsToWholeDaysClipsAndMerges(t *testing.T) {
	jstDay := func(d, h int) time.Time { return time.Date(2026, 3, d, h, 0, 0, 0, jst) }
	got := normalizeGaps(jstDay(2, 0), jstDay(10, 0), []Unavailable{
		{From: jstDay(5, 10), To: jstDay(6, 3), Reason: "a"},
		{From: jstDay(6, 0), To: jstDay(7, 0), Reason: "b"}, // touches the first once expanded
		{From: jstDay(1, 0), To: jstDay(3, 0), Reason: "c"}, // clipped to the window
		{From: jstDay(9, 12), To: jstDay(12, 0), Reason: "d"},
	})
	want := []UnavailablePeriod{
		{jstDay(2, 0), jstDay(3, 0), "c"}, {jstDay(5, 0), jstDay(7, 0), "a; b"}, {jstDay(9, 0), jstDay(10, 0), "d"},
	}
	if len(got) != len(want) {
		t.Fatalf("gaps %+v, want %+v", got, want)
	}
	for i := range want {
		if !got[i].From.Equal(want[i].From) || !got[i].To.Equal(want[i].To) || got[i].Reason != want[i].Reason {
			t.Fatalf("gap %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}
