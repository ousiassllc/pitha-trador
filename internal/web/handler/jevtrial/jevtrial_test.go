package jevtrial_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
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
		TrialRuns: true, Enabled: true, CapUSD: 0.1, CapIsDefault: true, TodayDate: "2026-11-13",
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
	if rec := do(e, http.MethodPost, "/settings/jev-trial", url.Values{"enabled": {"on"}, "cap": {" 0.25 "}}); rec.Code != http.StatusOK {
		t.Fatalf("save = %d", rec.Code)
	}
	got := f.saved[0]
	if !got.Enabled || len(got.Arms) != 0 || got.Cap == nil || *got.Cap != 0.25 {
		t.Errorf("form = %+v", got)
	}
	do(e, http.MethodPost, "/settings/jev-trial", nil)
	if got := f.saved[1]; got.Enabled || got.Cap != nil {
		t.Errorf("blank form = %+v, want off and no cap", got)
	}

	f.saveErr = errors.Join(jevtrial.ErrInvalidInput)
	if rec := do(e, http.MethodPost, "/settings/jev-trial", url.Values{"arm-0-id": {"x"}}); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("invalid = %d, want 422", rec.Code)
	}
	f.saveErr = errors.New("db down: secret detail")
	rec := do(e, http.MethodPost, "/settings/jev-trial", nil)
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "secret detail") {
		t.Errorf("storage error = %d %s, want 500 without the cause", rec.Code, rec.Body.String())
	}
}

func armForm(n int, id string) url.Values {
	p := "arm-" + strconv.Itoa(n) + "-"
	return url.Values{
		p + "id": {id}, p + "provider": {"openrouter"}, p + "model": {"m-" + id}, p + "prompt_version": {"trader-v3"},
		p + "daily_cost_cap_usd": {""}, p + "list_price_usd_per_mtok": {""}, p + "provider_only": {""}, p + "data_collection": {"deny"},
	}
}

func merge(forms ...url.Values) url.Values {
	out := url.Values{}
	for _, f := range forms {
		for k, v := range f {
			out[k] = v
		}
	}
	return out
}

// The rows become arms in the order of their numbers, with every field read.
func TestSave_CollectsTheRowsIntoArms(t *testing.T) {
	f := &fakeService{state: jevtrial.State{CapUSD: 0.1}}
	full := armForm(10, "last")
	full.Set("arm-10-daily_cost_cap_usd", " 0.02 ")
	full.Set("arm-10-list_price_usd_per_mtok", "0.24")
	full.Set("arm-10-provider_only", "chutes, deepinfra/fp8")
	full.Set("arm-10-data_collection", "allow")
	full.Set("arm-10-zdr", "1")
	full.Set("arm-10-endpoint", "/alpha/decisions")
	form := merge(armForm(2, "second"), armForm(0, "first"), full)
	form.Set("enabled", "on")
	if rec := do(engine(f), http.MethodPost, "/settings/jev-trial", form); rec.Code != http.StatusOK {
		t.Fatalf("save = %d %s", rec.Code, rec.Body.String())
	}
	got := f.saved[0].Arms
	if len(got) != 3 || got[0].ID != "first" || got[1].ID != "second" || got[2].ID != "last" {
		t.Fatalf("arms = %+v, want first, second, last", got)
	}
	want := jevtrialsettings.Arm{
		ID: "last", Provider: "openrouter", Model: "m-last", PromptVersion: "trader-v3",
		DailyCostCapUSD: 0.02, ListPriceUSDPerMtok: 0.24, ProviderOnly: []string{"chutes", "deepinfra/fp8"}, DataCollection: "allow", ZDR: true, Endpoint: "/alpha/decisions",
	}
	if !reflect.DeepEqual(got[2], want) {
		t.Errorf("arm = %+v, want %+v", got[2], want)
	}
	if got[0].ZDR || got[0].DailyCostCapUSD != 0 || got[0].ProviderOnly != nil || got[0].Endpoint != "" {
		t.Errorf("a blank row = %+v, want unset optional fields", got[0])
	}
}

// The form always sends data_collection (its select has a default), but the
// OpenRouter routing preferences cannot be stored for another provider: the
// default is dropped, a real choice is kept so validation refuses it.
func TestSave_RoutingDefaultIsNotSentToOtherProviders(t *testing.T) {
	f := &fakeService{state: jevtrial.State{CapUSD: 0.1}}
	custom := armForm(0, "mine")
	custom.Set("arm-0-provider", "custom")
	allow := armForm(1, "loose")
	allow.Set("arm-1-provider", "custom")
	allow.Set("arm-1-data_collection", "allow")
	if rec := do(engine(f), http.MethodPost, "/settings/jev-trial", merge(custom, allow)); rec.Code != http.StatusOK {
		t.Fatalf("save = %d %s", rec.Code, rec.Body.String())
	}
	got := f.saved[0].Arms
	if got[0].Provider != "custom" || got[0].DataCollection != "" {
		t.Errorf("custom arm = %+v, want no data_collection", got[0])
	}
	if got[1].DataCollection != "allow" {
		t.Errorf("an explicit allow = %q, want it kept", got[1].DataCollection)
	}
}

// A row that cannot be read is a 422 into the form's error slot and the service is not called.
func TestSave_UnreadableRowsSaveNothing(t *testing.T) {
	for name, form := range map[string]url.Values{
		"cap is text":        merge(armForm(0, "a"), url.Values{"arm-0-daily_cost_cap_usd": {"abc"}}),
		"price is text":      merge(armForm(0, "a"), url.Values{"arm-0-list_price_usd_per_mtok": {"x"}}),
		"unknown field":      merge(armForm(0, "a"), url.Values{"arm-0-colour": {"x"}}),
		"row without number": {"arm-x-id": {"a"}},
		"negative number":    {"arm--1-id": {"a"}},
		"no field":           {"arm-0": {"a"}},
	} {
		f := &fakeService{state: jevtrial.State{CapUSD: 0.1}}
		rec := do(engine(f), http.MethodPost, "/settings/jev-trial", form)
		if rec.Code != http.StatusUnprocessableEntity || rec.Header().Get("HX-Retarget") != "#jev-trial-error" {
			t.Errorf("%s: %d retarget %q, want 422 into the error slot", name, rec.Code, rec.Header().Get("HX-Retarget"))
		}
		if !strings.Contains(rec.Body.String(), "保存していません。") {
			t.Errorf("%s: no Japanese reason: %s", name, rec.Body.String())
		}
		if len(f.saved) != 0 {
			t.Errorf("%s: the service was called", name)
		}
	}
}

// The endpoint select shows the stored choice; an arm without one shows the default.
func TestPanel_ShowsTheStoredEndpoint(t *testing.T) {
	f := &fakeService{state: jevtrial.State{TrialRuns: true, CapUSD: 0.1, Arms: []jevtrial.ArmStatus{
		{Arm: jevtrialsettings.Arm{ID: "a-one", Provider: "openrouter", Model: "m1", PromptVersion: "trader-v3", Endpoint: "/alpha/decisions"}, CapUSD: 0.05},
		{Arm: jevtrialsettings.Arm{ID: "a-two", Provider: "openrouter", Model: "m2", PromptVersion: "trader-v3"}, CapUSD: 0.05},
	}}}
	body := do(engine(f), http.MethodGet, "/settings/jev-trial-panel", nil).Body.String()
	if got := strings.Count(body, `<option value="/alpha/decisions" selected`); got != 1 {
		t.Errorf("%d arms show /alpha/decisions selected, want 1", got)
	}
	if got := strings.Count(body, `<option value="" selected`); got < 1 {
		t.Errorf("the arm without an endpoint does not show the default selected")
	}
}

func TestPanel_ShowsTheStoredArmsAsRows(t *testing.T) {
	f := &fakeService{state: jevtrial.State{TrialRuns: true, CapUSD: 0.1, Arms: []jevtrial.ArmStatus{
		{Arm: jevtrialsettings.Arm{ID: "a-one", Provider: "openrouter", Model: "cloudflare/clef", PromptVersion: "trader-v3", DailyCostCapUSD: 0.02, ProviderOnly: []string{"x", "y"}, DataCollection: "allow", ZDR: true}, CapUSD: 0.02},
		{Arm: jevtrialsettings.Arm{ID: "a-two", Provider: "openrouter", Model: "m2", PromptVersion: "trader-v3"}, CapUSD: 0.05},
	}}}
	body := do(engine(f), http.MethodGet, "/settings/jev-trial-panel", nil).Body.String()
	if got := strings.Count(body, `data-testid="jev-trial-arm-field-row"`); got != 2 {
		t.Errorf("%d arm rows, want 2", got)
	}
	for _, want := range []string{
		`name="arm-0-id"`, `value="a-one"`, `value="cloudflare/clef"`, `name="arm-1-id"`, `value="a-two"`, `value="x, y"`, `value="0.02"`,
		`hx-post="/settings/jev-trial/arms/add"`, `hx-post="/settings/jev-trial/arms/remove?index=1"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("panel lacks %q", want)
		}
	}
	if strings.Contains(body, "<textarea") || strings.Contains(body, "JSON") {
		t.Error("the panel still has the JSON text area")
	}
	if !regexp.MustCompile(`<option value="allow"[^>]*selected`).MatchString(body) {
		t.Error("the data collection of the first arm is not shown as allow")
	}
}

func TestAddArm_AppendsABlankRowKeepingTheTypedOnes(t *testing.T) {
	f := &fakeService{}
	rec := do(engine(f), http.MethodPost, "/settings/jev-trial/arms/add", merge(armForm(0, "typed"), armForm(1, "other")))
	body := rec.Body.String()
	if rec.Code != http.StatusOK || strings.Count(body, `data-testid="jev-trial-arm-field-row"`) != 3 {
		t.Fatalf("add = %d, want 3 rows: %s", rec.Code, body)
	}
	for _, want := range []string{`value="typed"`, `value="m-other"`, `name="arm-2-id"`, `id="jev-trial-arm-editor"`} {
		if !strings.Contains(body, want) {
			t.Errorf("fragment lacks %q", want)
		}
	}
	if strings.Contains(body, "<section") || len(f.saved) != 0 {
		t.Error("add rendered the whole panel or saved")
	}
}

func TestAddArm_StopsAtTheMaximum(t *testing.T) {
	form := url.Values{}
	for i := range jevtrialsettings.MaxArms {
		form = merge(form, armForm(i, "a"+strconv.Itoa(i)))
	}
	rec := do(engine(&fakeService{}), http.MethodPost, "/settings/jev-trial/arms/add", form)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "最大 8 件") {
		t.Errorf("ninth arm = %d %s, want 422 naming the maximum", rec.Code, rec.Body.String())
	}
	// The add button is off in a full editor.
	if rec := do(engine(&fakeService{}), http.MethodPost, "/settings/jev-trial/arms/remove?index=0", merge(form, armForm(8, "a8"))); rec.Code != http.StatusOK ||
		!regexp.MustCompile(`<button[^>]*data-testid="jev-trial-arm-add"[^>]*disabled|<button[^>]*disabled[^>]*data-testid="jev-trial-arm-add"`).MatchString(rec.Body.String()) {
		t.Errorf("a full editor keeps the add button on: %s", rec.Body.String())
	}
}

func TestRemoveArm_DropsOneRowKeepingTheRest(t *testing.T) {
	f := &fakeService{}
	form := merge(armForm(0, "keep-a"), armForm(1, "drop"), armForm(2, "keep-b"))
	rec := do(engine(f), http.MethodPost, "/settings/jev-trial/arms/remove?index=1", form)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || strings.Count(body, `data-testid="jev-trial-arm-field-row"`) != 2 {
		t.Fatalf("remove = %d, want 2 rows: %s", rec.Code, body)
	}
	if strings.Contains(body, `value="drop"`) || !strings.Contains(body, `value="keep-a"`) || !strings.Contains(body, `value="keep-b"`) {
		t.Errorf("wrong rows kept: %s", body)
	}
	if !strings.Contains(body, `name="arm-1-id"`) || strings.Contains(body, `name="arm-2-id"`) {
		t.Error("the rows are not renumbered")
	}
	if len(f.saved) != 0 {
		t.Error("remove saved")
	}
	for _, index := range []string{"", "x", "-1", "3"} {
		if rec := do(engine(f), http.MethodPost, "/settings/jev-trial/arms/remove?index="+index, form); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("index %q = %d, want 422", index, rec.Code)
		}
	}
}
