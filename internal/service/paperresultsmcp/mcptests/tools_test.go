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

// periodTools take the from/to period (issues #848-#1096, twenty-three); health_today (#808) and promotion_checklist (#1091, needs a candidate) take none.
var periodTools = []string{
	"paper_performance_summary", "paper_daily_pnl", "paper_trades", "jev_calibration",
	"metalabel_history", "risk_rejections", "kill_switch_history", "collection_status",
	"metalabel_training_readiness", "jev_models", "session_diagnostics", "entry_quality_whatif", "disclosure_skip_summary", "jev_trial_compare", "jev_trial_cost",
	"jev_evaluation", "research_horizon_summary", "research_quote_source_compare", "disclosure_sources_status", "jquants_status",
	"jev_evaluation_slices", "jev_evaluation_trend", "candidate_ledger",
}

const healthToday = "health_today"

// allTools is every read tool of the dispatch table; ai_roles (#1080) and promotion_checklist (#1091) take no period either.
var allTools = append(slices.Clone(periodTools), healthToday, "ai_roles", "promotion_checklist")

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

// experimentTools (issue #871): create / close write one row each; list / compare only read.
var experimentTools = []string{"experiment_create", "experiment_close", "experiments_list", "experiment_compare"}

// jevShadowTools (issue #872): two reads and the one explicit write (promoting a prompt version).
var jevShadowTools = []string{"jev_shadow_compare", "jev_shadow_cost", "jev_prompt_promote"}
var researchStatusTools = []string{"research_daily_bars_status"}                                                  // #1082: read-only, no period
var replayTools = []string{"replay_estimate", "replay_start", "replay_stop", "replay_progress", "replay_results"} // #1085; two write, pinned in replaytests
var exportTools = []string{"training_dataset_export"}                                                             // #1094: writes files, not rows; pinned in exporttests
// researchTools (issues #1086 / #1087): backtest_start and the virtual Paper profile create / stop write one research row each; the rest read.
var researchTools = []string{"backtest_start", "backtest_get", "backtest_list", "virtual_paper_profile_create", "virtual_paper_profile_stop", "virtual_paper_rerun_failed", "virtual_paper_compare"}
var researchNoteTools = []string{"research_note_create", "research_note_update", "research_notes_list", "research_note_get", "fx_series_summary"} // #1095 / #1084: two notes writes, two reads, the FX summary

// writingTools are the tools that change state; every other tool is annotated read-only.
var writingTools = []string{
	"settings_set", "settings_write_enable", "settings_write_disable",
	"order_proposal_submit", "order_proposal_cancel", "order_proposals_disable",
	"experiment_create", "experiment_close",
	"jev_prompt_promote", "replay_start", "replay_stop", "training_dataset_export", "research_note_create", "research_note_update",
	"backtest_start", "virtual_paper_profile_create", "virtual_paper_profile_stop", "virtual_paper_rerun_failed",
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

// tools/list returns exactly the twenty-six read tools, the six settings, six order-proposal, four experiment and three Jev prompt tools, the research status tool,
// the five replay tools, the export tool, the six research tools and the five research-note / FX tools (63 in all); only the seventeen writing tools are not read-only.
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
	want := slices.Concat(allTools, settingsTools, proposalTools, experimentTools, jevShadowTools, researchStatusTools, replayTools, exportTools, researchTools, researchNoteTools)
	slices.Sort(want)
	if !slices.Equal(names, want) {
		t.Fatalf("tools/list = %v, want exactly %v", names, want)
	}
	if got := paperresultsmcp.ToolNames(); len(got) != len(allTools)+len(settingsTools)+len(proposalTools)+len(experimentTools)+len(jevShadowTools)+len(researchStatusTools)+len(replayTools)+len(exportTools)+len(researchTools)+len(researchNoteTools) {
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
		if slices.Contains(settingsTools, name) || slices.Contains(proposalTools, name) || slices.Contains(experimentTools, name) || slices.Contains(jevShadowTools, name) || slices.Contains(researchStatusTools, name) || slices.Contains(replayTools, name) || slices.Contains(exportTools, name) || slices.Contains(researchTools, name) || slices.Contains(researchNoteTools, name) {
			continue // their reach is pinned in settingstests, proposaltests, replaytests and exporttests
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
