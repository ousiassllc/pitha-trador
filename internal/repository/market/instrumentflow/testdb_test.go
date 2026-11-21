package instrumentflow_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
)

// newTestDB opens a fresh, fully migrated SQLite database in a temporary
// directory for a single test, closing it on cleanup.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Errorf("close conn: %v", err)
		}
	})
	return conn
}

func openTestDB(t *testing.T) *market.InstrumentRepository {
	t.Helper()
	return market.NewInstrumentRepository(newTestDB(t))
}

func mustCreateInstrument(t *testing.T, repo *market.InstrumentRepository, symbol string) domain.Instrument {
	t.Helper()
	inst, err := repo.Create(context.Background(), domain.Instrument{
		Symbol: symbol, Name: "Test " + symbol, Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatalf("Create instrument %q: %v", symbol, err)
	}
	return inst
}

func sectorPtr(s string) *string { return &s }
