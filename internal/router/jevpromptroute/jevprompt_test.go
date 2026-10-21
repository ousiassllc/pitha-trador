// Package jevpromptroute holds the router-level tests of the Settings screen's
// Jev prompt section (issue #872): the card, the panel with the switch, the
// candidate, the daily cost cap and the cost, the explicit promotion with its
// audit row, and that every state change needs the session and CSRF token. The
// pieces are covered in internal/service/jev/evalflow (shadow, promotion) and
// internal/service/paperresultsmcp/shadowtests (the MCP twin).
package jevpromptroute_test

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
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/service/jevprompt"
	"github.com/ousiassllc/pitha-trador/internal/web/middleware"
)

var csrfMeta = regexp.MustCompile(`<meta name="csrf-token" content="([^"]+)"`)

type fullSecrets struct{}

func (fullSecrets) Get(context.Context, string) (string, bool, error) { return "x", true, nil }
func (fullSecrets) Set(context.Context, string, string) error         { return nil }
func (fullSecrets) Delete(context.Context, string) error              { return nil }

type app struct {
	t        *testing.T
	engine   *gin.Engine
	db       *sql.DB
	settings *system.RuntimeSettingsRepository
	cookie   *http.Cookie
	csrf     string
}

var now = time.Date(2026, 10, 22, 3, 0, 0, 0, time.UTC) // 12:00 JST

func newApp(t *testing.T) *app {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := jevprompt.New(db)
	svc.SetClock(func() time.Time { return now })
	a := &app{t: t, db: db, settings: system.NewRuntimeSettingsRepository(db)}
	a.engine = router.New(router.WithSecretsStore(fullSecrets{}), router.WithJevPromptSettings(svc))
	page := httptest.NewRecorder()
	a.engine.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/setup", nil))
	match := csrfMeta.FindStringSubmatch(page.Body.String())
	if len(page.Result().Cookies()) != 1 || match == nil {
		t.Fatalf("GET /setup gave cookies=%v csrf=%v", page.Result().Cookies(), match)
	}
	a.cookie, a.csrf = page.Result().Cookies()[0], match[1]
	return a
}

func (a *app) get(path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.AddCookie(a.cookie)
	rec := httptest.NewRecorder()
	a.engine.ServeHTTP(rec, req)
	return rec
}

func (a *app) post(path string, form url.Values, withCSRF bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
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

func (a *app) stored(key string) (string, bool) {
	v, ok, err := a.settings.Get(context.Background(), key)
	if err != nil {
		a.t.Fatal(err)
	}
	return v, ok
}

func (a *app) rows(query string) int {
	var n int
	if err := a.db.QueryRow(query).Scan(&n); err != nil {
		a.t.Fatal(err)
	}
	return n
}

// Without the service the card is hidden and the routes are absent.
func TestNeedsTheService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New(router.WithSecretsStore(fullSecrets{}))
	for _, path := range []string{"/settings/jev-prompt-panel"} {
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s without the service = %d, want 404", path, rec.Code)
		}
	}
}

func TestCardAndPanelShowTheDefaults(t *testing.T) {
	a := newApp(t)
	page := a.get("/settings/integrations").Body.String()
	for _, want := range []string{`id="modal-jev-prompt"`, `data-testid="settings-card-jev-prompt"`, `hx-get="/settings/jev-prompt-panel"`} {
		if !strings.Contains(page, want) {
			t.Errorf("the integrations page lacks %q", want)
		}
	}
	body := a.get("/settings/jev-prompt-panel").Body.String()
	for _, want := range []string{
		`data-testid="jev-prompt-live-version">trader-v3<`,
		`data-testid="jev-prompt-enabled"`, // unchecked: the shadow is off by default
		"暫定の案で、まだ承認されていません",
		`data-testid="jev-prompt-cost-today">0 USD`, `data-testid="jev-prompt-cost-total">0 USD`,
		"まだ昇格していません", "Paper だけで動き、Live では動きません",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("panel lacks %q", want)
		}
	}
	if strings.Contains(body, `data-testid="jev-prompt-enabled" checked`) || regexp.MustCompile(`checked[^>]*data-testid="jev-prompt-enabled"|data-testid="jev-prompt-enabled"[^>]*checked`).MatchString(body) {
		t.Error("the shadow switch is checked by default")
	}
	// The live version is not offered as a candidate.
	if strings.Contains(body, `<option value="trader-v3"`) {
		t.Error("the live version is offered as a candidate / promotion target")
	}
}

func TestSavingTheShadowSettings(t *testing.T) {
	a := newApp(t)
	// No CSRF token: refused, nothing stored.
	if rec := a.post("/settings/jev-prompt/shadow", url.Values{"enabled": {"on"}, "candidate": {"trader-v4"}, "cap": {"0.5"}}, false); rec.Code == http.StatusOK {
		t.Fatalf("POST without CSRF = %d", rec.Code)
	}
	if _, ok := a.stored(jevshadowsettings.KeyEnabled); ok {
		t.Fatal("a POST without CSRF stored the switch")
	}

	rec := a.post("/settings/jev-prompt/shadow", url.Values{"enabled": {"on"}, "candidate": {"trader-v4"}, "cap": {"0.5"}}, true)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "保存しました。") {
		t.Fatalf("save = %d %s", rec.Code, rec.Body.String())
	}
	for key, want := range map[string]string{jevshadowsettings.KeyEnabled: "1", jevshadowsettings.KeyCandidateVersion: `"trader-v4"`, jevshadowsettings.KeyDailyCostCapUSD: "0.5"} {
		if v, ok := a.stored(key); !ok || v != want {
			t.Errorf("%s = %q (%v), want %q", key, v, ok, want)
		}
	}
	if body := a.get("/settings/jev-prompt-panel").Body.String(); strings.Contains(body, "暫定の案で、まだ承認されていません") {
		t.Error("a stored cap is still shown as the unapproved proposal")
	}

	// Unchecked = off; a blank cap restores the proposal; no candidate clears it.
	a.post("/settings/jev-prompt/shadow", url.Values{"candidate": {""}, "cap": {""}}, true)
	if v, _ := a.stored(jevshadowsettings.KeyEnabled); v != "0" {
		t.Errorf("switch = %q, want 0", v)
	}
	if _, ok := a.stored(jevshadowsettings.KeyDailyCostCapUSD); ok {
		t.Error("a blank cap did not restore the proposed default")
	}
	if _, ok := a.stored(jevshadowsettings.KeyCandidateVersion); ok {
		t.Error("an empty candidate did not clear it")
	}
}

func TestInvalidShadowInputIsRejectedAndSavesNothing(t *testing.T) {
	a := newApp(t)
	for name, form := range map[string]url.Values{
		"unknown candidate": {"enabled": {"on"}, "candidate": {"trader-v999"}, "cap": {"1"}},
		"live as candidate": {"enabled": {"on"}, "candidate": {"trader-v3"}, "cap": {"1"}},
		"cap zero":          {"enabled": {"on"}, "candidate": {"trader-v4"}, "cap": {"0"}},
		"cap negative":      {"enabled": {"on"}, "candidate": {"trader-v4"}, "cap": {"-1"}},
		"cap huge":          {"enabled": {"on"}, "candidate": {"trader-v4"}, "cap": {"1000000"}},
		"cap text":          {"enabled": {"on"}, "candidate": {"trader-v4"}, "cap": {"abc"}},
		"cap NaN":           {"enabled": {"on"}, "candidate": {"trader-v4"}, "cap": {"NaN"}},
	} {
		rec := a.post("/settings/jev-prompt/shadow", form, true)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: status %d, want 422", name, rec.Code)
		}
		for _, key := range jevshadowsettings.Keys() {
			if _, ok := a.stored(key); ok {
				t.Errorf("%s: stored %s", name, key)
			}
		}
	}
}

func TestPromotionIsExplicitAuditedAndShownInThePanel(t *testing.T) {
	a := newApp(t)
	if rec := a.post("/settings/jev-prompt/promote", url.Values{"version": {"trader-v4"}}, false); rec.Code == http.StatusOK {
		t.Fatalf("promotion without CSRF = %d", rec.Code)
	}
	if n := a.rows(`SELECT COUNT(*) FROM jev_prompt_promotions`); n != 0 {
		t.Fatalf("promotion rows = %d before any promotion", n)
	}
	// Saving the shadow settings is not a promotion.
	a.post("/settings/jev-prompt/shadow", url.Values{"enabled": {"on"}, "candidate": {"trader-v4"}, "cap": {"1"}}, true)
	if _, ok := a.stored(jevshadowsettings.KeyLiveVersion); ok || a.rows(`SELECT COUNT(*) FROM jev_prompt_promotions`) != 0 {
		t.Fatal("enabling the shadow with a candidate changed the live version")
	}

	rec := a.post("/settings/jev-prompt/promote", url.Values{"version": {"trader-v4"}}, true)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "trader-v3 から trader-v4 に昇格しました") {
		t.Fatalf("promote = %d %s", rec.Code, rec.Body.String())
	}
	if v, _ := a.stored(jevshadowsettings.KeyLiveVersion); v != `"trader-v4"` {
		t.Fatalf("live version = %q", v)
	}
	var actor, client, from, to string
	if err := a.db.QueryRow(`SELECT actor, client, from_version, to_version FROM jev_prompt_promotions`).Scan(&actor, &client, &from, &to); err != nil {
		t.Fatal(err)
	}
	if actor != "gui" || client != "" || from != "trader-v3" || to != "trader-v4" {
		t.Fatalf("audit row = %s %q %s %s", actor, client, from, to)
	}
	body := a.get("/settings/jev-prompt-panel").Body.String()
	for _, want := range []string{`data-testid="jev-prompt-live-version">trader-v4<`, `data-testid="jev-prompt-promotion-row"`, "GUI", "trader-v3", "trader-v4"} {
		if !strings.Contains(body, want) {
			t.Errorf("panel after promotion lacks %q", want)
		}
	}
	// The live version is no longer offered as a candidate / target; trader-v3 is now.
	if strings.Contains(body, `<option value="trader-v4"`) {
		t.Error("the new live version is still offered")
	}

	for _, version := range []string{"trader-v4", "trader-v999", ""} {
		if rec := a.post("/settings/jev-prompt/promote", url.Values{"version": {version}}, true); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("promote %q = %d, want 422", version, rec.Code)
		}
	}
	if n := a.rows(`SELECT COUNT(*) FROM jev_prompt_promotions`); n != 1 {
		t.Errorf("promotion rows = %d, want 1", n)
	}
}

func TestPanelShowsTheCostAndWhyTheShadowStopped(t *testing.T) {
	a := newApp(t)
	inst, err := market.NewInstrumentRepository(a.db).Create(context.Background(), domain.Instrument{Symbol: "7203", Name: "7203 Inc", Market: "TSE Prime", IsActive: true})
	if err != nil {
		t.Fatal(err)
	}
	decisions := judgement.NewDecisionRepository(a.db)
	for _, row := range []struct {
		at   string
		cost float64
	}{{"2026-10-21T14:00:00Z", 0.02}, {"2026-10-22T00:30:00Z", 0.03}, {"2026-10-22T01:30:00Z", 0.01}} {
		live, err := decisions.Insert(context.Background(), domain.JevDecision{
			InstrumentID: inst.ID, Symbol: "7203", Timestamp: now, DecisionType: domain.JevDecisionTypeTrader,
			StateHash: "h", StateJSON: "{}", QuestionVersion: "trader-v3", ResponseJSON: "{}", ModelID: "m",
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := a.db.Exec(`INSERT INTO jev_shadow_decisions (jev_decision_id, prompt_version, model_id, response_json, direction, regime, entry_quality,
			confidence, toxic_flow, liquidity_stressed, continuation_probability, latency_ms, input_tokens, request_cost, created_at)
			VALUES (?, 'trader-v4', 'm', '{}', 'LONG', 'TREND', 'good', 0.5, 0, 0, 0, 1, 1000, ?, ?)`, live.ID, row.cost, row.at); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.settings.Set(context.Background(), jevshadowsettings.KeyHalt, `{"date":"2026-10-22","reason":"daily_cost_cap","spent_usd":0.04,"cap_usd":0.03,"at":"2026-10-22T01:00:00Z"}`, now); err != nil {
		t.Fatal(err)
	}
	body := a.get("/settings/jev-prompt-panel").Body.String()
	for _, want := range []string{
		`data-testid="jev-prompt-cost-today">0.04 USD`, `data-testid="jev-prompt-cost-total">0.06 USD`,
		"1 日の費用上限（0.03 USD）に達したため、今日のシャドーは止まっています", "ライブの判断は続いています",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("panel lacks %q\n%s", want, body)
		}
	}
	if n := strings.Count(body, `data-testid="jev-prompt-daily-row"`); n != 2 {
		t.Errorf("daily rows = %d, want 2 (2026-10-21 23:00 JST and 2026-10-22)", n)
	}
}
