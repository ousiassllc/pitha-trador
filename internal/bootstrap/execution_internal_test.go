package bootstrap

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
)

func approvedLongSignal(inst domain.Instrument) domain.TradeSignal {
	return domain.TradeSignal{
		InstrumentID: inst.ID, Symbol: inst.Symbol, Direction: domain.JevDirectionLong,
		RiskPassed: true, PolicyVersion: "v1",
	}
}

func mustOpenPaperPosition(t *testing.T, svc *Services, inst domain.Instrument, price float64) domain.Position {
	t.Helper()
	executor := paperExecutor{engine: svc.Execution, sizer: svc.Risk}
	snap := domain.Snapshot{InstrumentID: inst.ID, Symbol: inst.Symbol, Price: price, Timestamp: time.Now().UTC()}
	if err := executor.ExecuteSignal(context.Background(), approvedLongSignal(inst), snap); err != nil {
		t.Fatalf("ExecuteSignal: %v", err)
	}
	position, err := svc.Positions.GetOpenByInstrument(context.Background(), inst.ID)
	if err != nil {
		t.Fatalf("GetOpenByInstrument after ExecuteSignal: %v", err)
	}
	return position
}

func TestPaperExecutor_SizesEntryFromRiskLimitsAndSkipsRepeatEntry(t *testing.T) {
	svc := newTestServices(t, nil)
	inst := mustCreateInstrument(t, svc, "7203")

	position := mustOpenPaperPosition(t, svc, inst, 2500)
	if position.Quantity != 200 || position.EntryPrice != 2500 || position.Side != domain.PositionSideLong {
		t.Fatalf("opened position = %+v, want LONG %d shares at 2500", position, 200)
	}

	// A second approved signal while the position is still open is not an
	// error for the jev-trader job, and must not open a second position.
	snap := domain.Snapshot{InstrumentID: inst.ID, Symbol: inst.Symbol, Price: 2510, Timestamp: time.Now().UTC()}
	if err := (paperExecutor{engine: svc.Execution, sizer: svc.Risk}).ExecuteSignal(context.Background(), approvedLongSignal(inst), snap); err != nil {
		t.Fatalf("second ExecuteSignal = %v, want nil (skipped)", err)
	}
	orders, err := svc.Orders.List(context.Background(), "", 10)
	if err != nil {
		t.Fatalf("List orders: %v", err)
	}
	if len(orders) != 1 {
		t.Errorf("paper orders = %d, want 1 (repeat entry skipped)", len(orders))
	}
}

func TestBuildServices_KillSwitchForceClosesOpenPaperPositions(t *testing.T) {
	svc := newTestServices(t, nil)
	inst := mustCreateInstrument(t, svc, "7203")
	position := mustOpenPaperPosition(t, svc, inst, 2500)

	if _, err := svc.Risk.TriggerKillSwitch(context.Background(), domain.KillReasonUnexpectedPosition, nil); err != nil {
		t.Fatalf("TriggerKillSwitch: %v", err)
	}

	closed, err := svc.Positions.Get(context.Background(), position.ID)
	if err != nil {
		t.Fatalf("Get position: %v", err)
	}
	if closed.IsOpen() || closed.ExitReason == nil || *closed.ExitReason != domain.ExitReasonForceClose {
		t.Errorf("position after kill switch = %+v, want closed with %q", closed, domain.ExitReasonForceClose)
	}
}

func TestHandleMarketData_ClosesPaperPositionWhenNewBarHitsStopLoss(t *testing.T) {
	server := kabuFakeServer(t, map[string]any{
		"Symbol": "7203", "CurrentPrice": 2400.0, "VWAP": 2450.0,
		"TradingVolume": 1000000.0, "TradingValue": 2.4e9,
	})
	defer server.Close()

	svc := newTestServices(t, server)
	inst := mustCreateInstrument(t, svc, "7203")
	position := mustOpenPaperPosition(t, svc, inst, 2500)
	if _, err := svc.MarketData.IssueToken(context.Background()); err != nil {
		t.Fatalf("IssueToken: %v", err)
	}

	payload, _ := json.Marshal(marketDataJobPayload{InstrumentID: inst.ID, Symbol: inst.Symbol})
	if err := svc.handleMarketData(context.Background(), repository.Job{PayloadJSON: string(payload)}); err != nil {
		t.Fatalf("handleMarketData: %v", err)
	}

	closed, err := svc.Positions.Get(context.Background(), position.ID)
	if err != nil {
		t.Fatalf("Get position: %v", err)
	}
	// 2500 -> 2400 is -4%, past config/risk.yaml-derived stop_loss_pct=0.6.
	if closed.IsOpen() || closed.ExitReason == nil || *closed.ExitReason != domain.ExitReasonStopLoss {
		t.Errorf("position after -4%% bar = %+v, want closed with %q", closed, domain.ExitReasonStopLoss)
	}
}
