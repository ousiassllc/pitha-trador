package jevtrial_test

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

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/jevtrial"
	handler "github.com/ousiassllc/pitha-trador/internal/web/handler/jevtrial"
)

type fakeService struct {
	state    jevtrial.State
	stateErr error
	saveErr  error
	saved    []jevtrial.Form
}

func (f *fakeService) State(context.Context) (jevtrial.State, error) { return f.state, f.stateErr }
func (f *fakeService) Save(_ context.Context, form jevtrial.Form) error {
	f.saved = append(f.saved, form)
	return f.saveErr
}

func engine(svc handler.Service) *gin.Engine {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	handler.New(svc).Register(e)
	return e
}

func do(e *gin.Engine, method, path string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestPanel_ShowsHaltReasonsAndPerArmNumbers(t *testing.T) {
	until := time.Date(2026, 11, 13, 4, 0, 0, 0, time.UTC)
	arm := func(id string) jevtrialsettings.Arm {
		return jevtrialsettings.Arm{ID: id, Provider: "openrouter", Model: "m-" + id, PromptVersion: "trader-v3"}
	}
	f := &fakeService{state: jevtrial.State{
		TrialRuns: true, Enabled: true, CapUSD: 0.1, CapIsDefault: true, ArmsJSON: "[]", TodayDate: "2026-11-13",
		Today: jevtrial.Spend{Calls: 5, CountedCostUSD: 0.0546000000004}, Total: jevtrial.Spend{Calls: 9, CountedCostUSD: 0.2},
		Halt: &jevtrialsettings.Halt{Date: "2026-11-13", Reason: jevtrialsettings.HaltReasonPaymentRequired, Status: 402},
		Arms: []jevtrial.ArmStatus{
			{Arm: arm("a-cap"), CapUSD: 0.02, Today: jevtrial.Spend{Calls: 2, CountedCostUSD: 0.02}, FailureCap: 20, Halt: &jevtrialsettings.Halt{Reason: jevtrialsettings.HaltReasonArmCostCap, CapUSD: 0.02}},
			{Arm: arm("a-fail"), CapUSD: 0.05, FailuresToday: 20, FailureCap: 20, Halt: &jevtrialsettings.Halt{Reason: jevtrialsettings.HaltReasonDailyFailureCap, FailureCap: 20}},
			{Arm: arm("a-403"), CapUSD: 0.05, FailureCap: 20, Halt: &jevtrialsettings.Halt{Reason: jevtrialsettings.HaltReasonForbidden, Status: 403}},
			{Arm: arm("a-429"), CapUSD: 0.05, FailureCap: 20, ThrottleActive: true, Throttle: &jevtrialsettings.Throttle{Status: 429, Until: until.Format(time.RFC3339)}},
			{Arm: arm("a-ok"), CapUSD: 0.05, FailureCap: 20},
		},
	}}
	rec := do(engine(f), http.MethodGet, "/settings/jev-trial-panel", nil)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("GET = %d, Cache-Control %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	for _, want := range []string{
		`data-testid="jev-trial-cost-today">0.0546 USD`, "（5 回）", "402（支払い不足）", "このアームの上限（0.02 USD）", "失敗が 1 日の上限（20 回）",
		"403（権限なし）", "429（レート制限・過負荷）のため", "動作中", "0.02 / 0.02", "20 / 20",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("panel lacks %q", want)
		}
	}
	if strings.Contains(body, "OpenRouter の API キー:") {
		t.Error("key status shown without being known")
	}
}

func TestPanel_KeyStatusIsYesOrNoOnly(t *testing.T) {
	for _, set := range []bool{true, false} {
		f := &fakeService{state: jevtrial.State{TrialRuns: true, CapUSD: 0.1, KeyKnown: true, KeySet: set}}
		body := do(engine(f), http.MethodGet, "/settings/jev-trial-panel", nil).Body.String()
		if !strings.Contains(body, "OpenRouter の API キー:") || strings.Contains(body, "設定済み") != set {
			t.Errorf("set=%v: %s", set, body)
		}
	}
}

func TestPanel_OutsidePaperSaysTheTrialDoesNotRun(t *testing.T) {
	f := &fakeService{state: jevtrial.State{TrialRuns: false, CapUSD: 0.1}}
	if body := do(engine(f), http.MethodGet, "/settings/jev-trial-panel", nil).Body.String(); !strings.Contains(body, `data-testid="jev-trial-not-paper"`) {
		t.Error("no note that the trial does not run outside Paper")
	}
}

func TestSave_PassesTheFormAndMapsErrors(t *testing.T) {
	f := &fakeService{state: jevtrial.State{CapUSD: 0.1}}
	e := engine(f)
	if rec := do(e, http.MethodPost, "/settings/jev-trial", url.Values{"enabled": {"on"}, "arms": {"[]"}, "cap": {" 0.25 "}}); rec.Code != http.StatusOK {
		t.Fatalf("save = %d", rec.Code)
	}
	got := f.saved[0]
	if !got.Enabled || got.ArmsJSON != "[]" || got.Cap == nil || *got.Cap != 0.25 {
		t.Errorf("form = %+v", got)
	}
	do(e, http.MethodPost, "/settings/jev-trial", url.Values{"arms": {"[]"}})
	if got := f.saved[1]; got.Enabled || got.Cap != nil {
		t.Errorf("blank form = %+v, want off and no cap", got)
	}

	f.saveErr = errors.Join(jevtrial.ErrInvalidInput)
	if rec := do(e, http.MethodPost, "/settings/jev-trial", url.Values{"arms": {"x"}}); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("invalid = %d, want 422", rec.Code)
	}
	f.saveErr = errors.New("db down: secret detail")
	rec := do(e, http.MethodPost, "/settings/jev-trial", url.Values{"arms": {"[]"}})
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "secret detail") {
		t.Errorf("storage error = %d %s, want 500 without the cause", rec.Code, rec.Body.String())
	}
}
