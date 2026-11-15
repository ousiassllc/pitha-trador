package evalrecord_test

import (
	"database/sql"
	"fmt"
	"os"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/repository/evalrecord"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

func TestMain(m *testing.M) {
	code := m.Run()
	testkit.RemoveTemplate()
	os.Exit(code)
}

func decision(t *testing.T, w *sql.DB, id int, ts, kind, direction string) {
	t.Helper()
	testkit.MustExec(t, w, `INSERT OR IGNORE INTO instruments (id, symbol, name, market) VALUES (1, '7203', 'a', 'TSE')`)
	testkit.MustExec(t, w, `INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash, state_json,
		question_version, response_json, direction, confidence, latency_ms, model_id, created_at)
		VALUES (?, 1, 'S', ?, ?, 'h', '{}', 'v', '{}', ?, 0.7, 5, 'm', ?)`, id, testkit.Stored(t, ts), kind, direction, testkit.Stored(t, ts))
}

// DecisionDays lists the JST day of trader LONG/SHORT decisions only: the JST day boundary (15:00Z is
// already the next day), the half-open window, and the decision type / direction filter.
func TestDecisionDaysUseJSTDaysOfTraderLongShortDecisionsOnly(t *testing.T) {
	_, w := testkit.NewMigratedDB(t)
	decision(t, w, 1, "2026-10-05T14:59:59Z", "trader", "LONG")  // 23:59:59 JST on 10-05
	decision(t, w, 2, "2026-10-05T15:00:00Z", "trader", "SHORT") // 00:00 JST on 10-06
	decision(t, w, 3, "2026-10-06T01:00:00Z", "trader", "LONG")  // 10-06 again
	decision(t, w, 4, "2026-10-07T01:00:00Z", "trader", "NONE")
	decision(t, w, 5, "2026-10-08T01:00:00Z", "scout", "LONG")
	decision(t, w, 6, "2026-10-09T01:00:00Z", "trader", "LONG")
	got, err := evalrecord.NewRepository(w).DecisionDays(t.Context(), "2026-10-05", "2026-10-09")
	if err != nil || fmt.Sprint(got) != "[2026-10-05 2026-10-06]" {
		t.Fatalf("DecisionDays = %v, %v; want [2026-10-05 2026-10-06]", got, err)
	}
	if got, err := evalrecord.NewRepository(w).DecisionDays(t.Context(), "2026-10-05", "2026-10-10"); err != nil || fmt.Sprint(got) != "[2026-10-05 2026-10-06 2026-10-09]" {
		t.Fatalf("DecisionDays = %v, %v", got, err)
	}
	if _, err := evalrecord.NewRepository(w).DecisionDays(t.Context(), "bad", "2026-10-10"); err == nil {
		t.Error("a bad day is accepted")
	}
}

// List honors the window (to exclusive), the filters and the row limit (more than limit is ok=false).
func TestListWindowFiltersAndLimit(t *testing.T) {
	_, w := testkit.NewMigratedDB(t)
	r := evalrecord.NewRepository(w)
	var rows []evalrecord.Row
	for _, day := range []string{"2026-10-05", "2026-10-06", "2026-10-07"} {
		for _, h := range []int{10, 20} {
			rows = append(rows, evalrecord.Row{Day: day, ModelID: "m", QuestionVersion: "v", HorizonMinutes: h, TakeProfitBps: 30, StopLossBps: 30, BarrierCostBps: 10, Decisions: 2, Evaluated: 1, Entered: 1, Wins: 1, LLMSum: 7})
		}
	}
	if n, err := r.InsertDay(t.Context(), rows); err != nil || n != 6 {
		t.Fatalf("InsertDay = %d, %v", n, err)
	}
	got, ok, err := evalrecord.List(t.Context(), w, "2026-10-05", "2026-10-07", evalrecord.Filter{}, 100)
	if err != nil || !ok || len(got) != 4 || got[0].Day != "2026-10-05" || got[0].HorizonMinutes != 10 || got[3].Day != "2026-10-06" || got[3].HorizonMinutes != 20 {
		t.Fatalf("List = %+v, %v, %v", got, ok, err)
	}
	h := 20
	if got, _, _ := evalrecord.List(t.Context(), w, "2026-10-05", "2026-10-08", evalrecord.Filter{HorizonMinutes: &h}, 100); len(got) != 3 {
		t.Errorf("horizon filter = %d rows, want 3", len(got))
	}
	if got, _, _ := evalrecord.List(t.Context(), w, "2026-10-05", "2026-10-08", evalrecord.Filter{ModelID: "x"}, 100); len(got) != 0 {
		t.Errorf("model filter = %d rows, want 0", len(got))
	}
	if got, ok, _ := evalrecord.List(t.Context(), w, "2026-10-05", "2026-10-08", evalrecord.Filter{}, 6); !ok || len(got) != 6 {
		t.Errorf("exactly the limit = %d rows ok=%v, want all 6", len(got), ok)
	}
	if got, ok, _ := evalrecord.List(t.Context(), w, "2026-10-05", "2026-10-08", evalrecord.Filter{}, 5); ok || got != nil {
		t.Errorf("over the limit = %d rows ok=%v, want nothing partial", len(got), ok)
	}
	span, err := evalrecord.RecordedSpan(t.Context(), w)
	if err != nil || span.Days != 3 || span.FirstDay != "2026-10-05" || span.LastDay != "2026-10-07" {
		t.Errorf("span = %+v, %v", span, err)
	}
}
