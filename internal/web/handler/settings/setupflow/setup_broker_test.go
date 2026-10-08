// Package setupflow tests the broker-dependent Setup screen (issue #734)
// through the real opsettings.Service over in-memory stores.
package setupflow_test

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

type memSecrets map[string]string

func (m memSecrets) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := m[key]
	return v, ok, nil
}
func (m memSecrets) Set(_ context.Context, key, value string) error { m[key] = value; return nil }
func (m memSecrets) Delete(_ context.Context, key string) error     { delete(m, key); return nil }

type memRuntime map[string]string

func (m memRuntime) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := m[key]
	return v, ok, nil
}
func (m memRuntime) Set(_ context.Context, key, value string, _ time.Time) error {
	m[key] = value
	return nil
}
func (m memRuntime) Delete(_ context.Context, key string) error { delete(m, key); return nil }

func strategy() config.StrategyConfig {
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

func newEngine(secrets memSecrets, runtime memRuntime) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := settings.NewSettingsHandler(secrets).WithOperationalSettings(opsettings.New(runtime, strategy(), "/data/logs"))
	engine := gin.New()
	engine.GET("/setup", h.SetupPage)
	engine.GET("/settings", h.Page)
	engine.POST("/ops-settings/:key", h.SaveOps)
	return engine
}

func get(engine *gin.Engine, path string) string {
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec.Body.String()
}

func hasCard(body, id string) bool {
	return strings.Contains(body, `data-testid="settings-card-`+id+`"`)
}

func TestSetupPage_KabuIsTheDefaultAndOffersTheBrokerSelection(t *testing.T) {
	body := get(newEngine(memSecrets{}, memRuntime{}), "/setup")
	for _, id := range []string{"jev", "kabu", "slack", "broker"} {
		if !hasCard(body, id) {
			t.Errorf("kabu Setup lacks the %s card", id)
		}
	}
	for _, id := range []string{"tachibana"} {
		if hasCard(body, id) {
			t.Errorf("kabu Setup offers the %s card", id)
		}
	}
	if !strings.Contains(body, `data-testid="setting-field-row-`+config.KeyBrokerProvider+`"`) {
		t.Error("Setup does not offer the broker.provider row")
	}
	if !strings.Contains(body, `data-testid="setup-incomplete"`) {
		t.Error("an empty kabu setup must be incomplete")
	}
}

func TestSetupPage_TachibanaDemoNeedsOnlyJevAuthIDAndKeyPath(t *testing.T) {
	runtime := memRuntime{config.KeyBrokerProvider: `"tachibana"`}
	secrets := memSecrets{config.KeyJevAPIKey: "j", config.KeyTachibanaDemoAuthID: "demo-id-SECRET"}
	engine := newEngine(secrets, runtime)

	body := get(engine, "/setup")
	if !hasCard(body, "tachibana") || hasCard(body, "kabu") {
		t.Errorf("tachibana Setup must offer the tachibana card and not kabu; body=%s", body)
	}
	if !strings.Contains(body, `data-testid="setting-field-row-`+config.KeyTachibanaDemoPrivateKeyPath+`"`) {
		t.Error("tachibana Setup does not offer the private key path row")
	}
	if !strings.Contains(body, `data-testid="setup-incomplete"`) {
		t.Error("without the key path the setup must stay incomplete")
	}
	if strings.Contains(body, "demo-id-SECRET") {
		t.Error("the stored 認証ID is shown back")
	}

	runtime[config.KeyTachibanaDemoPrivateKeyPath] = `"/keys/demo-PATH.pem"`
	body = get(engine, "/setup")
	if !strings.Contains(body, `data-testid="setup-complete"`) {
		t.Errorf("demo ID + key path (no KABU_API_PASSWORD) must complete the setup; body=%s", body)
	}
	if strings.Contains(body, "/keys/demo-PATH.pem") {
		t.Error("the stored key path is shown back")
	}
}

func TestSetupPage_TachibanaProductionRequiresTheProductionIDAndPath(t *testing.T) {
	runtime := memRuntime{
		config.KeyBrokerProvider:              `"tachibana"`,
		config.KeyTachibanaEnvironment:        `"production"`,
		config.KeyTachibanaDemoPrivateKeyPath: `"/keys/demo.pem"`,
	}
	secrets := memSecrets{config.KeyJevAPIKey: "j", config.KeyTachibanaDemoAuthID: "d"}
	engine := newEngine(secrets, runtime)
	if body := get(engine, "/setup"); !strings.Contains(body, `data-testid="setup-incomplete"`) {
		t.Error("demo credentials must not satisfy a production selection")
	}
	secrets[config.KeyTachibanaProdAuthID] = "p"
	runtime[config.KeyTachibanaProdPrivateKeyPath] = `"/keys/prod.pem"`
	if body := get(engine, "/setup"); !strings.Contains(body, `data-testid="setup-complete"`) {
		t.Errorf("production ID + path must complete the setup; body=%s", body)
	}
}

// Saving the broker (or environment) on /setup reloads it, so the offered
// connections and 必須 badges follow; the row's setup status is refreshed too.
func TestSaveOps_FromSetupRefreshesThePageAndSetupStatus(t *testing.T) {
	engine := newEngine(memSecrets{}, memRuntime{})
	post := func(key, value, referer string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/ops-settings/"+key, strings.NewReader(url.Values{"value": {value}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")
		req.Header.Set("Referer", referer)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		return rec
	}

	rec := post(config.KeyBrokerProvider, "tachibana", "http://localhost/setup")
	if rec.Code != http.StatusOK || rec.Header().Get("HX-Refresh") != "true" {
		t.Fatalf("status = %d, HX-Refresh = %q, want 200 and true", rec.Code, rec.Header().Get("HX-Refresh"))
	}
	if !strings.Contains(rec.Body.String(), `id="setup-status"`) {
		t.Error("the response lacks the out-of-band setup status")
	}
	if rec := post(config.KeyBrokerProvider, "kabu", "http://localhost/settings"); rec.Header().Get("HX-Refresh") != "" || strings.Contains(rec.Body.String(), `id="setup-status"`) {
		t.Error("a save from /settings must not refresh or carry the setup status")
	}
}

func TestSettingsPage_RequiredBadgeFollowsTheSelectedBroker(t *testing.T) {
	kabu := get(newEngine(memSecrets{}, memRuntime{}), "/settings")
	tachibana := get(newEngine(memSecrets{}, memRuntime{config.KeyBrokerProvider: `"tachibana"`}), "/settings")
	required := func(body, id string) bool {
		i := strings.Index(body, `data-testid="settings-card-`+id+`"`)
		if i < 0 {
			t.Fatalf("no %s card", id)
		}
		end := strings.Index(body[i:], "</li>")
		return strings.Contains(body[i:i+end], ">必須<")
	}
	if !required(kabu, "kabu") || required(kabu, "tachibana") {
		t.Error("with kabu selected only kabuステーション is 必須")
	}
	if required(tachibana, "kabu") || !required(tachibana, "tachibana") {
		t.Error("with tachibana selected only 立花証券 e支店 is 必須")
	}
}
