package startup_test

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/startup"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/service/migratebundle"
	"github.com/ousiassllc/pitha-trador/internal/service/migratebundle/bundletest"
	"github.com/ousiassllc/pitha-trador/internal/service/migratebundle/importflow"
)

func count(t *testing.T, path, table string) int {
	t.Helper()
	conn, err := sqlitedb.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	var n int
	if err := conn.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// OpenDatabase applies a confirmed import before opening: the next start
// finds the bundle's data in the application's own database path.
func TestOpenDatabase_AppliesAConfirmedImportBeforeOpening(t *testing.T) {
	src, _ := bundletest.OpenDB(t)
	bundletest.Seed(t, src)
	var bundle bytes.Buffer
	if _, err := migratebundle.NewExporter(src).Export(context.Background(), &bundle); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "pitha.db")
	live, err := startup.OpenDatabase(path, false)
	if err != nil {
		t.Fatal(err)
	}
	svc := importflow.New(live, path)
	p, err := svc.Preview(context.Background(), &bundle)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Confirm(context.Background(), p.Token, false); err != nil {
		t.Fatal(err)
	}
	if err := live.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := startup.OpenDatabase(path, false)
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	defer func() { _ = db.Close() }()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM market_snapshots`).Scan(&n); err != nil || n != 3 {
		t.Fatalf("market_snapshots = %d (%v), want the bundle's 3", n, err)
	}
	if res, _ := importflow.New(db, path).LastResult(); res == nil || res.Status != importflow.StatusApplied {
		t.Fatalf("LastResult = %+v", res)
	}
}

// A read-only open (cmd/backtestverify) never applies an import.
func TestOpenDatabase_ReadOnlyDoesNotApply(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pitha.db")
	live, err := startup.OpenDatabase(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := live.Close(); err != nil {
		t.Fatal(err)
	}
	ro, err := startup.OpenDatabase(path, true)
	if err != nil {
		t.Fatalf("read-only open: %v", err)
	}
	_ = ro.Close()
	if n := count(t, path, "instruments"); n != 0 {
		t.Fatalf("instruments = %d", n)
	}
}
