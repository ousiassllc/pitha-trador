package mcptests

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// The seeded database carries sentinel strings in raw_data_json,
// state_json, response_json, secrets, runtime_settings, params/features/
// payload/detail columns and price columns; the API key is known too. None
// of it may appear in any tool output (success or error, any argument
// shape) or in the captured logs.
func TestOutputsAndLogsNeverContainRawDataOrSecrets(t *testing.T) {
	srv := seededServer(t, nil)

	var bodies []string
	collect := func(r response) { bodies = append(bodies, string(r.body)) }
	for _, tool := range periodTools {
		collect(srv.call(t, tool, period))
		collect(srv.call(t, tool, map[string]any{}))
		collect(srv.call(t, tool, map[string]any{"from": "2025-01-01", "to": "2026-10-19"}))
		collect(srv.call(t, tool, map[string]any{"from": "bad"}))
		collect(srv.call(t, tool, args(map[string]any{"unknown": testkit.SentinelRaw})))
	}
	for _, a := range []map[string]any{
		{}, period, {"unknown": testkit.SentinelRaw}, {"from": "2025-01-01", "to": "2026-10-19"},
	} {
		collect(srv.call(t, healthToday, a))
	}
	for _, extra := range []map[string]any{
		{"limit": 200}, {"limit": 1}, {"limit": 201}, {"horizon_minutes": 5},
	} {
		for _, tool := range []string{"paper_trades", "metalabel_history", "kill_switch_history", "jev_calibration"} {
			collect(srv.call(t, tool, args(extra)))
		}
	}
	collect(srv.post(t, "Bearer "+testAPIKey, rpc("tools/list", nil)))
	collect(srv.post(t, "Bearer "+testAPIKey, rpc("initialize", map[string]any{"protocolVersion": "2025-06-18"})))
	collect(srv.post(t, "Bearer wrong", callBody("paper_trades", period)))
	collect(srv.post(t, "Bearer "+testAPIKey, callBody(testkit.SentinelSecretKey, period)))

	forbidden := append(testkit.Sentinels(), testAPIKey, "encrypted_value", "raw_data_json", "state_json", "response_json")
	check := func(where, text string) {
		for _, f := range forbidden {
			if strings.Contains(text, f) {
				t.Errorf("%s contains %q", where, f)
			}
		}
	}
	for i, b := range bodies {
		check("response "+string(rune('A'+i%26)), b)
	}
	logs := srv.logs.String()
	if logs == "" {
		t.Fatal("expected the tool calls to be logged")
	}
	// The unknown tool name above is a sentinel-looking input: it is not
	// echoed in any response, and the log keeps only the (empty) tool name.
	for _, f := range forbidden {
		if f == testkit.SentinelSecretKey {
			continue
		}
		if strings.Contains(logs, f) {
			t.Errorf("log contains %q", f)
		}
	}
	if strings.Contains(logs, testkit.SentinelSecretKey) {
		t.Errorf("log echoes a client-supplied tool name")
	}
}

// Every returned object has exactly the whitelisted keys of the API spec:
// a column that is not in these sets cannot be in a response.
func TestResponseKeysAreExactlyTheWhitelist(t *testing.T) {
	srv := seededServer(t, nil)
	data := func(tool string, a map[string]any) map[string]any {
		var d map[string]any
		srv.call(t, tool, a).data(t, tool, &d)
		return d
	}
	keys := func(m map[string]any) []string {
		out := make([]string, 0, len(m))
		for k := range m {
			out = append(out, k)
		}
		slices.Sort(out)
		return out
	}
	first := func(d map[string]any, key string) map[string]any {
		list, ok := d[key].([]any)
		if !ok || len(list) == 0 {
			t.Fatalf("%q is not a non-empty list in %v", key, d)
		}
		return list[0].(map[string]any)
	}
	want := func(name string, got map[string]any, wantKeys string) {
		t.Helper()
		w := strings.Fields(wantKeys)
		slices.Sort(w)
		if g := keys(got); !slices.Equal(g, w) {
			t.Errorf("%s keys = %v, want %v", name, g, w)
		}
	}

	want("performance", data("paper_performance_summary", period),
		"closed_trade_count open_position_count win_count loss_count flat_count win_rate total_pnl_jpy expectancy_jpy expectancy_return_pct profit_factor max_drawdown_jpy capital_jpy max_drawdown_capital_ratio by_origin")
	daily := data("paper_daily_pnl", period)
	want("daily envelope", daily, "days")
	want("daily row", first(daily, "days"), "date trade_count pnl_jpy win_count loss_count")
	trades := data("paper_trades", period)
	want("trades envelope", trades, "trades next_cursor")
	want("trade row", first(trades, "trades"), "id symbol side quantity entry_price opened_at exit_price closed_at exit_reason realized_pnl fees_jpy origin")
	cal := data("jev_calibration", period)
	want("calibration", cal, "decisions horizons")
	want("decisions", cal["decisions"].(map[string]any), "scout_count trader_count")
	horizon := first(cal, "horizons")
	want("horizon", horizon, "horizon_minutes by_label_version")
	version := first(horizon, "by_label_version")
	want("label version", version, "label_version sample_count excluded_count hit_rate brier buckets out_of_bucket_count barrier_counts avg_net_return_bps")
	want("bucket", first(version, "buckets"), "low high sample_count hit_rate")
	want("barrier counts", version["barrier_counts"].(map[string]any), "tp sl timeout")
	meta := data("metalabel_history", period)
	want("metalabel", meta, "models prediction_counts retrain_skips")
	want("model", first(meta, "models"),
		"version algorithm label_version status reject_reason reject_reason_truncated trained_at train_start train_end oos_start oos_end train_samples oos_samples oos_auc oos_expected_value_bps")
	want("prediction counts", meta["prediction_counts"].(map[string]any), "take skip")
	want("retrain skip", first(meta, "retrain_skips"), "skipped_at reason_code usable_rows required_rows")
	risk := data("risk_rejections", period)
	want("risk", risk, "risk_passed_count other_reject_count buckets other_reject_breakdown proposal_risk_passed_count proposal_risk_rejected_count")
	want("risk bucket", first(risk, "buckets"), "reason count")
	want("other reject bucket", first(risk, "other_reject_breakdown"), "reason count")
	kill := data("kill_switch_history", period)
	want("kill switch", kill, "events")
	want("kill event", first(kill, "events"), "id triggered_at reason resolved_at resolved_by")
	coll := data("collection_status", period)
	want("collection", coll, "latest_snapshot_at missing_session_dates jobs daily_bar_run_failed_count label_skip_count job_failures alerts retention")
	want("job", first(coll, "jobs"), "queue succeeded_count failed_count latest_succeeded_finished_at")
	failures := coll["job_failures"].(map[string]any)
	want("job failures", failures, "total by_day by_queue by_error_code by_day_and_error_code skipped_total skipped_by_queue")
	want("failure day", first(failures, "by_day"), "date count")
	want("failure queue", first(failures, "by_queue"), "queue count")
	want("failure code", first(failures, "by_error_code"), "code count")
	want("failure day code", first(failures, "by_day_and_error_code"), "date code count")
	want("skipped queue", first(failures, "skipped_by_queue"), "queue count")
	want("retention", coll["retention"].(map[string]any), "succeeded_job_days failed_job_days note")
	health := data(healthToday, map[string]any{})
	want("health", health, "date trading_day needs_attention attention_codes paper_trades meta_model bootstrap market_data missing_session_dates insufficient_history_dates daily_guard kill_switch retrain order_proposals")
	want("health paper trades", health["paper_trades"].(map[string]any), "opened_count closed_count")
	want("health meta model", health["meta_model"].(map[string]any), "adopted version trained_at")
	want("health bootstrap", health["bootstrap"].(map[string]any), "enabled effective signals_today")
	want("health market data", health["market_data"].(map[string]any), "kabu_login market_data app_running alerts")
	want("health daily guard", health["daily_guard"].(map[string]any), "halted reason halted_at")
	want("health kill switch", health["kill_switch"].(map[string]any), "active unresolved_count since reasons flag_set")
	want("health retrain", health["retrain"].(map[string]any), "skipped latest_skipped_at reason_code")
	want("health order proposals", health["order_proposals"].(map[string]any), "stalled_count oldest_stalled_since")
	readiness := data("metalabel_training_readiness", period)
	want("readiness", readiness, "as_of rows_from rows_to lookback_days matches_nightly_retrain funnel features last_retrain split")
	want("readiness step", first(readiness, "funnel"), "step condition rows")
	readyFeatures := readiness["features"].(map[string]any)
	want("readiness features", readyFeatures, "training_rows usable_rows dropped_incomplete missing")
	want("readiness feature", first(readyFeatures, "missing"), "feature missing_rows")
	lastRetrain := readiness["last_retrain"].(map[string]any)
	want("readiness last retrain", lastRetrain, "last_run latest_skip latest_model_trained_at dropped_incomplete_status")
	want("readiness latest skip", lastRetrain["latest_skip"].(map[string]any), "skipped_at reason_code usable_rows required_rows dropped_incomplete")
	readySplit := readiness["split"].(map[string]any)
	want("readiness split", readySplit, "llm_cutoff llm_cutoff_status oos_start train_ready train_skip_code train oos")
	want("readiness train", readySplit["train"].(map[string]any), "rows required_rows fit_rows calibration_rows required_fit_rows required_calibration_rows")
	want("readiness oos", readySplit["oos"].(map[string]any), "rows required_rows days required_days required_takes ready")
	want("performance by origin", first(data("paper_performance_summary", period), "by_origin"),
		"origin closed_trade_count win_count loss_count win_rate total_pnl_jpy expectancy_jpy")
}

// Protocol basics a connector depends on, all behind the key.
func TestProtocolBasics(t *testing.T) {
	srv := seededServer(t, nil)
	auth := "Bearer " + testAPIKey

	var init struct {
		Result struct {
			ProtocolVersion string         `json:"protocolVersion"`
			Capabilities    map[string]any `json:"capabilities"`
			ServerInfo      map[string]any `json:"serverInfo"`
		} `json:"result"`
	}
	r := srv.post(t, auth, rpc("initialize", map[string]any{"protocolVersion": "2025-03-26"}))
	if err := json.Unmarshal(r.body, &init); err != nil || r.status != http.StatusOK ||
		init.Result.ProtocolVersion != "2025-03-26" || init.Result.Capabilities["tools"] == nil {
		t.Fatalf("initialize: %d %s %v", r.status, r.body, err)
	}
	if _, has := init.Result.Capabilities["resources"]; has {
		t.Fatal("server must advertise tools only")
	}
	if r := srv.post(t, auth, rpc("initialize", map[string]any{"protocolVersion": "1999-01-01"})); !strings.Contains(string(r.body), "2025-06-18") {
		t.Fatalf("unknown version must negotiate to the newest supported: %s", r.body)
	}
	if r := srv.post(t, auth, `{"jsonrpc":"2.0","method":"notifications/initialized"}`); r.status != http.StatusAccepted || len(r.body) != 0 {
		t.Fatalf("notification: %d %q", r.status, r.body)
	}
	if r := srv.post(t, auth, rpc("ping", nil)); r.status != http.StatusOK {
		t.Fatalf("ping: %d", r.status)
	}
	for _, bad := range []string{"not json", `[{"jsonrpc":"2.0","id":1,"method":"ping"}]`, `{"jsonrpc":"1.0","id":1,"method":"ping"}`, `{"jsonrpc":"2.0","id":1}`} {
		if r := srv.post(t, auth, bad); r.status != http.StatusBadRequest {
			t.Errorf("%q: status %d, want 400", bad, r.status)
		}
	}
	req, _ := http.NewRequest(http.MethodGet, srv.http.URL, nil)
	req.Header.Set("Authorization", auth)
	if r := srv.do(t, req); r.status != http.StatusMethodNotAllowed {
		t.Fatalf("authenticated GET: %d, want 405", r.status)
	}
}
