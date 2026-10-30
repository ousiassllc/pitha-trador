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

// periodTools are the tools taking the from/to period arguments (thirteen since
// metalabel_training_readiness / jev_models, issues #848 / #849,
// session_diagnostics, issue #850, entry_quality_whatif, issue #911, and
// disclosure_skip_summary, issue #935); health_today
// (issue #808) takes none, so it is called separately (healthToday).
var periodTools = []string{
	"paper_performance_summary", "paper_daily_pnl", "paper_trades", "jev_calibration",
	"metalabel_history", "risk_rejections", "kill_switch_history", "collection_status",
	"metalabel_training_readiness", "jev_models", "session_diagnostics", "entry_quality_whatif", "disclosure_skip_summary",
}

const healthToday = "health_today"

// allTools is every tool of the dispatch table.
var allTools = append(slices.Clone(periodTools), healthToday)

// settingsTools are the tools of issues #806 / #807: the read tools of the
// whitelisted settings and the only tools that write (settings_set and the
// write switch), each through mcpset.Writer.
var settingsTools = []string{
	"settings_get", "settings_diff", "settings_audit", "settings_set", "settings_write_enable", "settings_write_disable",
}

// proposalTools are the tools of issues #827 / #828 / #829: record a Paper order
// proposal, ask for its cancellation (the application cancels), switch the
// intake off (there is no tool that switches it on), and read the proposals
// back (list, one in full, a summary; these three only read).
var proposalTools = []string{
	"order_proposal_submit", "order_proposal_cancel", "order_proposals_disable",
	"order_proposals_list", "order_proposal_get", "order_proposal_summary",
}

// experimentTools are the tools of issue #871: record a tuning experiment and
// end it (these two write one row each), and list / compare them (read only).
var experimentTools = []string{"experiment_create", "experiment_close", "experiments_list", "experiment_compare"}

// jevShadowTools are the tools of issue #872: the shadow comparison and its
// cost (reads) and the explicit promotion of a prompt version (the one write).
var jevShadowTools = []string{"jev_shadow_compare", "jev_shadow_cost", "jev_prompt_promote"}

// writingTools are the tools that change state; every other tool is annotated
// read-only.
var writingTools = []string{
	"settings_set", "settings_write_enable", "settings_write_disable",
	"order_proposal_submit", "order_proposal_cancel", "order_proposals_disable",
	"experiment_create", "experiment_close",
	"jev_prompt_promote",
}

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

// tools/list returns exactly the fourteen read tools, the six settings tools, the
// six order-proposal tools, the four experiment tools and the three Jev prompt tools
// (33 in all); only the nine writing tools are not marked read-only.
func TestToolsListReturnsExactlyTheReadToolsAndTheSettingsTools(t *testing.T) {
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
		wantReadOnly := !slices.Contains(writingTools, tl.Name)
		if tl.Annotations["readOnlyHint"] != wantReadOnly || tl.Annotations["destructiveHint"] != false {
			t.Errorf("%s: annotations %v, want readOnlyHint=%v and destructiveHint=false", tl.Name, tl.Annotations, wantReadOnly)
		}
		if tl.InputSchema["additionalProperties"] != false {
			t.Errorf("%s: schema must reject unknown arguments", tl.Name)
		}
	}
	slices.Sort(names)
	want := slices.Concat(allTools, settingsTools, proposalTools, experimentTools, jevShadowTools)
	slices.Sort(want)
	if !slices.Equal(names, want) {
		t.Fatalf("tools/list = %v, want exactly %v", names, want)
	}
	if got := paperresultsmcp.ToolNames(); len(got) != len(allTools)+len(settingsTools)+len(proposalTools)+len(experimentTools)+len(jevShadowTools) {
		t.Fatalf("dispatch table has %d entries: %v", len(got), got)
	}
}

// No write-style tool can exist beyond the settings and order-proposal tools: the dispatch table
// (not just tools/list) is scanned for write/order/kill-switch-operation names
// outside those fixed tool sets, and calling such names - or any unknown
// name - gives the spec'd invalid_argument.
func TestNoWriteToolsExist(t *testing.T) {
	forbidden := []string{"order", "cancel", "update", "set", "delete", "insert", "write", "create", "place",
		"submit", "send", "trigger", "resolve", "reset", "clear", "operate", "enable", "disable", "toggle",
		"close", "remove", "drop", "put", "post", "execute", "exec", "run", "sql", "query", "setting", "settings",
		"secret", "secrets", "key", "login", "token", "kabu"}
	for _, name := range paperresultsmcp.ToolNames() {
		if slices.Contains(settingsTools, name) || slices.Contains(proposalTools, name) || slices.Contains(experimentTools, name) || slices.Contains(jevShadowTools, name) {
			continue // their reach is pinned in settingstests and proposaltests
		}
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
		// Nothing turns order proposals on from the MCP.
		"order_proposals_enable", "order_proposal_enable", "order_proposal_set", "order_proposals_set",
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
