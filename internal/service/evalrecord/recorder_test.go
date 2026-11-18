package evalrecord_test

import (
	"context"
	"database/sql"
	"math"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/evalrecord"
	"github.com/ousiassllc/pitha-trador/internal/service/evalrecord"
	fx "github.com/ousiassllc/pitha-trador/internal/service/evalrecord/evalfixture"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/evaldiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

const eps = 1e-9

func TestMain(m *testing.M) {
	code := m.Run()
	testkit.RemoveTemplate()
	os.Exit(code)
}

var jst = time.FixedZone("JST", 9*60*60)

// at is 09:00 JST of 2026-10-<day> (+ minutes).
func at(day, minutes int) time.Time {
	return time.Date(2026, 10, day, 9, minutes, 0, 0, jst)
}

func f(v float64) *float64 { return &v }

// seed is the fixture of the package (nets in bps, from evalfixture's scenario table):
//
//	10-05 m1/v3  id1 LONG up   r5 +1 passed  llm +30 random   0 momentum +30 contrarian -30
//	             id2 SHORT mild r5 -1        llm -20 random -10 momentum -20 contrarian   0
//	             id3 LONG, no bars           (excluded: no entry quote)
//	10-05 m2/v4  id4 LONG down r5 +1         llm -30 random   0 momentum -30 contrarian +30
//	10-06 m1/v3  id5 SHORT dip r5 NULL       llm   0 random -10 momentum 0   contrarian 0
//	10-07 m1/v3  id6 LONG, no bars           (nothing evaluable: a row with evaluated=0)
func seed(t *testing.T, w *sql.DB) {
	t.Helper()
	fx.Seed(t, w,
		fx.Spec{ID: 1, At: at(5, 0), Direction: "LONG", Model: "m1", Version: "v3", Scenario: fx.Up, R5: f(1), Passed: true},
		fx.Spec{ID: 2, At: at(5, 20), Direction: "SHORT", Model: "m1", Version: "v3", Scenario: fx.Mild, R5: f(-1)},
		fx.Spec{ID: 3, At: at(5, 40), Direction: "LONG", Model: "m1", Version: "v3"},
		fx.Spec{ID: 4, At: at(5, 50), Direction: "LONG", Model: "m2", Version: "v4", Scenario: fx.Down, R5: f(1), Instr: 2},
		fx.Spec{ID: 5, At: at(6, 0), Direction: "SHORT", Model: "m1", Version: "v3", Scenario: fx.Dip},
		fx.Spec{ID: 6, At: at(7, 0), Direction: "LONG", Model: "m1", Version: "v3"},
	)
}

func newRecorder(t *testing.T, w *sql.DB, now string, keep int) *evalrecord.Recorder {
	t.Helper()
	clock, err := time.ParseInLocation("2006-01-02 15:04", now, jst)
	if err != nil {
		t.Fatal(err)
	}
	return evalrecord.New(repo.NewRepository(w), w, func(context.Context) (int, error) { return keep, nil }).WithClock(func() time.Time { return clock })
}

// stored reads the whole table, ordered by day, model.
func stored(t *testing.T, w *sql.DB) []repo.Row {
	t.Helper()
	rows, ok, err := repo.List(t.Context(), w, "2000-01-01", "2100-01-01", repo.Filter{}, 100000)
	if err != nil || !ok {
		t.Fatalf("List: %v %v", ok, err)
	}
	slices.SortFunc(rows, func(a, b repo.Row) int {
		return strings.Compare(a.Day+a.ModelID, b.Day+b.ModelID)
	})
	return rows
}

func count(t *testing.T, w *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := w.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM `+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func near(a, b float64) bool { return math.Abs(a-b) < eps }

func wantRow(t *testing.T, got repo.Row, want repo.Row) {
	t.Helper()
	if got.Day != want.Day || got.ModelID != want.ModelID || got.QuestionVersion != want.QuestionVersion || got.HorizonMinutes != 10 ||
		got.Decisions != want.Decisions || got.Evaluated != want.Evaluated || got.Entered != want.Entered || got.Wins != want.Wins ||
		got.Excluded() != want.Decisions-want.Evaluated ||
		!near(got.LLMSum, want.LLMSum) || !near(got.RandomSum, want.RandomSum) || !near(got.MomentumSum, want.MomentumSum) || !near(got.ContrarianSum, want.ContrarianSum) ||
		got.TakeProfitBps != 30 || got.StopLossBps != 30 || got.BarrierCostBps != 10 {
		t.Errorf("row %s %s/%s = %+v, want %+v (horizon 10, label 30/30/10)", want.Day, want.ModelID, want.QuestionVersion, got, want)
	}
}

// One row per day per cohort, one day per Run (oldest first), ErrMore-style More until the
// last, the arithmetic of the counts and the sums are the hand-computed ones, a day with
// decisions but nothing evaluable still has its rows (evaluated 0), and a Run with
// nothing left does nothing.
func TestRecorder_OneRowPerDayPerCohortAndOneDayPerRun(t *testing.T) {
	_, w := testkit.NewMigratedDB(t)
	seed(t, w)
	rec := newRecorder(t, w, "2026-10-07 16:30", 365)
	for i, want := range []struct {
		day  string
		rows int
		more bool
	}{{"2026-10-05", 2, true}, {"2026-10-06", 1, true}, {"2026-10-07", 1, false}, {"", 0, false}} {
		res, err := rec.Run(t.Context())
		if err != nil || res.Day != want.day || res.Rows != want.rows || res.More != want.more {
			t.Fatalf("run %d = %+v, %v; want %+v", i+1, res, err, want)
		}
	}
	got := stored(t, w)
	if len(got) != 4 {
		t.Fatalf("rows = %d, want 4: %+v", len(got), got)
	}
	wantRow(t, got[0], repo.Row{Day: "2026-10-05", ModelID: "m1", QuestionVersion: "v3", Decisions: 3, Evaluated: 2, Entered: 1, Wins: 1, LLMSum: 10, RandomSum: -10, MomentumSum: 10, ContrarianSum: -30})
	wantRow(t, got[1], repo.Row{Day: "2026-10-05", ModelID: "m2", QuestionVersion: "v4", Decisions: 1, Evaluated: 1, LLMSum: -30, RandomSum: 0, MomentumSum: -30, ContrarianSum: 30})
	wantRow(t, got[2], repo.Row{Day: "2026-10-06", ModelID: "m1", QuestionVersion: "v3", Decisions: 1, Evaluated: 1, LLMSum: 0, RandomSum: -10})
	wantRow(t, got[3], repo.Row{Day: "2026-10-07", ModelID: "m1", QuestionVersion: "v3", Decisions: 1, Evaluated: 0})
}

// The stored sums are exactly the sums of the evaluation's own rows of the day, whatever
// the label settings are (a changed barrier cost moves them, the record follows).
func TestRecorder_StoredSumsEqualTheEvaluationRowsOfTheDay(t *testing.T) {
	_, w := testkit.NewMigratedDB(t)
	seed(t, w)
	testkit.Setting(t, w, "calibration.barrier_cost_bps", "13")
	rec := newRecorder(t, w, "2026-10-07 16:30", 365)
	for range 3 {
		if _, err := rec.Run(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range stored(t, w) {
		from, err := time.ParseInLocation(time.DateOnly, row.Day, jst)
		if err != nil {
			t.Fatal(err)
		}
		rows, meta, err := evaldiag.Rows(t.Context(), w, query.Range{From: from, To: from.AddDate(0, 0, 1)}, evaldiag.Request{})
		if err != nil {
			t.Fatal(err)
		}
		var llm, random, momentum, contrarian float64
		evaluated, wins, entered := 0, 0, 0
		for _, r := range rows {
			if r.ModelID != row.ModelID || r.QuestionVersion != row.QuestionVersion {
				continue
			}
			llm, random, momentum, contrarian = llm+r.LLM, random+r.Random, momentum+r.Momentum, contrarian+r.Contrarian
			evaluated++
			if r.LLM > 0 {
				wins++
			}
			if r.Entered {
				entered++
			}
		}
		if row.BarrierCostBps != 13 || meta.BarrierCostBps != 13 {
			t.Errorf("%s: barrier cost stored %v, evaluated %v, want 13", row.Day, row.BarrierCostBps, meta.BarrierCostBps)
		}
		if row.Evaluated != evaluated || row.Wins != wins || row.Entered != entered ||
			!near(row.LLMSum, llm) || !near(row.RandomSum, random) || !near(row.MomentumSum, momentum) || !near(row.ContrarianSum, contrarian) {
			t.Errorf("%s %s/%s stored %+v, evaluation rows give n=%d wins=%d entered=%d llm=%v random=%v momentum=%v contrarian=%v",
				row.Day, row.ModelID, row.QuestionVersion, row, evaluated, wins, entered, llm, random, momentum, contrarian)
		}
	}
	// A cost of 13 moves the timeout rows only: id2 (SHORT mild: +10... gross -10, net -23 after 13).
	if got := stored(t, w)[0]; !near(got.LLMSum, 30-23) {
		t.Errorf("llm sum at cost 13 = %v, want %v", got.LLMSum, 30-23)
	}
}

// The record is of its day: a day that is recorded is never rewritten or duplicated, not
// by a later Run, not by a direct second insert, and not after the settings changed.
func TestRecorder_ARecordedDayIsNeverRewrittenOrDuplicated(t *testing.T) {
	_, w := testkit.NewMigratedDB(t)
	seed(t, w)
	rec := newRecorder(t, w, "2026-10-07 16:30", 365)
	for range 3 {
		if _, err := rec.Run(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	before := stored(t, w)
	testkit.Setting(t, w, "calibration.barrier_cost_bps", "25")
	for range 2 {
		if res, err := rec.Run(t.Context()); err != nil || res.Day != "" || res.Rows != 0 {
			t.Fatalf("second pass = %+v, %v; want nothing", res, err)
		}
	}
	// A direct insert of the same key with other numbers is ignored.
	r := repo.NewRepository(w)
	changed := before[0]
	changed.LLMSum, changed.Decisions, changed.Evaluated = 999, 50, 50
	if n, err := r.InsertDay(t.Context(), []repo.Row{changed}); err != nil || n != 0 {
		t.Fatalf("InsertDay of a recorded key = %d, %v; want 0", n, err)
	}
	after := stored(t, w)
	if len(after) != len(before) {
		t.Fatalf("rows %d -> %d", len(before), len(after))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Errorf("row %d changed: %+v -> %+v", i, before[i], after[i])
		}
	}
}

// Decisions before the training cutoff are dropped from the evaluation: they stay in
// decisions and count as excluded, and a day entirely before the cutoff still has a row
// (evaluated 0).
func TestRecorder_PreCutoffDecisionsAreExcludedButCounted(t *testing.T) {
	_, w := testkit.NewMigratedDB(t)
	seed(t, w)
	testkit.Setting(t, w, "metalabel.llm_cutoff", `"2026-10-06"`)
	testkit.Setting(t, w, "metalabel.llm_cutoff_model", `"m1"`)
	rec := newRecorder(t, w, "2026-10-06 16:30", 365)
	for range 2 {
		if _, err := rec.Run(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	got := stored(t, w)
	if len(got) != 3 || got[0].Day != "2026-10-05" || got[0].Decisions != 3 || got[0].Evaluated != 0 || got[0].Excluded() != 3 || got[0].LLMSum != 0 {
		t.Errorf("rows = %+v; want 10-05 m1 with 3 decisions, 0 evaluated", got)
	}
	if got[2].Day != "2026-10-06" || got[2].Evaluated != 1 {
		t.Errorf("the cutoff day itself is evaluated: %+v", got[2])
	}
}

// Only the last WindowDays days (today included) and only days within the retention are
// recorded: an older day with decisions stays unrecorded, and a day the purge would
// delete at once is not recorded either.
func TestRecorder_WindowAndRetentionBoundWhichDaysAreRecorded(t *testing.T) {
	_, w := testkit.NewMigratedDB(t)
	fx.Seed(t, w,
		fx.Spec{ID: 1, At: at(1, 0), Direction: "LONG", Model: "m1", Version: "v3", Scenario: fx.Up},
		fx.Spec{ID: 2, At: at(2, 0), Direction: "LONG", Model: "m1", Version: "v3", Scenario: fx.Up},
		fx.Spec{ID: 3, At: at(8, 0), Direction: "LONG", Model: "m1", Version: "v3", Scenario: fx.Up},
		fx.Spec{ID: 4, At: at(9, 0), Direction: "LONG", Model: "m1", Version: "v3", Scenario: fx.Up},
	)
	// today 10-08: the window is 10-02..10-08 (7 days); with a retention of 365 days 10-01 is outside it.
	rec := newRecorder(t, w, "2026-10-08 16:30", 365)
	var days []string
	for {
		res, err := rec.Run(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if res.Day == "" {
			break
		}
		days = append(days, res.Day)
	}
	if strings.Join(days, ",") != "2026-10-02,2026-10-08" {
		t.Errorf("recorded %v, want 10-02 and 10-08 (10-01 is outside the window, 10-09 is the future)", days)
	}
	// Retention of 1 day on 10-08: cutoff 10-07, so 10-02 would be purged at once and is not recorded.
	_, w2 := testkit.NewMigratedDB(t)
	fx.Seed(t, w2, fx.Spec{ID: 2, At: at(2, 0), Direction: "LONG", Model: "m1", Version: "v3", Scenario: fx.Up},
		fx.Spec{ID: 3, At: at(8, 0), Direction: "LONG", Model: "m1", Version: "v3", Scenario: fx.Up})
	rec2 := newRecorder(t, w2, "2026-10-08 16:30", 1)
	for range 3 {
		if _, err := rec2.Run(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if got := stored(t, w2); len(got) != 1 || got[0].Day != "2026-10-08" {
		t.Errorf("rows = %+v, want only 2026-10-08", got)
	}
}
