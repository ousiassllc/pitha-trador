package risk

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
)

// NewEngine refuses a missing Snapshots, so only an Engine that lost it later
// can reach this branch: a new entry is rejected with a fixed reason instead
// of passing with its price-based limits skipped (issue #841).
func TestCheck_RejectsNewEntryWhenSnapshotsUnavailable(t *testing.T) {
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "risk_test.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	e := NewEngine(Config{
		Limits:     config.RiskLimits{InitialCapital: 30_000_000, MaxOpenPositions: 5, MaxSameDirectionPositions: 3, MaxTotalExposurePct: 20, MaxPositionPerSymbolPct: 2, MaxTradeLossPct: 0.25},
		KillSwitch: system.NewKillSwitchRepository(db),
		Settings:   system.NewRuntimeSettingsRepository(db),
		Snapshots:  market.NewSnapshotRepository(db),
	})
	e.snapshots = nil

	passed, reason := e.Check(context.Background(), 1, "LONG")
	if passed || reason != ReasonSnapshotsUnavailable {
		t.Fatalf("Check = (%v, %q), want (false, %q)", passed, reason, ReasonSnapshotsUnavailable)
	}
}
