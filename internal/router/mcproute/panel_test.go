package mcproute_test

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/settings"
)

func TestSettingsPage_MCPCardNeedsWithMCPSettings(t *testing.T) {
	a := newApp(t, fullSecrets{})
	body := a.get("/settings/integrations").Body.String()
	for _, want := range []string{`id="modal-mcp"`, `data-testid="settings-card-mcp"`, `hx-get="/settings/mcp-panel"`, `hx-trigger="load, mcp-refresh from:body"`, "外部連携"} {
		if !strings.Contains(body, want) {
			t.Errorf("with the service wired, /settings/integrations lacks %q", want)
		}
	}
	// The probe runs only in the lazily loaded fragment.
	if a.dials != 0 {
		t.Errorf("GET /settings/integrations dialled %d times; the TCP probe belongs to the panel fragment", a.dials)
	}

	rec := newAppWithout(t).get("/settings/integrations")
	if strings.Contains(rec.Body.String(), "modal-mcp") || strings.Contains(rec.Body.String(), "mcp-panel") {
		t.Error("without WithMCPSettings, /settings/integrations must not show the MCP card")
	}
}

func newAppWithout(t *testing.T) *app {
	t.Helper()
	a := newApp(t, fullSecrets{})
	a.engine = router.New(router.WithSecretsStore(fullSecrets{}))
	return a
}

func TestMCPRoutesAreAbsentWithoutWithMCPSettings(t *testing.T) {
	a := newAppWithout(t)
	a.session()
	if rec := a.get("/settings/mcp-panel"); rec.Code != http.StatusNotFound && rec.Code != http.StatusBadRequest {
		t.Errorf("GET /settings/mcp-panel = %d, want 404-ish", rec.Code)
	}
	if _, err := os.Stat(a.path); err == nil {
		t.Error("a settings file appeared")
	}
}

func TestPanel_StateTextForEachState(t *testing.T) {
	cases := []struct {
		name    string
		cfg     settings.Settings
		dialOK  bool
		badge   string
		state   string
		noteHas string
		dials   int
	}{
		{"listening", settings.Settings{Enabled: true, Listen: "127.0.0.1:48110", APIKey: "pitha-mcp-k"}, true, "待受中", "listening", "", 1},
		{"stopped: not running", settings.Settings{Enabled: true, Listen: "127.0.0.1:48110", APIKey: "pitha-mcp-k"}, false, "停止中", "stopped", "見つかりません", 1},
		{"no key", settings.Settings{Enabled: true, Listen: "127.0.0.1:48110"}, true, "キー未設定", "no_key", "", 0},
		{"disabled", settings.Settings{Listen: "127.0.0.1:48110", APIKey: "pitha-mcp-k"}, true, "停止中", "stopped", "無効に設定", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := newApp(t, fullSecrets{})
			a.dialOK = tc.dialOK
			if err := settings.Save(a.path, tc.cfg); err != nil {
				t.Fatal(err)
			}
			rec := a.get("/settings/mcp-panel")
			body := rec.Body.String()
			if rec.Code != http.StatusOK || !strings.Contains(body, tc.badge) || !strings.Contains(body, `data-state="`+tc.state+`"`) {
				t.Fatalf("status %d, want badge %q state %q; body: %s", rec.Code, tc.badge, tc.state, body)
			}
			if tc.noteHas != "" && !strings.Contains(body, tc.noteHas) {
				t.Errorf("body lacks note %q", tc.noteHas)
			}
			if a.dials != tc.dials {
				t.Errorf("dials = %d, want %d", a.dials, tc.dials)
			}
		})
	}
}

func TestPanel_ShowsMaskedHeaderLocationsAndRestartNote(t *testing.T) {
	a := newApp(t, fullSecrets{})
	body := a.get("/settings/mcp-panel").Body.String()
	for _, want := range []string{
		"Authorization: Bearer ●●●●●●●●", "http://127.0.0.1:48110/", "Grok Bot に登録する値",
		"paper-results-mcp の配置先", "見つからない", "設定ファイルの場所", a.path, "再起動",
		"API キーを生成", "未設定",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("panel lacks %q", want)
		}
	}
	if strings.Contains(body, "mcp-key-reveal") || strings.Contains(body, "hx-confirm") {
		t.Error("a fresh panel must have no reveal block and no regenerate confirm")
	}
	if err := os.WriteFile(strings.TrimSuffix(a.path, settings.FileName)+"paper-results-mcp", []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if body := a.get("/settings/mcp-panel").Body.String(); !strings.Contains(body, "見つかる") {
		t.Error("an existing executable must read 見つかる")
	}
}

func TestSave_PersistsEnabledAndListen(t *testing.T) {
	a := newApp(t, fullSecrets{})
	a.session()
	rec := a.save(tailnetIP+":48120", true)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "保存しました") || !strings.Contains(rec.Body.String(), tailnetIP+":48120") {
		t.Fatalf("status %d; body %s", rec.Code, rec.Body)
	}
	if got := a.loaded(); !got.Enabled || got.Listen != tailnetIP+":48120" {
		t.Fatalf("stored %+v", got)
	}
	// An unchecked box sends no `enabled`.
	if rec := a.save("127.0.0.1:48110", false); rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if got := a.loaded(); got.Enabled {
		t.Fatalf("unchecked box left Enabled=%v", got.Enabled)
	}
}

func TestSave_RejectsDisallowedListenOnTheServer(t *testing.T) {
	rejected := []string{
		"0.0.0.0:48110", "[::]:48110", ":48110", "192.168.1.20:48110", "10.0.0.5:48110", "8.8.8.8:48110",
		"localhost:48110", "example.com:48110", "127.0.0.1:0", "100.64.1.2:48110",
	}
	for _, listen := range rejected {
		t.Run(listen, func(t *testing.T) {
			a := newApp(t, fullSecrets{})
			a.session()
			if err := settings.Save(a.path, settings.Settings{Listen: settings.DefaultListen, APIKey: "pitha-mcp-k"}); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(a.path)
			rec := a.save(listen, true)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422; body %s", rec.Code, rec.Body)
			}
			if !strings.Contains(rec.Body.String(), "待受アドレスが許可されていません") {
				t.Errorf("error text missing from %s", rec.Body)
			}
			after, _ := os.ReadFile(a.path)
			if string(before) != string(after) {
				t.Error("file changed on a rejected address")
			}
		})
	}
	for _, listen := range []string{"127.0.0.1:48110", "[::1]:48110", tailnetIP + ":48110"} {
		t.Run("accept "+listen, func(t *testing.T) {
			a := newApp(t, fullSecrets{})
			a.session()
			if rec := a.save(listen, true); rec.Code != http.StatusOK {
				t.Fatalf("status = %d; body %s", rec.Code, rec.Body)
			}
		})
	}
}

func TestMCPActionsNeedCSRFAndSetupCompletion(t *testing.T) {
	a := newApp(t, fullSecrets{})
	a.session()
	for _, path := range []string{"/settings/mcp", "/settings/mcp/key"} {
		if rec := a.post(path, url.Values{"listen": {"127.0.0.1:48110"}}, false); rec.Code != http.StatusForbidden {
			t.Errorf("POST %s without the CSRF token = %d, want 403", path, rec.Code)
		}
	}
	if _, err := os.Stat(a.path); err == nil {
		t.Error("a request without CSRF wrote the file")
	}

	// Before setup is complete the guard keeps the MCP routes closed (unlike
	// POST /settings/:key, which the Setup screen needs).
	fresh := newApp(t, fullSecrets{empty: true})
	fresh.session()
	for _, path := range []string{"/settings/mcp", "/settings/mcp/key"} {
		rec := fresh.post(path, url.Values{"listen": {"127.0.0.1:48110"}}, true)
		if rec.Code != http.StatusNoContent || rec.Header().Get("HX-Redirect") != "/setup" {
			t.Errorf("POST %s before setup = %d / %q, want 204 + HX-Redirect /setup", path, rec.Code, rec.Header().Get("HX-Redirect"))
		}
	}
	if rec := fresh.get("/settings/mcp-panel"); rec.Code != http.StatusFound {
		t.Errorf("GET panel before setup = %d, want redirect", rec.Code)
	}
	if _, err := os.Stat(fresh.path); err == nil {
		t.Error("a request before setup wrote the file")
	}
}

func TestRegenerate_RevealsTheKeyOnceWithNoStoreAndNoHistory(t *testing.T) {
	a := newApp(t, fullSecrets{})
	a.session()
	rec := a.post("/settings/mcp/key", url.Values{}, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d; body %s", rec.Code, rec.Body)
	}
	key := a.loaded().APIKey
	if !strings.HasPrefix(key, settings.KeyPrefix) {
		t.Fatalf("stored key %q", key)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`value="` + key + `"`, "Authorization: Bearer " + key, `readonly`, `hx-history="false"`, `autocomplete="off"`,
		"二度と表示されません", "data-copy-from=\"mcp-new-key\"", "data-copy-from=\"mcp-new-header\"",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("reveal response lacks %q", want)
		}
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	// The key shows in exactly: the input value, and the header line.
	if n := strings.Count(body, key); n != 2 {
		t.Errorf("key occurs %d times in the reveal response, want 2 (field + header line)", n)
	}
	// Re-render of the same panel afterwards is masked and asks to confirm.
	again := a.get("/settings/mcp-panel")
	if strings.Contains(again.Body.String(), key) || strings.Contains(again.Body.String(), "mcp-key-reveal") {
		t.Error("the panel shows the key after the reveal")
	}
	if !strings.Contains(again.Body.String(), "hx-confirm") || !strings.Contains(again.Body.String(), "再生成") {
		t.Error("with a key stored the button must be 再生成 and ask to confirm")
	}
	if cc := again.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("panel Cache-Control = %q, want no-store", cc)
	}
}

func TestRegenerate_ReplacesTheKeyEveryTime(t *testing.T) {
	a := newApp(t, fullSecrets{})
	a.session()
	a.post("/settings/mcp/key", url.Values{}, true)
	first := a.loaded().APIKey
	a.post("/settings/mcp/key", url.Values{}, true)
	if second := a.loaded().APIKey; second == first || second == "" {
		t.Fatalf("second key %q vs first %q", second, first)
	}
}
