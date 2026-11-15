package experimenttests

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/experiment"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// experiments_list (issue #880): a row costs no more than it must, one row that
// cannot be summarised is that row's error and not the list's, and an
// experiment that ended long ago is not aggregated again.

// plantMany plants n closed trades of pnl 10 at one instant, in one statement.
func plantMany(t *testing.T, w *sql.DB, closedAt string, n int) {
	t.Helper()
	testkit.MustExec(t, w, `WITH RECURSIVE seq(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM seq WHERE i < ?)
		INSERT INTO positions (instrument_id, entry_order_id, symbol, side, quantity, entry_price, current_price, unrealized_pnl, realized_pnl, opened_at, closed_at)
		SELECT 1, 99, '7203', 'LONG', 100, 1000, 1000, 0, 10, ?, ? FROM seq`, n, testkit.Stored(t, "2026-09-30T00:00:00Z"), testkit.Stored(t, closedAt))
}

type listed struct {
	Experiments []experiment.Summary `json:"experiments"`
}

func listAll(t *testing.T, srv *testServer) []experiment.Summary {
	t.Helper()
	var l listed
	srv.call(t, "experiments_list", nil).data(t, "experiments_list", &l)
	return l.Experiments
}

// A running experiment whose period holds more than query.MaxClosedScan trades
// cannot be aggregated (experiment_compare fails for it); the list shows that
// row's error with its trade counts, and every other row as it was.
func TestOneRowThatCannotBeSummarisedDoesNotRemoveTheOthers(t *testing.T) {
	srv, w := newServer(t)
	plantImproved(t, w)
	good := create(t, srv, createArgs(key, nil))
	closeAt(t, srv, good.ID, endedAt)

	const bigStart = "2026-10-10T00:00:00Z"
	plantMany(t, w, "2026-10-05T00:00:00Z", 10) // the before period of the big one
	plantMany(t, w, "2026-10-12T00:00:00Z", query.MaxClosedScan+1)
	big := create(t, srv, createArgs(key, map[string]any{"started_at": bigStart}))

	rows := listAll(t, srv)
	if len(rows) != 2 || rows[0].Experiment.ID != big.ID || rows[1].Experiment.ID != good.ID {
		t.Fatalf("rows = %+v, want the big experiment then the good one", rows)
	}
	if r := rows[0]; r.Error != experiment.SummaryLimitExceeded || r.Verdict != "" || r.Judged || r.AfterTrades != query.MaxClosedScan+1 || r.BeforeTrades < 64 {
		t.Errorf("big row = %+v, want limit_exceeded with its trade counts and no verdict", r)
	}
	if r := rows[1]; r.Error != "" || r.Verdict != experiment.VerdictImproved || !r.Judged || r.BeforeTrades != 64 || r.AfterTrades != 64 {
		t.Errorf("good row = %+v, want its judged verdict untouched", r)
	}
}

// An experiment that is withheld without a test (here: too few trades) is
// listed however long its period is: counting needs no scan limit.
func TestARowWithheldWithoutATestIsListedEvenOverTheScanLimit(t *testing.T) {
	srv, w := newServer(t)
	plantMany(t, w, "2026-10-12T00:00:00Z", query.MaxClosedScan+1)
	create(t, srv, createArgs(key, map[string]any{"started_at": "2026-10-10T00:00:00Z"})) // before period: nothing

	rows := listAll(t, srv)
	if len(rows) != 1 || rows[0].Error != "" || rows[0].Verdict != experiment.VerdictInsufficientTrades || rows[0].AfterTrades != query.MaxClosedScan+1 {
		t.Fatalf("rows = %+v, want insufficient_trades with 5001 after-trades and no error", rows)
	}
}

// The list reads no trade for a row it can settle by counting: a confounded
// experiment is not aggregated at all.
func TestAListRowThatIsWithheldReadsNoTrades(t *testing.T) {
	driver, counter := testkit.NewCountingDriver(t)
	srv, w := newServerOn(t, driver)
	plantImproved(t, w)
	e := create(t, srv, createArgs(key, nil))
	closeAt(t, srv, e.ID, endedAt)
	auditRow(t, w, inside, "set", "risk.max_daily_trades", "3", "4") // confounds

	before := len(counter.StatementLog())
	rows := listAll(t, srv)
	if len(rows) != 1 || rows[0].Verdict != experiment.VerdictConfounded || rows[0].BeforeTrades != 64 || rows[0].AfterTrades != 64 {
		t.Fatalf("rows = %+v, want confounded with the counts", rows)
	}
	for _, stmt := range counter.StatementLog()[before:] {
		if strings.Contains(stmt, "realized_pnl, p.entry_price") || strings.Contains(stmt, "ORDER BY p.closed_at") {
			t.Errorf("the list read the trades of a withheld row: %s", testkit.FirstLine(stmt))
		}
	}

	// The row says what experiment_compare says.
	if c := compare(t, srv, e.ID, nil); c.Verdict != rows[0].Verdict || c.Judged != rows[0].Judged {
		t.Errorf("compare %s/%v, list %s/%v", c.Verdict, c.Judged, rows[0].Verdict, rows[0].Judged)
	}
}

func readsTrades(stmts []string) bool {
	for _, s := range stmts {
		if strings.Contains(s, "FROM positions") {
			return true
		}
	}
	return false
}

// An experiment that ended long ago is aggregated once; the next listing reads
// nothing but the experiment table. A running one is aggregated every time.
func TestAFinishedExperimentIsNotAggregatedAgain(t *testing.T) {
	driver, counter := testkit.NewCountingDriver(t)
	srv, w := newServerOn(t, driver)
	plantImproved(t, w)
	finished := create(t, srv, createArgs(key, nil))
	closeAt(t, srv, finished.ID, endedAt) // the fixed clock is two weeks later

	mark := len(counter.StatementLog())
	first := listAll(t, srv)
	if !readsTrades(counter.StatementLog()[mark:]) || first[0].Verdict != experiment.VerdictImproved {
		t.Fatalf("the first listing must aggregate: %+v", first)
	}
	mark = len(counter.StatementLog())
	second := listAll(t, srv)
	if stmts := counter.StatementLog()[mark:]; readsTrades(stmts) || len(second) != 1 || second[0].Verdict != first[0].Verdict || second[0].AfterTrades != 64 {
		t.Fatalf("the second listing aggregated again or differs: %v %+v", stmts, second)
	}

	running := create(t, srv, createArgs(key, map[string]any{"started_at": "2026-10-10T00:00:00Z"}))
	listAll(t, srv)
	mark = len(counter.StatementLog())
	rows := listAll(t, srv)
	if !readsTrades(counter.StatementLog()[mark:]) || len(rows) != 2 || rows[0].Experiment.ID != running.ID {
		t.Fatalf("a running experiment must be summarised every time: %+v", rows)
	}
}

// A different min_trades is a different verdict: it is not served from the
// entry made for another one.
func TestTheCacheIsPerMinTrades(t *testing.T) {
	srv, w := newServer(t)
	plantImproved(t, w)
	e := create(t, srv, createArgs(key, nil))
	closeAt(t, srv, e.ID, endedAt)
	if rows := listAll(t, srv); rows[0].Verdict != experiment.VerdictImproved {
		t.Fatalf("rows = %+v", rows)
	}
	var l listed
	srv.call(t, "experiments_list", map[string]any{"min_trades": 100}).data(t, "experiments_list", &l)
	if r := l.Experiments[0]; r.Verdict != experiment.VerdictInsufficientTrades || r.MinTrades != 100 {
		t.Errorf("min_trades 100: %+v, want insufficient_trades at 100", r)
	}
}
