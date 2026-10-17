package actuals

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/actuals"
	"github.com/ousiassllc/pitha-trador/internal/repository/actuals/actualstest"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
)

// seededDB/emptyDB are opened once for the package (migrating a database is
// the slow part) and only read by the tests.
var seededDB, emptyDB *sql.DB

func TestMain(m *testing.M) { os.Exit(run(m)) }

func run(m *testing.M) int {
	dir, err := os.MkdirTemp("", "actuals-test")
	if err != nil {
		panic(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	open := func(name string) *sql.DB {
		db, err := sqlitedb.Open(filepath.Join(dir, name))
		if err != nil {
			panic(err)
		}
		return db
	}
	seededDB, emptyDB = open("seeded.db"), open("empty.db")
	defer func() { _ = seededDB.Close(); _ = emptyDB.Close() }()
	actualstest.Seed(nopTB{}, seededDB)
	return m.Run()
}

func seededService(t *testing.T) *Service {
	t.Helper()
	return New(repo.NewReader(seededDB), actualstest.Capital, DefaultCriteria())
}

func near(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-6 {
		t.Errorf("%s = %v, want %v", name, got, want)
	}
}

// The overall figures of the synthetic database against the hand-computed
// values documented on actualstest.Trades.
func TestEvaluate_SyntheticOverall(t *testing.T) {
	e, err := seededService(t).Evaluate(context.Background(), Range{})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	o := e.Overall
	if o.N != 6 || o.Days != 3 || o.Wins != 3 {
		t.Fatalf("n/days/wins = %d/%d/%d, want 6/3/3", o.N, o.Days, o.Wins)
	}
	near(t, "win rate", o.WinRate.Value, 0.5)
	near(t, "net pnl", o.NetPnLJPY, -1000)
	near(t, "EV JPY", o.EVJPY.Value, -1000.0/6)
	near(t, "EV bps", o.EVBps.Value, -100.0/6)
	near(t, "profit factor", o.ProfitFactor.Value, 1040.0/2040)
	near(t, "max drawdown JPY", o.MaxDrawdownJPY.Value, 1760)
	near(t, "max drawdown %", o.MaxDrawdownPct.Value, 1.76)
	if e.InitialCapital != actualstest.Capital {
		t.Errorf("InitialCapital = %v, want %v", e.InitialCapital, actualstest.Capital)
	}

	// Costs: fees 5 trades x 2 fills x 10; slippage from the recorded bps of
	// trades 1,2,3,5 (trade 4 and the manual one recorded none), per fill
	// qty * (price - ref) with ref = price/(1+b) for a buy and price/(1-b) for
	// a sell (long entry buys, short entry sells).
	near(t, "fees", o.FeesJPY, 100)
	near(t, "gross before fees", o.GrossBeforeFeesJPY, -900)
	buy := func(price, bps float64) float64 { return 100 * (price - price/(1+bps/1e4)) }
	sell := func(price, bps float64) float64 { return 100 * (price/(1-bps/1e4) - price) }
	slippage := buy(1000, 5) + sell(1006, 3) + // trade 1 LONG
		sell(1000, 4) + buy(1010, 6) + // trade 2 SHORT
		buy(1000, 2) + sell(1003, 2) + // trade 3 LONG
		buy(1000, 1) + sell(1002, 1) // trade 5 LONG
	near(t, "slippage", o.SlippageJPY, slippage)
	near(t, "gross before costs", o.GrossBeforeCostsJPY, -900+slippage)
	if o.SlippageRecordedFill != 8 {
		t.Errorf("recorded slippage fills = %d, want 8", o.SlippageRecordedFill)
	}

	// n=6 is below every threshold: all sample-based metrics are 判定不能.
	for name, m := range map[string]Metric{"win rate": o.WinRate, "EV": o.EVBps, "PF": o.ProfitFactor, "DD": o.MaxDrawdownJPY} {
		if m.Reliable || m.N != 6 {
			t.Errorf("%s = %+v, want unreliable with n=6", name, m)
		}
	}
	if e.Verdict.Status != StatusUndetermined {
		t.Errorf("verdict = %v, want undetermined for 3 days / 6 trades", e.Verdict.Status)
	}
}

func TestEvaluate_SyntheticGroups(t *testing.T) {
	e, err := seededService(t).Evaluate(context.Background(), Range{})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	check := func(name string, groups []Group, want map[string][2]float64) {
		t.Helper()
		if len(groups) != len(want) {
			t.Fatalf("%s: %d groups %+v, want %d", name, len(groups), groups, len(want))
		}
		for _, g := range groups {
			w, ok := want[g.Key]
			if !ok {
				t.Errorf("%s: unexpected key %q", name, g.Key)
				continue
			}
			if float64(g.N) != w[0] || math.Abs(g.NetPnLJPY-w[1]) > 1e-9 {
				t.Errorf("%s[%q] = n %d net %v, want n %v net %v", name, g.Key, g.N, g.NetPnLJPY, w[0], w[1])
			}
		}
	}
	check("day", e.ByDay, map[string][2]float64{"2026-10-12": {2, -440}, "2026-10-13": {2, -740}, "2026-10-14": {2, 180}})
	check("week", e.ByWeek, map[string][2]float64{"2026-W42": {6, -1000}})
	check("direction", e.ByDirection, map[string][2]float64{"LONG": {5, 20}, "SHORT": {1, -1020}})
	check("exit reason", e.ByExitReason, map[string][2]float64{"take_profit": {2, 860}, "stop_loss": {2, -2040}, "max_holding": {1, 180}, "manual": {1, 0}})
	check("meta model", e.ByMetaModel, map[string][2]float64{"v-a": {4, 20}, "": {2, -1020}})
	check("A/B arm", e.ByABArm, map[string][2]float64{"momentum": {3, 1040}, "regime": {1, -1020}, "": {2, -1020}})

	if last := e.ByMetaModel[len(e.ByMetaModel)-1]; last.Key != "" {
		t.Errorf("trades without a model must sort last, got %q", last.Key)
	}
	for _, g := range e.ByDirection {
		if g.Key == "LONG" {
			// LONG nets 580, 280, -1020, 180, 0: cumulative peak 1040 → trough 20 .. drawdown 1020.
			near(t, "LONG max drawdown", g.MaxDrawdownJPY.Value, 1020)
			near(t, "LONG win rate", g.WinRate.Value, 0.6)
		}
	}
}

func TestEvaluate_SyntheticRange(t *testing.T) {
	r := Range{From: time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)}
	e, err := seededService(t).Evaluate(context.Background(), r)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if e.Overall.N != 2 || e.Overall.NetPnLJPY != -740 || e.From == nil || e.To == nil {
		t.Fatalf("overall = n %d net %v from %v to %v, want the two trades of 10-13", e.Overall.N, e.Overall.NetPnLJPY, e.From, e.To)
	}
	// Decision 3 (confidence .7, tie) and 4 (.9, hit) of the 13th: v2 grades only decision 4.
	for _, j := range e.Jev {
		if j.LabelVersion == 2 && (j.HitRate.N != 1 || j.HitRate.Value != 1) {
			t.Errorf("v2 hit rate in range = %+v, want n=1 value 1", j.HitRate)
		}
	}
}

func TestEvaluate_SyntheticJev(t *testing.T) {
	e, err := seededService(t).Evaluate(context.Background(), Range{})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(e.Jev) != 2 || e.Jev[0].LabelVersion != 1 || e.Jev[1].LabelVersion != 2 {
		t.Fatalf("Jev = %+v, want label versions 1 and 2 kept apart", e.Jev)
	}
	v1, v2 := e.Jev[0], e.Jev[1]
	if v1.HitRate.N != 2 || v2.HitRate.N != 4 {
		t.Errorf("sample sizes v1/v2 = %d/%d, want 2/4", v1.HitRate.N, v2.HitRate.N)
	}
	near(t, "v1 hit", v1.HitRate.Value, 0.5)
	near(t, "v1 brier", v1.Brier.Value, 0.265)
	near(t, "v2 hit", v2.HitRate.Value, 0.5)
	near(t, "v2 brier", v2.Brier.Value, 0.165)
	if v2.HitRate.Reliable || v2.Brier.Reliable {
		t.Error("4 samples must be 判定不能")
	}
}

// The meta model's OOS metrics sit beside the realized EV of the trades it
// scored (newest prediction per decision) and of the trades closed in its OOS
// window.
func TestEvaluate_SyntheticMeta(t *testing.T) {
	e, err := seededService(t).Evaluate(context.Background(), Range{})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(e.Meta) != 2 {
		t.Fatalf("Meta = %+v, want 2 models", e.Meta)
	}
	old, a := e.Meta[0], e.Meta[1]
	if old.Version != "v-old" || a.Version != "v-a" {
		t.Fatalf("order = %s, %s", old.Version, a.Version)
	}
	if a.Linked.N != 4 {
		t.Fatalf("v-a linked trades = %d, want 4 (trades 1,2,3,5; trade 1 uses v-a's newer prediction)", a.Linked.N)
	}
	near(t, "v-a predicted EV", a.PredictedEVBps.Value, 25) // (40+20+30+10)/4
	near(t, "v-a realized EV", a.Linked.EVBps.Value, 0.5)   // (58-102+28+18)/4
	near(t, "v-a EV gap", a.EVGapBps.Value, -24.5)          // ((18)+(-122)+(-2)+(8))/4
	near(t, "v-a OOS AUC", *a.OOSAUC, 0.62)
	near(t, "v-a OOS EV", *a.OOSExpectedBps, 7.5)
	if !a.OOSReliable || !old.OOSReliable {
		t.Errorf("OOSReliable v-a=%v v-old=%v, want true/true (120 and 40 samples vs 30)", a.OOSReliable, old.OOSReliable)
	}
	if old.Linked.N != 0 {
		t.Errorf("v-old linked = %d, want 0 (superseded by v-a on decision 1)", old.Linked.N)
	}
	// v-old's OOS window is [12th 00:00, 13th 00:00): trades 1 and 2 closed in it.
	if old.InOOSWindow.N != 2 || old.InOOSWindow.NetPnLJPY != -440 {
		t.Errorf("v-old window trades = n %d net %v, want 2 / -440", old.InOOSWindow.N, old.InOOSWindow.NetPnLJPY)
	}
	if a.InOOSWindow.N != 0 {
		t.Errorf("v-a window (10th-11th) trades = %d, want 0", a.InOOSWindow.N)
	}
}

func TestEvaluate_EmptyDatabase(t *testing.T) {
	e, err := New(repo.NewReader(emptyDB), 0, DefaultCriteria()).Evaluate(context.Background(), Range{})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if e.Overall.N != 0 || e.Overall.WinRate.Reliable || e.Overall.MaxDrawdownPct.Defined {
		t.Errorf("empty overall = %+v", e.Overall)
	}
	if e.Verdict.Status != StatusUndetermined || len(e.Verdict.Checks) != 4 {
		t.Errorf("verdict = %+v, want 判定不能 with 4 checks", e.Verdict)
	}
	if e.ByDay == nil || e.Jev == nil || e.Meta == nil {
		t.Error("empty slices must be non-nil so the JSON has [] not null")
	}
}

// nopTB lets TestMain seed without a *testing.T: a failure panics.
type nopTB struct{ testing.TB }

func (nopTB) Helper() {}
func (nopTB) Fatalf(format string, args ...any) {
	panic(fmt.Sprintf(format, args...))
}
