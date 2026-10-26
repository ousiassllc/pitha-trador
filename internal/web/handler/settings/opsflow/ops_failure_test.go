package opsflow_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/settings"
)

// failingRuntimeStore is a store whose writes fail, the way a rolled-back
// transaction does when the audit row of a whitelisted key cannot be written
// (mcpset.Audited, issue #813): the handler sees only an error from Save/Reset.
type failingRuntimeStore struct{ memRuntimeStore }

func (*failingRuntimeStore) Set(context.Context, string, string, time.Time) error {
	return errors.New("injected failure")
}

func (*failingRuntimeStore) Delete(context.Context, string) error {
	return errors.New("injected failure")
}

// A save or reset the service rejects (for a whitelisted key: its audit row
// could not be written, so the change was rolled back) answers 500 with the
// "not saved" toast the page routes into #toast-region, never a refreshed row
// that would look saved, and never the error text (issue #818).
func TestSaveAndResetOps_StoreFailureIs500WithAToast(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &failingRuntimeStore{memRuntimeStore{rows: map[string]string{}}}
	h := settings.NewSettingsHandler(newFakeSecretsStore()).WithOperationalSettings(opsettings.New(store, opsStrategy(), "/data/logs"))
	engine := settingsRouter(h)
	engine.POST("/ops-settings/:key", h.SaveOps)
	engine.DELETE("/ops-settings/:key", h.ResetOps)

	for name, tc := range map[string]struct {
		rec  *httptest.ResponseRecorder
		want string
	}{
		"save":  {opsRequest(engine, http.MethodPost, "screener.top_n", url.Values{"value": {"30"}}, true), "保存に失敗しました"},
		"reset": {opsRequest(engine, http.MethodDelete, "screener.top_n", nil, true), "既定に戻すのに失敗しました"},
	} {
		body := tc.rec.Body.String()
		if tc.rec.Code != http.StatusInternalServerError {
			t.Errorf("%s: status = %d, want 500", name, tc.rec.Code)
		}
		if !strings.Contains(body, `data-testid="toast"`) || !strings.Contains(body, tc.want) {
			t.Errorf("%s: body lacks the error toast %q: %s", name, tc.want, body)
		}
		if strings.Contains(body, "injected failure") || strings.Contains(body, "setting-field-row-") {
			t.Errorf("%s: body leaks the error or renders a row: %s", name, body)
		}
	}
	if len(store.rows) != 0 {
		t.Errorf("store = %v, want untouched", store.rows)
	}
}
