// Package labtest holds the test doubles of the backtest lab and of the MCP's
// backtest tools (issue #1086): a ConfigSource over small synthetic bars that
// the real replay engine runs, and one that blocks until released. Tests only.
package labtest

import (
	"context"
	"sync"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
	"github.com/ousiassllc/pitha-trador/internal/service/policy/policytest"
)

// Day1 and Day2 are the starts (09:00 JST) of the two trading days of Configs.
var (
	Day1 = time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	Day2 = time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
)

// Period covers both days (JST dates 2026-10-05 and 2026-10-06).
func Period() backtest.Period {
	return backtest.Period{Start: time.Date(2026, 10, 4, 15, 0, 0, 0, time.UTC), End: time.Date(2026, 10, 6, 15, 0, 0, 0, time.UTC)}
}

// barsPerDay is the bars per day of each instrument.
const barsPerDay = 16

type fixedModels struct{ model *metalabel.Model }

func (f fixedModels) ModelAt(time.Time) (*metalabel.Model, error) { return f.model, nil }

// Configs returns two instruments over two days with a Jev LONG decision at
// every bar: 7203 climbs 0.5% a bar, 6758 falls 0.5% a bar, so the first wins
// and the second loses whatever the exit; the exit is TP 1% / SL 5% / 10
// minutes, a taking meta model decides the entries, and no Risk Engine sizes
// the trades. Every call builds fresh values.
func Configs() []backtest.RunConfig {
	return []backtest.RunConfig{
		config1(1, "7203", 2000, 1.005),
		config1(2, "6758", 3000, 0.995),
	}
}

func config1(id int64, symbol string, price, mult float64) backtest.RunConfig {
	var bars []domain.Snapshot
	var decisions []domain.JevDecision
	for _, day := range []time.Time{Day1, Day2} {
		p := price
		for i := range barsPerDay {
			ts := day.Add(time.Duration(i) * time.Minute)
			feature := featureengine.Compute(featureengine.Input{
				Timestamp: ts, Current: featureengine.Reading{Price: p, VWAP: p},
				History: bars[max(0, len(bars)-featureengine.HistoryLookbackBars):],
			})
			feature.OrderFlowFeatures = policytest.Feature().OrderFlowFeatures
			spread := 10.0
			bars = append(bars, domain.Snapshot{InstrumentID: id, Timestamp: ts, Price: p, SpreadBps: &spread, Feature: feature})
			if i > 0 { // the first bar of a day has no 1-minute return, a model input
				decisions = append(decisions, longDecision(id, ts))
			}
			p *= mult
		}
	}
	thresholds := policy.Thresholds{
		Policy: config.PolicyConfig{Long: config.PolicyDirectionThresholds{
			MinProbability: 0.68, MinEntryQuality: domain.JevEntryQualityStrong,
			MinContinuationProbability: 0.60, MaxToxicFlow: 0.35, MaxLiquidityStressed: 0.25,
		}},
		MaxSpreadBps: 50,
	}
	thresholds.Policy.Short = thresholds.Policy.Long
	model := policytest.TakingModel()
	return backtest.RunConfig{
		InstrumentID: id, Symbol: symbol, Snapshots: bars, Decisions: backtest.NewSliceDecisionSource(decisions),
		Models: fixedModels{&model}, Thresholds: thresholds,
		Exit: backtest.ExitRule{StopLossPct: 5, TakeProfitPct: 1.0, MaxHolding: 10 * time.Minute},
	}
}

func longDecision(id int64, ts time.Time) domain.JevDecision {
	long, strong := domain.JevDirectionLong, domain.JevEntryQualityStrong
	conf, cont, toxic, stress := 0.80, 0.70, 0.10, 0.10
	return domain.JevDecision{
		InstrumentID: id, Timestamp: ts, Direction: &long, EntryQuality: &strong,
		Confidence: &conf, ContinuationProbability: &cont, ToxicFlow: &toxic, LiquidityStressed: &stress,
	}
}

// Source is a lab.ConfigSource that returns Configs, counts its loads and
// remembers the last period and replay mode it was asked for.
type Source struct {
	mu     sync.Mutex
	loads  int
	Period backtest.Period
}

// RunConfigs implements backtestlab.ConfigSource.
func (s *Source) RunConfigs(_ context.Context, period backtest.Period) ([]backtest.RunConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loads++
	s.Period = period
	return Configs(), nil
}

// Loads is how many times RunConfigs was called.
func (s *Source) Loads() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loads
}

// Blocking is a ConfigSource whose RunConfigs signals Entered and then waits
// for Release (or ctx): a run that is in flight for as long as the test wants,
// with no sleeping. Close Release to let it continue with Configs.
type Blocking struct {
	Entered chan struct{}
	Release chan struct{}
}

// NewBlocking returns a Blocking with unbuffered-by-need channels.
func NewBlocking() *Blocking {
	return &Blocking{Entered: make(chan struct{}, 8), Release: make(chan struct{})}
}

// RunConfigs implements backtestlab.ConfigSource.
func (b *Blocking) RunConfigs(ctx context.Context, _ backtest.Period) ([]backtest.RunConfig, error) {
	b.Entered <- struct{}{}
	select {
	case <-b.Release:
		return Configs(), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
