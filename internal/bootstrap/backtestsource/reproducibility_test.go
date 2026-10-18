package backtestsource

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest/parity"
	servicemetalabel "github.com/ousiassllc/pitha-trador/internal/service/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/policy/policytest"
)

// storeTakingModel records an active meta-labeling model that takes every
// candidate, trained at trainedAt: the replay then enters on the hard guards
// alone from trainedAt on.
func storeTakingModel(t *testing.T, env testEnv, trainedAt time.Time) {
	t.Helper()
	model := policytest.TakingModel()
	params, err := model.ParamsJSON()
	if err != nil {
		t.Fatalf("ParamsJSON: %v", err)
	}
	if _, err := env.Models.InsertModel(context.Background(), metalabel.Model{
		Version: "taking-" + trainedAt.Format("0102"), Algorithm: servicemetalabel.AlgorithmLogisticRegression,
		LabelVersion: domain.CalibrationLabelVersionTripleBarrier, Status: metalabel.ModelActive,
		TrainedAt: trainedAt, TrainStart: trainedAt, TrainEnd: trainedAt, OOSStart: trainedAt, OOSEnd: trainedAt,
		FeatureNames: model.FeatureNames, ParamsJSON: params,
	}); err != nil {
		t.Fatalf("InsertModel: %v", err)
	}
}

// recordPaperPosition stores a closed Paper position the way Execution leaves it.
func recordPaperPosition(t *testing.T, env testEnv, inst domain.Instrument, openedAt time.Time, side, reason string) {
	t.Helper()
	ctx := context.Background()
	orders := trading.NewOrderRepository(env.Conn)
	insert := func(orderSide string, at time.Time) domain.PaperOrder {
		price := 2000.0
		o, err := orders.Insert(ctx, domain.PaperOrder{
			InstrumentID: inst.ID, Symbol: inst.Symbol, Side: orderSide, OrderType: domain.OrderTypeMarket,
			Quantity: 100, Status: domain.OrderStatusFilled, FilledPrice: &price, SubmittedAt: at,
		})
		if err != nil {
			t.Fatalf("insert order: %v", err)
		}
		return o
	}
	entry := insert(domain.OrderSideBuy, openedAt)
	position, err := env.Positions.Open(ctx, domain.Position{
		InstrumentID: inst.ID, EntryOrderID: entry.ID, Symbol: inst.Symbol, Side: side,
		Quantity: 100, EntryPrice: 2000, CurrentPrice: 2000, OpenedAt: openedAt,
	})
	if err != nil {
		t.Fatalf("Open position: %v", err)
	}
	exit := insert(domain.OrderSideSell, openedAt.Add(time.Minute))
	if _, err := env.Positions.Close(ctx, position.ID, exit.ID, 2001, 100, reason, openedAt.Add(time.Minute)); err != nil {
		t.Fatalf("Close position: %v", err)
	}
}

// TestSource_VerifyReproducibility covers the three outcomes of FR-BT-4's
// check against the DB: no Paper entry in the period (データ無し, not a
// failure), Paper entries the replay reproduces (合格), and Paper entries it
// does not (不合格, with the numbers).
func TestSource_VerifyReproducibility(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	inst := mustCreateInstrument(t, env, "7203")
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	storeTakingModel(t, env, base.Add(-time.Hour))
	recordLiveBars(t, env, inst, base, 30)
	period := backtest.Period{Start: base, End: base.Add(30 * time.Minute)}
	th := parity.Thresholds{MinEntryMatchRate: 0.9, MinExitReasonMatchRate: 0.9, Tolerance: time.Minute}

	report, err := env.Source.VerifyReproducibility(ctx, period, th)
	if err != nil || report.Verdict != parity.VerdictNoData || report.Err() != nil {
		t.Fatalf("no paper entries: report %+v, err %v; want データ無し and no failure", report, err)
	}

	// Paper opened exactly what the replay opens.
	configs, err := env.Source.RunConfigs(ctx, period)
	if err != nil {
		t.Fatalf("RunConfigs: %v", err)
	}
	replayed, err := backtest.ShadowBacktestPortfolioTrades(ctx, configs, period)
	if err != nil || len(replayed) == 0 {
		t.Fatalf("replay = %d trades, err %v; want some", len(replayed), err)
	}
	for _, trade := range replayed {
		recordPaperPosition(t, env, inst, trade.EntryTimestamp, trade.Direction, trade.ExitReason)
	}
	report, err = env.Source.VerifyReproducibility(ctx, period, th)
	if err != nil {
		t.Fatalf("VerifyReproducibility: %v", err)
	}
	if report.Verdict != parity.VerdictPass || report.EntryMatchRate != 1 || report.ExitReasonMatchRate != 1 {
		t.Fatalf("identical entries: %+v, want 合格 with every rate 1", report)
	}
}

func TestSource_VerifyReproducibilityFailsWhenPaperEnteredElsewhere(t *testing.T) {
	env := newTestEnv(t)
	inst := mustCreateInstrument(t, env, "7203")
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	storeTakingModel(t, env, base.Add(-time.Hour))
	recordLiveBars(t, env, inst, base, 30)
	// Paper entered at a minute the replay did not (no decision is open there:
	// the replay is mid-position or out of bars).
	recordPaperPosition(t, env, inst, base.Add(29*time.Minute+30*time.Second), domain.JevDirectionShort, domain.ExitReasonStopLoss)

	period := backtest.Period{Start: base, End: base.Add(30 * time.Minute)}
	report, err := env.Source.VerifyReproducibility(context.Background(), period,
		parity.Thresholds{MinEntryMatchRate: 0.9, MinExitReasonMatchRate: 0.9, Tolerance: time.Second})
	if err != nil {
		t.Fatalf("VerifyReproducibility: %v", err)
	}
	if report.Verdict != parity.VerdictFail || report.Err() == nil || report.EntryMatchRate != 0 {
		t.Fatalf("report = %+v, want 不合格 with entry match rate 0", report)
	}
}

// TestSource_ReplayUsesNoModelTrainedAfterTheDecisions: the walk-forward
// guarantee through the DB - a model trained after the whole period must not
// score it, so the replay (like live then) never entered.
func TestSource_ReplayUsesNoModelTrainedAfterTheDecisions(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	inst := mustCreateInstrument(t, env, "7203")
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	recordLiveBars(t, env, inst, base, 30)
	period := backtest.Period{Start: base, End: base.Add(30 * time.Minute)}
	replay := func() []backtest.Trade {
		configs, err := env.Source.RunConfigs(ctx, period)
		if err != nil {
			t.Fatalf("RunConfigs: %v", err)
		}
		trades, err := backtest.ShadowBacktestPortfolioTrades(ctx, configs, period)
		if err != nil {
			t.Fatalf("replay: %v", err)
		}
		return trades
	}

	if got := replay(); len(got) != 0 {
		t.Fatalf("no model stored: %d trades, want 0", len(got))
	}
	storeTakingModel(t, env, period.End.Add(time.Hour))
	if got := replay(); len(got) != 0 {
		t.Fatalf("model trained after the period: %d trades, want 0 (look-ahead)", len(got))
	}
}
