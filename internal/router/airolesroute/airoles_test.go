// Package airolesroute holds the router-level tests of the Settings screen's
// 役割ごとのモデル section (issue #1080): the page order (providers, then the
// roles' models), the panel, that saving needs the session and CSRF token, that a
// rejected form saves nothing, and that a saved change is recorded.
package airolesroute_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/config/airole"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/service/airoles"
	"github.com/ousiassllc/pitha-trador/internal/web/middleware"
)

var csrfMeta = regexp.MustCompile(`<meta name="csrf-token" content="([^"]+)"`)

// keySet reports the OpenRouter key as set and the 独自 API key as unset; it has no value to give.
type keySet struct{}

func (keySet) Get(_ context.Context, key string) (string, bool, error) {
	return "", key == "OPENROUTER_API_KEY", nil
}

// allSet is the screen's secrets store: every key set, so the Setup Guard lets the pages through.
type allSet struct{}

func (allSet) Get(context.Context, string) (string, bool, error) { return "x", true, nil }
func (allSet) Set(context.Context, string, string) error         { return nil }
func (allSet) Delete(context.Context, string) error              { return nil }

type app struct {
	t      *testing.T
	engine *gin.Engine
	db     *sql.DB
	cookie *http.Cookie
	csrf   string
}

func newApp(t *testing.T) *app {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := airoles.New(system.NewRuntimeSettingsRepository(db), system.NewAIRoleRepository(db), keySet{})
	a := &app{t: t, db: db}
	a.engine = router.New(router.WithSecretsStore(allSet{}), router.WithAIRoleSettings(svc))
	page := httptest.NewRecorder()
	a.engine.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/setup", nil))
	match := csrfMeta.FindStringSubmatch(page.Body.String())
	if len(page.Result().Cookies()) != 1 || match == nil {
		t.Fatalf("GET /setup gave cookies=%v csrf=%v", page.Result().Cookies(), match)
	}
	a.cookie, a.csrf = page.Result().Cookies()[0], match[1]
	return a
}

func (a *app) do(method, path string, form url.Values, withCSRF bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	if withCSRF {
		req.Header.Set(middleware.CSRFHeader, a.csrf)
	}
	req.AddCookie(a.cookie)
	rec := httptest.NewRecorder()
	a.engine.ServeHTTP(rec, req)
	return rec
}

func (a *app) count(query string) int {
	a.t.Helper()
	var n int
	if err := a.db.QueryRow(query).Scan(&n); err != nil {
		a.t.Fatal(err)
	}
	return n
}

func TestNeedsTheService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New(router.WithSecretsStore(allSet{}))
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/ai-roles-panel", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET without the service = %d, want 404", rec.Code)
	}
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/integrations", nil))
	if strings.Contains(rec.Body.String(), `id="modal-ai-roles"`) || !strings.Contains(rec.Body.String(), "settings-connections-heading") {
		t.Error("without the service the page must show the connections as before and no roles card")
	}
}

// The page lists the providers first, then the roles' models, then the other connections.
func TestIntegrationsPageOrdersProvidersThenRolesThenOtherConnections(t *testing.T) {
	a := newApp(t)
	rec := a.do(http.MethodGet, "/settings/integrations", nil, false)
	page := rec.Body.String()
	order := []string{"settings-ai-providers-heading", "settings-ai-roles-heading", "settings-connections-heading"}
	last := -1
	for _, id := range order {
		i := strings.Index(page, `id="`+id+`"`)
		if i < 0 || i < last {
			t.Fatalf("section %s is missing or out of order (index %d after %d)", id, i, last)
		}
		last = i
	}
	providers := page[strings.Index(page, `id="settings-ai-providers-heading"`):strings.Index(page, `id="settings-ai-roles-heading"`)]
	for _, want := range []string{"独自 API（Jev）", "OpenRouter"} {
		if !strings.Contains(providers, want) {
			t.Errorf("the provider section lacks %q", want)
		}
	}
	others := page[strings.Index(page, `id="settings-connections-heading"`):]
	for _, want := range []string{"Luna", "Sol", "Opus"} {
		if !strings.Contains(others, want) {
			t.Errorf("the other connections lack %q", want)
		}
	}
	for _, notThere := range []string{`data-testid="settings-card-jev"`, `data-testid="settings-card-openrouter"`} {
		if strings.Contains(others, notThere) {
			t.Errorf("%s is listed again among the other connections", notThere)
		}
	}
	for _, want := range []string{`id="modal-ai-roles"`, `hx-get="/settings/ai-roles-panel"`} {
		if !strings.Contains(page, want) {
			t.Errorf("the page lacks %q", want)
		}
	}
}

func TestPanelShowsEveryRoleWithTheDefaultAndOnlyTheKeyState(t *testing.T) {
	a := newApp(t)
	body := a.do(http.MethodGet, "/settings/ai-roles-panel", nil, false).Body.String()
	for _, role := range airole.Roles() {
		if !strings.Contains(body, `data-testid="ai-role-`+role+`"`) {
			t.Errorf("no form for role %s", role)
		}
	}
	for _, want := range []string{"既定です（今までと同じ接続先・キー・モデル）", "OpenRouterの API キー: 設定済み", "独自 APIの API キー: 未設定", "この画面でだけ変えられます", "MCP からは変えられません"} {
		if !strings.Contains(strings.ReplaceAll(body, " ", ""), strings.ReplaceAll(want, " ", "")) {
			t.Errorf("the panel lacks %q", want)
		}
	}
	if strings.Contains(body, "ai-role-reset-") {
		t.Error("a reset button is shown while nothing is stored")
	}
}

func TestSaveStoresRecordsAndNeedsTheTokenAndAValidForm(t *testing.T) {
	a := newApp(t)
	form := url.Values{"provider": {"openrouter"}, "model": {"vendor/jev-model"}}

	if rec := a.do(http.MethodPost, "/settings/ai-roles/jev", form, false); rec.Code != http.StatusForbidden {
		t.Fatalf("POST without the CSRF token = %d, want 403", rec.Code)
	}
	if rec := a.do(http.MethodDelete, "/settings/ai-roles/jev", nil, false); rec.Code != http.StatusForbidden {
		t.Fatalf("DELETE without the CSRF token = %d, want 403", rec.Code)
	}
	if n := a.count(`SELECT COUNT(*) FROM runtime_settings WHERE key LIKE 'ai.role.%'`); n != 0 {
		t.Fatalf("refused requests stored %d rows", n)
	}

	for name, bad := range map[string]url.Values{
		"openrouter without a model": {"provider": {"openrouter"}, "model": {""}},
		"unknown provider":           {"provider": {"chatgpt"}, "model": {"m"}},
		"model with a space":         {"provider": {"openrouter"}, "model": {"a b"}},
	} {
		rec := a.do(http.MethodPost, "/settings/ai-roles/jev", bad, true)
		if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "保存していません") {
			t.Errorf("%s: status %d body %s", name, rec.Code, rec.Body.String())
		}
	}
	if rec := a.do(http.MethodPost, "/settings/ai-roles/nobody", form, true); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("unknown role = %d, want 422", rec.Code)
	}
	if n := a.count(`SELECT COUNT(*) FROM runtime_settings WHERE key LIKE 'ai.role.%'`) + a.count(`SELECT COUNT(*) FROM ai_role_history`); n != 0 {
		t.Fatalf("rejected forms saved %d rows", n)
	}

	rec := a.do(http.MethodPost, "/settings/ai-roles/jev", form, true)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "再起動が必要です") || !strings.Contains(rec.Body.String(), `data-testid="ai-role-reset-jev"`) {
		t.Fatalf("save = %d %s", rec.Code, rec.Body.String())
	}
	var stored string
	if err := a.db.QueryRow(`SELECT value FROM runtime_settings WHERE key = 'ai.role.jev'`).Scan(&stored); err != nil || stored != `{"provider":"openrouter","model":"vendor/jev-model"}` {
		t.Fatalf("stored = %q (%v)", stored, err)
	}
	if n := a.count(`SELECT COUNT(*) FROM ai_role_history WHERE kind = 'saved' AND actor = 'gui' AND role = 'jev' AND provider = 'openrouter' AND before_provider = 'custom'`); n != 1 {
		t.Errorf("history rows for the live Jev switch = %d, want 1", n)
	}
	if !strings.Contains(rec.Body.String(), "設定を保存: 独自 API / モデルの指定なし → OpenRouter / vendor/jev-model") {
		t.Errorf("the history in the panel does not show the change: %s", rec.Body.String())
	}

	rec = a.do(http.MethodDelete, "/settings/ai-roles/jev", nil, true)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `data-testid="ai-role-reset-jev"`) {
		t.Fatalf("reset = %d %s", rec.Code, rec.Body.String())
	}
	if n := a.count(`SELECT COUNT(*) FROM runtime_settings WHERE key LIKE 'ai.role.%'`); n != 0 {
		t.Errorf("the setting is still stored after the reset (%d)", n)
	}
}

// Sol, Opus and Luna apply a save at their next call (issue #1112): the notice says so and the panel
// does not tell the operator to restart; only the live Jev needs one.
func TestSaveAndResetOfSolOpusLunaNeedNoRestart(t *testing.T) {
	a := newApp(t)
	form := url.Values{"provider": {"openrouter"}, "model": {"vendor/model"}}
	for _, role := range []string{"sol", "opus", "luna"} {
		rec := a.do(http.MethodPost, "/settings/ai-roles/"+role, form, true)
		body := rec.Body.String()
		if rec.Code != http.StatusOK || !strings.Contains(body, "保存しました。次の呼び出しから反映されます。") {
			t.Fatalf("save %s = %d %s", role, rec.Code, body)
		}
		if strings.Contains(body, "保存しました。反映にはアプリの再起動が必要です。") {
			t.Errorf("save %s asks for a restart", role)
		}
		if !strings.Contains(body, "Sol・Opus・Luna は保存後すぐに反映されます") || !strings.Contains(body, "Jev の売買判断は再起動が必要です") {
			t.Errorf("the panel does not explain what is hot reloaded: %s", body)
		}
		rec = a.do(http.MethodDelete, "/settings/ai-roles/"+role, nil, true)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "既定に戻しました。次の呼び出しから反映されます。") {
			t.Fatalf("reset %s = %d %s", role, rec.Code, rec.Body.String())
		}
	}
}
