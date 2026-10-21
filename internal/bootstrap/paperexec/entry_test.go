package paperexec_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap"
	"github.com/ousiassllc/pitha-trador/internal/bootstrap/paperexec"
	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

// entryAt is inside the 前場 (2026-09-29 10:00 JST).
var entryAt = time.Date(2026, 9, 29, 10, 0, 0, 0, marketcalendar.JST)

// paperServices builds the real Paper wiring with the roomy 30M-yen limits (2%
// per symbol: 600,000 yen) and an execution clock inside the session.
func paperServices(t *testing.T) (*bootstrap.Services, domain.Instrument) {
	t.Helper()
	state, err := bootstrap.Run(bootstrap.Config{DBPath: filepath.Join(t.TempDir(), "pitha.db")})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	t.Cleanup(func() { _ = state.Close() })
	state.Risk.Paper.InitialCapital, state.Risk.Paper.MaxPositionPerSymbolPct, state.Risk.Paper.MaxTotalExposurePct = 30_000_000, 2, 20
	svc := bootstrap.BuildServices(state, config.Secrets{KabuAPIPassword: "test-password", JevBaseURL: "http://127.0.0.1:1"},
		bootstrap.WithExecutionClock(func() time.Time { return entryAt.Add(time.Minute) }))
	inst, err := svc.Instruments.Create(context.Background(), domain.Instrument{Symbol: "7203", Name: "Toyota", Market: "TSE Prime", IsActive: true})
	if err != nil {
		t.Fatalf("create instrument: %v", err)
	}
	return svc, inst
}

func storeQuote(t *testing.T, svc *bootstrap.Services, inst domain.Instrument, price float64) {
	t.Helper()
	if _, err := svc.Snapshots.Insert(context.Background(), domain.Snapshot{
		InstrumentID: inst.ID, Symbol: inst.Symbol, Timestamp: entryAt, Price: price, SpreadBps: new(float64), RawDataJSON: "{}",
	}); err != nil {
		t.Fatalf("insert snapshot: %v", err)
	}
}

func execute(t *testing.T, svc *bootstrap.Services, inst domain.Instrument, price float64) {
	t.Helper()
	signal := domain.TradeSignal{InstrumentID: inst.ID, Symbol: inst.Symbol, Direction: domain.JevDirectionLong, RiskPassed: true, PolicyVersion: "v1"}
	snap := domain.Snapshot{InstrumentID: inst.ID, Symbol: inst.Symbol, Price: price, Timestamp: entryAt}
	if err := (paperexec.Executor{Engine: svc.Execution, Risk: svc.Risk}).ExecuteSignal(context.Background(), signal, snap); err != nil {
		t.Fatalf("ExecuteSignal = %v, want nil", err)
	}
}

// Issue #840: Enter gets the quantity Risk Engine's Check judged the
// candidate at and does not size the order again. The judgement reads the
// latest stored quote (2500: 200 shares); the snapshot handed to
// ExecuteSignal is priced at 1000, where sizing again would give 600 shares.
func TestExecuteSignal_EntersWithTheQuantityCheckSettledOn(t *testing.T) {
	svc, inst := paperServices(t)
	storeQuote(t, svc, inst, 2500)
	if got := svc.Risk.PositionSize(context.Background(), 1000); got != 600 {
		t.Fatalf("PositionSize(1000) = %d, want 600 (sizing again must differ from Check's 200)", got)
	}

	execute(t, svc, inst, 1000)
	position, err := svc.Positions.GetOpenByInstrument(context.Background(), inst.ID)
	if err != nil {
		t.Fatalf("GetOpenByInstrument: %v", err)
	}
	if position.Quantity != 200 {
		t.Fatalf("entered %d shares, want 200 (the quantity Check settled on, not 600 from sizing again)", position.Quantity)
	}
}

// A candidate Risk Engine rejects at entry time (here: no stored quote to
// price it) opens nothing and is not an error for the job.
func TestExecuteSignal_SkipsEntryRejectedByRiskEngineAtEntry(t *testing.T) {
	svc, inst := paperServices(t)
	execute(t, svc, inst, 2500)
	if orders, err := svc.Orders.List(context.Background(), "", 10); err != nil || len(orders) != 0 {
		t.Fatalf("orders = (%v, %v), want none", orders, err)
	}
}
