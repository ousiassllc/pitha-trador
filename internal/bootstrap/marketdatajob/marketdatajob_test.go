package marketdatajob

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
)

func marketDataJob(t *testing.T, inst domain.Instrument) jobqueue.Job {
	t.Helper()
	payload, err := json.Marshal(marketDataJobPayload{InstrumentID: inst.ID, Symbol: inst.Symbol})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return jobqueue.Job{PayloadJSON: string(payload)}
}

func TestHandleMarketData_FetchesComputesAndPersistsSnapshot(t *testing.T) {
	env := newTestEnv(t)
	env.Fake.board = marketdata.Board{
		Symbol: "7203", CurrentPrice: 2500, VWAP: 2490, TradingVolume: 1000000, TradingValue: 2.49e9,
	}
	inst := mustCreateInstrument(t, env, "7203")

	if err := env.HandleMarketData(context.Background(), marketDataJob(t, inst)); err != nil {
		t.Fatalf("HandleMarketData: %v", err)
	}

	snaps, err := env.Snapshots.ListByInstrument(context.Background(), inst.ID, 10)
	if err != nil {
		t.Fatalf("ListByInstrument: %v", err)
	}
	if len(snaps) != 1 {
		t.Fatalf("len(snaps) = %d, want 1", len(snaps))
	}
	if snaps[0].Price != 2500.0 {
		t.Errorf("Price = %v, want 2500.0", snaps[0].Price)
	}
	if snaps[0].Feature.VWAP != 2490.0 {
		t.Errorf("Feature.VWAP = %v, want 2490.0", snaps[0].Feature.VWAP)
	}
}

func TestHandleMarketData_ReturnsErrorWithoutSwallowingOnFetchFailure(t *testing.T) {
	env := newTestEnv(t)
	env.Fake.err = marketdata.ErrNoToken
	inst := mustCreateInstrument(t, env, "9999")

	if err := env.HandleMarketData(context.Background(), marketDataJob(t, inst)); !errors.Is(err, marketdata.ErrNoToken) {
		t.Fatalf("HandleMarketData error = %v, want the board failure returned as-is", err)
	}

	snaps, err := env.Snapshots.ListByInstrument(context.Background(), inst.ID, 10)
	if err != nil {
		t.Fatalf("ListByInstrument: %v", err)
	}
	if len(snaps) != 0 {
		t.Errorf("len(snaps) = %d, want 0 (no snapshot persisted on fetch failure)", len(snaps))
	}
}

func TestHandleMarketData_ReturnsErrorOnUnmarshalableJobPayload(t *testing.T) {
	env := newTestEnv(t)

	if err := env.HandleMarketData(context.Background(), jobqueue.Job{PayloadJSON: "not-json"}); err == nil {
		t.Fatal("HandleMarketData: want error for unmarshalable job payload, got nil")
	}
}

func TestHandleFeatureCalc_IsANoOp(t *testing.T) {
	if err := HandleFeatureCalc(context.Background(), jobqueue.Job{PayloadJSON: "{}"}); err != nil {
		t.Errorf("HandleFeatureCalc: %v, want nil", err)
	}
}

// Paper Trading's per-bar step (issue #49): a fresh bar past the
// stop-loss threshold closes the open position.
func TestHandleMarketData_ClosesPaperPositionWhenNewBarHitsStopLoss(t *testing.T) {
	env := newTestEnv(t)
	env.Fake.board = marketdata.Board{
		Symbol: "7203", CurrentPrice: 2400, VWAP: 2450, TradingVolume: 1000000, TradingValue: 2.4e9,
	}
	inst := mustCreateInstrument(t, env, "7203")
	if _, err := env.Execution.Enter(context.Background(), execution.EntryRequest{
		Signal: domain.TradeSignal{
			InstrumentID: inst.ID, Symbol: inst.Symbol, Direction: domain.JevDirectionLong,
			RiskPassed: true, PolicyVersion: "v1",
		},
		Quantity: 100, Price: 2500, Now: tradingHours,
	}); err != nil {
		t.Fatalf("Enter: %v", err)
	}
	position, err := env.Positions.GetOpenByInstrument(context.Background(), inst.ID)
	if err != nil {
		t.Fatalf("GetOpenByInstrument: %v", err)
	}

	if err := env.HandleMarketData(context.Background(), marketDataJob(t, inst)); err != nil {
		t.Fatalf("HandleMarketData: %v", err)
	}

	closed, err := env.Positions.Get(context.Background(), position.ID)
	if err != nil {
		t.Fatalf("Get position: %v", err)
	}
	// 2500 -> 2400 is -4%, past config/risk.yaml-derived stop_loss_pct=0.6.
	if closed.IsOpen() || closed.ExitReason == nil || *closed.ExitReason != domain.ExitReasonStopLoss {
		t.Errorf("position after -4%% bar = %+v, want closed with %q", closed, domain.ExitReasonStopLoss)
	}
}

// FR-ENTRY-8 / issue #512: the stop-loss exit is judged at the Handler's
// clock, not the wall clock - a bar stamped in the 昼休み (11:30-12:30 JST)
// cannot fill, so the position stays open until the next in-session bar.
func TestHandleMarketData_KeepsPaperPositionOpenWhenStopLossBarArrivesAtLunchBreak(t *testing.T) {
	env := newTestEnv(t)
	env.Now = func() time.Time { return time.Date(2026, 9, 29, 12, 0, 0, 0, marketcalendar.JST) }
	env.Fake.board = marketdata.Board{
		Symbol: "7203", CurrentPrice: 2400, VWAP: 2450, TradingVolume: 1000000, TradingValue: 2.4e9,
	}
	inst := mustCreateInstrument(t, env, "7203")
	if _, err := env.Execution.Enter(context.Background(), execution.EntryRequest{
		Signal: domain.TradeSignal{
			InstrumentID: inst.ID, Symbol: inst.Symbol, Direction: domain.JevDirectionLong,
			RiskPassed: true, PolicyVersion: "v1",
		},
		Quantity: 100, Price: 2500, Now: tradingHours,
	}); err != nil {
		t.Fatalf("Enter: %v", err)
	}
	position, err := env.Positions.GetOpenByInstrument(context.Background(), inst.ID)
	if err != nil {
		t.Fatalf("GetOpenByInstrument: %v", err)
	}

	if err := env.HandleMarketData(context.Background(), marketDataJob(t, inst)); err != nil {
		t.Fatalf("HandleMarketData: %v", err)
	}

	after, err := env.Positions.Get(context.Background(), position.ID)
	if err != nil {
		t.Fatalf("Get position: %v", err)
	}
	if !after.IsOpen() {
		t.Errorf("position after a lunch-break stop-loss bar = %+v, want still open", after)
	}
}
