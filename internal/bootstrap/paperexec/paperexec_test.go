package paperexec_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/paperexec"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

// Issue #685: Enter judges the session at the bar's timestamp, so a stale
// bar stamped inside 前場 must not open a position while the wall clock
// (Config.Now) is outside the session. The Sizer is nil on purpose: the
// wall-clock gate must drop the signal before sizing.
func TestExecuteSignal_DropsStaleBarInsideSessionWhenWallClockOutsideSession(t *testing.T) {
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	instruments := market.NewInstrumentRepository(db)
	inst, err := instruments.Create(context.Background(), domain.Instrument{Symbol: "7203", Name: "Toyota", Market: "TSE Prime", IsActive: true})
	if err != nil {
		t.Fatalf("create instrument: %v", err)
	}
	orders := trading.NewOrderRepository(db)
	positions := trading.NewPositionRepository(db)
	cfg := execution.DefaultConfig()
	cfg.Calendar = marketcalendar.TSE
	barAt := time.Date(2026, 9, 29, 10, 0, 0, 0, marketcalendar.JST) // inside 前場
	cfg.Now = func() time.Time { return barAt.Add(11 * time.Hour) }  // 21:00 JST, next session not open
	engine := execution.NewEngine(execution.Deps{
		Orders: orders, Positions: positions, Snapshots: market.NewSnapshotRepository(db),
		Decisions: judgement.NewDecisionRepository(db), Signals: trading.NewSignalRepository(db), Instruments: instruments,
	}, cfg)

	signal := domain.TradeSignal{InstrumentID: inst.ID, Symbol: "7203", Direction: domain.JevDirectionLong, RiskPassed: true, PolicyVersion: "v1"}
	snap := domain.Snapshot{InstrumentID: inst.ID, Symbol: "7203", Price: 2500, Timestamp: barAt}
	if err := (paperexec.Executor{Engine: engine}).ExecuteSignal(context.Background(), signal, snap); err != nil {
		t.Fatalf("ExecuteSignal = %v, want nil (dropped)", err)
	}
	if _, err := positions.GetOpenByInstrument(context.Background(), inst.ID); err == nil {
		t.Fatal("a position was opened while the wall clock is outside the trading session")
	}
	if got, err := orders.List(context.Background(), "", 10); err != nil || len(got) != 0 {
		t.Fatalf("orders = %v, %v; want none", got, err)
	}
}
