// Package shadowflow_test holds the Jev shadow-evaluation tests (candidate
// prompt shadow runs, throttling, promotion, exit plan, isolation). They
// only use jev's exported API and live in their own directory to keep
// internal/service/jev/evalflow under the linterly line budget (#1173). The
// helpers below are this package's own (sibling test packages do not import
// each other).
package shadowflow_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha_test.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustCreateInstrument(t *testing.T, instruments *market.InstrumentRepository, symbol string) domain.Instrument {
	t.Helper()
	inst, err := instruments.Create(context.Background(), domain.Instrument{
		Symbol: symbol, Name: symbol + " Inc", Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatalf("create instrument %q: %v", symbol, err)
	}
	return inst
}
