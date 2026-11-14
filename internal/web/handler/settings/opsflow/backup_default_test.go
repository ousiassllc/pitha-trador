package opsflow_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/settings"
)

// backupRow is the rendered 保存先 row of the page body.
func backupRow(t *testing.T, body string) string {
	t.Helper()
	marker := `data-testid="setting-field-row-` + config.KeyBackupDir + `"`
	i := strings.Index(body, marker)
	if i < 0 {
		t.Fatal("page has no backup directory row")
	}
	rest := body[i:]
	return rest[:strings.Index(rest, "</form>")]
}

// Issue #890: with no destination stored the Settings screen shows the default
// same-PC folder as the default and the same-disk caveat; saving an external
// path replaces both.
func TestSettingsPage_BackupDirShowsDefaultFolderAndSameDiskCaveat(t *testing.T) {
	_, store := opsRouter()
	h := settings.NewSettingsHandler(newFakeSecretsStore()).WithOperationalSettings(
		opsettings.New(store, opsStrategy(), "/data/logs").WithDefaultBackupDir("/data/backups"))
	engine := settingsRouter(h)
	engine.POST("/ops-settings/:key", h.SaveOps)

	row := backupRow(t, allPagesBody(t, engine))
	if !strings.Contains(row, "/data/backups") || strings.Contains(row, "無効") {
		t.Errorf("unset row must show the default folder and not call the backup disabled: %s", row)
	}
	if !strings.Contains(row, `data-testid="warning-`+config.KeyBackupDir+`"`) || !strings.Contains(row, "ディスクが故障すると") {
		t.Errorf("unset row must carry the same-disk caveat: %s", row)
	}

	dir := t.TempDir()
	rec := opsRequest(engine, http.MethodPost, config.KeyBackupDir, url.Values{"value": {dir}}, true)
	if rec.Code != http.StatusOK || store.rows[config.KeyBackupDir] != `"`+dir+`"` {
		t.Fatalf("status = %d, store = %v", rec.Code, store.rows)
	}
	if strings.Contains(rec.Body.String(), "ディスクが故障すると") {
		t.Errorf("an available external path must not show the same-disk caveat: %s", rec.Body.String())
	}
}
