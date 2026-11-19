package backtesttests

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
)

// runView is the part of backtest_start / backtest_get / backtest_list's rows the tests read.
type runView struct {
	RunID         int64   `json:"run_id"`
	Status        string  `json:"status"`
	CreatedAt     string  `json:"created_at"`
	StartedAt     *string `json:"started_at"`
	FinishedAt    *string `json:"finished_at"`
	ReplayMode    string  `json:"replay_mode"`
	CodeVersion   string  `json:"code_version"`
	ExploredCount int     `json:"explored_count"`
	ErrorCode     *string `json:"error_code"`
	Caution       string  `json:"caution"`
	Period        struct {
		From            string  `json:"from"`
		To              string  `json:"to"`
		FirstSnapshotAt *string `json:"first_snapshot_at"`
		LastSnapshotAt  *string `json:"last_snapshot_at"`
	} `json:"period"`
}

type variantView struct {
	Index           int      `json:"index"`
	Label           string   `json:"label"`
	Params          any      `json:"params"`
	TradeCount      int      `json:"trade_count"`
	WinRate         *float64 `json:"win_rate"`
	ExpectancyPct   *float64 `json:"expectancy_pct"`
	MaxDrawdownPct  float64  `json:"max_drawdown_pct"`
	IsReferenceBest bool     `json:"is_reference_best"`
	ExpectancyCI    struct {
		Lo    *float64 `json:"lo"`
		Hi    *float64 `json:"hi"`
		Level float64  `json:"level"`
		Days  int      `json:"days"`
		HasCI bool     `json:"has_ci"`
	} `json:"expectancy_ci"`
	Days []struct {
		Date       string  `json:"date"`
		TradeCount int     `json:"trade_count"`
		NetPnLPct  float64 `json:"net_pnl_pct"`
	} `json:"days"`
	Sensitivity []struct {
		ExtraCostBps  float64 `json:"extra_cost_bps"`
		ExpectancyPct float64 `json:"expectancy_pct"`
	} `json:"sensitivity"`
}

type detailView struct {
	runView
	ExtraCostBps      []float64       `json:"extra_cost_bps"`
	RequestedVariants json.RawMessage `json:"requested_variants"`
	Variants          []variantView   `json:"variants"`
}

func count(t *testing.T, w *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := w.QueryRowContext(t.Context(), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// tableDump is every row of table as text, to compare a table before and after.
func tableDump(t *testing.T, w *sql.DB, table string) string {
	t.Helper()
	rows, err := w.QueryContext(t.Context(), "SELECT * FROM "+table+" ORDER BY 1") //nolint:gosec // test: a fixed table name
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	cols, _ := rows.Columns()
	var out strings.Builder
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(vals)
		out.Write(b)
		out.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out.String()
}
