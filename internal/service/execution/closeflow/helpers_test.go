// Package closeflow_test holds the Engine.Close / Engine.CloseAll tests of
// execution.Engine (FR-EXIT-*, FR-RISK-3). They only use execution's
// exported API and live in their own directory to keep
// internal/service/execution under the linterly line budget (#248). The
// helpers below are this package's own (sibling test packages do not
// import each other).
package closeflow_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
)

// testEngine bundles a real (in-memory-SQLite-backed) Engine plus the
// repositories and instrument fixture the close tests need.
type testEngine struct {
	engine      *execution.Engine
	positions   *trading.PositionRepository
	instruments *market.InstrumentRepository
	instrument  domain.Instrument
}

func newTestEngine(t *testing.T, cfg execution.Config) testEngine {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close conn: %v", err)
		}
	})

	instruments := market.NewInstrumentRepository(db)
	inst, err := instruments.Create(context.Background(), domain.Instrument{
		Symbol: "7203", Name: "トヨタ自動車", Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatalf("create instrument fixture: %v", err)
	}

	positions := trading.NewPositionRepository(db)
	engine := execution.NewEngine(execution.Deps{
		Orders:      trading.NewOrderRepository(db),
		Positions:   positions,
		Snapshots:   market.NewSnapshotRepository(db),
		Decisions:   judgement.NewDecisionRepository(db),
		Signals:     trading.NewSignalRepository(db),
		Instruments: instruments,
	}, cfg)

	return testEngine{engine: engine, positions: positions, instruments: instruments, instrument: inst}
}

func longSignal(instrumentID int64) domain.TradeSignal {
	return domain.TradeSignal{
		InstrumentID: instrumentID, Symbol: "7203", Direction: domain.JevDirectionLong,
		RiskPassed: true, PolicyVersion: "v1",
	}
}

func shortSignal(instrumentID int64) domain.TradeSignal {
	return domain.TradeSignal{
		InstrumentID: instrumentID, Symbol: "7203", Direction: domain.JevDirectionShort,
		RiskPassed: true, PolicyVersion: "v1",
	}
}
