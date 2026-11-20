package backtestsource

import (
	"context"
	"log/slog"
	"os"
	"reflect"
	"runtime"
	"testing"
	"time"

	configdefaults "github.com/ousiassllc/pitha-trador/config"
	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/backtestlab"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/backtestjob"
)

// scaleEnvVar switches on TestSource_MaxConfigurationAtDatabaseScale, the
// timing run of issue #1128. It is a measurement, not a check: nothing in CI
// asserts on wall-clock time. Run it with
//
//	PITHA_BACKTEST_SCALE=1 go test -run TestSource_MaxConfigurationAtDatabaseScale -v -count=1 -timeout 30m ./internal/bootstrap/backtestsource/
const scaleEnvVar = "PITHA_BACKTEST_SCALE"

// scaleRun is what a replay over a fixture measured.
type scaleRun struct {
	counts   scaleCounts
	dbBytes  int64
	build    time.Duration
	load     time.Duration // RunConfigs alone
	total    time.Duration // backtestlab.Run: the load plus every variant's replay
	result   backtestlab.Result
	coverage backtestlab.Coverage
}

// runAtScale stores spec's history in a fresh database, opens it read-only the
// way cmd/paper-results-mcp does (backtestsource.NewForDB over
// sqlitedb.OpenReadOnly: a write would fail), replays the largest request the
// lab accepts over [period start, +periodDays) with the REAL Source and checks
// that nothing guarded was written. ctx bounds the replay.
func runAtScale(ctx context.Context, t *testing.T, spec scaleSpec, leadDays, periodDays int) scaleRun {
	t.Helper()
	// The replayed Risk Engine logs every rejected signal; a month of 45
	// instruments under 12 parameter sets would bury the test's output.
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.DiscardHandler))
	t.Cleanup(func() { slog.SetDefault(prev) })
	spec.windowStart = spec.windowStart.AddDate(0, 0, -leadDays)
	spec.windowDays = leadDays + periodDays
	period := backtest.Period{Start: spec.windowStart.AddDate(0, 0, leadDays)}
	period.End = period.Start.AddDate(0, 0, periodDays)

	env := newTestEnv(t)
	built := time.Now()
	run := scaleRun{counts: writeScaleFixture(t, env, spec)}
	run.build = time.Since(built)

	var path string
	if err := env.Conn.QueryRowContext(ctx, `SELECT file FROM pragma_database_list WHERE name = 'main'`).Scan(&path); err != nil {
		t.Fatalf("database file: %v", err)
	}
	if fi, err := os.Stat(path); err == nil {
		run.dbBytes = fi.Size()
	}
	readOnly, err := sqlitedb.OpenReadOnly(path)
	if err != nil {
		t.Fatalf("OpenReadOnly: %v", err)
	}
	t.Cleanup(func() { _ = readOnly.Close() })
	strategy, err := config.LoadStrategyBytes(configdefaults.DefaultStrategyYAML)
	if err != nil {
		t.Fatalf("LoadStrategyBytes: %v", err)
	}
	risk, err := config.LoadRiskBytes(configdefaults.DefaultRiskYAML)
	if err != nil {
		t.Fatalf("LoadRiskBytes: %v", err)
	}
	src := NewForDB(readOnly, strategy, risk, ReplayLive)
	req := maxRequest(t)

	before := tableState(t, env.Conn)
	start := time.Now()
	if _, err := src.RunConfigs(ctx, period); err != nil {
		t.Fatalf("RunConfigs: %v", err)
	}
	run.load = time.Since(start)
	start = time.Now()
	run.result, run.coverage, err = backtestlab.Run(ctx, src, period, req)
	run.total = time.Since(start)
	if err != nil {
		t.Fatalf("backtestlab.Run after %v: %v", run.total, err)
	}

	if after := tableState(t, env.Conn); !reflect.DeepEqual(before, after) {
		t.Errorf("a replay changed what it must only read:\nbefore %v\nafter  %v", before, after)
	}
	for table, want := range map[string]int{"market_snapshots": run.counts.snapshots, "jev_decisions": run.counts.decisions} {
		var got int
		if err := env.Conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&got); err != nil || got != want {
			t.Errorf("%s rows = %d (err %v), want the %d the fixture stored", table, got, err, want)
		}
	}
	checkReplay(t, run, period, req)
	t.Logf("%d snapshots, %d decisions: fixture built in %v, load %v, whole run %v (not asserted)",
		run.counts.snapshots, run.counts.decisions, run.build.Round(time.Millisecond), run.load.Round(time.Millisecond), run.total.Round(time.Millisecond))
	return run
}

// checkReplay asserts the replay really went through the stored history: every
// variant ran with the requested sensitivity rows, the baseline traded, the
// coverage lies in the period, and the overrides changed the outcome.
func checkReplay(t *testing.T, run scaleRun, period backtest.Period, req backtestlab.Request) {
	t.Helper()
	if got := len(run.result.Variants); got != len(req.Variants) {
		t.Fatalf("variants = %d, want %d", got, len(req.Variants))
	}
	if run.coverage.FirstSnapshotAt == nil || run.coverage.FirstSnapshotAt.Before(period.Start) || !run.coverage.LastSnapshotAt.Before(period.End) {
		t.Fatalf("coverage = %+v, want the stored bars inside %v-%v", run.coverage, period.Start, period.End)
	}
	if base := run.result.Variants[0]; base.TradeCount == 0 || len(base.Days) == 0 {
		t.Fatalf("baseline = %+v, want trades from the stored decisions over %d bars", base, run.counts.snapshots)
	}
	distinct := map[int]bool{}
	for i, v := range run.result.Variants {
		if len(v.Sensitivity) != len(req.ExtraCostBps) {
			t.Errorf("variants[%d] has %d sensitivity rows, want %d", i, len(v.Sensitivity), len(req.ExtraCostBps))
		}
		distinct[v.TradeCount] = true
	}
	if len(distinct) < 2 {
		t.Errorf("every variant made the same number of trades, so the overrides were never applied: %v", distinct)
	}
}

// TestSource_RealSourceReplaysStoredHistoryWithoutWriting is issue #1128's
// integration test at a size CI can afford: the real Source over a read-only
// database of 330 one-minute bars a day per instrument, with Trader and Scout
// decisions stored the way the live job stores them, replayed through the
// lab with the maximum request (12 parameter sets, 6 extra-cost rows). It
// asserts what the replay produced and that runtime_settings, orders, positions
// and the other guarded tables are the same afterwards.
func TestSource_RealSourceReplaysStoredHistoryWithoutWriting(t *testing.T) {
	spec := scaleSpec{instruments: 3, windowStart: time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC), traderEvery: 2, scoutEvery: 5}
	run := runAtScale(context.Background(), t, spec, 3, 3) // 3 lead-in days: only the Friday before the Monday trades
	if run.counts.snapshots < 3*330*3 || run.counts.decisions == 0 {
		t.Fatalf("fixture = %+v, want a non-trivial history", run.counts)
	}
}

// TestSource_MaxConfigurationAtDatabaseScale measures the largest run an MCP
// call can ask for (period of backtestlab.MaxPeriodDays, backtestlab.MaxVariants
// sets, backtestlab.MaxExtraCostPoints extra-cost rows) over the history of the
// ranking watch list at its largest (45 instruments, FR-SCAN-3) for a month,
// and logs the numbers docs/architecture/paper-results-mcp-backtest.md §7
// records. It only runs with PITHA_BACKTEST_SCALE set, never asserts on a
// duration, and fails only if the replay does not finish inside
// backtestjob.DefaultRunTimeout, the cap the job enforces.
func TestSource_MaxConfigurationAtDatabaseScale(t *testing.T) {
	if os.Getenv(scaleEnvVar) == "" {
		t.Skipf("timing run: set %s=1 (see backtestsource_scale_test.go)", scaleEnvVar)
	}
	ctx, cancel := context.WithTimeout(context.Background(), backtestjob.DefaultRunTimeout)
	defer cancel()
	spec := scaleSpec{instruments: 45, windowStart: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), traderEvery: 2, scoutEvery: 5}
	run := runAtScale(ctx, t, spec, 3, backtestlab.MaxPeriodDays)

	trades := 0
	for _, v := range run.result.Variants {
		trades += v.TradeCount
	}
	t.Logf("fixture: %d instruments, %d trading days (%d-day period plus 3 days of lead-in), %d market_snapshots, %d jev_decisions, db %.0f MiB, built in %v",
		spec.instruments, run.counts.tradingDays, backtestlab.MaxPeriodDays, run.counts.snapshots, run.counts.decisions, float64(run.dbBytes)/(1<<20), run.build.Round(time.Millisecond))
	t.Logf("max configuration: %d variants x %d extra-cost rows over %d days: %d trades in all variants (baseline %d)",
		len(run.result.Variants), len(run.result.Variants[0].Sensitivity), backtestlab.MaxPeriodDays, trades, run.result.Variants[0].TradeCount)
	t.Logf("wall time: load of the stored history (RunConfigs) %v; whole run (backtestlab.Run, load + replay of every variant) %v; cap %v (%.1f%% used)",
		run.load.Round(time.Millisecond), run.total.Round(time.Millisecond), backtestjob.DefaultRunTimeout, 100*float64(run.total)/float64(backtestjob.DefaultRunTimeout))
	t.Logf("machine: %s/%s, %d CPUs (GOMAXPROCS %d), %s", runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), runtime.GOMAXPROCS(0), runtime.Version())
}
