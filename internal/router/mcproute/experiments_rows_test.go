package mcproute_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/experiment"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// issue #880: one experiment that cannot be aggregated (a period with more
// closed trades than one aggregate may read) is shown as that row's error; the
// other rows are still listed. The Settings screen used to answer 500 for the
// whole list.
func TestExperimentsListShowsAFailingRowAmongTheOthers(t *testing.T) {
	a, db := newExperimentsApp(t)
	gate := mcpset.NewGate(db)
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Second)
	bigStart := now.AddDate(0, 0, -20)

	seed := func(at time.Time, n int) {
		testkit.MustExec(t, db, `WITH RECURSIVE seq(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM seq WHERE i < ?)
			INSERT INTO positions (instrument_id, entry_order_id, symbol, side, quantity, entry_price, current_price, unrealized_pnl, realized_pnl, opened_at, closed_at)
			SELECT 1, 99, '7203', 'LONG', 100, 1000, 1000, 0, 10, ?, ? FROM seq`, n, testkit.Stored(t, "2026-09-30T00:00:00Z"), testkit.Stored(t, at.Format(time.RFC3339)))
	}
	seed(bigStart.AddDate(0, 0, -5), 100) // the before period of the big experiment
	seed(bigStart.AddDate(0, 0, 2), query.MaxClosedScan+1)
	testkit.MustExec(t, db, `INSERT INTO mcp_settings_audit (at, actor, client, action, setting_key, before_value, after_value)
		VALUES (?, 'gui', '', 'set', 'risk.paper.max_trade_loss_pct', '1', '0.5')`, testkit.Stored(t, bigStart.Format(time.RFC3339)))

	change := experiment.Change{Kind: experiment.KindSetting, Name: "risk.paper.max_trade_loss_pct", Before: "1", After: "0.5"}
	big, err := gate.CreateExperiment(ctx, experiment.Request{Hypothesis: "big", Change: change, StartedAt: bigStart})
	if err != nil {
		t.Fatal(err)
	}
	small, err := gate.CreateExperiment(ctx, experiment.Request{Hypothesis: "small", Change: change, StartedAt: now.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}

	rec := a.get("/settings/mcp/experiments")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d, want 200 although one row cannot be summarised: %s", rec.Code, body)
	}
	rows := strings.Split(body, `data-testid="mcp-experiment-row"`)[1:]
	if len(rows) != 2 {
		t.Fatalf("%d rows, want both experiments: %s", len(rows), body)
	}
	// Newest first: the small one lists its verdict, the big one its error.
	if !strings.Contains(rows[0], `data-experiment-id="`+itoa(small.ID)+`"`) || !strings.Contains(rows[0], `data-verdict="insufficient_trades"`) || strings.Contains(rows[0], "mcp-experiment-row-error") {
		t.Errorf("the other row is not intact: %s", rows[0])
	}
	if !strings.Contains(rows[1], `data-experiment-id="`+itoa(big.ID)+`"`) || !strings.Contains(rows[1], `data-testid="mcp-experiment-row-error"`) || !strings.Contains(rows[1], "集計できません") || !strings.Contains(rows[1], "5,000") {
		t.Errorf("the failing row does not say why: %s", rows[1])
	}

	// Its comparison says it too, instead of a bare 500.
	if rec := a.get("/settings/mcp/experiments/" + itoa(big.ID)); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("comparison of the failing experiment = %d, want 422", rec.Code)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
