package settings_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/settings"
)

// memRuntimeStore is an in-memory opsettings.Store.
type memRuntimeStore struct{ rows map[string]string }

func (m *memRuntimeStore) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := m.rows[key]
	return v, ok, nil
}

func (m *memRuntimeStore) Set(_ context.Context, key, value string, _ time.Time) error {
	m.rows[key] = value
	return nil
}

func (m *memRuntimeStore) Delete(_ context.Context, key string) error {
	delete(m.rows, key)
	return nil
}

func opsStrategy() config.StrategyConfig {
	dir := config.PolicyDirectionThresholds{MinProbability: 0.7, MinEntryQuality: "good", MinContinuationProbability: 0.6, MaxToxicFlow: 0.3, MaxLiquidityStressed: 0.3}
	return config.StrategyConfig{
		Policy: config.PolicyConfig{Long: dir, Short: dir},
		FastScreener: config.FastScreenerConfig{
			MinPrice: 100, MaxPrice: 5000, MinTurnover5mJPY: 1e7, MaxSpreadBps: 20, MinVolumeRatio: 2,
			MinAbsReturn5mPct: 0.5, MinRealizedVolatility: 0.1, TopN: 10,
			Weights: config.FastScreenerWeights{VolumeRatio: 1, AbsReturn5m: 1, BreakoutStrength: 1, OrderbookImbalance: 1, VolatilityExpansion: 1},
		},
	}
}

func opsRouter() (*gin.Engine, *memRuntimeStore) {
	gin.SetMode(gin.TestMode)
	store := &memRuntimeStore{rows: map[string]string{}}
	h := settings.NewSettingsHandler(newFakeSecretsStore()).WithOperationalSettings(opsettings.New(store, opsStrategy(), "/data/logs"))
	engine := settingsRouter(h)
	engine.POST("/ops-settings/:key", h.SaveOps)
	engine.DELETE("/ops-settings/:key", h.ResetOps)
	return engine, store
}

func opsRequest(engine *gin.Engine, method, key string, form url.Values, htmx bool) *httptest.ResponseRecorder {
	var req *http.Request
	if form != nil {
		req = httptest.NewRequest(method, "/ops-settings/"+key, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req = httptest.NewRequest(method, "/ops-settings/"+key, nil)
	}
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func TestSettingsPage_ShowsEveryOperationalSettingAndHidesItWithoutService(t *testing.T) {
	engine, _ := opsRouter()
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings", nil))
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	for _, key := range opsettings.Keys() {
		if n := strings.Count(body, `data-testid="setting-field-row-`+key+`"`); n != 1 {
			t.Errorf("page renders %d rows for %q, want exactly 1", n, key)
		}
	}
	for _, id := range []string{"backup", "logdir", "policy-long", "policy-short", "screener"} {
		if !strings.Contains(body, `data-testid="settings-card-`+id+`"`) {
			t.Errorf("page has no card %q", id)
		}
	}

	plain := httptest.NewRecorder()
	settingsRouter(settings.NewSettingsHandler(newFakeSecretsStore())).ServeHTTP(plain, httptest.NewRequest(http.MethodGet, "/settings", nil))
	if strings.Contains(plain.Body.String(), "setting-field-row-") || strings.Contains(plain.Body.String(), "運用設定") {
		t.Error("the 運用設定 section must be hidden without OperationalSettings")
	}
}

func TestSaveOps_StoresOnlyThePathKeyAndRendersTheRow(t *testing.T) {
	engine, store := opsRouter()
	store.rows["screener.top_n"] = "20"

	rec := opsRequest(engine, http.MethodPost, "policy.long.min_probability", url.Values{
		"value":          {"0.75"},
		"screener.top_n": {"99"}, // other keys in the body are ignored
	}, true)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if len(store.rows) != 2 || store.rows["policy.long.min_probability"] != "0.75" || store.rows["screener.top_n"] != "20" {
		t.Errorf("store = %v, want only policy.long.min_probability added", store.rows)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`data-testid="setting-field-row-policy.long.min_probability"`,
		`data-testid="notice-policy.long.min_probability"`,
		`data-testid="overridden-policy.long.min_probability"`,
		`value="0.75"`,
		`id="setting-group-status-policy-long"`, `hx-swap-oob="true"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("response missing %q; body=%s", want, body)
		}
	}
}

func TestSaveOps_BackupDirIsStoredAndShowsWarningWhenUnavailable(t *testing.T) {
	engine, store := opsRouter()
	dir := t.TempDir() + "/not-mounted"

	rec := opsRequest(engine, http.MethodPost, config.KeyBackupDir, url.Values{"value": {dir}}, true)
	if rec.Code != http.StatusOK || store.rows[config.KeyBackupDir] != `"`+dir+`"` {
		t.Fatalf("status = %d, store = %v", rec.Code, store.rows)
	}
	if !strings.Contains(rec.Body.String(), `data-testid="warning-`+config.KeyBackupDir+`"`) {
		t.Errorf("a missing destination must show a warning; body=%s", rec.Body.String())
	}
}

func TestSaveOps_RejectsBadInputWithoutWriting(t *testing.T) {
	cases := map[string]struct{ key, value string }{
		"blank":        {"policy.long.min_probability", "   "},
		"out of range": {"policy.long.min_probability", "2"},
		"not a number": {"screener.min_price", "abc"},
		"relative dir": {config.KeyBackupDir, "backups"},
		"unknown key":  {"risk.max_position_size", "1"},
		"secret key":   {config.KeyJevAPIKey, "x"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			engine, store := opsRouter()
			rec := opsRequest(engine, http.MethodPost, tc.key, url.Values{"value": {tc.value}}, true)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
			}
			if len(store.rows) != 0 {
				t.Errorf("store written: %v", store.rows)
			}
		})
	}
}

func TestResetOps_RemovesTheStoredValue(t *testing.T) {
	engine, store := opsRouter()
	store.rows["screener.top_n"] = "25"
	store.rows["screener.min_price"] = "150"

	rec := opsRequest(engine, http.MethodDelete, "screener.top_n", nil, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if _, ok := store.rows["screener.top_n"]; ok || store.rows["screener.min_price"] != "150" {
		t.Errorf("store = %v, want only screener.top_n removed", store.rows)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `value="10"`) || strings.Contains(body, `data-testid="overridden-screener.top_n"`) {
		t.Errorf("row must show the yaml default again; body=%s", body)
	}
}

func TestOpsRoutes_NonHTMXRedirectsBackToSettings(t *testing.T) {
	engine, store := opsRouter()
	rec := opsRequest(engine, http.MethodPost, "screener.top_n", url.Values{"value": {"30"}}, false)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/settings" || store.rows["screener.top_n"] != "30" {
		t.Errorf("status = %d, location = %q, store = %v", rec.Code, rec.Header().Get("Location"), store.rows)
	}
}
