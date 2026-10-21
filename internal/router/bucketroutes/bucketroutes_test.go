// Package bucketroutes tests the Settings screen's 資産ごとの予算枠 page (issue
// #824) through the HTTP handlers with the real opsettings.Service, the real
// audit trail and a migrated SQLite database: a save that would take the
// budgets' sum over the operating capital is refused with its reason on
// screen and leaves no audit row, a valid save stores the value and its audit
// row. It lives under internal/router because the web layer may not import
// the repository.
package bucketroutes_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/bucketsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/papersettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/settings"
	"github.com/ousiassllc/pitha-trador/internal/web/settingspages"
)

func TestMain(m *testing.M) {
	code := m.Run()
	testkit.RemoveTemplate()
	os.Exit(code)
}

// noSecrets is the SecretsStore the pages under test never read from.
type noSecrets struct{}

func (noSecrets) Get(context.Context, string) (string, bool, error) { return "", false, nil }
func (noSecrets) Set(context.Context, string, string) error         { return nil }
func (noSecrets) Delete(context.Context, string) error              { return nil }

type fixture struct {
	engine *gin.Engine
	db     *sql.DB
	store  *system.RuntimeSettingsRepository
}

// newFixture serves the Settings pages and the ops-settings routes over a
// migrated database holding capital 500,000 = stock 300,000 + bond_etf 200,000.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	_, db := testkit.NewMigratedDB(t)
	store := system.NewRuntimeSettingsRepository(db)
	ops := mcpset.NewAudited(opsettings.New(store, config.StrategyConfig{}, "").WithTradingMode(metalabelsettings.ModePaper), mcpset.NewGate(db))
	h := settings.NewSettingsHandler(noSecrets{}).WithOperationalSettings(ops)
	engine := gin.New()
	for _, page := range settingspages.Pages() {
		engine.GET(page.Path(), h.SubPage(page, false))
	}
	engine.POST("/ops-settings/:key", h.SaveOps)
	engine.DELETE("/ops-settings/:key", h.ResetOps)
	for key, value := range map[string]string{
		papersettings.KeyInitialCapitalJPY: "500000",
		bucketsettings.KeyStockBudgetJPY:   "300000",
		bucketsettings.KeyBondETFBudgetJPY: "200000",
	} {
		if err := store.Set(t.Context(), key, value, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	return &fixture{engine: engine, db: db, store: store}
}

func (f *fixture) do(method, path string, form url.Values) *httptest.ResponseRecorder {
	var req *http.Request
	if form != nil {
		req = httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	f.engine.ServeHTTP(rec, req)
	return rec
}

func (f *fixture) save(key, value string) *httptest.ResponseRecorder {
	return f.do(http.MethodPost, "/ops-settings/"+key, url.Values{"value": {value}})
}

func (f *fixture) reset(key string) *httptest.ResponseRecorder {
	return f.do(http.MethodDelete, "/ops-settings/"+key, nil)
}

func (f *fixture) auditRows(t *testing.T) []mcpset.Entry {
	t.Helper()
	rows, err := mcpset.ListAudit(t.Context(), f.db, mcpset.MaxAuditList, 0)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func (f *fixture) stored(t *testing.T, key string) string {
	t.Helper()
	v, _, err := f.store.Get(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// A save of a budget, a capital below the budgets' sum, or a Reset to a
// default that does not fit is refused through the HTTP handler with the
// reason in the response, and leaves the stored value and the audit log as
// they were (issue #824).
func TestSumOverCapitalRejectedLeavesNoAuditRow(t *testing.T) {
	cases := []struct {
		name string
		do   func(f *fixture) *httptest.ResponseRecorder
		key  string
		want string // the stored value after the refusal
	}{
		{"a budget that takes the sum over the capital", func(f *fixture) *httptest.ResponseRecorder {
			return f.save(bucketsettings.KeyBondETFBudgetJPY, "300000")
		}, bucketsettings.KeyBondETFBudgetJPY, "200000"},
		{"a capital below the budgets' sum", func(f *fixture) *httptest.ResponseRecorder {
			return f.save(papersettings.KeyInitialCapitalJPY, "400000")
		}, papersettings.KeyInitialCapitalJPY, "500000"},
		{"a reset whose default (stock = the whole capital) takes the sum over the capital", func(f *fixture) *httptest.ResponseRecorder {
			return f.reset(bucketsettings.KeyStockBudgetJPY)
		}, bucketsettings.KeyStockBudgetJPY, "300000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			rec := tc.do(f)
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "枠の合計") {
				t.Fatalf("status = %d, body = %s; want 400 naming the budgets' sum", rec.Code, rec.Body.String())
			}
			if got := f.stored(t, tc.key); got != tc.want {
				t.Errorf("%s = %q after the refusal, want %q", tc.key, got, tc.want)
			}
			if rows := f.auditRows(t); len(rows) != 0 {
				t.Errorf("audit rows = %+v after a refused change, want none", rows)
			}
		})
	}
}

// A save the budgets allow is stored with its audit row (actor gui, the
// before and after values), and the page then shows the new total and the
// remaining capital.
func TestValidSaveStoresValueAndAuditRow(t *testing.T) {
	f := newFixture(t)
	key := bucketsettings.KeyStockBudgetJPY

	rec := f.save(key, "250000")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := f.stored(t, key); got != "250000" {
		t.Fatalf("stored stock budget = %q, want 250000", got)
	}
	rows := f.auditRows(t)
	if len(rows) != 1 || rows[0].Actor != mcpset.ActorGUI || rows[0].Action != mcpset.ActionSet || rows[0].Key != key ||
		rows[0].Before == nil || *rows[0].Before != "300000" || rows[0].After == nil || *rows[0].After != "250000" {
		t.Fatalf("audit rows = %+v, want one gui set 300000 -> 250000", rows)
	}
	// The saved group's 計算結果 panel is refreshed out-of-band: total 450,000, remaining 50,000.
	for _, want := range []string{`id="setting-group-derived-asset-buckets"`, "450,000 円", "50,000 円"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("save response missing %q: %s", want, rec.Body.String())
		}
	}

	page := f.do(http.MethodGet, "/settings/asset-buckets", nil)
	body := page.Body.String()
	if page.Code != http.StatusOK {
		t.Fatalf("GET page = %d", page.Code)
	}
	for _, want := range []string{
		`data-testid="setting-field-row-` + bucketsettings.KeyStockBudgetJPY + `"`,
		`data-testid="setting-field-row-` + bucketsettings.KeyBondETFBudgetJPY + `"`,
		"資産ごとの予算枠", "枠の合計", "残額", "450,000 円", "50,000 円",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
}

// The page is the only editor of the two budgets (the paper-risk page keeps
// the capital) and, with nothing stored, shows the defaults: stock = the
// capital, bond_etf = 0.
func TestPageShowsDefaultsWhenNothingStored(t *testing.T) {
	f := newFixture(t)
	for _, key := range bucketsettings.Keys() {
		if err := f.store.Delete(t.Context(), key); err != nil {
			t.Fatal(err)
		}
	}
	risk := f.do(http.MethodGet, "/settings/risk", nil).Body.String()
	for _, key := range bucketsettings.Keys() {
		if strings.Contains(risk, `data-testid="setting-field-row-`+key+`"`) {
			t.Errorf("/settings/risk also edits %s", key)
		}
	}
	body := f.do(http.MethodGet, "/settings/asset-buckets", nil).Body.String()
	// capital 500,000 = total 500,000 (stock 500,000 + bond_etf 0), remaining 0.
	for _, want := range []string{"株 500,000 円 + 債券ETF 0 円", ">0 円<"} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q: %s", want, body)
		}
	}
}

// Raising the operating capital does not grow the stock budget (issue #834,
// FR-RISK-10), and both pages say so: the capital page (/settings/risk) and
// the budgets page (/settings/asset-buckets) each carry the notice in their
// group's note, so the operator knows to save the stock budget too.
func TestPagesWarnThatRaisingCapitalDoesNotRaiseStockBudget(t *testing.T) {
	f := newFixture(t)
	for _, tc := range []struct{ name, path, noteID string }{
		{"capital page", "/settings/risk", `data-testid="modal-paper-risk-note"`},
		{"budgets page", "/settings/asset-buckets", `data-testid="modal-asset-buckets-note"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := f.do(http.MethodGet, tc.path, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s = %d", tc.path, rec.Code)
			}
			body := rec.Body.String()
			if !strings.Contains(body, tc.noteID) {
				t.Fatalf("%s missing the group note %s", tc.path, tc.noteID)
			}
			for _, want := range []string{
				"運用資金を上げても株の枠は自動で増えません",
				"株の枠も保存してください",
				"bucket_budget_exceeded",
			} {
				if !strings.Contains(body, want) {
					t.Errorf("%s missing %q", tc.path, want)
				}
			}
		})
	}
}

// While part of the operating capital is in no budget (remaining > 0) the
// budgets page's 計算結果 panel says that part is unused; with nothing left
// over (or the budgets over the capital) it does not.
func TestBudgetsPageShowsUnusedCapitalNotice(t *testing.T) {
	f := newFixture(t)
	unused := `data-testid="setting-group-derived-asset-buckets-bucket-unused"`

	// capital 500,000 = stock 300,000 + bond_etf 200,000: nothing left over.
	if body := f.do(http.MethodGet, "/settings/asset-buckets", nil).Body.String(); strings.Contains(body, unused) {
		t.Fatalf("unused-capital notice shown with remaining 0: %s", body)
	}

	// Raise the capital by 100,000: the stock budget stays 300,000, so 100,000 is unused.
	if rec := f.save(papersettings.KeyInitialCapitalJPY, "600000"); rec.Code != http.StatusOK {
		t.Fatalf("save capital: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := f.stored(t, bucketsettings.KeyStockBudgetJPY); got != "300000" {
		t.Fatalf("stock budget = %q after raising the capital, want it unchanged (300000)", got)
	}
	body := f.do(http.MethodGet, "/settings/asset-buckets", nil).Body.String()
	for _, want := range []string{unused, "100,000 円 が、どの枠にも割り当てられておらず使われていません"} {
		if !strings.Contains(body, want) {
			t.Errorf("budgets page missing %q: %s", want, body)
		}
	}

	// Saving the stock budget with the surplus uses it up: the notice goes away.
	if rec := f.save(bucketsettings.KeyStockBudgetJPY, "400000"); rec.Code != http.StatusOK {
		t.Fatalf("save stock budget: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if body := f.do(http.MethodGet, "/settings/asset-buckets", nil).Body.String(); strings.Contains(body, unused) {
		t.Errorf("unused-capital notice still shown with remaining 0: %s", body)
	}
}
