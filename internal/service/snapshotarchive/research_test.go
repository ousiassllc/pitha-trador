package snapshotarchive

import (
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

func insertQuotes(t *testing.T, db *sql.DB, symbol string, start time.Time, n int) {
	t.Helper()
	for i := range n {
		if _, err := db.Exec(`INSERT INTO research_quote_samples (timestamp, source, symbol, price, bid, via, session_phase)
			VALUES (?, 'tachibana', ?, ?, NULL, 'event', 'morning')`, sqlutil.FormatTime(start.Add(time.Duration(i)*time.Minute)), symbol, 100+float64(i)); err != nil {
			t.Fatal(err)
		}
	}
}

// The research table is archived the same way as market_snapshots, into its own
// folder, with its own manifest; market_snapshots' folder is untouched.
func TestResearchQuotesArchiveLivesInItsOwnFolderWithItsOwnManifest(t *testing.T) {
	_, db, _ := newArchiver(t)
	dir := filepath.Join(t.TempDir(), "snapshot_archive")
	a := NewResearchQuotes(db, dir)
	a.pageSize = 3
	insertQuotes(t, db, "7203", utc(2026, 6, 29, 0, 30), 5)
	insertQuotes(t, db, "6758", utc(2026, 6, 29, 0, 30), 2)
	insertQuotes(t, db, "7203", utc(2026, 7, 1, 0, 30), 2) // the cutoff's own date

	res, err := a.ArchiveBefore(context.Background(), cutoff)
	if err != nil {
		t.Fatal(err)
	}
	if res.DeleteBefore != "2026-07-01" || len(res.Days) != 1 || res.Days[0].Rows != 7 {
		t.Fatalf("result = %+v, want the one day 2026-06-29 with 7 rows", res)
	}
	file := filepath.Join(dir, "research_quote_samples", "2026", "research_quote_samples-2026-06-29.csv.gz")
	f, err := os.Open(file)
	if err != nil {
		t.Fatalf("archive file: %v", err)
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	recs, err := csv.NewReader(gz).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 8 || !strings.Contains(strings.Join(recs[0], ","), "session_phase") {
		t.Fatalf("file has %d records, header %v; want header + 7 rows with every column", len(recs), recs[0])
	}
	if recs[1][indexOf(recs[0], "bid")] != "" {
		t.Errorf("a NULL bid is %q, want an empty field", recs[1][indexOf(recs[0], "bid")])
	}

	m, err := ReadTableManifest(dir, ResearchQuoteTable)
	if err != nil || m.Table != ResearchQuoteTable || len(m.Days) != 1 || m.Days[0].File != "2026/research_quote_samples-2026-06-29.csv.gz" {
		t.Fatalf("manifest = %+v, %v", m, err)
	}
	if _, err := os.Stat(filepath.Join(dir, Table)); !os.IsNotExist(err) {
		t.Errorf("market_snapshots' folder was touched (stat err = %v)", err)
	}

	r, err := a.Reconcile(context.Background(), "2026-06-29")
	if err != nil || r.Status != StatusMatch || r.Table != ResearchQuoteTable || !r.ManifestMatch || r.DBRows != 7 {
		t.Fatalf("reconcile = %+v, %v", r, err)
	}
	// Deleting the day's rows turns the match into archive-only: the archive keeps them.
	if _, err := db.Exec(`DELETE FROM research_quote_samples WHERE timestamp < '2026-07-01'`); err != nil {
		t.Fatal(err)
	}
	if r, _ = a.Reconcile(context.Background(), "2026-06-29"); r.Status != StatusArchiveOnly || r.ArchiveRows != 7 {
		t.Errorf("after the delete: %+v, want archive-only with 7 rows", r)
	}
}
