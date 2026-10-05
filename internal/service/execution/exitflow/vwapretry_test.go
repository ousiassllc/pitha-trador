package exitflow_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/service/fillmodel"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

// vwapCrossEngine is an Engine whose only enabled exit is VWAP逆クロス,
// holding one LONG position of 100 shares opened at 2000 at opened.
func vwapCrossEngine(t *testing.T, cfg execution.Config, opened time.Time) testEngine {
	t.Helper()
	cfg.Fill = fillmodel.Model{}
	cfg.StopLossPct, cfg.TakeProfitPct, cfg.TrailingStopPct, cfg.MaxHoldingMinutes = 0, 0, 0, 0
	te := newTestEngine(t, cfg)
	if _, err := te.engine.Enter(context.Background(), execution.EntryRequest{
		Signal: longSignal(te.instrument.ID), Quantity: 100, Price: 2000, Now: opened,
	}); err != nil {
		t.Fatalf("Enter: %v", err)
	}
	return te
}

func vwapSnapshot(instrumentID int64, price, vwap float64, at time.Time) domain.Snapshot {
	snap := snapshotAt(instrumentID, price, at)
	snap.Feature.VWAP = vwap
	return snap
}

// Regression test for issue #524: the VWAP baseline must not advance past
// an exit that Close failed to execute, or the still-adverse next
// evaluation is mistaken for "already crossed" and never exits.
func TestEngine_OnSnapshot_VWAPCrossExitIsRetriedAfterCloseFailure(t *testing.T) {
	opened := time.Date(2026, 9, 28, 1, 0, 0, 0, time.UTC)
	te := vwapCrossEngine(t, execution.DefaultConfig(), opened)
	ctx, id := context.Background(), te.instrument.ID

	// Favorable side of VWAP: no exit, baseline recorded.
	if result, err := te.engine.OnSnapshot(ctx, vwapSnapshot(id, 2005, 2000, opened.Add(time.Minute))); err != nil || result.Exited {
		t.Fatalf("favorable OnSnapshot = %+v, %v; want position held", result, err)
	}

	// Crossing to the adverse side while the exit order cannot be written.
	if _, err := te.db.Exec(`CREATE TRIGGER orders_busy BEFORE INSERT ON paper_orders BEGIN SELECT RAISE(ABORT, 'database is locked'); END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	if _, err := te.engine.OnSnapshot(ctx, vwapSnapshot(id, 1995, 2000, opened.Add(2*time.Minute))); err == nil {
		t.Fatal("OnSnapshot with a failing Close returned nil error")
	}
	if _, err := te.db.Exec(`DROP TRIGGER orders_busy`); err != nil {
		t.Fatalf("drop trigger: %v", err)
	}

	// Still adverse on the next bar: the cross is judged again and exits.
	result, err := te.engine.OnSnapshot(ctx, vwapSnapshot(id, 1995, 2000, opened.Add(3*time.Minute)))
	if err != nil || !result.Exited {
		t.Fatalf("retry OnSnapshot = %+v, %v; want the vwap_cross exit", result, err)
	}
	if result.Position == nil || result.Position.ExitReason == nil || *result.Position.ExitReason != domain.ExitReasonVWAPCross {
		t.Errorf("closed position = %+v, want closed with %q", result.Position, domain.ExitReasonVWAPCross)
	}
}

// Regression test for issue #524: an exit deferred by the 昼休み
// (ErrOutsideTradingSession) is judged again at the next in-session bar.
func TestEngine_OnSnapshot_VWAPCrossExitIsRetriedAfterLunchBreak(t *testing.T) {
	cfg := execution.DefaultConfig()
	cfg.Calendar = marketcalendar.Calendar{}
	jst := func(h, m int) time.Time { return time.Date(2026, 9, 28, h, m, 0, 0, marketcalendar.JST) }
	te := vwapCrossEngine(t, cfg, jst(10, 0))
	ctx, id := context.Background(), te.instrument.ID

	if result, err := te.engine.OnSnapshot(ctx, vwapSnapshot(id, 2005, 2000, jst(10, 1))); err != nil || result.Exited {
		t.Fatalf("favorable OnSnapshot = %+v, %v; want position held", result, err)
	}
	result, err := te.engine.OnSnapshot(ctx, vwapSnapshot(id, 1995, 2000, jst(11, 35)))
	if err != nil || result.Exited || result.Position == nil {
		t.Fatalf("lunch-break OnSnapshot = %+v, %v; want the exit deferred with the position open", result, err)
	}
	result, err = te.engine.OnSnapshot(ctx, vwapSnapshot(id, 1995, 2000, jst(12, 35)))
	if err != nil || !result.Exited {
		t.Fatalf("afternoon OnSnapshot = %+v, %v; want the vwap_cross exit", result, err)
	}
}
