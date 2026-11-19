package backtesttests

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/repository/backtestrun"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
	"github.com/ousiassllc/pitha-trador/internal/version"
)

var twoVariants = []map[string]any{
	{"label": "baseline", "params": map[string]any{}},
	{"label": "tp-0.3", "params": map[string]any{"take_profit_pct": 0.3}},
}

func TestStartReturnsAQueuedRunAndGetReadsTheResult(t *testing.T) {
	srv := newServer(t, options{})
	queued := srv.start(t, map[string]any{"variants": twoVariants, "extra_cost_bps": []float64{0, 5}})
	if queued.Status != "queued" || queued.RunID != 1 || queued.ExploredCount != 2 || queued.ReplayMode != "paper" {
		t.Fatalf("start = %+v", queued)
	}
	srv.waitFinished(t, queued.RunID)

	got := srv.get(t, queued.RunID)
	if got.Status != "succeeded" || got.ErrorCode != nil || got.StartedAt == nil || got.FinishedAt == nil {
		t.Fatalf("get = %+v", got.runView)
	}
	if got.CodeVersion != version.Version || got.ExploredCount != 2 || !strings.Contains(got.Caution, "多重比較") {
		t.Errorf("code version %q, explored %d, caution %q", got.CodeVersion, got.ExploredCount, got.Caution)
	}
	if got.Period.From != "2026-10-05" || got.Period.To != "2026-10-07" ||
		got.Period.FirstSnapshotAt == nil || *got.Period.FirstSnapshotAt != "2026-10-05T00:00:00Z" ||
		got.Period.LastSnapshotAt == nil || *got.Period.LastSnapshotAt != "2026-10-06T00:15:00Z" {
		t.Errorf("period = %+v: the requested JST dates and the data actually found are both reported", got.Period)
	}
	if len(got.ExtraCostBps) != 2 || len(got.Variants) != 2 {
		t.Fatalf("extra costs %v, %d variants", got.ExtraCostBps, len(got.Variants))
	}
	var requested []struct {
		Label string `json:"label"`
	}
	if err := json.Unmarshal(got.RequestedVariants, &requested); err != nil || len(requested) != 2 || requested[1].Label != "tp-0.3" {
		t.Errorf("requested variants = %s (%v)", got.RequestedVariants, err)
	}
	a, b := got.Variants[0], got.Variants[1]
	if a.TradeCount == 0 || a.ExpectancyPct == nil || a.WinRate == nil || len(a.Days) != 2 || !a.ExpectancyCI.HasCI || a.ExpectancyCI.Level != 0.95 || len(a.Sensitivity) != 2 {
		t.Errorf("baseline = %+v", a)
	}
	if a.TradeCount == b.TradeCount && *a.ExpectancyPct == *b.ExpectancyPct {
		t.Errorf("a different take profit gave the same result: %+v vs %+v", a, b)
	}
	if (a.IsReferenceBest) == (b.IsReferenceBest) {
		t.Errorf("exactly one variant is the reference best, got %v and %v", a.IsReferenceBest, b.IsReferenceBest)
	}
	params, _ := json.Marshal(b.Params)
	if string(params) != `{"take_profit_pct":0.3}` {
		t.Errorf("the parameters recorded with the result = %s", params)
	}
}

func TestReplayModeAndPeriodReachTheSource(t *testing.T) {
	src := newServer(t, options{})
	src.startAndWait(t, map[string]any{"replay_mode": "live"})
	if got := <-src.modes; got != "live" {
		t.Errorf("the source was asked for mode %q, want live", got)
	}
	src.startAndWait(t, nil)
	if got := <-src.modes; got != "paper" {
		t.Errorf("the default replay mode = %q, want paper", got)
	}
	src.call(t, "backtest_start", withPeriod(map[string]any{"replay_mode": "shadow"})).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
}

// Same input and parameters: the stored result is byte-identical (the interval included).
func TestTheSameInputGivesByteIdenticalResults(t *testing.T) {
	srv := newServer(t, options{})
	args := map[string]any{"variants": twoVariants}
	first := srv.start(t, args)
	srv.waitFinished(t, first.RunID)
	second := srv.start(t, args)
	srv.waitFinished(t, second.RunID)

	var results []string
	for _, id := range []int64{first.RunID, second.RunID} {
		var res string
		if err := srv.w.QueryRowContext(t.Context(), `SELECT result_json FROM backtest_runs WHERE id = ?`, id).Scan(&res); err != nil {
			t.Fatal(err)
		}
		results = append(results, res)
	}
	if results[0] == "" || results[0] != results[1] {
		t.Fatalf("two runs of one input differ:\n%s\n%s", results[0], results[1])
	}
	a, b := srv.get(t, first.RunID), srv.get(t, second.RunID)
	if a.Variants[0].ExpectancyCI.Lo == nil || *a.Variants[0].ExpectancyCI.Lo != *b.Variants[0].ExpectancyCI.Lo {
		t.Errorf("the interval differs between two runs")
	}
}

// A backtest only reads: runtime_settings (and the settings audit) are the same
// before and after, and the only statements that write are the four of backtest_runs.
func TestARunNeverChangesRuntimeSettingsAndWritesOnlyItsOwnTable(t *testing.T) {
	driverName, counter := testkit.NewCountingDriver(t)
	srv := newServer(t, options{driver: driverName})
	testkit.MustExec(t, srv.w, `INSERT INTO runtime_settings (key, value, updated_at) VALUES ('risk.paper.max_trade_loss_pct', '1', '2026-10-01T00:00:00.000000000Z')`)
	beforeSettings := tableDump(t, srv.w, "runtime_settings")
	beforeAudit := tableDump(t, srv.w, "mcp_settings_audit")
	beforePositions := tableDump(t, srv.w, "positions")

	// A variant that names the very parameters the settings hold must not move them either.
	srv.startAndWait(t, map[string]any{"variants": []map[string]any{{"params": map[string]any{"max_trade_loss_pct": 2.0, "stop_loss_pct": 3.0, "fee_bps": 9.0}}}})
	srv.startAndWait(t, nil)

	if got := tableDump(t, srv.w, "runtime_settings"); got != beforeSettings {
		t.Errorf("runtime_settings changed:\n%s\n->\n%s", beforeSettings, got)
	}
	if got := tableDump(t, srv.w, "mcp_settings_audit"); got != beforeAudit {
		t.Errorf("the settings audit changed")
	}
	if got := tableDump(t, srv.w, "positions"); got != beforePositions {
		t.Errorf("positions changed")
	}
	writes := 0
	for _, stmt := range counter.StatementLog() {
		s := strings.ToUpper(strings.TrimSpace(stmt))
		if !isWrite(s) {
			continue
		}
		writes++
		if !strings.Contains(s, "BACKTEST_RUNS") {
			t.Errorf("a write outside backtest_runs: %s", testkit.FirstLine(stmt))
		}
	}
	// Per run: orphan sweep (once), insert, start, finish.
	if writes != 1+2*3 {
		t.Errorf("%d write statements, want 7 (one orphan sweep, then insert, start and finish for each of two runs)", writes)
	}
}

func TestToolsAreRegisteredWithTheirWriteHints(t *testing.T) {
	srv := newServer(t, options{})
	res := srv.rpc(t, "tools/list", map[string]any{})
	var env struct {
		Result struct {
			Tools []struct {
				Name        string `json:"name"`
				Annotations struct {
					ReadOnlyHint bool `json:"readOnlyHint"`
				} `json:"annotations"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(res.body, &env); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"backtest_start": false, "backtest_get": true, "backtest_list": true}
	for _, tool := range env.Result.Tools {
		if ro, ok := want[tool.Name]; ok {
			if tool.Annotations.ReadOnlyHint != ro {
				t.Errorf("%s: readOnlyHint = %v, want %v", tool.Name, tool.Annotations.ReadOnlyHint, ro)
			}
			delete(want, tool.Name)
		}
	}
	if len(want) != 0 {
		t.Errorf("tools missing from tools/list: %v", want)
	}
}

func TestStartIsRefusedWhileWritesAreSwitchedOff(t *testing.T) {
	srv := newServer(t, options{})
	srv.call(t, "settings_write_disable", map[string]any{}).data(t, "settings_write_disable", &struct{}{})
	srv.call(t, "backtest_start", withPeriod(nil)).wantError(t, http.StatusForbidden, paperresultsmcp.CodeWriteDisabled)
	if n := count(t, srv.w, `SELECT COUNT(*) FROM backtest_runs`); n != 0 {
		t.Fatalf("%d rows written while the write switch is off", n)
	}
	// Reading still works, and a run accepted before the switch went off still ends.
	var list struct {
		Runs []runView `json:"runs"`
	}
	srv.call(t, "backtest_list", map[string]any{}).data(t, "backtest_list", &list)
	if len(list.Runs) != 0 {
		t.Errorf("list = %+v", list)
	}
}

func TestARunAcceptedBeforeTheSwitchGoesOffStillEnds(t *testing.T) {
	srv := newServer(t, options{source: nil})
	r := srv.start(t, nil)
	srv.call(t, "settings_write_disable", map[string]any{}).data(t, "settings_write_disable", &struct{}{})
	srv.waitFinished(t, r.RunID)
	if got := srv.get(t, r.RunID); got.Status != backtestrun.StatusSucceeded {
		t.Fatalf("status %q: a run that was accepted must still reach its final status", got.Status)
	}
}

func TestWithoutASourceStartIsUnavailableAndWritesNothing(t *testing.T) {
	srv := newServerWithoutSource(t)
	srv.call(t, "backtest_start", withPeriod(nil)).wantError(t, http.StatusServiceUnavailable, paperresultsmcp.CodeBacktestUnavailable)
	if n := count(t, srv.w, `SELECT COUNT(*) FROM backtest_runs`); n != 0 {
		t.Fatalf("%d rows written", n)
	}
}

func isWrite(upperStatement string) bool {
	for _, verb := range []string{"INSERT", "UPDATE", "DELETE", "CREATE", "DROP", "ALTER"} {
		if strings.HasPrefix(upperStatement, verb) {
			return true
		}
	}
	return false
}
