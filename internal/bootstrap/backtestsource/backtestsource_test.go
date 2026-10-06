package backtestsource

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	configdefaults "github.com/ousiassllc/pitha-trador/config"
	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine"
	"github.com/ousiassllc/pitha-trador/internal/service/fillmodel"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
	"github.com/ousiassllc/pitha-trador/internal/service/selfimprove"
)

// testEnv is a Source over a fresh DB with the compiled-in
// strategy.yaml/risk.yaml defaults, plus the repositories tests record
// live history through.
type testEnv struct {
	Conn        *sql.DB
	Config      execution.Config
	Source      *Source
	Instruments *market.InstrumentRepository
	Snapshots   *market.SnapshotRepository
	Decisions   *judgement.DecisionRepository
}

func newTestEnv(t *testing.T) testEnv {
	t.Helper()
	return newTestEnvWith(t, func(*execution.Config) {})
}

// newTestEnvWith is newTestEnv with Paper Trading's execution.Config
// (which a backtest shares: exit rule, fill model, calendar) adjusted by
// tune.
func newTestEnvWith(t *testing.T, tune func(*execution.Config)) testEnv {
	t.Helper()
	conn, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	strategy, err := config.LoadStrategyBytes(configdefaults.DefaultStrategyYAML)
	if err != nil {
		t.Fatalf("LoadStrategyBytes: %v", err)
	}
	riskCfg, err := config.LoadRiskBytes(configdefaults.DefaultRiskYAML)
	if err != nil {
		t.Fatalf("LoadRiskBytes: %v", err)
	}
	execCfg := execution.ConfigFromRiskLimits(riskCfg.Paper)
	tune(&execCfg)
	env := testEnv{
		Conn:        conn,
		Config:      execCfg,
		Instruments: market.NewInstrumentRepository(conn),
		Snapshots:   market.NewSnapshotRepository(conn),
		Decisions:   judgement.NewDecisionRepository(conn),
	}
	runtimePolicy := selfimprove.NewRuntimePolicy(system.NewRuntimeSettingsRepository(conn), judgement.NewProposalRepository(conn), strategy.Policy)
	env.Source = New(env.Instruments, env.Snapshots, env.Decisions, policy.ThresholdsFromStrategy(*strategy), runtimePolicy, execCfg)
	return env
}

func mustCreateInstrument(t *testing.T, env testEnv, symbol string) domain.Instrument {
	t.Helper()
	inst, err := env.Instruments.Create(context.Background(), domain.Instrument{
		Symbol: symbol, Name: symbol + " Inc.", Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatalf("Create instrument %q: %v", symbol, err)
	}
	return inst
}

// recordLiveBars persists n 1-minute bars for inst exactly the way the
// live market-data job does (Feature computed from the
// featureengine.HistoryLookbackBars most recent persisted bars), with a
// steadily rising price and a Jev Trader LONG decision recorded at every
// bar whose response_json clears config/strategy.yaml's LONG thresholds.
func recordLiveBars(t *testing.T, env testEnv, inst domain.Instrument, base time.Time, n int) {
	t.Helper()
	ctx := context.Background()
	spread := 10.0
	response, _ := json.Marshal(map[string]any{
		"entry_quality": domain.JevEntryQualityStrong, "continuation_probability": 0.8,
		"toxic_flow": 0.1, "liquidity_stressed": 0.1,
	})
	direction, confidence := domain.JevDirectionLong, 0.8
	var volume int64
	var turnover float64
	for i := 0; i < n; i++ {
		ts := base.Add(time.Duration(i) * time.Minute)
		price := 2000 * (1 + 0.002*float64(i))
		volume += int64(1000 + (i*i%13)*400)
		turnover += 5_000_000 // cumulative session value; 25M per 5 minutes clears min_turnover_5m_jpy
		history, err := env.Snapshots.ListByInstrument(ctx, inst.ID, featureengine.HistoryLookbackBars)
		if err != nil {
			t.Fatalf("ListByInstrument: %v", err)
		}
		feature := featureengine.Compute(featureengine.Input{
			Timestamp: ts, Current: featureengine.Reading{Price: price, VWAP: price, Volume: volume, Turnover: turnover}, History: history,
		})
		if _, err := env.Snapshots.Insert(ctx, domain.Snapshot{
			InstrumentID: inst.ID, Symbol: inst.Symbol, Timestamp: ts, Price: price,
			SpreadBps: &spread, Volume: volume, Turnover: turnover, Feature: feature,
		}); err != nil {
			t.Fatalf("insert snapshot %d: %v", i, err)
		}
		if _, err := env.Decisions.Insert(ctx, domain.JevDecision{
			InstrumentID: inst.ID, Symbol: inst.Symbol, Timestamp: ts, DecisionType: domain.JevDecisionTypeTrader,
			StateHash: "hash", StateJSON: "{}", QuestionVersion: "v1", ResponseJSON: string(response),
			Direction: &direction, Confidence: &confidence, ModelID: "test-model",
		}); err != nil {
			t.Fatalf("insert trader decision %d: %v", i, err)
		}
	}
}

func TestSource_RunWalkForwardReplaysRecordedHistory(t *testing.T) {
	env := newTestEnv(t)
	inst := mustCreateInstrument(t, env, "7203")
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	recordLiveBars(t, env, inst, base, 90)

	// Start the backtest mid-history so the first bars in range need the
	// warmup bars before them to pass the look-ahead check.
	start := base.Add(30 * time.Minute)
	result, err := env.Source.RunWalkForward(context.Background(), backtest.WalkForwardConfig{
		Start: start, End: start.Add(60 * time.Minute),
		TrainingPeriod: 20 * time.Minute, ValidationPeriod: 10 * time.Minute, ForwardPeriod: 10 * time.Minute,
	})
	if err != nil {
		t.Fatalf("RunWalkForward: %v", err)
	}
	if len(result.Splits) != 3 {
		t.Fatalf("len(Splits) = %d, want 3", len(result.Splits))
	}
	if result.Combined.TradeCount == 0 || result.Combined.Expectancy <= 0 {
		t.Errorf("Combined = %+v, want profitable Forward trades from the recorded LONG decisions on a rising price", result.Combined)
	}
	for i, sp := range result.Splits {
		if sp.Training.TradeCount != 0 {
			t.Errorf("Splits[%d].Training.TradeCount = %d, want 0", i, sp.Training.TradeCount)
		}
	}
}

// TestSource_BacktestFillsMatchPaperExecution is #509's shared-assumption
// check: a backtest trade's entry/exit prices equal what Paper Trading's
// Execution Engine fills for the same bars - including the 9:00 寄り
// being an auction fill, not a ザラ場 fill.
func TestSource_BacktestFillsMatchPaperExecution(t *testing.T) {
	env := newTestEnvWith(t, func(cfg *execution.Config) { cfg.Calendar = marketcalendar.TSE })
	ctx := context.Background()
	inst := mustCreateInstrument(t, env, "7203")
	base := time.Date(2026, 9, 1, 9, 0, 0, 0, marketcalendar.JST) // 火曜 9:00 JST
	recordLiveBars(t, env, inst, base, 30)

	period := backtest.Period{Start: base, End: base.Add(30 * time.Minute)}
	configs, err := env.Source.RunConfigs(ctx, period)
	if err != nil || len(configs) != 1 {
		t.Fatalf("RunConfigs = %d configs, err %v; want 1", len(configs), err)
	}
	trades, err := backtest.ShadowBacktestTrades(ctx, configs[0], period)
	if err != nil || len(trades) == 0 {
		t.Fatalf("ShadowBacktestTrades = %d trades, err %v; want at least 1", len(trades), err)
	}
	trade := trades[0]

	engine := execution.NewEngine(execution.Deps{
		Orders: trading.NewOrderRepository(env.Conn), Positions: trading.NewPositionRepository(env.Conn),
	}, env.Config)
	entrySnap := snapshotAt(t, env, inst, trade.EntryTimestamp)
	entry, err := engine.Enter(ctx, execution.EntryRequest{
		Signal:   domain.TradeSignal{InstrumentID: inst.ID, Symbol: inst.Symbol, Direction: trade.Direction, RiskPassed: true},
		Quantity: 100, Price: entrySnap.Price, Book: fillmodel.BookOf(entrySnap), Now: entrySnap.Timestamp,
	})
	if err != nil {
		t.Fatalf("Enter: %v", err)
	}
	if entry.Position.EntryPrice != trade.EntryPrice {
		t.Errorf("paper entry %v != backtest entry %v", entry.Position.EntryPrice, trade.EntryPrice)
	}
	if trade.EntryPrice == entrySnap.Price {
		t.Errorf("entry filled at the signal price %v: no execution cost applied", entrySnap.Price)
	}

	exitSnap := snapshotAt(t, env, inst, trade.ExitTimestamp)
	closed, err := engine.Close(ctx, entry.Position.ID, domain.ExitReasonManual, exitSnap.Price, fillmodel.BookOf(exitSnap), exitSnap.Timestamp)
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	exitOrder, err := trading.NewOrderRepository(env.Conn).Get(ctx, *closed.ExitOrderID)
	if err != nil {
		t.Fatalf("Get exit order: %v", err)
	}
	if *exitOrder.FilledPrice != trade.ExitPrice {
		t.Errorf("paper exit %v != backtest exit %v", *exitOrder.FilledPrice, trade.ExitPrice)
	}
}

func snapshotAt(t *testing.T, env testEnv, inst domain.Instrument, ts time.Time) domain.Snapshot {
	t.Helper()
	snaps, err := env.Snapshots.ListByInstrumentRange(context.Background(), inst.ID, ts, ts.Add(time.Minute))
	if err != nil || len(snaps) != 1 {
		t.Fatalf("snapshot at %v = %d rows, err %v; want 1", ts, len(snaps), err)
	}
	return snaps[0]
}

// TestSource_ForEachRunConfigReadsSnapshotsWithoutRawData is issue #597:
// the replay never reads raw_data_json, so neither the in-range bars nor
// the warmup bars carry it, and the stored rows really do have it.
func TestSource_ForEachRunConfigReadsSnapshotsWithoutRawData(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	inst := mustCreateInstrument(t, env, "7203")
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	recordLiveBars(t, env, inst, base, 40)
	if _, err := env.Conn.ExecContext(ctx, `UPDATE market_snapshots SET raw_data_json = '{"board":"large"}'`); err != nil {
		t.Fatalf("seed raw_data_json: %v", err)
	}
	if got := snapshotAt(t, env, inst, base); got.RawDataJSON == "" {
		t.Fatal("fixture rows have no raw_data_json; the exclusion would be vacuous")
	}

	period := backtest.Period{Start: base.Add(20 * time.Minute), End: base.Add(40 * time.Minute)}
	var seen []backtest.RunConfig
	err := env.Source.ForEachRunConfig(ctx, period, func(cfg backtest.RunConfig) error {
		seen = append(seen, cfg)
		return nil
	})
	if err != nil || len(seen) != 1 {
		t.Fatalf("ForEachRunConfig = %d configs, err %v; want 1", len(seen), err)
	}
	cfg := seen[0]
	if cfg.WarmupBars == 0 || len(cfg.Snapshots) <= cfg.WarmupBars {
		t.Fatalf("WarmupBars = %d of %d snapshots; want warmup and in-range bars", cfg.WarmupBars, len(cfg.Snapshots))
	}
	for i, s := range cfg.Snapshots {
		if s.RawDataJSON != "" {
			t.Fatalf("Snapshots[%d].RawDataJSON = %q, want empty", i, s.RawDataJSON)
		}
	}
}

func TestSource_ForEachRunConfigStopsOnCallbackError(t *testing.T) {
	env := newTestEnv(t)
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for _, sym := range []string{"7203", "6758"} {
		recordLiveBars(t, env, mustCreateInstrument(t, env, sym), base, 5)
	}
	stop := errors.New("stop")

	calls := 0
	err := env.Source.ForEachRunConfig(context.Background(), backtest.Period{Start: base, End: base.Add(time.Hour)}, func(backtest.RunConfig) error {
		calls++
		return stop
	})

	if !errors.Is(err, stop) || calls != 1 {
		t.Errorf("err = %v after %d calls, want the callback error after 1 call", err, calls)
	}
}
