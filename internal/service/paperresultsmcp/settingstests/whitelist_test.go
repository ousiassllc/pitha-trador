package settingstests

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/mcpwritesettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
)

func errorDetail(t *testing.T, r response) string {
	t.Helper()
	var e struct {
		Error struct {
			Data map[string]string `json:"data"`
		} `json:"error"`
	}
	if err := json.Unmarshal(r.body, &e); err != nil {
		t.Fatal(err)
	}
	return e.Error.Data["detail"]
}

// Every whitelisted key is writable with a valid value and the stored value is
// the one the Settings screen would store.
func TestEveryWhitelistedKeyIsWritable(t *testing.T) {
	srv, w := settingsServer(t, nil)
	valid := map[string]any{
		"risk.paper.initial_capital_jpy": 1000000, "risk.paper.max_position_per_symbol_pct": 50,
		"risk.paper.max_total_exposure_pct": 80, "risk.paper.max_trade_loss_pct": 0.5,
		"policy.paper.long.min_entry_quality": "good", "policy.paper.short.min_entry_quality": "strong",
		"risk.bucket.stock.budget_jpy": 400000, "risk.bucket.bond_etf.budget_jpy": 300000,
		"risk.operating_capital_jpy": 2000000, "risk.daily_loss_limit_pct": 2, "risk.profit_lock.trigger_pct": 3,
		"risk.profit_lock.giveback_pct": 40, "risk.max_daily_trades": 7, "risk.daily_loss_limit_jpy": 20000,
		"risk.profit_lock.trigger_jpy": 30000,
		"metalabel.llm_cutoff":         "2025-06-30", "metalabel.llm_cutoff_model": "jev-model-x",
		"metalabel.llm_cutoff_confirmed_on": "2026-10-19", "metalabel.llm_cutoff_source": "vendor page",
		"jev.shadow.candidate_version": "trader-v5", "jev.shadow.enabled": "true",
	}
	// Turning the shadow on needs a candidate, so the candidate goes first.
	set(srv, t, "jev.shadow.candidate_version", "trader-v5").data(t, "settings_set", &struct{}{})
	for _, key := range mcpwritesettings.Keys() {
		v, ok := valid[key]
		if !ok {
			t.Fatalf("the test has no valid value for whitelisted key %s", key)
		}
		var out struct {
			Value string `json:"value"`
		}
		set(srv, t, key, v).data(t, "settings_set", &out)
		if _, stored := storedValue(t, w, key); !stored {
			t.Errorf("%s was not stored in runtime_settings", key)
		}
	}
	if len(valid) != len(mcpwritesettings.Keys()) {
		t.Errorf("valid values cover %d keys, the whitelist has %d", len(valid), len(mcpwritesettings.Keys()))
	}
	st := getState(srv, t)
	for key := range valid {
		if st.item(t, key).Source != "stored" {
			t.Errorf("%s not stored after set", key)
		}
	}
}

// Anything outside the whitelist is rejected before it reaches the database:
// Live-affecting or dangerous keys, the bootstrap toggle, secrets, the write
// switch itself, unknown names and empty keys.
func TestSettingsSetRejectsEverythingOutsideTheWhitelist(t *testing.T) {
	srv, w := settingsServer(t, nil)
	for _, key := range []string{
		"paper.bootstrap_entry_enabled",         // #798: not remotely switchable
		"risk.live.max_position_per_symbol_pct", // no Live value
		"risk.live.initial_capital_jpy",
		"policy.min_entry_quality",      // a real setting, just not whitelisted
		"policy.long.min_entry_quality", // #861: the Live threshold; only the Paper keys are writable
		"policy.short.min_entry_quality",
		"policy.long.min_probability",
		"policy.paper.long.min_probability", // there is no Paper probability key
		"screener.top_n",
		"metalabel.min_ev_bps", // known to opsettings, not whitelisted
		"metalabel.model",
		"broker.tachibana.password",
		"broker.kind",
		"backup.dir",
		"system.paused", // kill switch / system state
		"system.kill_switch_released",
		"secrets.kabu_api_password",
		"mcp.settings_write_enabled", // the switch has its own tools
		"order.place", "transfer.withdraw",
		"nonexistent.key", "RISK.PAPER.MAX_TRADE_LOSS_PCT", " risk.paper.max_trade_loss_pct", "",
	} {
		r := set(srv, t, key, 1)
		if key == "" {
			r.wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
			continue
		}
		r.wantError(t, http.StatusForbidden, paperresultsmcp.CodeKeyNotAllowed)
	}
	if n := count(t, w, `SELECT COUNT(*) FROM runtime_settings`) + count(t, w, `SELECT COUNT(*) FROM mcp_settings_audit`); n != 0 {
		t.Fatalf("rejected writes left %d rows behind", n)
	}
	// And no read tool shows a key outside the whitelist.
	for _, it := range getState(srv, t).Items {
		if !mcpwritesettings.IsAllowed(it.Key) {
			t.Errorf("settings_get shows %s", it.Key)
		}
	}
}

// Values are validated like the Settings screen validates them; a rejected
// value changes nothing and is not audited.
func TestSettingsSetValidatesLikeTheSettingsScreen(t *testing.T) {
	srv, w := settingsServer(t, nil)
	for _, c := range []struct {
		key   string
		value any
	}{
		{"risk.paper.max_trade_loss_pct", 3.01}, // above papersettings.MaxTradeLossPct
		{"risk.paper.max_trade_loss_pct", 0.09},
		{"risk.paper.max_position_per_symbol_pct", 0},
		{"risk.paper.max_total_exposure_pct", 100.5},
		{"risk.paper.initial_capital_jpy", -1},
		{"risk.paper.initial_capital_jpy", "abc"},
		{"risk.daily_loss_limit_pct", 101},
		{"risk.max_daily_trades", 1.5},
		{"policy.paper.long.min_entry_quality", "great"}, // not one of poor/fair/good/strong/exceptional
		{"policy.paper.short.min_entry_quality", 3},
		{"policy.paper.short.min_entry_quality", "GOOD"},
		{"risk.operating_capital_jpy", 2e12},
		{"metalabel.llm_cutoff", "2025-13-45"},
		{"metalabel.llm_cutoff_confirmed_on", "yesterday"},
		{"metalabel.llm_cutoff_model", ""},
		{"metalabel.llm_cutoff_model", "line\nbreak"},
		{"metalabel.llm_cutoff_source", strings.Repeat("あ", 201)},
	} {
		r := set(srv, t, c.key, c.value)
		r.wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidValue)
		if errorDetail(t, r) == "" {
			t.Errorf("%s=%v: invalid_value carries no detail", c.key, c.value)
		}
	}
	// Inclusive bounds are accepted.
	for _, v := range []float64{0.1, 3.0} {
		set(srv, t, "risk.paper.max_trade_loss_pct", v).data(t, "settings_set", &struct{}{})
	}
	if n := count(t, w, `SELECT COUNT(*) FROM mcp_settings_audit`); n != 2 {
		t.Fatalf("audit rows = %d, want only the 2 accepted writes", n)
	}
	// Wrong argument shapes.
	for _, args := range []any{
		map[string]any{"key": "risk.paper.max_trade_loss_pct"},
		map[string]any{"key": "risk.paper.max_trade_loss_pct", "value": true},
		map[string]any{"key": "risk.paper.max_trade_loss_pct", "value": nil},
		map[string]any{"key": "risk.paper.max_trade_loss_pct", "value": []int{1}},
		map[string]any{"key": "risk.paper.max_trade_loss_pct", "value": 1, "force": true},
	} {
		srv.call(t, "settings_set", args).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
	}
}
