package jevtrialroute_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
)

const codexArms = `[{"id":"cx-sol","provider":"codex_chatgpt","model":"gpt-6.1-sol","prompt_version":"trader-v3"}]`

// Issue #1081: the ChatGPT subscription provider is off by default, and nothing
// but the form that also turns its switch on can save an arm of it.
func TestCodexProviderIsOffByDefaultOnTheScreen(t *testing.T) {
	a := newApp(t)
	body := a.get("/settings/jev-trial-panel").Body.String()
	for _, want := range []string{`data-testid="jev-trial-codex-enabled"`, `data-testid="jev-trial-codex-today"`, "まだ確認していません",
		`placeholder="30（既定値）"`, `placeholder="500000（既定値）"`} {
		if !strings.Contains(body, want) {
			t.Errorf("panel lacks %q", want)
		}
	}
	if strings.Contains(body, `jev-trial-codex-stop`) {
		t.Error("a stop note is shown without a stop")
	}
}

func TestCodexArmIsRefusedWhileTheProviderSwitchIsOff(t *testing.T) {
	a := newApp(t)
	rec := a.post("/settings/jev-trial", url.Values{"enabled": {"on"}, "arms": {codexArms}}, true)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "ChatGPT サブスク（Codex）を有効にする") {
		t.Fatalf("save = %d %s", rec.Code, rec.Body.String())
	}
	for _, key := range []string{jevtrialsettings.KeyEnabled, jevtrialsettings.KeyArms, jevtrialsettings.KeyCodexEnabled} {
		if _, ok := a.stored(key); ok {
			t.Errorf("a rejected form stored %s", key)
		}
	}
}

func TestCodexArmIsSavedWithTheSwitchAndShowsTheSubscriptionRow(t *testing.T) {
	a := newApp(t)
	rec := a.post("/settings/jev-trial", url.Values{"enabled": {"on"}, "arms": {codexArms}, "codex_enabled": {"on"}, "codex_call_cap": {"12"}, "codex_token_cap": {"3000"}}, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("save = %d %s", rec.Code, rec.Body.String())
	}
	for key, want := range map[string]string{jevtrialsettings.KeyCodexEnabled: "1", jevtrialsettings.KeyCodexDailyCallCap: "12", jevtrialsettings.KeyCodexDailyTokenCap: "3000"} {
		if v, _ := a.stored(key); v != want {
			t.Errorf("%s = %q, want %q", key, v, want)
		}
	}
	body := a.get("/settings/jev-trial-panel").Body.String()
	for _, want := range []string{"cx-sol", "サブスク（USD なし）", `value="12"`, `value="3000"`} {
		if !strings.Contains(body, want) {
			t.Errorf("panel lacks %q\n%s", want, body)
		}
	}
	// Turning the provider off again keeps the saved arm from running: the arm row says so.
	if rec := a.post("/settings/jev-trial", url.Values{"arms": {codexArms}}, true); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("an arm of the provider with its switch off = %d, want 422", rec.Code)
	}
	if rec := a.post("/settings/jev-trial", url.Values{"arms": {"[]"}}, true); rec.Code != http.StatusOK {
		t.Fatalf("turn everything off = %d", rec.Code)
	}
	if v, _ := a.stored(jevtrialsettings.KeyCodexEnabled); v != "0" {
		t.Errorf("codex enabled = %q, want 0", v)
	}
	if _, ok := a.stored(jevtrialsettings.KeyCodexDailyCallCap); ok {
		t.Error("a blank cap did not restore the default")
	}
}

func TestCodexInvalidCapsAreRejectedAndSaveNothing(t *testing.T) {
	a := newApp(t)
	for name, form := range map[string]url.Values{
		"call cap zero":   {"arms": {"[]"}, "codex_call_cap": {"0"}},
		"call cap text":   {"arms": {"[]"}, "codex_call_cap": {"many"}},
		"call cap huge":   {"arms": {"[]"}, "codex_call_cap": {"1000000"}},
		"token cap zero":  {"arms": {"[]"}, "codex_token_cap": {"0"}},
		"token cap float": {"arms": {"[]"}, "codex_token_cap": {"1.5"}},
	} {
		if rec := a.post("/settings/jev-trial", form, true); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: status %d, want 422", name, rec.Code)
		}
	}
	if _, ok := a.stored(jevtrialsettings.KeyArms); ok {
		t.Error("a rejected form stored the arms")
	}
}

// The screen shows the login state and why the provider is stopped, in words, with
// the time it resumes; it never shows the CLI's text (none is stored).
func TestCodexScreenShowsLoginAndStopReason(t *testing.T) {
	a := newApp(t)
	set := func(key, value string) {
		t.Helper()
		if err := a.settings.Set(t.Context(), key, value, time2026()); err != nil {
			t.Fatal(err)
		}
	}
	set(jevtrialsettings.KeyCodexLogin, `{"state":"chatgpt","at":"2026-11-13T02:59:00Z"}`)
	set(jevtrialsettings.KeyCodexHalt, `{"reason":"usage_limit","until":"2026-11-13T09:00:00Z","until_known":true,"at":"2026-11-13T02:30:00Z"}`)
	body := a.get("/settings/jev-trial-panel").Body.String()
	for _, want := range []string{"ChatGPT でログイン済み", "ChatGPT の使用量の上限に達したため", "18:00", "再試行しません"} {
		if !strings.Contains(body, want) {
			t.Errorf("panel lacks %q\n%s", want, body)
		}
	}
	if strings.Contains(body, "再開の時刻が分からない") {
		t.Error("a known reset time is shown as unknown")
	}

	set(jevtrialsettings.KeyCodexHalt, `{"reason":"failed","until":"2026-11-13T15:00:00Z","until_known":false,"at":"2026-11-13T02:30:00Z"}`)
	body = a.get("/settings/jev-trial-panel").Body.String()
	for _, want := range []string{"上限と確認できた文面ではありません", "再開の時刻が分からないため、その日いっぱい"} {
		if !strings.Contains(body, want) {
			t.Errorf("panel lacks %q for a plain failure", want)
		}
	}
	if strings.Contains(body, "使用量の上限に達した") {
		t.Error("a failure that matched no usage-limit text is shown as a usage limit")
	}
}

func time2026() time.Time { return time.Date(2026, 11, 13, 3, 0, 0, 0, time.UTC) }
