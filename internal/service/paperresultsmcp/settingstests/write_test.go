package settingstests

import (
	"math"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

type perfData struct {
	CapitalJPY              *float64 `json:"capital_jpy"`
	MaxDrawdownCapitalRatio *float64 `json:"max_drawdown_capital_ratio"`
}

func near(t *testing.T, name string, got *float64, want float64) {
	t.Helper()
	if got == nil || math.Abs(*got-want) > 1e-9 {
		t.Errorf("%s = %v, want %v", name, got, want)
	}
}

// seededServer is a server over a fully seeded database.
func seededServer(t *testing.T, mutate func(*paperresultsmcp.Options)) *testServer {
	t.Helper()
	path, w := testkit.NewMigratedDB(t)
	testkit.SeedAll(t, w)
	opts := paperresultsmcp.Options{Settings: enabledSettings(), DBPath: path}
	if mutate != nil {
		mutate(&opts)
	}
	return newTestServer(t, opts)
}

var period = map[string]any{"from": "2026-10-01", "to": "2026-10-10"}

// Writes off: settings_set is refused and stores nothing; the read tools and
// the switch tools still work; turning writes back on restores settings_set.
func TestWriteSwitchBlocksSettingsSet(t *testing.T) {
	srv, w := settingsServer(t, nil)
	var sw struct {
		WriteEnabled bool  `json:"write_enabled"`
		AuditID      int64 `json:"audit_id"`
	}
	srv.call(t, "settings_write_disable", map[string]any{}).data(t, "settings_write_disable", &sw)
	if sw.WriteEnabled || sw.AuditID == 0 {
		t.Fatalf("disable = %+v", sw)
	}
	if st := getState(srv, t); st.WriteEnabled {
		t.Fatal("settings_get still reports writes on")
	}
	set(srv, t, "risk.paper.max_trade_loss_pct", 2).wantError(t, http.StatusForbidden, paperresultsmcp.CodeWriteDisabled)
	if _, ok := storedValue(t, w, "risk.paper.max_trade_loss_pct"); ok {
		t.Fatal("a refused write was stored")
	}
	// The read tools are unaffected.
	srv.call(t, "settings_diff", map[string]any{}).data(t, "settings_diff", &settingsDiff{})
	srv.call(t, "settings_audit", map[string]any{}).data(t, "settings_audit", &auditList{})
	srv.call(t, "paper_daily_pnl", period).data(t, "paper_daily_pnl", &struct{}{})

	srv.call(t, "settings_write_enable", map[string]any{}).data(t, "settings_write_enable", &sw)
	if !sw.WriteEnabled {
		t.Fatalf("enable = %+v", sw)
	}
	set(srv, t, "risk.paper.max_trade_loss_pct", 2).data(t, "settings_set", &struct{}{})

	var al auditList
	srv.call(t, "settings_audit", map[string]any{}).data(t, "settings_audit", &al)
	var actions []string
	for _, e := range al.Entries {
		actions = append(actions, e.Action)
	}
	if !slices.Equal(actions, []string{"set", "write_enable", "write_disable"}) {
		t.Fatalf("audit actions (newest first) = %v", actions)
	}
	if e := al.Entries[2]; e.Key != "mcp.settings_write_enabled" || e.Before == nil || *e.Before != "1" || e.After == nil || *e.After != "0" {
		t.Fatalf("write_disable row = %+v", e)
	}
}

// A write switch stored by the Settings screen (0) is honoured by the MCP
// process, and the MCP's own flip is seen by the application: one value, one
// place.
func TestWriteSwitchIsSharedThroughRuntimeSettings(t *testing.T) {
	srv, w := settingsServer(t, nil)
	testkit.MustExec(t, w, `INSERT INTO runtime_settings (key, value, updated_at) VALUES ('mcp.settings_write_enabled', '0', 't')`)
	set(srv, t, "risk.paper.max_trade_loss_pct", 2).wantError(t, http.StatusForbidden, paperresultsmcp.CodeWriteDisabled)
	srv.call(t, "settings_write_enable", map[string]any{}).data(t, "settings_write_enable", &struct{}{})
	if v, _ := storedValue(t, w, "mcp.settings_write_enabled"); v != "1" {
		t.Fatalf("stored switch = %q after settings_write_enable", v)
	}
	// A damaged stored value fails closed.
	testkit.MustExec(t, w, `UPDATE runtime_settings SET value = '"yes"' WHERE key = 'mcp.settings_write_enabled'`)
	set(srv, t, "risk.paper.max_trade_loss_pct", 2).wantError(t, http.StatusForbidden, paperresultsmcp.CodeWriteDisabled)
}

// The settings tools need the Bearer key like every request; an
// unauthenticated write stores nothing and opens no database connection.
func TestSettingsToolsNeedTheKey(t *testing.T) {
	path, w := testkit.NewMigratedDB(t)
	driverName, counter := testkit.NewCountingDriver(t)
	srv := newTestServer(t, paperresultsmcp.Options{Settings: enabledSettings(), DBPath: path, DriverName: driverName})
	for _, name := range settingsTools {
		args := map[string]any{}
		if name == "settings_set" {
			args = map[string]any{"key": "risk.paper.max_trade_loss_pct", "value": 2}
		}
		for _, auth := range []string{"", "Bearer wrong-key", "Basic " + testAPIKey} {
			srv.post(t, auth, callBody(name, args)).wantError(t, http.StatusUnauthorized, paperresultsmcp.CodeUnauthorized)
		}
	}
	if counter.Opens.Load() != 0 || counter.Statements.Load() != 0 {
		t.Fatalf("unauthenticated calls opened %d connections / %d statements", counter.Opens.Load(), counter.Statements.Load())
	}
	if n := count(t, w, `SELECT COUNT(*) FROM runtime_settings`) + count(t, w, `SELECT COUNT(*) FROM mcp_settings_audit`); n != 0 {
		t.Fatalf("unauthenticated calls stored %d rows", n)
	}
}

// The write path is narrow: across a whole settings and experiments session
// the only statements that change anything are the runtime_settings upsert
// (one key per write), the mcp_settings_audit INSERT, and - issue #871 - the
// tuning_experiments INSERT (one per accepted experiment) and the UPDATE that
// ends a running experiment. Refused calls (two variables, a key off the
// whitelist, writes switched off, an experiment that already ended) write
// nothing, and the read connection still refuses writes.
func TestSettingsWritePathIsNarrow(t *testing.T) {
	path, w := testkit.NewMigratedDB(t)
	testkit.SeedPositions(t, w)
	driverName, counter := testkit.NewCountingDriver(t)
	srv := newTestServer(t, paperresultsmcp.Options{Settings: enabledSettings(), DBPath: path, DriverName: driverName})
	set(srv, t, "risk.paper.max_trade_loss_pct", 2).data(t, "settings_set", &struct{}{})
	srv.call(t, "settings_write_disable", map[string]any{}).data(t, "settings_write_disable", &struct{}{})
	srv.call(t, "settings_write_enable", map[string]any{}).data(t, "settings_write_enable", &struct{}{})
	set(srv, t, "nope", 1).wantError(t, http.StatusForbidden, paperresultsmcp.CodeKeyNotAllowed)
	for _, tool := range []string{"settings_get", "settings_diff", "settings_audit", "experiments_list"} {
		srv.call(t, tool, map[string]any{}).data(t, tool, &struct{}{})
	}

	change := func(name string) map[string]any {
		return map[string]any{"kind": "setting", "name": name, "before": 1, "after": 2}
	}
	create := func(changes ...map[string]any) response {
		return srv.call(t, "experiment_create", map[string]any{
			"hypothesis": "1 回の最大損失を 1% から 2% に上げる", "changes": changes, "started_at": "2026-10-10T00:00:00Z"})
	}
	var created struct {
		ID int64 `json:"id"`
	}
	create(change("risk.paper.max_trade_loss_pct")).data(t, "experiment_create", &created)
	create(change("risk.paper.max_trade_loss_pct"), change("risk.max_daily_trades")).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeMultipleVariables)
	create(change("min_entry_quality")).wantError(t, http.StatusForbidden, paperresultsmcp.CodeKeyNotAllowed)
	srv.call(t, "experiment_compare", map[string]any{"experiment_id": created.ID}).data(t, "experiment_compare", &struct{}{})
	srv.call(t, "experiment_close", map[string]any{"experiment_id": created.ID}).data(t, "experiment_close", &struct{}{})
	srv.call(t, "experiment_close", map[string]any{"experiment_id": created.ID}).wantError(t, http.StatusConflict, paperresultsmcp.CodeExperimentNotRunning)
	srv.call(t, "settings_write_disable", map[string]any{}).data(t, "settings_write_disable", &struct{}{})
	create(change("risk.paper.max_trade_loss_pct")).wantError(t, http.StatusForbidden, paperresultsmcp.CodeWriteDisabled)

	allowed := map[string]*regexp.Regexp{
		"runtime_settings":   regexp.MustCompile(`(?is)^\s*INSERT INTO runtime_settings\b`),
		"mcp_settings_audit": regexp.MustCompile(`(?is)^\s*INSERT INTO mcp_settings_audit\b`),
		"tuning_experiments": regexp.MustCompile(`(?is)^\s*(INSERT INTO tuning_experiments\b|UPDATE tuning_experiments SET status = \?, ended_at = \? WHERE id = \? AND status = 'running')`),
	}
	written := map[string]int{}
	for _, stmt := range counter.StatementLog() {
		s := strings.ToUpper(strings.TrimSpace(stmt))
		changes := strings.HasPrefix(s, "INSERT") || strings.HasPrefix(s, "UPDATE") || strings.HasPrefix(s, "DELETE") ||
			strings.HasPrefix(s, "CREATE") || strings.HasPrefix(s, "DROP") || strings.HasPrefix(s, "ALTER")
		if !changes {
			if strings.Contains(s, "SECRETS") || strings.Contains(s, "SELECT *") {
				t.Errorf("statement reads outside the settings tables: %s", testkit.FirstLine(stmt))
			}
			continue
		}
		matched := false
		for table, re := range allowed {
			if re.MatchString(stmt) {
				written[table]++
				matched = true
			}
		}
		if !matched {
			t.Errorf("a write statement outside the allowed set: %s", testkit.FirstLine(stmt))
		}
	}
	// One accepted experiment (one INSERT) that was ended once (one UPDATE; the
	// second close and every refused call wrote nothing).
	if written["tuning_experiments"] != 2 || count(t, w, `SELECT COUNT(*) FROM tuning_experiments`) != 1 {
		t.Errorf("tuning_experiments writes = %d, want one INSERT and one UPDATE", written["tuning_experiments"])
	}
	// Reads never took the write path and the read connection is still read-only.
	db, err := srv.DB()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO runtime_settings (key, value, updated_at) VALUES ('x', '1', 't')`); err == nil {
		t.Error("the read connection accepted a write")
	}
	if n := count(t, w, `SELECT COUNT(*) FROM positions`); n != 8 {
		t.Fatalf("positions changed: %d", n)
	}
}

// The Paper capital of the return-rate calculation follows the Settings value
// (runtime_settings) and falls back to risk.yaml's (the server option).
func TestPaperCapitalFollowsTheSettingsValue(t *testing.T) {
	srv := seededServer(t, nil) // CapitalJPY option: 30,000,000 (risk.yaml's)
	var d perfData
	srv.call(t, "paper_performance_summary", period).data(t, "paper_performance_summary", &d)
	near(t, "capital_jpy (fallback)", d.CapitalJPY, 30_000_000)

	set(srv, t, "risk.paper.initial_capital_jpy", 1_000_000).data(t, "settings_set", &struct{}{})
	srv.call(t, "paper_performance_summary", period).data(t, "paper_performance_summary", &d)
	near(t, "capital_jpy (Settings value)", d.CapitalJPY, 1_000_000)
	near(t, "max_drawdown_capital_ratio", d.MaxDrawdownCapitalRatio, 3000.0/1_000_000)

	// With no risk.yaml value at all, the Settings value still gives the ratio.
	noCap := seededServer(t, func(o *paperresultsmcp.Options) { o.CapitalJPY = -1 })
	set(noCap, t, "risk.paper.initial_capital_jpy", 2_000_000).data(t, "settings_set", &struct{}{})
	var nc perfData
	noCap.call(t, "paper_performance_summary", period).data(t, "paper_performance_summary", &nc)
	near(t, "capital_jpy (no risk.yaml)", nc.CapitalJPY, 2_000_000)
}

// No secret or raw board value can come out of the settings tools: the
// seeded sentinel strings never appear and the tools' keys are the whitelist's.
func TestSettingsToolsReturnNoSecrets(t *testing.T) {
	srv := seededServer(t, nil)
	set(srv, t, "metalabel.llm_cutoff_source", "official page").data(t, "settings_set", &struct{}{})
	for _, tool := range []string{"settings_get", "settings_diff", "settings_audit"} {
		body := string(srv.call(t, tool, map[string]any{}).body)
		for _, s := range testkit.Sentinels() {
			if strings.Contains(body, s) {
				t.Errorf("%s leaks %q", tool, s)
			}
		}
		if strings.Contains(body, testAPIKey) {
			t.Errorf("%s returns the API key", tool)
		}
	}
	if logs := srv.logs.String(); strings.Contains(logs, "official page") {
		t.Errorf("a setting value reached the operational log: %s", logs)
	}
}
