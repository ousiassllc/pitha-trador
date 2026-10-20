package opsflow_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/barriersettings"
	"github.com/ousiassllc/pitha-trador/internal/config/costsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/settings"
)

// riskRouter is opsRouter with the risk.yaml reference wired in (initial
// capital 30,000,000 円, max_daily_loss_pct 1) when ref is true.
func riskRouter(ref bool) (*gin.Engine, *memRuntimeStore) {
	if ref {
		return riskRouterWithLimits(&config.RiskLimits{InitialCapital: 30_000_000, MaxDailyLossPct: 1})
	}
	return riskRouterWithLimits(nil)
}

// riskRouterWithLimits is opsRouter with limits (config/risk.yaml's Paper
// section) wired in as the risk reference; nil wires none.
func riskRouterWithLimits(limits *config.RiskLimits) (*gin.Engine, *memRuntimeStore) {
	gin.SetMode(gin.TestMode)
	store := &memRuntimeStore{rows: map[string]string{}}
	svc := opsettings.New(store, opsStrategy(), "/data/logs")
	if limits != nil {
		svc.WithRiskReference(*limits)
	}
	h := settings.NewSettingsHandler(newFakeSecretsStore()).WithOperationalSettings(svc)
	engine := settingsRouter(h)
	engine.POST("/ops-settings/:key", h.SaveOps)
	engine.DELETE("/ops-settings/:key", h.ResetOps)
	return engine, store
}

func settingsBody(engine *gin.Engine) string {
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/risk", nil))
	return rec.Body.String()
}

func allRules(store *memRuntimeStore) {
	store.rows[dailyguardsettings.KeyOperatingCapitalJPY] = "30000000"
	store.rows[dailyguardsettings.KeyLossLimitPct] = "1"
	store.rows[dailyguardsettings.KeyProfitLockTriggerPct] = "0.5"
	store.rows[dailyguardsettings.KeyProfitLockGivebackPct] = "50"
	store.rows[dailyguardsettings.KeyMaxTrades] = "20"
}

// issue #782: a capital equal to initial_capital with the loss rate equal to
// the Kill Switch rate is consistent, and all three rules enabled passes the
// Live pre-check.
func TestSettingsPage_DailyGuardChecksPassWhenConsistentAndAllRulesEnabled(t *testing.T) {
	engine, store := riskRouter(true)
	allRules(store)
	body := settingsBody(engine)

	for _, key := range []string{"consistency", "live"} {
		derivedLine(t, body, key) // headings render
	}
	if line := derivedLine(t, body, "consistency-ok"); !strings.Contains(line, "問題なし") {
		t.Errorf("consistency-ok = %s", line)
	}
	if line := derivedLine(t, body, "live-ok"); !strings.Contains(line, "3つのルールすべて有効") {
		t.Errorf("live-ok = %s", line)
	}
	if line := derivedLine(t, body, "live"); !strings.Contains(line, "警告のみ（拒否はしない）") || !strings.Contains(line, "決定待ち") {
		t.Errorf("live heading lacks the warn-only note: %s", line)
	}
	for _, code := range []string{dailyguardsettings.WarnCapitalMismatch, dailyguardsettings.WarnLossLimitAboveKillSwitch, dailyguardsettings.WarnLiveLossLimit} {
		if strings.Contains(body, `-derived-daily-guard-`+code) || strings.Contains(body, "derived-daily-guard-"+code) {
			t.Errorf("unexpected warning %s; body=%s", code, body)
		}
	}
}

// A different capital warns on the page, and changing the capital through
// the screen flips the warning in the out-of-band panel of the same answer.
func TestSaveOps_DailyGuardCapitalChangeFlipsConsistencyWarning(t *testing.T) {
	engine, store := riskRouter(true)
	allRules(store)
	store.rows[dailyguardsettings.KeyOperatingCapitalJPY] = "500000"

	body := settingsBody(engine)
	line := derivedLine(t, body, dailyguardsettings.WarnCapitalMismatch)
	for _, want := range []string{"注意:", "運用資金 500,000 円", "initial_capital 30,000,000 円"} {
		if !strings.Contains(line, want) {
			t.Errorf("page capital-mismatch line lacks %q: %s", want, line)
		}
	}

	rec := opsRequest(engine, http.MethodPost, dailyguardsettings.KeyOperatingCapitalJPY, url.Values{"value": {"1000000"}}, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if line := derivedLine(t, rec.Body.String(), dailyguardsettings.WarnCapitalMismatch); !strings.Contains(line, "運用資金 1,000,000 円") {
		t.Errorf("oob capital-mismatch line after saving 1,000,000 = %s", line)
	}

	rec = opsRequest(engine, http.MethodPost, dailyguardsettings.KeyOperatingCapitalJPY, url.Values{"value": {"30000000"}}, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), dailyguardsettings.WarnCapitalMismatch) {
		t.Errorf("capital equal to initial_capital still warns: %s", rec.Body.String())
	}
	if line := derivedLine(t, rec.Body.String(), "consistency-ok"); !strings.Contains(line, "問題なし") {
		t.Errorf("consistency-ok = %s", line)
	}
}

func TestSettingsPage_DailyGuardLossLimitAboveKillSwitchWarns(t *testing.T) {
	engine, store := riskRouter(true)
	allRules(store)
	store.rows[dailyguardsettings.KeyLossLimitPct] = "2"

	line := derivedLine(t, settingsBody(engine), dailyguardsettings.WarnLossLimitAboveKillSwitch)
	for _, want := range []string{"注意:", "600,000 円", "300,000 円", "強制決済が発動します"} {
		if !strings.Contains(line, want) {
			t.Errorf("line lacks %q: %s", want, line)
		}
	}
}

// Without the risk reference the 整合チェック block is not shown at all.
func TestSettingsPage_DailyGuardWithoutRiskReferenceSkipsConsistency(t *testing.T) {
	engine, store := riskRouter(false)
	allRules(store)
	store.rows[dailyguardsettings.KeyOperatingCapitalJPY] = "500000"

	body := settingsBody(engine)
	for _, gone := range []string{"整合チェック", "consistency", dailyguardsettings.WarnCapitalMismatch} {
		if strings.Contains(body, gone) {
			t.Errorf("unwired risk reference still renders %q", gone)
		}
	}
	if !strings.Contains(body, "3つのルールすべて有効") {
		t.Errorf("the Live pre-check must still render: %s", body)
	}
}

// Deleting (resetting) a rule's value shows the Live pre-check warning in the
// out-of-band panel; the page always names each disabled rule, and nothing is
// rejected (the save still answers 200).
func TestResetOps_DailyGuardLivePrecheckWarnsOnDisabledRule(t *testing.T) {
	engine, store := riskRouter(true)
	allRules(store)

	rec := opsRequest(engine, http.MethodDelete, dailyguardsettings.KeyMaxTrades, nil, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("reset status = %d", rec.Code)
	}
	if line := derivedLine(t, rec.Body.String(), dailyguardsettings.WarnLiveMaxTrades); !strings.Contains(line, "注意:") || !strings.Contains(line, "最大取引回数が無効") {
		t.Errorf("live-max-trades line = %s", line)
	}
	if strings.Contains(rec.Body.String(), "3つのルールすべて有効") {
		t.Errorf("all-enabled text shown with a disabled rule: %s", rec.Body.String())
	}

	rec = opsRequest(engine, http.MethodDelete, dailyguardsettings.KeyProfitLockGivebackPct, nil, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("reset status = %d", rec.Code)
	}
	if line := derivedLine(t, rec.Body.String(), dailyguardsettings.WarnLiveProfitLock); !strings.Contains(line, "利益確保が無効") {
		t.Errorf("live-profit-lock line = %s", line)
	}

	body := settingsBody(engine)
	for _, code := range []string{dailyguardsettings.WarnLiveMaxTrades, dailyguardsettings.WarnLiveProfitLock} {
		derivedLine(t, body, code)
	}
	if strings.Contains(body, "derived-daily-guard-"+dailyguardsettings.WarnLiveLossLimit) {
		t.Errorf("the enabled loss limit must not warn")
	}
}

func TestSettingsPage_DailyGuardNothingSetWarnsAllThreeRules(t *testing.T) {
	engine, _ := riskRouter(true)
	body := settingsBody(engine)
	for _, code := range []string{dailyguardsettings.WarnLiveLossLimit, dailyguardsettings.WarnLiveProfitLock, dailyguardsettings.WarnLiveMaxTrades} {
		if line := derivedLine(t, body, code); !strings.Contains(line, "注意:") {
			t.Errorf("%s line = %s", code, line)
		}
	}
	// An unset capital is already reported by the panel and is not repeated by the consistency check.
	if line := derivedLine(t, body, "consistency-ok"); !strings.Contains(line, "問題なし") {
		t.Errorf("consistency-ok = %s", line)
	}
}

// issue #779: a reset whose default would leave an invalid combination (SL 60
// and a 15 bps fee, then SL back to 30 under a label cost of 40) is a 400 that
// keeps the stored value.
func TestResetOps_RejectsAnInvalidDefaultCombination(t *testing.T) {
	engine, store := opsRouter()
	store.rows[barriersettings.KeyStopLossBps] = "60"
	store.rows[costsettings.KeyFeeBps] = "15"

	rec := opsRequest(engine, http.MethodDelete, barriersettings.KeyStopLossBps, nil, true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if got := store.rows[barriersettings.KeyStopLossBps]; got != "60" {
		t.Fatalf("stored SL after the rejected reset = %q, want 60", got)
	}
}
