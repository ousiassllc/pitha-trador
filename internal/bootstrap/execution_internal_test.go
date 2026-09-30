package bootstrap

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/paperexec"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

// tradingHours is a fixed instant inside the 前場 (2026-09-29 10:00 JST):
// paper entries are session-gated, so tests must not enter at time.Now().
var tradingHours = time.Date(2026, 9, 29, 10, 0, 0, 0, marketcalendar.JST)

func approvedLongSignal(inst domain.Instrument) domain.TradeSignal {
	return domain.TradeSignal{
		InstrumentID: inst.ID, Symbol: inst.Symbol, Direction: domain.JevDirectionLong,
		RiskPassed: true, PolicyVersion: "v1",
	}
}

func mustOpenPaperPosition(t *testing.T, svc *Services, inst domain.Instrument, price float64) domain.Position {
	t.Helper()
	executor := paperexec.Executor{Engine: svc.Execution, Sizer: svc.Risk}
	snap := domain.Snapshot{InstrumentID: inst.ID, Symbol: inst.Symbol, Price: price, Timestamp: tradingHours}
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
	svc := newTestServices(t)
	inst := mustCreateInstrument(t, svc, "7203")

	position := mustOpenPaperPosition(t, svc, inst, 2500)
	if position.Quantity != 200 || position.EntryPrice != 2500 || position.Side != domain.PositionSideLong {
		t.Fatalf("opened position = %+v, want LONG %d shares at 2500", position, 200)
	}

	// A second approved signal while the position is still open is not an
	// error for the jev-trader job, and must not open a second position.
	snap := domain.Snapshot{InstrumentID: inst.ID, Symbol: inst.Symbol, Price: 2510, Timestamp: tradingHours}
	if err := (paperexec.Executor{Engine: svc.Execution, Sizer: svc.Risk}).ExecuteSignal(context.Background(), approvedLongSignal(inst), snap); err != nil {
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
	svc := newTestServices(t)
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

// Wiring: the Engine BuildServices builds gates entries to 東証立会時間,
// and a signal arriving after the close is dropped rather than retried.
func TestPaperExecutor_DropsEntryOutsideTradingSession(t *testing.T) {
	svc := newTestServices(t)
	inst := mustCreateInstrument(t, svc, "7203")
	night := domain.Snapshot{InstrumentID: inst.ID, Symbol: inst.Symbol, Price: 2500, Timestamp: tradingHours.Add(11 * time.Hour)}
	executor := paperexec.Executor{Engine: svc.Execution, Sizer: svc.Risk}
	if err := executor.ExecuteSignal(context.Background(), approvedLongSignal(inst), night); err != nil {
		t.Fatalf("ExecuteSignal at 21:00 JST = %v, want nil (dropped)", err)
	}
	if _, err := svc.Positions.GetOpenByInstrument(context.Background(), inst.ID); err == nil {
		t.Fatal("a position was opened outside the trading session")
	}
}
