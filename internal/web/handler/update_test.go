package handler_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/service/updater"
	"github.com/ousiassllc/pitha-trador/internal/web/handler"
)

// fakeUpdateController is a handler.UpdateController whose CheckForUpdate
// replaces status with afterCheck (the way the real Checker records its
// outcome) and counts calls.
type fakeUpdateController struct {
	status     updater.Status
	afterCheck updater.Status
	checkErr   error
	checks     int
}

func (f *fakeUpdateController) Status() updater.Status { return f.status }

func (f *fakeUpdateController) CheckForUpdate(context.Context) error {
	f.checks++
	f.status = f.afterCheck
	return f.checkErr
}

func newUpdateEngine(controller handler.UpdateController) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := handler.NewUpdateHandler(controller)
	engine := gin.New()
	engine.GET("/system/update-status", h.Status)
	engine.GET("/system/update-panel", h.Panel)
	engine.POST("/system/update-check", h.Check)
	return engine
}

func serve(engine *gin.Engine, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestUpdateHandler_Status_BannerStates(t *testing.T) {
	tests := []struct {
		name    string
		status  updater.Status
		want    []string
		wantNot string
	}{
		{name: "nothing available", status: updater.Status{CheckedAt: time.Now()}, wantNot: "update-banner"},
		{
			name:   "blocked by safety gate",
			status: updater.Status{Available: true, Version: "v0.2.0", Blocked: true},
			want:   []string{`data-testid="update-banner"`, "v0.2.0", "安全条件が揃い次第"},
		},
		{
			name:   "installer ready",
			status: updater.Status{Available: true, Version: "v0.2.0", Ready: true},
			want:   []string{`data-testid="update-banner"`, "v0.2.0", "まもなく自動的に再起動"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serve(newUpdateEngine(&fakeUpdateController{status: tt.status}), http.MethodGet, "/system/update-status")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			body := rec.Body.String()
			for _, w := range tt.want {
				if !strings.Contains(body, w) {
					t.Errorf("body missing %q; body=%s", w, body)
				}
			}
			if tt.wantNot != "" && strings.Contains(body, tt.wantNot) {
				t.Errorf("body contains %q, want none; body=%s", tt.wantNot, body)
			}
		})
	}
}

func TestUpdateHandler_NilController_BannerEmptyPanelExplainsAndCheckRejected(t *testing.T) {
	engine := newUpdateEngine(nil)

	if rec := serve(engine, http.MethodGet, "/system/update-status"); rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "" {
		t.Errorf("GET /system/update-status = %d %q, want 200 with empty body", rec.Code, rec.Body.String())
	}
	// cmd/server: the panel must say why there is nothing to check (issue
	// #241) instead of staying blank, and offer no check button.
	rec := serve(engine, http.MethodGet, "/system/update-panel")
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "アップデート機能がない") {
		t.Errorf("GET /system/update-panel = %d %q, want 200 explaining the build has no updater", rec.Code, body)
	}
	if strings.Contains(body, "update-check-button") {
		t.Errorf("panel offers a check button without an updater; body=%s", body)
	}
	if rec := serve(engine, http.MethodPost, "/system/update-check"); rec.Code != http.StatusNotFound {
		t.Errorf("POST /system/update-check status = %d, want 404", rec.Code)
	}
}

func TestUpdateHandler_Check_RunsCheckAndRendersFreshPanel(t *testing.T) {
	controller := &fakeUpdateController{
		afterCheck: updater.Status{CheckedAt: time.Now(), Available: true, Version: "v0.3.0", Blocked: true},
	}
	rec := serve(newUpdateEngine(controller), http.MethodPost, "/system/update-check")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	if controller.checks != 1 {
		t.Fatalf("CheckForUpdate calls = %d, want 1", controller.checks)
	}
	if got := rec.Header().Get("HX-Trigger"); got != "updateStatusChanged" {
		t.Errorf("HX-Trigger = %q, want updateStatusChanged (refreshes Header's banner)", got)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "新バージョン v0.3.0 が利用可能です") {
		t.Errorf("panel does not report the newer release; body=%s", body)
	}
}

func TestUpdateHandler_Check_UpToDate(t *testing.T) {
	controller := &fakeUpdateController{afterCheck: updater.Status{CheckedAt: time.Now()}}
	rec := serve(newUpdateEngine(controller), http.MethodPost, "/system/update-check")

	if !strings.Contains(rec.Body.String(), "最新バージョンです") {
		t.Fatalf("panel does not report up to date; body=%s", rec.Body.String())
	}
}

func TestUpdateHandler_Check_FailureIsShownInPanelNotAsHTTPError(t *testing.T) {
	controller := &fakeUpdateController{
		afterCheck: updater.Status{CheckedAt: time.Now(), LastError: "boom"},
		checkErr:   errors.New("boom"),
	}
	rec := serve(newUpdateEngine(controller), http.MethodPost, "/system/update-check")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 so HTMX swaps the fragment", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "アップデートの確認に失敗しました") {
		t.Errorf("panel does not report the failure; body=%s", body)
	}
	if strings.Contains(body, "boom") {
		t.Errorf("panel leaks the raw error message; body=%s", body)
	}
}

func TestUpdateHandler_Panel_DevBuildAndNeverChecked(t *testing.T) {
	dev := serve(newUpdateEngine(&fakeUpdateController{status: updater.Status{CheckedAt: time.Now(), DevBuild: true}}), http.MethodGet, "/system/update-panel")
	if !strings.Contains(dev.Body.String(), "開発ビルドのため") {
		t.Errorf("dev build panel missing notice; body=%s", dev.Body.String())
	}
	fresh := serve(newUpdateEngine(&fakeUpdateController{}), http.MethodGet, "/system/update-panel")
	if !strings.Contains(fresh.Body.String(), "まだ確認していません") {
		t.Errorf("never-checked panel missing notice; body=%s", fresh.Body.String())
	}
}

// A newer release held by the safety gate must show that it is held and
// which condition holds it (issue #241).
func TestUpdateHandler_Panel_BlockedNamesTheGateCondition(t *testing.T) {
	cases := map[updater.BlockKind]string{
		updater.BlockOpenPositions: "ポジションを保有しているため",
		updater.BlockKillSwitch:    "Kill Switch が発動しているため",
		updater.BlockRecentOrder:   "直近に発注があったため",
	}
	for kind, want := range cases {
		t.Run(string(kind), func(t *testing.T) {
			status := updater.Status{CheckedAt: time.Now(), Available: true, Version: "v0.3.0", Blocked: true, BlockedKind: kind}
			rec := serve(newUpdateEngine(&fakeUpdateController{status: status}), http.MethodGet, "/system/update-panel")
			body := rec.Body.String()
			if !strings.Contains(body, "更新を保留しています") || !strings.Contains(body, want) {
				t.Errorf("panel does not report the hold and %q; body=%s", want, body)
			}
		})
	}
	unblocked := serve(newUpdateEngine(&fakeUpdateController{status: updater.Status{CheckedAt: time.Now(), Available: true, Version: "v0.3.0"}}), http.MethodGet, "/system/update-panel")
	if strings.Contains(unblocked.Body.String(), "保留") {
		t.Errorf("panel reports a hold for an unblocked release; body=%s", unblocked.Body.String())
	}
}

// A failed check must name its class (network, rate limit, verification,
// ...) in the panel, and never the raw error text (issue #241).
func TestUpdateHandler_Panel_FailureNamesTheErrorKind(t *testing.T) {
	cases := map[updater.ErrorKind]string{
		updater.ErrorNetwork:      "ネットワークに接続できませんでした",
		updater.ErrorRateLimit:    "レート制限",
		updater.ErrorVerification: "検証に失敗しました",
		updater.ErrorRelease:      "リリース情報",
		updater.ErrorOther:        "原因を特定できませんでした",
	}
	for kind, want := range cases {
		t.Run(string(kind), func(t *testing.T) {
			status := updater.Status{CheckedAt: time.Now(), LastError: "https://secret.example/x: boom", ErrorKind: kind}
			body := serve(newUpdateEngine(&fakeUpdateController{status: status}), http.MethodGet, "/system/update-panel").Body.String()
			if !strings.Contains(body, want) {
				t.Errorf("panel lacks %q; body=%s", want, body)
			}
			if strings.Contains(body, "secret.example") {
				t.Errorf("panel leaks the raw error message; body=%s", body)
			}
		})
	}
}

// The check button is disabled while its request runs, drops repeated
// presses, and a progress element shows meanwhile (issue #241).
func TestUpdateHandler_Panel_CheckButtonShowsProgressAndBlocksDoublePress(t *testing.T) {
	body := serve(newUpdateEngine(&fakeUpdateController{}), http.MethodGet, "/system/update-panel").Body.String()
	for _, want := range []string{
		`hx-disabled-elt="this"`,
		`hx-sync="this:drop"`,
		`hx-indicator="#update-check-progress"`,
		`id="update-check-progress"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("panel lacks %q; body=%s", want, body)
		}
	}
}
