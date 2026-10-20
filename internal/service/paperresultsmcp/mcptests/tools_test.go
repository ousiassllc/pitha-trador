package mcptests

import (
	"encoding/json"
	"math"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
)

// eightTools are the tools taking the from/to period arguments; health_today
// (issue #808) takes none, so it is called separately (healthToday).
var eightTools = []string{
	"paper_performance_summary", "paper_daily_pnl", "paper_trades", "jev_calibration",
	"metalabel_history", "risk_rejections", "kill_switch_history", "collection_status",
}

const healthToday = "health_today"

// allTools is every tool of the dispatch table.
var allTools = append(slices.Clone(eightTools), healthToday)

func near(t *testing.T, name string, got *float64, want float64) {
	t.Helper()
	if got == nil || math.Abs(*got-want) > 1e-9 {
		t.Errorf("%s = %v, want %v", name, deref(got), want)
	}
}

func deref(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

func wantNull(t *testing.T, name string, got *float64) {
	t.Helper()
	if got != nil {
		t.Errorf("%s = %v, want null", name, *got)
	}
}

// tools/list returns exactly the nine tool names, each marked read-only.
func TestToolsListReturnsExactlyTheNineTools(t *testing.T) {
	srv := seededServer(t, nil)
	r := srv.post(t, "Bearer "+testAPIKey, rpc("tools/list", nil))
	var env struct {
		Result struct {
			Tools []struct {
				Name        string         `json:"name"`
				InputSchema map[string]any `json:"inputSchema"`
				Annotations map[string]any `json:"annotations"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(r.body, &env); err != nil || r.status != http.StatusOK {
		t.Fatalf("tools/list: %d %s %v", r.status, r.body, err)
	}
	var names []string
	for _, tl := range env.Result.Tools {
		names = append(names, tl.Name)
		if tl.Annotations["readOnlyHint"] != true || tl.Annotations["destructiveHint"] != false {
			t.Errorf("%s: not annotated read-only: %v", tl.Name, tl.Annotations)
		}
		if tl.InputSchema["additionalProperties"] != false {
			t.Errorf("%s: schema must reject unknown arguments", tl.Name)
		}
	}
	slices.Sort(names)
	want := slices.Clone(allTools)
	slices.Sort(want)
	if !slices.Equal(names, want) {
		t.Fatalf("tools/list = %v, want exactly %v", names, want)
	}
	if got := paperresultsmcp.ToolNames(); len(got) != 9 {
		t.Fatalf("dispatch table has %d entries: %v", len(got), got)
	}
}

// No write-style tool can exist: the dispatch table (not just tools/list) is
// scanned for write/order/settings/kill-switch-operation names, and calling
// such names - or any unknown name - gives the spec'd invalid_argument.
func TestNoWriteToolsExist(t *testing.T) {
	forbidden := []string{"order", "cancel", "update", "set", "delete", "insert", "write", "create", "place",
		"submit", "send", "trigger", "resolve", "reset", "clear", "operate", "enable", "disable", "toggle",
		"close", "remove", "drop", "put", "post", "execute", "exec", "run", "sql", "query", "setting", "settings",
		"secret", "secrets", "key", "login", "token", "kabu"}
	for _, name := range paperresultsmcp.ToolNames() {
		if !slices.Contains(allTools, name) {
			t.Errorf("dispatch table has unexpected tool %q", name)
		}
		for _, tok := range strings.Split(name, "_") {
			if slices.Contains(forbidden, tok) {
				t.Errorf("tool %q has write/secret-style token %q", name, tok)
			}
		}
		// The one name that mentions the kill switch only reads its history.
		if strings.Contains(name, "kill") && name != "kill_switch_history" {
			t.Errorf("kill switch tool %q is not the read-only history", name)
		}
	}

	srv := seededServer(t, nil)
	for _, name := range []string{
		"place_order", "cancel_order", "kill_switch_operate", "kill_switch_resolve", "kill_switch",
		"update_settings", "set_runtime_setting", "delete_position", "insert_trade", "paper_order",
		"execute_sql", "kabu_station_status", "kabu_api_error_guidance", "paper_performance_summary ",
		"PAPER_TRADES", "", "../paper_trades",
	} {
		srv.call(t, name, map[string]any{}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
	}
	srv.post(t, "Bearer "+testAPIKey, rpc("tools/call", map[string]any{})).
		wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
	for _, method := range []string{"resources/list", "prompts/list", "tools/execute", "sampling/createMessage"} {
		r := srv.post(t, "Bearer "+testAPIKey, rpc(method, nil))
		if !strings.Contains(string(r.body), "-32601") {
			t.Errorf("%s: want method not found, got %s", method, r.body)
		}
	}
}

// Unknown or mistyped arguments are invalid_argument (not invalid_period).
func TestUnknownArgumentsAreInvalidArgument(t *testing.T) {
	srv := seededServer(t, nil)
	for _, tc := range []struct {
		tool string
		args map[string]any
		want paperresultsmcp.Code
	}{
		{"paper_daily_pnl", args(map[string]any{"limit": 5}), paperresultsmcp.CodeInvalidArgument},
		{"paper_performance_summary", args(map[string]any{"sql": "SELECT 1"}), paperresultsmcp.CodeInvalidArgument},
		{"paper_trades", args(map[string]any{"limit": "ten"}), paperresultsmcp.CodeInvalidArgument},
		{"paper_trades", args(map[string]any{"limit": 0}), paperresultsmcp.CodeInvalidArgument},
		{"paper_trades", args(map[string]any{"cursor": "!!not-a-cursor!!"}), paperresultsmcp.CodeInvalidArgument},
		{"jev_calibration", args(map[string]any{"horizon_minutes": 0}), paperresultsmcp.CodeInvalidArgument},
		{"paper_daily_pnl", map[string]any{"from": 20261001, "to": "2026-10-10"}, paperresultsmcp.CodeInvalidArgument},
	} {
		srv.call(t, tc.tool, tc.args).wantError(t, http.StatusUnprocessableEntity, tc.want)
	}
}
