// Package exitflow_test holds FR-EXIT-1 exit-condition (EvaluateExit) tests of execution.Engine. They
// only use execution's exported API and live in their own directory to keep
// internal/service/execution under the linterly line budget (#248, #509).
// The helpers below are this package's own (sibling test packages do not
// import each other).
package exitflow_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
)

// testEngine bundles a real (SQLite-backed) Engine plus the repositories and
// instrument fixture these tests need.
type testEngine struct {
	db         *sql.DB
	engine     *execution.Engine
	orders     *trading.OrderRepository
	positions  *trading.PositionRepository
	snapshots  *market.SnapshotRepository
	instrument domain.Instrument
}

func newTestEngine(t *testing.T, cfg execution.Config) testEngine {
	t.Helper()
	return newTestEngineWithThresholds(t, cfg, nil)
}

func newTestEngineWithThresholds(t *testing.T, cfg execution.Config, entry execution.EntryThresholdSource) testEngine {
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

	orders := trading.NewOrderRepository(db)
	positions := trading.NewPositionRepository(db)
	snapshots := market.NewSnapshotRepository(db)
	engine := execution.NewEngine(execution.Deps{
		Orders:          orders,
		Positions:       positions,
		Snapshots:       snapshots,
		Decisions:       judgement.NewDecisionRepository(db),
		Signals:         trading.NewSignalRepository(db),
		Instruments:     instruments,
		EntryThresholds: entry,
	}, cfg)

	return testEngine{db: db, engine: engine, orders: orders, positions: positions, snapshots: snapshots, instrument: inst}
}

func longSignal(instrumentID int64) domain.TradeSignal {
	return domain.TradeSignal{
		InstrumentID: instrumentID, Symbol: "7203", Direction: domain.JevDirectionLong,
		RiskPassed: true, PolicyVersion: "v1",
	}
}

func snapshotAt(instrumentID int64, price float64, at time.Time) domain.Snapshot {
	return domain.Snapshot{InstrumentID: instrumentID, Symbol: "7203", Timestamp: at, Price: price}
}
