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
	for _, tool := range eightTools {
		collect(srv.call(t, tool, period))
		collect(srv.call(t, tool, map[string]any{}))
		collect(srv.call(t, tool, map[string]any{"from": "2025-01-01", "to": "2026-10-19"}))
		collect(srv.call(t, tool, map[string]any{"from": "bad"}))
		collect(srv.call(t, tool, args(map[string]any{"unknown": testkit.SentinelRaw})))
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
		"closed_trade_count open_position_count win_count loss_count flat_count win_rate total_pnl_jpy expectancy_jpy expectancy_return_pct profit_factor max_drawdown_jpy capital_jpy max_drawdown_capital_ratio")
	daily := data("paper_daily_pnl", period)
	want("daily envelope", daily, "days")
	want("daily row", first(daily, "days"), "date trade_count pnl_jpy win_count loss_count")
	trades := data("paper_trades", period)
	want("trades envelope", trades, "trades next_cursor")
	want("trade row", first(trades, "trades"), "id symbol side quantity entry_price opened_at exit_price closed_at exit_reason realized_pnl fees_jpy")
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
	want("risk", risk, "risk_passed_count other_reject_count buckets other_reject_breakdown")
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
