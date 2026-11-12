// Package jevtrialroute holds the router-level tests of the Settings screen's
// model-comparison trial section: the card, the panel with the switch, the arms
// and the cap, that the form is the only way to turn the trial on, that every
// state change needs the session and CSRF token, and that a rejected form saves
// nothing.
package jevtrialroute_test

import (
	"context"
	"database/sql"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/service/jevtrial"
	"github.com/ousiassllc/pitha-trador/internal/web/middleware"
)

var csrfMeta = regexp.MustCompile(`<meta name="csrf-token" content="([^"]+)"`)

type fullSecrets struct{}

func (fullSecrets) Get(context.Context, string) (string, bool, error) { return "x", true, nil }
func (fullSecrets) Set(context.Context, string, string) error         { return nil }
func (fullSecrets) Delete(context.Context, string) error              { return nil }

const validArms = `[{"id":"or-clef-flash","provider":"openrouter","model":"cloudflare/clef-flash","prompt_version":"trader-v3"}]`

type app struct {
	t        *testing.T
	engine   *gin.Engine
	db       *sql.DB
	settings *system.RuntimeSettingsRepository
	cookie   *http.Cookie
	csrf     string
}

func newApp(t *testing.T) *app {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := jevtrial.New(db)
	svc.SetClock(func() time.Time { return time.Date(2026, 11, 13, 3, 0, 0, 0, time.UTC) })
	a := &app{t: t, db: db, settings: system.NewRuntimeSettingsRepository(db)}
	a.engine = router.New(router.WithSecretsStore(fullSecrets{}), router.WithJevTrialSettings(svc))
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

// Without the service the card is hidden and the routes are absent.
func TestNeedsTheService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New(router.WithSecretsStore(fullSecrets{}))
	for _, path := range []string{"/settings/jev-trial-panel"} {
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
	for _, want := range []string{`id="modal-jev-trial"`, `data-testid="settings-card-jev-trial"`, `hx-get="/settings/jev-trial-panel"`} {
		if !strings.Contains(page, want) {
			t.Errorf("the integrations page lacks %q", want)
		}
	}
	body := a.get("/settings/jev-trial-panel").Body.String()
	for _, want := range []string{
		`data-testid="jev-trial-enabled"`, `data-testid="jev-trial-arms"`, `data-testid="jev-trial-paper-only"`,
		`data-testid="jev-trial-cap-effective">0.1 USD</span>（既定値）`, `placeholder="0.1（既定値）"`,
		`data-testid="jev-trial-cost-today">0 USD`, `data-testid="jev-trial-arms-empty"`, "2026-11-13",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("panel lacks %q", want)
		}
	}
	if regexp.MustCompile(`checked[^>]*data-testid="jev-trial-enabled"|data-testid="jev-trial-enabled"[^>]*checked`).MatchString(body) {
		t.Error("the trial switch is checked by default")
	}
	if strings.Contains(body, "OpenRouter の API キー:") {
		t.Error("the key status is shown although no key store is wired")
	}
}

func TestSavingTheForm(t *testing.T) {
	a := newApp(t)
	form := url.Values{"enabled": {"on"}, "arms": {validArms}, "cap": {"0.5"}}
	// No CSRF token: refused, nothing stored.
	if rec := a.post("/settings/jev-trial", form, false); rec.Code == http.StatusOK {
		t.Fatalf("POST without CSRF = %d", rec.Code)
	}
	if _, ok := a.stored(jevtrialsettings.KeyEnabled); ok {
		t.Fatal("a POST without CSRF stored the switch")
	}

	rec := a.post("/settings/jev-trial", form, true)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "保存しました。") {
		t.Fatalf("save = %d %s", rec.Code, rec.Body.String())
	}
	if v, _ := a.stored(jevtrialsettings.KeyEnabled); v != "1" {
		t.Errorf("enabled = %q", v)
	}
	if v, _ := a.stored(jevtrialsettings.KeyDailyCostCapUSD); v != "0.5" {
		t.Errorf("cap = %q", v)
	}
	body := a.get("/settings/jev-trial-panel").Body.String()
	for _, want := range []string{`data-testid="jev-trial-cap-effective">0.5 USD</span>（保存した値）`, `data-testid="jev-trial-arm-row"`, "or-clef-flash", "cloudflare/clef-flash", "動作中"} {
		if !strings.Contains(body, want) {
			t.Errorf("panel after save lacks %q\n%s", want, body)
		}
	}

	// Unchecked = off (always allowed); a blank cap restores the default.
	if rec := a.post("/settings/jev-trial", url.Values{"arms": {validArms}, "cap": {""}}, true); rec.Code != http.StatusOK {
		t.Fatalf("disable = %d", rec.Code)
	}
	if v, _ := a.stored(jevtrialsettings.KeyEnabled); v != "0" {
		t.Errorf("enabled = %q, want 0", v)
	}
	if _, ok := a.stored(jevtrialsettings.KeyDailyCostCapUSD); ok {
		t.Error("a blank cap did not restore the default")
	}
}

func TestInvalidInputIsRejectedAndSavesNothing(t *testing.T) {
	a := newApp(t)
	for name, form := range map[string]url.Values{
		"arms not JSON":       {"enabled": {"on"}, "arms": {"nope"}, "cap": {"1"}},
		"unregistered prompt": {"enabled": {"on"}, "arms": {`[{"id":"a","provider":"openrouter","model":"m","prompt_version":"trader-v999"}]`}},
		"unknown field":       {"enabled": {"on"}, "arms": {`[{"id":"a","provider":"openrouter","model":"m","prompt_version":"trader-v3","x":1}]`}},
		"cap zero":            {"enabled": {"on"}, "arms": {validArms}, "cap": {"0"}},
		"cap negative":        {"enabled": {"on"}, "arms": {validArms}, "cap": {"-1"}},
		"cap huge":            {"enabled": {"on"}, "arms": {validArms}, "cap": {"1000000"}},
		"cap text":            {"enabled": {"on"}, "arms": {validArms}, "cap": {"abc"}},
		"cap NaN":             {"enabled": {"on"}, "arms": {validArms}, "cap": {"NaN"}},
	} {
		rec := a.post("/settings/jev-trial", form, true)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: status %d, want 422", name, rec.Code)
		}
		if got := rec.Header().Get("HX-Retarget"); got != "#jev-trial-error" {
			t.Errorf("%s: HX-Retarget = %q, want the form's error slot", name, got)
		}
		if body := rec.Body.String(); !strings.Contains(body, "data-inline-alert") || strings.Contains(body, "data-toast") {
			t.Errorf("%s: body is not an inline alert: %s", name, body)
		}
		for _, key := range jevtrialsettings.Keys() {
			if _, ok := a.stored(key); ok {
				t.Errorf("%s: stored %s", name, key)
			}
		}
	}
}

// The panel never shows an Issue / PR number or a development note.
func TestPanelShowsNoIssueReferences(t *testing.T) {
	a := newApp(t)
	a.post("/settings/jev-trial", url.Values{"enabled": {"on"}, "arms": {validArms}}, true)
	devNote := regexp.MustCompile(`(?i)\b(?:issue|pr)\s*#?\d+|\bFR-[A-Z]+-\d+|#\d+`)
	tags := regexp.MustCompile(`(?s)<[^>]*>`)
	for _, path := range []string{"/settings/jev-trial-panel", "/settings/integrations"} {
		text := html.UnescapeString(tags.ReplaceAllString(a.get(path).Body.String(), " "))
		if m := devNote.FindString(text); m != "" {
			t.Errorf("GET %s shows a development reference %q", path, m)
		}
	}
}
