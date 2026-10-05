package retention

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// explainDetails returns the EXPLAIN QUERY PLAN detail lines of query.
func explainDetails(t *testing.T, db *sql.DB, query string, args ...any) []string {
	t.Helper()
	rows, err := db.Query("EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var plan []string
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return plan
}

// assertIndexRange fails unless every line touching table is an index search
// ("SEARCH table USING ... INDEX"), not a "SCAN table" and not a temp
// B-tree sort.
func assertIndexRange(t *testing.T, plan []string, table string) {
	t.Helper()
	searched := false
	for _, line := range plan {
		if strings.Contains(line, "SCAN "+table) {
			t.Errorf("full scan of %s in plan: %v", table, plan)
		}
		if strings.Contains(line, "TEMP B-TREE") {
			t.Errorf("temp b-tree sort in plan: %v", plan)
		}
		if strings.Contains(line, "SEARCH "+table) && strings.Contains(line, "INDEX") {
			searched = true
		}
	}
	if !searched {
		t.Errorf("no index search of %s in plan: %v", table, plan)
	}
}

func TestPurgeQueries_UseIndexRangeScans(t *testing.T) {
	_, db := newService(t, Policy{})

	assertIndexRange(t,
		explainDetails(t, db, selectExpiredSnapshotIDsSQL, daysAgo(90), 1000), "market_snapshots")
	assertIndexRange(t,
		explainDetails(t, db, deleteExpiredJobsSQL, "succeeded", daysAgo(7), 1000), "jobs")
}

// A purge over many expired rows must not starve another connection's
// INSERT: each batch is a short write transaction, so the writer gets in
// between batches long before busy_timeout (60s) expires.
func TestPurge_DoesNotBlockConcurrentInsert(t *testing.T) {
	s, db := newService(t, Policy{})
	const expired, kept = 30000, 30000
	inst, err := market.NewInstrumentRepository(db).Create(context.Background(), domain.Instrument{
		Symbol: "7203", Name: "Toyota", Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	seedSnapshots(t, db, inst.ID, expired, fixedNow.AddDate(0, 0, -120))
	seedSnapshots(t, db, inst.ID, kept, fixedNow.AddDate(0, 0, -60))

	var wg sync.WaitGroup
	wg.Add(1)
	var purgeErr error
	go func() {
		defer wg.Done()
		purgeErr = s.Purge(context.Background())
	}()

	var worst time.Duration
	for i := range 50 {
		start := time.Now()
		ts := sqlutil.FormatTime(fixedNow.Add(time.Duration(i) * time.Second))
		_, err := db.Exec(insertBarSQL, inst.ID, ts, ts)
		if err != nil {
			t.Fatalf("concurrent insert %d: %v", i, err)
		}
		worst = max(worst, time.Since(start))
	}
	wg.Wait()
	if purgeErr != nil {
		t.Fatalf("Purge: %v", purgeErr)
	}
	if worst > 5*time.Second {
		t.Errorf("slowest concurrent INSERT took %v, want well under busy_timeout", worst)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM market_snapshots`); got != kept+50 {
		t.Errorf("market_snapshots = %d, want %d", got, kept+50)
	}
}

// seedSnapshots bulk-inserts n one-minute snapshots for instrumentID starting
// at from, in one transaction.
func seedSnapshots(t *testing.T, db *sql.DB, instrumentID int64, n int, from time.Time) {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.Prepare(insertBarSQL)
	if err != nil {
		t.Fatal(err)
	}
	for i := range n {
		ts := sqlutil.FormatTime(from.Add(time.Duration(i) * time.Minute))
		if _, err := stmt.Exec(instrumentID, ts, ts); err != nil {
			t.Fatalf("seed snapshot %d: %v", i, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

const insertBarSQL = `INSERT INTO market_snapshots
	(instrument_id, symbol, timestamp, price, volume, turnover, vwap, price_vs_vwap_bps, raw_data_json, created_at)
	VALUES (?, 'S', ?, 1, 1, 1, 1, 0, '{}', ?)`
