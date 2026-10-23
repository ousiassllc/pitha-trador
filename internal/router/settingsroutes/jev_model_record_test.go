package settingsroutes_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/settings"
)

// issue #885: saving JEV_MODEL through the Settings screen's save and delete
// routes writes the switch record (time, before, after, actor); saving another
// secret writes none and its value never reaches the record.
func TestSettingsSave_RecordsJevModelSwitches(t *testing.T) {
	gin.SetMode(gin.TestMode)
	conn, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	h := settings.NewSettingsHandler(system.NewSecretsRepository(conn))
	engine := gin.New()
	engine.POST("/settings/:key", h.Save)
	engine.DELETE("/settings/:key", h.Delete)

	send := func(method, key string, form url.Values) {
		t.Helper()
		req := httptest.NewRequest(method, "/settings/"+key, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s: status = %d, want 200", method, key, rec.Code)
		}
	}
	send(http.MethodPost, config.KeyJevAPIKey, url.Values{"value": {"sk-secret-api-key"}})
	send(http.MethodPost, config.KeyJevModel, url.Values{"value": {" jev-1.12.0 "}})
	send(http.MethodPost, config.KeyJevModel, url.Values{"value": {"jev-1.13.0"}})
	send(http.MethodDelete, config.KeyJevModel, nil)

	rows, err := conn.Query(`SELECT actor, COALESCE(before_value, '<NULL>'), COALESCE(after_value, '<NULL>'), at FROM jev_model_changes ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var got []string
	for rows.Next() {
		var actor, before, after, at string
		if err := rows.Scan(&actor, &before, &after, &at); err != nil {
			t.Fatal(err)
		}
		if at == "" || strings.Contains(before+after, "sk-secret-api-key") {
			t.Errorf("bad row: at=%q before=%q after=%q", at, before, after)
		}
		got = append(got, actor+":"+before+"->"+after)
	}
	want := []string{"gui:<NULL>->jev-1.12.0", "gui:jev-1.12.0->jev-1.13.0", "gui:jev-1.13.0-><NULL>"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("records = %v, want %v", got, want)
	}
}
