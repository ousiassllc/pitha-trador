package experimenttests

import (
	"database/sql"
	"math"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"gonum.org/v1/gonum/stat/distuv"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/experiment"
)

// issue #887: the production sigma of the net pnl per trade is measured on a
// copy of the production DB, with the SQL of docs/architecture/
// paper-results-mcp-experiments.md §5.1. These tests run THAT SQL (read out of
// the document, so the two cannot drift apart) against planted trades and hold
// it to the numbers of experiment_compare, which is what the min_trades floor
// is about: the same set (closed, realized_pnl set) and the same values. They
// say nothing about the value of sigma, which is not measured here.

const experimentsDoc = "../../../../docs/architecture/paper-results-mcp-experiments.md"

var sqlFence = regexp.MustCompile("(?s)```sql\n(.*?)```")

// docSQL is the n-th ```sql block of the experiments document.
func docSQL(t *testing.T, n int) string {
	t.Helper()
	doc, err := os.ReadFile(experimentsDoc)
	if err != nil {
		t.Fatal(err)
	}
	blocks := sqlFence.FindAllStringSubmatch(string(doc), -1)
	if len(blocks) <= n {
		t.Fatalf("the document has %d sql blocks, want at least %d", len(blocks), n+1)
	}
	return blocks[n][1]
}

func bounds(from, to string) []any {
	f, _ := time.Parse(time.RFC3339, from)
	e, _ := time.Parse(time.RFC3339, to)
	return []any{sql.Named("from", sqlutil.FormatTime(f)), sql.Named("to", sqlutil.FormatTime(e))}
}

func near(a, b float64) bool { return math.Abs(a-b) <= 1e-9*math.Max(1, math.Abs(b)) }

func TestMeasurementSQLIsThePerformanceSummarysSet(t *testing.T) {
	srv, w := newServer(t)
	// A trade still open and one with no realized_pnl are not in the set; neither
	// is one closed outside the period. newServer's seeded trades are.
	plantImproved(t, w)
	e := create(t, srv, createArgs(key, nil))
	closeAt(t, srv, e.ID, endedAt)
	c := compare(t, srv, e.ID, nil)

	for _, p := range []struct {
		name     string
		period   experiment.Period
		wantTest experiment.Sample
	}{{"before", c.Before, c.PerTradePnl.Before}, {"after", c.After, c.PerTradePnl.After}} {
		var n int
		var mean, sd sql.NullFloat64
		if err := w.QueryRow(docSQL(t, 0), bounds(p.period.From, p.period.To)...).Scan(&n, &mean, &sd); err != nil {
			t.Fatalf("%s: %v", p.name, err)
		}
		if n != p.period.Performance.ClosedTradeCount || n != p.wantTest.N || n < 64 {
			t.Errorf("%s: n = %d, want closed_trade_count %d", p.name, n, p.period.Performance.ClosedTradeCount)
		}
		if p.wantTest.MeanJPY == nil || !mean.Valid || !near(mean.Float64, *p.wantTest.MeanJPY) || !near(mean.Float64, *p.period.Performance.ExpectancyJPY) {
			t.Errorf("%s: mean = %v, want expectancy_jpy %v", p.name, mean, p.period.Performance.ExpectancyJPY)
		}
		if p.wantTest.SDJPY == nil || !sd.Valid || !near(sd.Float64, *p.wantTest.SDJPY) {
			t.Errorf("%s: sd = %v, want per_trade_pnl sd_jpy %v", p.name, sd, p.wantTest.SDJPY)
		}
	}
}

// An empty set gives n=0 and a one-trade set no standard deviation (NULL), never an error.
func TestMeasurementSQLWithTooFewTrades(t *testing.T) {
	_, w := newServer(t)
	var n int
	var mean, sd sql.NullFloat64
	err := w.QueryRow(docSQL(t, 0), bounds("2026-07-01T00:00:00Z", "2026-07-02T00:00:00Z")...).Scan(&n, &mean, &sd)
	if err != nil || n != 0 || mean.Valid || sd.Valid {
		t.Fatalf("empty period: n=%d mean=%v sd=%v err=%v, want n=0 and no mean or sd", n, mean, sd, err)
	}
	// One closed trade (newServer seeds one at 2026-10-01T00:30:00Z).
	if err := w.QueryRow(docSQL(t, 0), bounds("2026-10-01T00:00:00Z", "2026-10-01T01:00:00Z")...).Scan(&n, &mean, &sd); err != nil {
		t.Fatal(err)
	}
	if n != 1 || !mean.Valid || sd.Valid {
		t.Errorf("one trade: n=%d mean=%v sd=%v, want n=1, a mean and no sd", n, mean, sd)
	}
}

// The per-day aggregation covers the same set: its counts and sums add up to the
// whole period's.
func TestMeasurementDailySQLCoversTheSameSet(t *testing.T) {
	_, w := newServer(t)
	plantImproved(t, w)
	args := bounds("2026-09-29T15:00:00Z", "2026-10-04T00:00:00Z")
	rows, err := w.Query(docSQL(t, 1), args...)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var days, total int
	var sum float64
	for rows.Next() {
		var day string
		var n int
		var s, ss float64
		if err := rows.Scan(&day, &n, &s, &ss); err != nil {
			t.Fatal(err)
		}
		if !regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`).MatchString(day) || n < 1 || ss < 0 {
			t.Errorf("day row = %q %d %v %v", day, n, s, ss)
		}
		days, total, sum = days+1, total+n, sum+s
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	var n int
	var mean, sd sql.NullFloat64
	if err := w.QueryRow(docSQL(t, 0), args...).Scan(&n, &mean, &sd); err != nil {
		t.Fatal(err)
	}
	if days < 2 || total != n || !near(sum/float64(total), mean.Float64) {
		t.Errorf("days=%d total=%d sum=%v, whole period n=%d mean=%v", days, total, sum, n, mean)
	}
}

// requiredN is §5.2's n(d): trades per period to tell a difference of d sigma
// (Welch, two-sided alpha 0.05, power 0.8, with the t-distribution correction).
func requiredN(d float64) int {
	std := distuv.UnitNormal
	za, zb := std.Quantile(0.975), std.Quantile(0.8)
	return int(math.Ceil(2*(za+zb)*(za+zb)/(d*d) + za*za/4))
}

// detectable is §5.2's d(n), the inverse.
func detectable(n int) float64 {
	std := distuv.UnitNormal
	za, zb := std.Quantile(0.975), std.Quantile(0.8)
	return math.Sqrt(2 * (za + zb) * (za + zb) / (float64(n) - za*za/4))
}

// The two tables of §5.2 and the 64 of MinTradesFloor follow from the formula
// (sigma-free: the tables are in units of sigma).
func TestSigmaTablesFollowTheFormula(t *testing.T) {
	for d, want := range map[float64]int{0.2: 394, 0.3: 176, 0.5: 64, 0.8: 26, 1.0: 17} {
		if got := requiredN(d); got != want {
			t.Errorf("n(d=%v) = %d, want %d", d, got, want)
		}
	}
	if requiredN(0.5) != experiment.MinTradesFloor {
		t.Errorf("the floor %d is not n(0.5) = %d", experiment.MinTradesFloor, requiredN(0.5))
	}
	for n, want := range map[int]float64{64: 0.499, 100: 0.398, 150: 0.325, 200: 0.281, 300: 0.229, 500: 0.177, 1000: 0.125} {
		if got := detectable(n); math.Abs(got-want) > 0.0005 {
			t.Errorf("d(n=%d) = %.4f, want %v", n, got, want)
		}
	}
	doc, err := os.ReadFile(experimentsDoc)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []string{
		"| 見分けられる差 $d$（$\\delta=d\\sigma$ の係数） | 0.499 | 0.398 | 0.325 | 0.281 | 0.229 | 0.177 | 0.125 |",
		"| 必要な件数 $n$（1 期間あたり） | 394 | 176 | 64 | 26 | 17 |",
		"| $\\rho=0.10$ | 1.20 | 1.40 | 1.90 |",
	} {
		if !strings.Contains(string(doc), row) {
			t.Errorf("the document lacks the table row %q", row)
		}
	}
	for _, c := range []struct {
		rho  float64
		m    int
		want float64
	}{{0.05, 3, 1.10}, {0.10, 5, 1.40}, {0.20, 10, 2.80}} {
		if got := 1 + float64(c.m-1)*c.rho; math.Abs(got-c.want) > 1e-9 {
			t.Errorf("DEFF(rho=%v, m=%d) = %v, want %v", c.rho, c.m, got, c.want)
		}
	}
}
