package experimenttests

import (
	"net/http"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/experiment"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// 変数 1 つの検査: a request naming two variables is refused and nothing is
// stored; so are the other invalid requests.
func TestCreateRefusesMoreThanOneVariable(t *testing.T) {
	srv, w := newServer(t)
	two := createArgs("risk.paper.max_trade_loss_pct", nil)
	two["changes"] = []map[string]any{
		settingChange("risk.paper.max_trade_loss_pct", 1, 0.5),
		settingChange("risk.max_daily_trades", 5, 3),
	}
	srv.call(t, "experiment_create", two).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeMultipleVariables)

	modelAndSetting := createArgs("risk.paper.max_trade_loss_pct", nil)
	modelAndSetting["changes"] = []map[string]any{
		settingChange("risk.paper.max_trade_loss_pct", 1, 0.5),
		{"kind": "model", "name": "jev-model", "before": "a", "after": "b"},
	}
	srv.call(t, "experiment_create", modelAndSetting).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeMultipleVariables)

	for name, args := range map[string]map[string]any{
		"no variable":          createArgs("x", map[string]any{"changes": []map[string]any{}}),
		"key off the list":     createArgs("min_entry_quality", nil),
		"unchanged value":      createArgs("risk.paper.max_trade_loss_pct", map[string]any{"changes": []map[string]any{settingChange("risk.paper.max_trade_loss_pct", 1, "1")}}),
		"empty hypothesis":     createArgs("risk.paper.max_trade_loss_pct", map[string]any{"hypothesis": ""}),
		"future start":         createArgs("risk.paper.max_trade_loss_pct", map[string]any{"started_at": "2026-10-19T00:00:00Z"}),
		"audit id of nothing":  createArgs("risk.paper.max_trade_loss_pct", map[string]any{"audit_id": 99}),
		"unknown argument":     createArgs("risk.paper.max_trade_loss_pct", map[string]any{"variables": 2}),
		"object as the change": createArgs("risk.paper.max_trade_loss_pct", map[string]any{"changes": []map[string]any{{"kind": "setting", "name": "k", "before": map[string]any{}, "after": 1}}}),
	} {
		if r := srv.call(t, "experiment_create", args); r.status == http.StatusOK {
			t.Errorf("%s: accepted: %s", name, r.body)
		}
	}
	if n := count(t, w, `SELECT COUNT(*) FROM tuning_experiments`); n != 0 {
		t.Fatalf("%d experiments stored by refused requests, want none", n)
	}
}

// 書き込み許可: experiment_create and experiment_close follow the write
// switch; reads do not.
func TestExperimentWritesFollowTheWriteSwitch(t *testing.T) {
	srv, w := newServer(t)
	e := create(t, srv, createArgs("risk.paper.max_trade_loss_pct", nil))
	srv.call(t, "settings_write_disable", map[string]any{}).data(t, "settings_write_disable", &struct{}{})

	srv.call(t, "experiment_create", createArgs("risk.max_daily_trades", nil)).wantError(t, http.StatusForbidden, paperresultsmcp.CodeWriteDisabled)
	srv.call(t, "experiment_close", map[string]any{"experiment_id": e.ID}).wantError(t, http.StatusForbidden, paperresultsmcp.CodeWriteDisabled)
	if n := count(t, w, `SELECT COUNT(*) FROM tuning_experiments`); n != 1 {
		t.Fatalf("%d experiments, want only the first", n)
	}
	if n := count(t, w, `SELECT COUNT(*) FROM tuning_experiments WHERE status = 'running'`); n != 1 {
		t.Fatal("the experiment was closed while writes were off")
	}
	var list struct {
		Experiments []experiment.Summary `json:"experiments"`
	}
	srv.call(t, "experiments_list", map[string]any{}).data(t, "experiments_list", &list)
	compare(t, srv, e.ID, nil)
	if len(list.Experiments) != 1 {
		t.Errorf("list while writes are off = %d rows, want 1", len(list.Experiments))
	}

	srv.call(t, "settings_write_enable", map[string]any{}).data(t, "settings_write_enable", &struct{}{})
	create(t, srv, createArgs("risk.max_daily_trades", nil))
}

func TestCloseEndsARunningExperimentOnce(t *testing.T) {
	srv, w := newServer(t)
	e := create(t, srv, createArgs("risk.paper.max_trade_loss_pct", nil))

	srv.call(t, "experiment_close", map[string]any{"experiment_id": 99}).wantError(t, http.StatusNotFound, paperresultsmcp.CodeExperimentNotFound)
	// The end must not precede the start or lie in the future.
	for _, at := range []string{"2026-10-01T14:59:59Z", "2026-10-19T00:00:00Z", "not a time"} {
		if r := srv.call(t, "experiment_close", map[string]any{"experiment_id": e.ID, "ended_at": at}); r.status != http.StatusUnprocessableEntity {
			t.Errorf("ended_at %q: status %d, want 422 (%s)", at, r.status, r.body)
		}
	}
	srv.call(t, "experiment_close", map[string]any{"experiment_id": e.ID, "status": "paused"}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)

	var closed experiment.Experiment
	srv.call(t, "experiment_close", map[string]any{"experiment_id": e.ID, "ended_at": endedAt}).data(t, "experiment_close", &closed)
	if closed.Status != experiment.StatusClosed || closed.EndedAt == nil || *closed.EndedAt != endedAt {
		t.Fatalf("closed = %+v", closed)
	}
	srv.call(t, "experiment_close", map[string]any{"experiment_id": e.ID}).wantError(t, http.StatusConflict, paperresultsmcp.CodeExperimentNotRunning)
	var ended string
	if err := w.QueryRow(`SELECT ended_at FROM tuning_experiments WHERE id = ?`, e.ID).Scan(&ended); err != nil || ended != testkit.Stored(t, endedAt) {
		t.Errorf("stored ended_at = %q (%v), want the first close kept", ended, err)
	}

	// An experiment with no end time closes now (the fixed clock).
	second := create(t, srv, createArgs("risk.max_daily_trades", nil))
	srv.call(t, "experiment_close", map[string]any{"experiment_id": second.ID}).data(t, "experiment_close", &closed)
	if closed.EndedAt == nil || *closed.EndedAt != "2026-10-18T03:00:00Z" {
		t.Errorf("default end = %v, want now", closed.EndedAt)
	}
}

// An aborted experiment is never judged, however its numbers look.
func TestAbortedExperimentIsNotJudged(t *testing.T) {
	srv, _ := newServer(t)
	e := create(t, srv, createArgs("risk.paper.max_trade_loss_pct", nil))
	srv.call(t, "experiment_close", map[string]any{"experiment_id": e.ID, "status": "aborted", "ended_at": endedAt}).data(t, "experiment_close", &experiment.Experiment{})
	if c := compare(t, srv, e.ID, nil); c.Verdict != experiment.VerdictAborted || c.Judged {
		t.Errorf("aborted: verdict %s judged=%v", c.Verdict, c.Judged)
	}
}

func TestListFiltersByStatusAndOrdersNewestFirst(t *testing.T) {
	srv, _ := newServer(t)
	first := create(t, srv, createArgs("risk.paper.max_trade_loss_pct", nil))
	second := create(t, srv, createArgs("risk.max_daily_trades", nil))
	closeAt(t, srv, first.ID, endedAt)

	list := func(args map[string]any) []int64 {
		var out struct {
			Experiments []experiment.Summary `json:"experiments"`
		}
		srv.call(t, "experiments_list", args).data(t, "experiments_list", &out)
		var ids []int64
		for _, r := range out.Experiments {
			ids = append(ids, r.Experiment.ID)
		}
		return ids
	}
	if got := list(map[string]any{}); !reflect.DeepEqual(got, []int64{second.ID, first.ID}) {
		t.Errorf("all = %v, want newest first", got)
	}
	if got := list(map[string]any{"status": "running"}); !reflect.DeepEqual(got, []int64{second.ID}) {
		t.Errorf("running = %v", got)
	}
	if got := list(map[string]any{"limit": 1}); !reflect.DeepEqual(got, []int64{second.ID}) {
		t.Errorf("limit 1 = %v", got)
	}
	srv.call(t, "experiments_list", map[string]any{"limit": 51}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeLimitExceeded)
	srv.call(t, "experiments_list", map[string]any{"status": "paused"}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
	srv.call(t, "experiment_compare", map[string]any{"experiment_id": 99}).wantError(t, http.StatusNotFound, paperresultsmcp.CodeExperimentNotFound)
}

// experiments_list and experiment_compare only read: every statement they run
// is a SELECT of named columns, none touches a table the other tools keep
// private, and they never open the write connection.
func TestExperimentReadsRunOnlyNamedColumnSelects(t *testing.T) {
	driverName, counter := testkit.NewCountingDriver(t)
	srv, _ := newServerOn(t, driverName)
	e := create(t, srv, createArgs("risk.paper.max_trade_loss_pct", nil))
	closeAt(t, srv, e.ID, endedAt)

	before := len(counter.StatementLog())
	srv.call(t, "experiments_list", nil).data(t, "experiments_list", &struct{}{})
	srv.call(t, "experiment_compare", map[string]any{"experiment_id": e.ID}).data(t, "experiment_compare", &struct{}{})
	stmts := counter.StatementLog()[before:]
	if len(stmts) == 0 {
		t.Fatal("no statement was recorded")
	}
	private := regexp.MustCompile(`(?i)\b(secrets|raw_data_json|state_json|response_json|encrypted_value)\b`)
	for _, stmt := range stmts {
		trimmed := strings.TrimSpace(stmt)
		if !strings.HasPrefix(strings.ToUpper(trimmed), "SELECT") && trimmed != "BEGIN" {
			t.Errorf("a read tool ran a non-SELECT statement: %s", testkit.FirstLine(stmt))
		}
		if strings.Contains(strings.ToUpper(stmt), "SELECT *") || private.MatchString(stmt) {
			t.Errorf("statement reads more than the tool needs: %s", testkit.FirstLine(stmt))
		}
	}
}
