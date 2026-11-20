package virtualpaper_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest/simrisk"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest/variant"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine"
	"github.com/ousiassllc/pitha-trador/internal/service/fillmodel"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
	"github.com/ousiassllc/pitha-trador/internal/service/policy/policytest"
	"github.com/ousiassllc/pitha-trador/internal/service/virtualpaper"
)

var jst = time.FixedZone("JST", 9*60*60)

// at is a JST wall-clock time of the "2006-01-02" day.
func at(day string, hour, minute int) time.Time {
	d, err := time.ParseInLocation(time.DateOnly, day, jst)
	if err != nil {
		panic(err)
	}
	return d.Add(time.Duration(hour)*time.Hour + time.Duration(minute)*time.Minute)
}

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// create stores an active profile created at the given JST time.
func create(t *testing.T, db *sql.DB, name string, created time.Time, params variant.Params) int64 {
	t.Helper()
	p, err := virtualpaper.Create(context.Background(), db, created, "127.0.0.1", virtualpaper.CreateArgs{Name: name, Params: params})
	if err != nil {
		t.Fatalf("create %q: %v", name, err)
	}
	return p.ID
}

type fixedModels struct{ model *metalabel.Model }

func (f fixedModels) ModelAt(time.Time) (*metalabel.Model, error) { return f.model, nil }

// The synthetic day: a LONG decision at minute 1 over a flat 1000 that jumps to
// 1015 (+1.5%) at minute 3 and falls back to 1008 at minute 6. Under the live
// exit (take profit 2%, trailing 0.5%) the trade ends by the trailing stop; with
// take_profit_pct 1 it ends at the take profit on minute 3; with take profit 5
// and no trailing stop nothing ends it before the data does.
var dayPrices = []float64{1000, 1000, 1000, 1015, 1015, 1015, 1008, 1008, 1008, 1008, 1008, 1008}

func liveExit() backtest.ExitRule {
	return backtest.ExitRule{
		StopLossPct: 1, TakeProfitPct: 2, TrailingStopPct: 0.5, MaxHolding: 20 * time.Minute,
		MinContinuationProbability: 0.6, ForceFlatBeforeClose: 10 * time.Minute, CooldownAfterLoss: 5 * time.Minute,
	}
}

func ptr[T any](v T) *T { return &v }

// configsOf builds the baseline RunConfigs of a JST day: one instrument, the
// synthetic bars from 10:00 JST, one LONG decision, the live rule with a taking
// model, and a simulated Risk Engine that sizes the position.
func configsOf(day string, prices []float64) []backtest.RunConfig {
	start := at(day, 10, 0)
	bars := make([]domain.Snapshot, 0, len(prices))
	for i, price := range prices {
		ts := start.Add(time.Duration(i) * time.Minute)
		f := featureengine.Compute(featureengine.Input{Timestamp: ts, Current: featureengine.Reading{Price: price, VWAP: price}, History: bars})
		f.OrderFlowFeatures = policytest.Feature().OrderFlowFeatures
		bars = append(bars, domain.Snapshot{InstrumentID: 1, Timestamp: ts, Price: price, SpreadBps: ptr(10.0), Feature: f})
	}
	th := policy.Thresholds{
		Policy: config.PolicyConfig{Long: config.PolicyDirectionThresholds{
			MinProbability: 0.68, MinEntryQuality: domain.JevEntryQualityStrong,
			MinContinuationProbability: 0.60, MaxToxicFlow: 0.35, MaxLiquidityStressed: 0.25,
		}},
		MaxSpreadBps: 50,
	}
	th.Policy.Short = th.Policy.Long
	model := policytest.TakingModel()
	decision := domain.JevDecision{
		InstrumentID: 1, Timestamp: start.Add(time.Minute), Direction: ptr(domain.JevDirectionLong),
		EntryQuality: ptr(domain.JevEntryQualityStrong), Confidence: ptr(0.80), ContinuationProbability: ptr(0.70),
		ToxicFlow: ptr(0.10), LiquidityStressed: ptr(0.10),
	}
	return []backtest.RunConfig{{
		InstrumentID: 1, Symbol: "7203", Snapshots: bars, Decisions: backtest.NewSliceDecisionSource([]domain.JevDecision{decision}),
		Models: fixedModels{&model}, Thresholds: th, Exit: liveExit(), Cost: fillmodel.Uniform(fillmodel.Model{SlippageBps: 2, FeeBps: 1}),
		Risk: &simrisk.Config{StopLossPct: 1, Limits: config.RiskLimits{
			InitialCapital: 100_000_000, MaxPositionPerSymbolPct: 10, MaxTotalExposurePct: 80, MaxDailyLossPct: 5, MaxTradeLossPct: 0.5,
			MaxOpenPositions: 2, MaxSameDirectionPositions: 2, MarketAdverseReturn5mPct: 0.3, MaxSpreadBps: 30, MaxConsecutiveLosses: 3,
			CooldownAfterLossMinutes: 5,
		}},
	}}
}

// daySource is the Source of the tests: the synthetic day for every JST day,
// except the days in empty (no bars) and the days in prices (their own series).
type daySource struct {
	empty  map[string]bool
	prices map[string][]float64
	err    error
	calls  []string
}

func (s *daySource) RunConfigs(_ context.Context, p backtest.Period) ([]backtest.RunConfig, error) {
	day := p.Start.In(jst).Format(time.DateOnly)
	s.calls = append(s.calls, day)
	if s.err != nil {
		return nil, s.err
	}
	if s.empty[day] {
		return nil, nil
	}
	if prices, ok := s.prices[day]; ok {
		return configsOf(day, prices), nil
	}
	return configsOf(day, dayPrices), nil
}

func recorder(db *sql.DB, src virtualpaper.Source, now time.Time) *virtualpaper.Recorder {
	return virtualpaper.NewRecorder(db, src).WithClock(func() time.Time { return now })
}

// runAll runs the recorder until it has no work left (bounded), returning the
// days the chunks replayed.
func runAll(t *testing.T, r *virtualpaper.Recorder) []string {
	t.Helper()
	var days []string
	for range 50 {
		res, err := r.Run(context.Background())
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if res.Day != "" {
			days = append(days, res.Day)
		}
		if !res.More {
			return days
		}
	}
	t.Fatal("the recorder never finished")
	return nil
}

func count(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

// dumpAllBut renders every row of every table whose name does not start with
// prefix, with the schema, so two dumps compare the whole database but for those.
func dumpAllBut(t *testing.T, db *sql.DB, prefix string) string {
	t.Helper()
	names, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for names.Next() {
		var n string
		if err := names.Scan(&n); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(n, prefix) {
			tables = append(tables, n)
		}
	}
	if err := names.Err(); err != nil {
		t.Fatal(err)
	}
	_ = names.Close()
	var b strings.Builder
	for _, tbl := range tables {
		fmt.Fprintf(&b, "== %s\n", tbl)
		rows, err := db.Query(`SELECT * FROM "` + tbl + `"`) //nolint:gosec // table names come from sqlite_master
		if err != nil {
			t.Fatalf("dump %s: %v", tbl, err)
		}
		cols, _ := rows.Columns()
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(&b, "%v\n", vals)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		_ = rows.Close()
	}
	return b.String()
}
