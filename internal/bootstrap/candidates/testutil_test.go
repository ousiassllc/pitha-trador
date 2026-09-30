package candidates

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/screener"
)

func ptrF(v float64) *float64 { return &v }

// newTestDB opens a fresh, fully migrated SQLite database in a temporary
// directory for a single test, closing it on cleanup.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// newTestRefresher builds a Refresher over a fresh DB with an empty
// strategy config (each test sets the FastScreener thresholds it needs)
// and no session gate (always in session).
func newTestRefresher(t *testing.T) *Refresher {
	t.Helper()
	conn := newTestDB(t)
	return &Refresher{
		Instruments: market.NewInstrumentRepository(conn),
		Snapshots:   market.NewSnapshotRepository(conn),
		Settings:    system.NewRuntimeSettingsRepository(conn),
		Jobs:        jobqueue.NewJobRepository(conn),
		Screener:    screener.NewLiveSource(),
		Strategy:    &config.StrategyConfig{},
	}
}

func mustCreateInstrument(t *testing.T, r *Refresher, symbol string) domain.Instrument {
	t.Helper()
	inst, err := r.Instruments.Create(context.Background(), domain.Instrument{
		Symbol: symbol, Name: symbol + " Inc.", Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatalf("Create instrument %q: %v", symbol, err)
	}
	return inst
}
