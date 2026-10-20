package opsflow_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/papersettings"
)

// paperLine returns the 計算結果 line `key` of the Paper group in body.
func paperLine(t *testing.T, body, key string) string {
	t.Helper()
	marker := `data-testid="setting-group-derived-paper-risk-` + key + `"`
	_, rest, ok := strings.Cut(body, marker)
	if !ok {
		t.Fatalf("no Paper derived line %q; body=%s", key, body)
	}
	line, _, _ := strings.Cut(rest, "</li>")
	return line
}

func hasPaperLine(body, key string) bool {
	return strings.Contains(body, `data-testid="setting-group-derived-paper-risk-`+key+`"`)
}

// issue #804: the Settings screen alone sets the Paper capital and the two
// exposure limits, next to the daily guard capital on the リスク・資金 page.
func TestSaveOps_PaperRiskValuesAreEditable(t *testing.T) {
	engine, store := opsRouter()

	for key, raw := range map[string]string{
		papersettings.KeyInitialCapitalJPY:        "500000",
		papersettings.KeyMaxPositionPerSymbolPct:  "100",
		papersettings.KeyMaxTotalExposurePct:      "100",
		papersettings.KeyMaxTradeLossPct:          "1.5",
		dailyguardsettings.KeyOperatingCapitalJPY: "500000",
	} {
		rec := opsRequest(engine, http.MethodPost, key, url.Values{"value": {raw}}, true)
		if rec.Code != http.StatusOK {
			t.Fatalf("POST %s: status = %d, body=%s", key, rec.Code, rec.Body.String())
		}
		if store.rows[key] != raw {
			t.Errorf("stored %s = %q, want %q", key, store.rows[key], raw)
		}
	}

	body := settingsBody(engine)
	for _, want := range []string{`name="value"`, "Paper の運用資金（円）", "1 銘柄の上限", "総エクスポージャの上限", "1 回の取引の最大損失", "日次ガード用の運用資金（円）", "再起動不要", "Live の値と動作はこの設定の影響を受けず"} {
		if !strings.Contains(body, want) {
			t.Errorf("Settings page lacks %q", want)
		}
	}
}

func TestSaveOps_PaperRiskRejectsOutOfRangeValues(t *testing.T) {
	engine, store := opsRouter()

	for name, tc := range map[string]struct{ key, raw string }{
		"zero capital":           {papersettings.KeyInitialCapitalJPY, "0"},
		"negative capital":       {papersettings.KeyInitialCapitalJPY, "-1"},
		"capital above 1 兆円":     {papersettings.KeyInitialCapitalJPY, "1e13"},
		"text capital":           {papersettings.KeyInitialCapitalJPY, "abc"},
		"zero symbol pct":        {papersettings.KeyMaxPositionPerSymbolPct, "0"},
		"symbol pct above 100":   {papersettings.KeyMaxPositionPerSymbolPct, "100.5"},
		"zero exposure pct":      {papersettings.KeyMaxTotalExposurePct, "0"},
		"exposure pct above 100": {papersettings.KeyMaxTotalExposurePct, "150"},
		"negative exposure pct":  {papersettings.KeyMaxTotalExposurePct, "-1"},
		"infinite capital":       {papersettings.KeyInitialCapitalJPY, "Inf"},
		"trade loss below 0.1":   {papersettings.KeyMaxTradeLossPct, "0.09"},
		"trade loss zero":        {papersettings.KeyMaxTradeLossPct, "0"},
		"trade loss above 3":     {papersettings.KeyMaxTradeLossPct, "3.1"},
		"trade loss negative":    {papersettings.KeyMaxTradeLossPct, "-0.5"},
	} {
		rec := opsRequest(engine, http.MethodPost, tc.key, url.Values{"value": {tc.raw}}, true)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: POST %s=%q status = %d, want 400", name, tc.key, tc.raw, rec.Code)
		}
	}
	if len(store.rows) != 0 {
		t.Fatalf("rejected values were stored: %v", store.rows)
	}
}

// Nothing stored shows the Paper initial values; resetting a saved value goes
// back to config/risk.yaml's.
func TestPaperRisk_DefaultsAndResetUseTheRiskYamlValues(t *testing.T) {
	engine, store := riskRouterWithLimits(&config.RiskLimits{InitialCapital: 30_000_000, MaxPositionPerSymbolPct: 2, MaxTotalExposurePct: 20, MaxDailyLossPct: 1})
	body := settingsBody(engine)
	if !strings.Contains(body, "30000000") {
		t.Errorf("the Paper capital row does not show the risk.yaml value; body=%s", body)
	}

	if rec := opsRequest(engine, http.MethodPost, papersettings.KeyInitialCapitalJPY, url.Values{"value": {"500000"}}, true); rec.Code != http.StatusOK {
		t.Fatalf("POST status = %d", rec.Code)
	}
	if store.rows[papersettings.KeyInitialCapitalJPY] != "500000" {
		t.Fatalf("stored = %q", store.rows[papersettings.KeyInitialCapitalJPY])
	}
	rec := opsRequest(engine, http.MethodDelete, papersettings.KeyInitialCapitalJPY, nil, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if _, ok := store.rows[papersettings.KeyInitialCapitalJPY]; ok {
		t.Fatal("Reset left the row")
	}
	if !strings.Contains(rec.Body.String(), "30000000") {
		t.Errorf("reset row does not show the risk.yaml default; body=%s", rec.Body.String())
	}
}

// issue #804: aligning the daily guard capital with the Paper capital on the
// same screen removes the mismatch warning, and the answer to either save
// refreshes both panels.
func TestSaveOps_PaperCapitalAlignedWithDailyGuardCapitalHasNoMismatchWarning(t *testing.T) {
	engine, store := riskRouterWithLimits(&config.RiskLimits{InitialCapital: 30_000_000, MaxPositionPerSymbolPct: 2, MaxTotalExposurePct: 20, MaxDailyLossPct: 1})
	store.rows[dailyguardsettings.KeyOperatingCapitalJPY] = "500000"

	// The Paper capital is still risk.yaml's 30M: mismatch.
	line := paperLine(t, settingsBody(engine), dailyguardsettings.WarnCapitalMismatch)
	for _, want := range []string{"運用資金 500,000 円", "30,000,000 円"} {
		if !strings.Contains(line, want) {
			t.Errorf("mismatch line lacks %q: %s", want, line)
		}
	}

	rec := opsRequest(engine, http.MethodPost, papersettings.KeyInitialCapitalJPY, url.Values{"value": {"500000"}}, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if hasPaperLine(body, dailyguardsettings.WarnCapitalMismatch) || strings.Contains(body, "daily-guard-"+dailyguardsettings.WarnCapitalMismatch) {
		t.Errorf("aligned capitals still warn: %s", body)
	}
	if line := paperLine(t, body, "consistency-ok"); !strings.Contains(line, "問題なし") {
		t.Errorf("Paper group consistency = %s", line)
	}
	// The daily guard's own panel is refreshed out-of-band by the same answer.
	if line := derivedLine(t, body, "consistency-ok"); !strings.Contains(line, "問題なし") {
		t.Errorf("daily guard group consistency = %s", line)
	}
}

// issue #804: with 500,000 yen / 100% / 100% a 5,000-yen stock (the screener's
// upper price in this fixture) fits the capital exactly; a smaller capital
// shows the notice that one 100-share lot exceeds it.
func TestPaperRisk_NoticeWhenOneLotExceedsTheCapital(t *testing.T) {
	engine, store := opsRouter()
	store.rows[papersettings.KeyInitialCapitalJPY] = "500000"
	store.rows[papersettings.KeyMaxPositionPerSymbolPct] = "100"
	store.rows[papersettings.KeyMaxTotalExposurePct] = "100"

	body := settingsBody(engine)
	if hasPaperLine(body, "lot-exceeds-capital") {
		t.Errorf("a 5,000-yen lot (500,000 yen) fits a 500,000-yen capital but the notice shows: %s", body)
	}
	if line := paperLine(t, body, "lot-symbol"); !strings.Contains(line, "5,000 円") {
		t.Errorf("per-symbol line = %s, want a 5,000 yen price ceiling", line)
	}

	rec := opsRequest(engine, http.MethodPost, papersettings.KeyInitialCapitalJPY, url.Values{"value": {"400000"}}, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	line := paperLine(t, rec.Body.String(), "lot-exceeds-capital")
	for _, want := range []string{"注意:", "5,000 円", "500,000 円", "400,000 円", "4,000 円 以下"} {
		if !strings.Contains(line, want) {
			t.Errorf("notice lacks %q: %s", want, line)
		}
	}
}

// issue #804: when the max loss per trade binds below the exposure limits
// (here a saved 0.25%) the panel says so instead of promising a lot that
// sizing skips; with the Paper initial 1.0% it does not bind at 5,000 yen.
func TestPaperRisk_TradeLossLimitBindingIsShown(t *testing.T) {
	engine, store := riskRouterWithLimits(&config.RiskLimits{InitialCapital: 500_000, MaxPositionPerSymbolPct: 100, MaxTotalExposurePct: 100, MaxTradeLossPct: 1.0, MaxDailyLossPct: 1})
	body := settingsBody(engine)
	if line := paperLine(t, body, "lot-trade-loss"); !strings.Contains(line, "8,333 円") || !strings.Contains(line, "最大損失 1%") {
		t.Errorf("trade-loss line = %s", line)
	}
	if hasPaperLine(body, "lot-trade-loss-binding") {
		t.Errorf("the 1.0%% default must not bind below the 5,000 yen exposure ceiling: %s", body)
	}

	rec := opsRequest(engine, http.MethodPost, papersettings.KeyMaxTradeLossPct, url.Values{"value": {"0.25"}}, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if store.rows[papersettings.KeyMaxTradeLossPct] != "0.25" {
		t.Fatalf("stored = %q", store.rows[papersettings.KeyMaxTradeLossPct])
	}
	body = rec.Body.String()
	if line := paperLine(t, body, "lot-trade-loss"); !strings.Contains(line, "2,083 円") || !strings.Contains(line, "最大損失 0.25%") {
		t.Errorf("trade-loss line = %s", line)
	}
	line := paperLine(t, body, "lot-trade-loss-binding")
	for _, want := range []string{"注意:", "2,083 円 を超える", "5,000 円 以下", "見送られます"} {
		if !strings.Contains(line, want) {
			t.Errorf("binding warning lacks %q: %s", want, line)
		}
	}
}

// issue #804 (comment): metalabel.llm_cutoff and its provenance keys are
// edited in the same Settings hierarchy, in the existing メタラベリング dialog
// (成績・検証 page) - one stored row each, not duplicated next to the capital.
func TestSettings_LLMCutoffKeysAreEditableInTheSameHierarchy(t *testing.T) {
	engine, _ := opsRouter()
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/results", nil))
	body := rec.Body.String()
	for _, key := range []string{metalabelsettings.KeyLLMCutoff, metalabelsettings.KeyLLMCutoffModel, metalabelsettings.KeyLLMCutoffConfirmedOn, metalabelsettings.KeyLLMCutoffSource} {
		if !strings.Contains(body, "/ops-settings/"+key) {
			t.Errorf("/settings/results has no editor for %s", key)
		}
		if strings.Contains(settingsBody(engine), "/ops-settings/"+key) {
			t.Errorf("/settings/risk duplicates the editor of %s", key)
		}
	}
}
