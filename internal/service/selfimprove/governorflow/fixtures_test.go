// Package governorflow_test holds the Governor/RuntimePolicy tests of
// selfimprove. They only use selfimprove's exported API and live in their
// own directory to keep internal/service/selfimprove under the linterly
// directory line budget.
package governorflow_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
)

func ptr[T any](v T) *T { return &v }

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// governorFixtures wires a Governor against a single fresh test
// database's real ProposalRepository/RuntimeSettingsRepository/
// PositionRepository, with one instrument already created.
type governorFixtures struct {
	proposals  *judgement.ProposalRepository
	settings   *system.RuntimeSettingsRepository
	positions  *trading.PositionRepository
	orders     *trading.OrderRepository
	instrument domain.Instrument
}

func newGovernorFixtures(t *testing.T) governorFixtures {
	t.Helper()
	db := newTestDB(t)
	instruments := market.NewInstrumentRepository(db)
	inst, err := instruments.Create(context.Background(), domain.Instrument{
		Symbol: "7203", Name: "トヨタ自動車", Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatalf("create instrument: %v", err)
	}
	return governorFixtures{
		proposals:  judgement.NewProposalRepository(db),
		settings:   system.NewRuntimeSettingsRepository(db),
		positions:  trading.NewPositionRepository(db),
		orders:     trading.NewOrderRepository(db),
		instrument: inst,
	}
}

// baselinePolicyConfig mirrors internal/service/backtest's own
// testThresholds fixture (runner_test.go): the LONG boundary
// uptrendRunConfig's decision comfortably clears at MinProbability 0.60.
func baselinePolicyConfig(minProbability float64) config.PolicyConfig {
	return config.PolicyConfig{
		Long: config.PolicyDirectionThresholds{
			MinProbability: minProbability, MinEntryQuality: domain.JevEntryQualityStrong,
			MinContinuationProbability: 0.60, MaxToxicFlow: 0.35, MaxLiquidityStressed: 0.25,
		},
		Short: config.PolicyDirectionThresholds{
			MinProbability: 0.60, MinEntryQuality: domain.JevEntryQualityStrong,
			MinContinuationProbability: 0.60, MaxToxicFlow: 0.35, MaxLiquidityStressed: 0.25,
		},
	}
}

// rampBars/longDecision mirror internal/service/backtest's own
// runner_test.go fixtures (unexported there, so duplicated here): a
// look-ahead-safe steady uptrend with a fixed 0.80-confidence LONG
// decision at every bar.
func rampBars(instrumentID int64, base time.Time, n int, startPrice, priceMultiplier, spreadBps float64) []domain.Snapshot {
	bars := make([]domain.Snapshot, 0, n)
	price := startPrice
	for i := 0; i < n; i++ {
		ts := base.Add(time.Duration(i) * time.Minute)
		feature := featureengine.Compute(featureengine.Input{
			Timestamp: ts,
			Current:   featureengine.Reading{Price: price, VWAP: price},
			History:   bars,
		})
		bars = append(bars, domain.Snapshot{
			InstrumentID: instrumentID, Timestamp: ts, Price: price,
			SpreadBps: ptr(spreadBps), Feature: feature,
		})
		price *= priceMultiplier
	}
	return bars
}

func longDecision(instrumentID int64, ts time.Time) domain.JevDecision {
	return domain.JevDecision{
		InstrumentID: instrumentID, Timestamp: ts,
		Direction: ptr(domain.JevDirectionLong), EntryQuality: ptr(domain.JevEntryQualityStrong),
		Confidence: ptr(0.80), ContinuationProbability: ptr(0.70),
		ToxicFlow: ptr(0.10), LiquidityStressed: ptr(0.10),
	}
}

// fakeShadowBacktestSource yields the same []backtest.RunConfig every
// time, regardless of the requested period (the fixture's own Snapshots
// already cover the period tests fix via selfimprove.WithNow).
type fakeShadowBacktestSource struct {
	configs []backtest.RunConfig
}

func (f fakeShadowBacktestSource) ForEachRunConfig(_ context.Context, _ backtest.Period, fn func(backtest.RunConfig) error) error {
	for _, cfg := range f.configs {
		if err := fn(cfg); err != nil {
			return err
		}
	}
	return nil
}

// uptrendInstrumentCopies is how many identical RunConfigs newUptrendSource
// returns.
const uptrendInstrumentCopies = 5

// newUptrendSource builds a fakeShadowBacktestSource whose
// RunConfigs' Thresholds.Policy are baseline: a 10-bar steady uptrend with
// a LONG decision every bar (0.80 confidence), profitable under any
// MinProbability <= 0.80.
func newUptrendSource(instrumentID int64, base time.Time, baseline config.PolicyConfig) fakeShadowBacktestSource {
	bars := rampBars(instrumentID, base, 10, 2000, 1.01, 10)
	decisions := make([]domain.JevDecision, 0, len(bars))
	for _, b := range bars {
		decisions = append(decisions, longDecision(instrumentID, b.Timestamp))
	}
	cfg := backtest.RunConfig{
		InstrumentID: instrumentID,
		Symbol:       "7203",
		Snapshots:    bars,
		Decisions:    backtest.NewSliceDecisionSource(decisions),
		Thresholds:   policy.Thresholds{Policy: baseline, MaxSpreadBps: 50},
		Exit:         backtest.ExitRule{StopLossPct: 5, TakeProfitPct: 1.0, MaxHolding: 10 * time.Minute},
	}
	// Replicate the instrument so each pass yields at least
	// assist.MinBacktestTrades trades (one copy trades only a few times
	// inside the 10-minute fixture window).
	configs := make([]backtest.RunConfig, uptrendInstrumentCopies)
	for i := range configs {
		configs[i] = cfg
	}
	return fakeShadowBacktestSource{configs: configs}
}

// closePosition opens then immediately closes one position at closedAt
// with the given realized PnL, for FR-SELFIMPROVE-6's realized
// Expectancy tracking fixtures.
func (f governorFixtures) closePosition(t *testing.T, closedAt time.Time, realizedPnL float64) {
	t.Helper()
	ctx := context.Background()
	entry, err := f.orders.Insert(ctx, domain.PaperOrder{
		InstrumentID: f.instrument.ID, Symbol: f.instrument.Symbol, Side: domain.OrderSideBuy,
		OrderType: domain.OrderTypeMarket, Quantity: 100, Status: domain.OrderStatusPending, SubmittedAt: closedAt,
	})
	if err != nil {
		t.Fatalf("insert entry order: %v", err)
	}
	entry, err = f.orders.Fill(ctx, entry.ID, 2100.0, 0, nil, closedAt)
	if err != nil {
		t.Fatalf("fill entry order: %v", err)
	}
	opened, err := f.positions.Open(ctx, domain.Position{
		InstrumentID: f.instrument.ID, EntryOrderID: entry.ID, Symbol: f.instrument.Symbol,
		Side: domain.PositionSideLong, Quantity: 100, EntryPrice: 2100, CurrentPrice: 2100, OpenedAt: closedAt,
	})
	if err != nil {
		t.Fatalf("open position: %v", err)
	}
	exit, err := f.orders.Insert(ctx, domain.PaperOrder{
		InstrumentID: f.instrument.ID, Symbol: f.instrument.Symbol, Side: domain.OrderSideSell,
		OrderType: domain.OrderTypeMarket, Quantity: 100, Status: domain.OrderStatusFilled, SubmittedAt: closedAt,
	})
	if err != nil {
		t.Fatalf("insert exit order: %v", err)
	}
	if _, err := f.positions.Close(ctx, opened.ID, exit.ID, 2100+realizedPnL, realizedPnL, domain.ExitReasonManual, closedAt); err != nil {
		t.Fatalf("close position: %v", err)
	}
}
