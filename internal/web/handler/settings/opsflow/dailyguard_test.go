package opsflow_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/settings"
)

// issue #753: every daily entry-halt threshold is edited on the Settings
// screen, one key per row, and 0 disables the rule.
func TestSaveOps_DailyGuardThresholdsAreEditable(t *testing.T) {
	engine, store := opsRouter()

	for key, tc := range map[string]struct{ raw, stored string }{
		dailyguardsettings.KeyOperatingCapitalJPY:   {"500000", "500000"},
		dailyguardsettings.KeyLossLimitPct:          {"1.5", "1.5"},
		dailyguardsettings.KeyProfitLockTriggerPct:  {"0.5", "0.5"},
		dailyguardsettings.KeyLossLimitJPY:          {"50000", "50000"},
		dailyguardsettings.KeyProfitLockTriggerJPY:  {"30000.5", "30000.5"},
		dailyguardsettings.KeyProfitLockGivebackPct: {"30", "30"},
		dailyguardsettings.KeyMaxTrades:             {"8", "8"},
	} {
		rec := opsRequest(engine, http.MethodPost, key, url.Values{"value": {tc.raw}}, true)
		if rec.Code != http.StatusOK {
			t.Fatalf("POST %s: status = %d, body=%s", key, rec.Code, rec.Body.String())
		}
		if store.rows[key] != tc.stored {
			t.Errorf("stored %s = %q, want %q", key, store.rows[key], tc.stored)
		}
		if !strings.Contains(rec.Body.String(), `id="setting-group-status-daily-guard"`) {
			t.Errorf("POST %s: response lacks the group status; body=%s", key, rec.Body.String())
		}
	}

	// 0 stores as a value that disables the rule.
	if rec := opsRequest(engine, http.MethodPost, dailyguardsettings.KeyLossLimitJPY, url.Values{"value": {"0"}}, true); rec.Code != http.StatusOK {
		t.Fatalf("POST 0: status = %d", rec.Code)
	}
	if store.rows[dailyguardsettings.KeyLossLimitJPY] != "0" {
		t.Errorf("stored 0 = %q", store.rows[dailyguardsettings.KeyLossLimitJPY])
	}
}

func TestSaveOps_DailyGuardRejectsInvalidValues(t *testing.T) {
	engine, store := opsRouter()

	for name, tc := range map[string]struct{ key, raw string }{
		"capital above 1 兆円":        {dailyguardsettings.KeyOperatingCapitalJPY, "1e308"},
		"negative loss limit":       {dailyguardsettings.KeyLossLimitJPY, "-1"},
		"negative capital":          {dailyguardsettings.KeyOperatingCapitalJPY, "-1"},
		"loss pct above 100":        {dailyguardsettings.KeyLossLimitPct, "100.5"},
		"negative loss pct":         {dailyguardsettings.KeyLossLimitPct, "-1"},
		"trigger pct above 100":     {dailyguardsettings.KeyProfitLockTriggerPct, "101"},
		"text":                      {dailyguardsettings.KeyLossLimitJPY, "abc"},
		"giveback above 100":        {dailyguardsettings.KeyProfitLockGivebackPct, "100.5"},
		"fractional trade count":    {dailyguardsettings.KeyMaxTrades, "2.5"},
		"negative trade count":      {dailyguardsettings.KeyMaxTrades, "-3"},
		"negative profit trigger":   {dailyguardsettings.KeyProfitLockTriggerJPY, "-100"},
		"infinite loss limit":       {dailyguardsettings.KeyLossLimitJPY, "Inf"},
		"not-a-number profit value": {dailyguardsettings.KeyProfitLockTriggerJPY, "NaN"},
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

func TestResetOps_DailyGuardResetDisablesTheRule(t *testing.T) {
	engine, store := opsRouter()
	store.rows[dailyguardsettings.KeyMaxTrades] = "5"

	rec := opsRequest(engine, http.MethodDelete, dailyguardsettings.KeyMaxTrades, nil, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if _, ok := store.rows[dailyguardsettings.KeyMaxTrades]; ok {
		t.Fatal("Reset left the row")
	}
	if !strings.Contains(rec.Body.String(), "0（無効）") {
		t.Errorf("response does not show the disabled default; body=%s", rec.Body.String())
	}
}

func derivedLine(t *testing.T, body, key string) string {
	t.Helper()
	marker := `data-testid="setting-group-derived-daily-guard-` + key + `"`
	_, rest, ok := strings.Cut(body, marker)
	if !ok {
		t.Fatalf("no derived line %q; body=%s", key, body)
	}
	line, _, _ := strings.Cut(rest, "</li>")
	return line
}

// issue #782: the group shows the yen values computed from capital × percent.
func TestSettingsPage_DailyGuardShowsDerivedYen(t *testing.T) {
	engine, store := opsRouter()
	store.rows[dailyguardsettings.KeyOperatingCapitalJPY] = "500000"
	store.rows[dailyguardsettings.KeyLossLimitPct] = "1"
	store.rows[dailyguardsettings.KeyProfitLockTriggerPct] = "0.5"
	store.rows[dailyguardsettings.KeyProfitLockGivebackPct] = "50"
	store.rows[dailyguardsettings.KeyMaxTrades] = "20"

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/risk", nil))
	body := rec.Body.String()
	for key, want := range map[string][]string{
		"capital":             {"運用資金", "500,000 円"},
		"loss-limit":          {"5,000 円", "運用資金 500,000 円 × 1%"},
		"profit-lock-trigger": {"2,500 円", "運用資金 500,000 円 × 0.5%"},
		"profit-lock":         {"最高損益から 50% 戻したら停止"},
		"max-trades":          {"20 回"},
	} {
		line := derivedLine(t, body, key)
		for _, w := range want {
			if !strings.Contains(line, w) {
				t.Errorf("derived %s line lacks %q: %s", key, w, line)
			}
		}
	}
}

func TestSettingsPage_DailyGuardLegacyDirectYenStillShown(t *testing.T) {
	engine, store := opsRouter()
	store.rows[dailyguardsettings.KeyLossLimitJPY] = "50000"
	store.rows[dailyguardsettings.KeyProfitLockTriggerJPY] = "20000"

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/risk", nil))
	body := rec.Body.String()
	if line := derivedLine(t, body, "loss-limit"); !strings.Contains(line, "50,000 円") || !strings.Contains(line, "円で直接指定・互換設定") {
		t.Errorf("loss-limit line = %s", line)
	}
	if line := derivedLine(t, body, "profit-lock-trigger"); !strings.Contains(line, "20,000 円") || !strings.Contains(line, "円で直接指定・互換設定") {
		t.Errorf("profit-lock-trigger line = %s", line)
	}
	if line := derivedLine(t, body, "capital"); !strings.Contains(line, "未設定") {
		t.Errorf("capital line = %s", line)
	}
	if line := derivedLine(t, body, "max-trades"); !strings.Contains(line, "無効") {
		t.Errorf("max-trades line = %s", line)
	}
}

func TestSettingsPage_DailyGuardPercentWithoutCapitalIsDisabled(t *testing.T) {
	engine, store := opsRouter()
	store.rows[dailyguardsettings.KeyLossLimitPct] = "1"

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/risk", nil))
	line := derivedLine(t, rec.Body.String(), "loss-limit")
	if !strings.Contains(line, "無効") || !strings.Contains(line, "運用資金が未設定のため無効") {
		t.Errorf("loss-limit line = %s", line)
	}
}

// Changing the capital through the Settings screen changes the computed yen
// values in the out-of-band panel of the same response, without a reload.
func TestSaveOps_DailyGuardCapitalChangeRefreshesDerivedPanel(t *testing.T) {
	engine, store := opsRouter()
	store.rows[dailyguardsettings.KeyOperatingCapitalJPY] = "500000"
	store.rows[dailyguardsettings.KeyLossLimitPct] = "1"
	store.rows[dailyguardsettings.KeyProfitLockTriggerPct] = "0.5"

	rec := opsRequest(engine, http.MethodPost, dailyguardsettings.KeyOperatingCapitalJPY, url.Values{"value": {"1000000"}}, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `id="setting-group-derived-daily-guard"`) || !strings.Contains(body, `hx-swap-oob`) {
		t.Fatalf("response lacks the out-of-band derived panel; body=%s", body)
	}
	if line := derivedLine(t, body, "loss-limit"); !strings.Contains(line, "10,000 円") {
		t.Errorf("loss-limit after capital 1,000,000 = %s", line)
	}
	if line := derivedLine(t, body, "profit-lock-trigger"); !strings.Contains(line, "5,000 円") {
		t.Errorf("profit-lock-trigger after capital 1,000,000 = %s", line)
	}

	// Reset of the percentage refreshes the panel too.
	rec = opsRequest(engine, http.MethodDelete, dailyguardsettings.KeyLossLimitPct, nil, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("reset status = %d", rec.Code)
	}
	if line := derivedLine(t, rec.Body.String(), "loss-limit"); !strings.Contains(line, "無効") {
		t.Errorf("loss-limit after resetting the percentage = %s", line)
	}
}

func TestSettingsPage_DailyGuardYenUsesThousandsSeparators(t *testing.T) {
	engine, store := opsRouter()
	store.rows[dailyguardsettings.KeyOperatingCapitalJPY] = "123456789"
	store.rows[dailyguardsettings.KeyLossLimitPct] = "1"

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/risk", nil))
	body := rec.Body.String()
	if line := derivedLine(t, body, "capital"); !strings.Contains(line, "123,456,789 円") {
		t.Errorf("capital line = %s", line)
	}
	if line := derivedLine(t, body, "loss-limit"); !strings.Contains(line, "1,234,568 円") {
		t.Errorf("loss-limit line = %s", line)
	}
}

// A percent that rounds to under 1 yen wins over a direct yen row, and the
// panel says why the rule is off.
func TestSettingsPage_DailyGuardPercentUnderOneYenShowsReason(t *testing.T) {
	engine, store := opsRouter()
	store.rows[dailyguardsettings.KeyOperatingCapitalJPY] = "10"
	store.rows[dailyguardsettings.KeyLossLimitPct] = "1"
	store.rows[dailyguardsettings.KeyLossLimitJPY] = "50000"

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/risk", nil))
	line := derivedLine(t, rec.Body.String(), "loss-limit")
	if !strings.Contains(line, "無効") || !strings.Contains(line, "1 円未満") || strings.Contains(line, "50,000") {
		t.Errorf("loss-limit line = %s", line)
	}
}

// failingDailyGuard is the real service whose DailyGuard read fails.
type failingDailyGuard struct{ *opsettings.Service }

func (failingDailyGuard) DailyGuard(context.Context) (dailyguardsettings.Resolved, error) {
	return dailyguardsettings.Resolved{}, errors.New("store down")
}

// An unreadable derived configuration is stated on the panel, on the page and
// in the out-of-band answer to a save, instead of vanishing or going stale.
func TestSettings_DailyGuardDerivedReadFailureShowsUnreadablePanel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &memRuntimeStore{rows: map[string]string{}}
	h := settings.NewSettingsHandler(newFakeSecretsStore()).
		WithOperationalSettings(failingDailyGuard{opsettings.New(store, opsStrategy(), "/data/logs")})
	engine := settingsRouter(h)
	engine.POST("/ops-settings/:key", h.SaveOps)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/risk", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("page status = %d", rec.Code)
	}
	if line := derivedLine(t, rec.Body.String(), "error"); !strings.Contains(line, "計算結果を読み取れませんでした") {
		t.Errorf("page error line = %s", line)
	}

	rec = opsRequest(engine, http.MethodPost, dailyguardsettings.KeyMaxTrades, url.Values{"value": {"8"}}, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("save status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `id="setting-group-derived-daily-guard"`) || !strings.Contains(body, `hx-swap-oob="true"`) {
		t.Errorf("save response lacks the out-of-band derived panel; body=%s", body)
	}
	if line := derivedLine(t, body, "error"); !strings.Contains(line, "計算結果を読み取れませんでした") {
		t.Errorf("save error line = %s", line)
	}
	if store.rows[dailyguardsettings.KeyMaxTrades] != "8" {
		t.Errorf("the save itself must still succeed; stored = %q", store.rows[dailyguardsettings.KeyMaxTrades])
	}
}
