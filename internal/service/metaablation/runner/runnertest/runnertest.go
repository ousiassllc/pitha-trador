// Package runnertest builds the synthetic database copy of the metaablation
// tests: a migrated temporary SQLite file with one instrument, a rejected
// model, labeled decisions in the model's training, original OOS and evaluation
// windows, and 1-minute market_snapshots for the evaluation days. All values
// are arbitrary fixtures.
package runnertest

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
)

// JST is the trading-day zone; T0 is Monday 2026-10-19 00:00 JST.
var (
	JST = time.FixedZone("JST", 9*3600)
	T0  = time.Date(2026, 10, 19, 0, 0, 0, 0, JST)
)

// Day is midnight JST n days after T0.
func Day(n int) time.Time { return T0.AddDate(0, 0, n) }

// EvalDays are the evaluation days (offsets from T0): Thu 10-29, Fri 10-30,
// Mon 11-02 and Wed 11-04 (11-03 is a holiday).
var EvalDays = []int{10, 11, 14, 16}

// Options shape the fixture.
type Options struct {
	// NoSnapshots leaves market_snapshots empty; MissingBarDay (when > 0) is an
	// evaluation day (offset) whose bars are left out.
	NoSnapshots   bool
	MissingBarDay int
	// ParamsJSON replaces the model's stored parameters (e.g. unreadable).
	ParamsJSON string
	// NoCohort stores the model without a trained cohort.
	NoCohort bool
}

// DB is the fixture database. Its file is closed when Build returns.
type DB struct {
	Path string
	// ModelVersion is the rejected model's version.
	ModelVersion string
}

// Build creates the fixture in a fresh directory of t.TempDir() and closes it.
func Build(t *testing.T, o Options) DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "db", "copy.db")
	conn, err := sqlitedb.Open(path)
	if err != nil {
		t.Fatalf("open fixture db: %v", err)
	}
	f := &fixture{t: t, ctx: context.Background(), o: o, db: conn}
	f.instrument()
	f.decisions()
	f.bars()
	version := f.model("lr-synth-1", Day(9), 0)
	if err := conn.Close(); err != nil {
		t.Fatalf("close fixture db: %v", err)
	}
	return DB{Path: path, ModelVersion: version}
}

// AddRejectedModel stores one more rejected model (trained at trainedAt, which
// may be later than the one `init` pinned) into the database at path.
func AddRejectedModel(t *testing.T, path, version string, trainedAt time.Time) {
	t.Helper()
	conn, err := sqlitedb.Open(path)
	if err != nil {
		t.Fatalf("open fixture db: %v", err)
	}
	f := &fixture{t: t, ctx: context.Background(), db: conn}
	f.model(version, trainedAt, 1)
	if err := conn.Close(); err != nil {
		t.Fatalf("close fixture db: %v", err)
	}
}
