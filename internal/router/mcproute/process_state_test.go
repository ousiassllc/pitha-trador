package mcproute_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/proc"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/settings"
	"github.com/ousiassllc/pitha-trador/internal/web/middleware"
)

func TestStart_RefusesWhatTheProcessWouldRefuse(t *testing.T) {
	tests := []struct {
		name string
		s    settings.Settings
		code int
		want string
	}{
		{"disabled (the default)", settings.Settings{Listen: settings.DefaultListen, APIKey: "k"}, http.StatusUnprocessableEntity, "無効"},
		{"no key", settings.Settings{Enabled: true, Listen: settings.DefaultListen}, http.StatusUnprocessableEntity, "API キーが未設定"},
		{"address outside the rule", settings.Settings{Enabled: true, Listen: "0.0.0.0:48110", APIKey: "k"}, http.StatusUnprocessableEntity, "許可範囲外"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := newApp(t, fullSecrets{})
			a.session()
			if err := settings.Save(a.path, tc.s); err != nil {
				t.Fatal(err)
			}
			rec := a.post("/settings/mcp/start", url.Values{}, true)
			if rec.Code != tc.code || !strings.Contains(rec.Body.String(), tc.want) {
				t.Fatalf("start = %d; body %s", rec.Code, rec.Body)
			}
			if len(a.children) != 0 {
				t.Fatal("a process was started")
			}
		})
	}
}

func TestStart_ExecutableMissingIsAnError(t *testing.T) {
	a := newApp(t, fullSecrets{})
	a.session()
	if err := settings.Save(a.path, settings.Settings{Enabled: true, Listen: settings.DefaultListen, APIKey: "k"}); err != nil {
		t.Fatal(err)
	}
	rec := a.post("/settings/mcp/start", url.Values{}, true)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "実行ファイルが見つかりません") {
		t.Fatalf("start = %d; body %s", rec.Code, rec.Body)
	}
	body := a.get("/settings/mcp-panel").Body.String()
	if !strings.Contains(body, `data-state="error"`) || !strings.Contains(body, "エラー") {
		t.Errorf("a failed start must show the error state; body %s", body)
	}
}

// 起動失敗・異常終了が状態表示（エラー）に出て、再起動ボタンで戻る。
func TestAnAbnormalExitShowsTheErrorStateUntilRestarted(t *testing.T) {
	a := newApp(t, fullSecrets{})
	a.seedValid(t)
	a.session()
	if rec := a.post("/settings/mcp/start", url.Values{}, true); rec.Code != http.StatusOK {
		t.Fatalf("start = %d", rec.Code)
	}
	a.children[0].end(proc.Exit{Code: 7})
	deadline := time.Now().Add(5 * time.Second)
	var body string
	for time.Now().Before(deadline) {
		if body = a.get("/settings/mcp-panel").Body.String(); strings.Contains(body, `data-state="error"`) {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	for _, want := range []string{`data-state="error"`, "エラー", "異常終了", "終了コード 7", `data-testid="mcp-restart"`} {
		if !strings.Contains(body, want) {
			t.Errorf("error panel lacks %q; body %s", want, body)
		}
	}
	if rec := a.post("/settings/mcp/restart", url.Values{}, true); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `data-state="listening"`) {
		t.Fatalf("restart after a crash = %d; body %s", rec.Code, rec.Body)
	}
}

// 起動中 (within the grace period, not listening yet).
func TestStartingState(t *testing.T) {
	a := newApp(t, fullSecrets{})
	a.seedValid(t)
	a.session()
	if rec := a.post("/settings/mcp/start", url.Values{}, true); rec.Code != http.StatusOK {
		t.Fatalf("start = %d", rec.Code)
	}
	a.dialOK = false // the process is alive but has not opened its port yet
	body := a.get("/settings/mcp-panel").Body.String()
	if !strings.Contains(body, `data-state="starting"`) || !strings.Contains(body, "起動中") {
		t.Errorf("a young process that is not listening must read 起動中; body %s", body)
	}
}

// A process started outside the application is shown but never touched or
// doubled.
func TestAnExternalProcessIsNeitherStoppedNorDoubled(t *testing.T) {
	a := newApp(t, fullSecrets{})
	a.seedValid(t)
	a.session()
	a.dialOK = true
	body := a.get("/settings/mcp-panel").Body.String()
	if !strings.Contains(body, `data-state="listening"`) || !strings.Contains(body, "アプリの外で起動された") {
		t.Errorf("external process not shown; body %s", body)
	}
	if rec := a.post("/settings/mcp/start", url.Values{}, true); rec.Code != http.StatusConflict {
		t.Errorf("start next to an external process = %d, want 409", rec.Code)
	}
	if rec := a.post("/settings/mcp/stop", url.Values{}, true); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "停止できません") {
		t.Errorf("stop of an external process = %d; body %s", rec.Code, rec.Body)
	}
	if len(a.children) != 0 {
		t.Fatal("a process was started next to an external one")
	}
}

func TestAutoStartNote(t *testing.T) {
	tests := []struct {
		name  string
		s     settings.Settings
		want  string
		start string
	}{
		{"enabled with key", settings.Settings{Enabled: true, Listen: settings.DefaultListen, APIKey: "k"}, "次回起動時も自動で起動します", "true"},
		{"disabled", settings.Settings{Listen: settings.DefaultListen, APIKey: "k"}, "自動では起動しません（無効です）", "false"},
		{"no key", settings.Settings{Enabled: true, Listen: settings.DefaultListen}, "自動では起動しません（API キーが未設定です）", "false"},
		{"address outside the rule", settings.Settings{Enabled: true, Listen: "0.0.0.0:48110", APIKey: "k"}, "自動では起動しません（待受アドレスが許可範囲外です）", "false"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := newApp(t, fullSecrets{})
			a.session()
			if err := settings.Save(a.path, tc.s); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(strings.TrimSuffix(a.path, settings.FileName)+"paper-results-mcp", []byte("x"), 0o755); err != nil {
				t.Fatal(err)
			}
			body := a.get("/settings/mcp-panel").Body.String()
			if !strings.Contains(body, tc.want) || !strings.Contains(body, `data-autostart="`+tc.start+`"`) {
				t.Errorf("panel lacks %q / autostart=%s; body %s", tc.want, tc.start, body)
			}
		})
	}
}

// Saving settings while the process runs tells the operator that a restart
// applies them.
func TestSavedChangeAsksForARestart(t *testing.T) {
	a := newApp(t, fullSecrets{})
	a.seedValid(t)
	a.session()
	_ = a.post("/settings/mcp/start", url.Values{}, true)
	if body := a.get("/settings/mcp-panel").Body.String(); strings.Contains(body, "mcp-config-changed") {
		t.Fatal("config-changed shown right after the start")
	}
	// A later modification time, set explicitly (no waiting on the clock).
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(a.path, later, later); err != nil {
		t.Fatal(err)
	}
	if rec := a.save("127.0.0.1:48111", true); rec.Code != http.StatusOK {
		t.Fatalf("save = %d", rec.Code)
	}
	// (the save above rewrote the file, so move its time again)
	if err := os.Chtimes(a.path, later.Add(time.Hour), later.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if body := a.get("/settings/mcp-panel").Body.String(); !strings.Contains(body, "mcp-config-changed") {
		t.Errorf("a saved change must ask for a restart; body %s", body)
	}
}

// A failed action asks the open panel to reload (an error response is not
// swapped into it), and a starting panel polls until the state settles.
func TestFailureRefreshesThePanelAndStartingPolls(t *testing.T) {
	a := newApp(t, fullSecrets{})
	a.session()
	if err := settings.Save(a.path, settings.Settings{Enabled: true, Listen: settings.DefaultListen, APIKey: "k"}); err != nil {
		t.Fatal(err)
	}
	rec := a.post("/settings/mcp/start", url.Values{}, true) // no executable: fails
	if rec.Header().Get("HX-Trigger") != "mcp-refresh" {
		t.Errorf("a failed action sets HX-Trigger %q, want mcp-refresh", rec.Header().Get("HX-Trigger"))
	}
	if !strings.Contains(a.get("/settings").Body.String(), `hx-trigger="load, mcp-refresh from:body"`) {
		t.Error("the panel container does not listen for mcp-refresh")
	}

	a.seedValid(t)
	_ = a.post("/settings/mcp/start", url.Values{}, true)
	if body := a.get("/settings/mcp-panel").Body.String(); strings.Contains(body, `hx-trigger="every`) {
		t.Error("a listening panel must not poll")
	}
	a.dialOK = false // alive, not listening yet
	if body := a.get("/settings/mcp-panel").Body.String(); !strings.Contains(body, `hx-trigger="every 2s"`) {
		t.Errorf("a starting panel must poll; body %s", body)
	}
}

// The promise "次回起動時も自動で起動します" is not made when there is nothing to launch.
func TestAutoStartNoteNeedsTheExecutable(t *testing.T) {
	a := newApp(t, fullSecrets{})
	a.session()
	if err := settings.Save(a.path, settings.Settings{Enabled: true, Listen: settings.DefaultListen, APIKey: "k"}); err != nil {
		t.Fatal(err)
	}
	body := a.get("/settings/mcp-panel").Body.String()
	if strings.Contains(body, "次回起動時も自動で起動します") || !strings.Contains(body, "実行ファイルが見つかりません") {
		t.Errorf("no executable, yet the panel promises an auto start; body %s", body)
	}
}

// Closing the page mid-request is not a server error.
func TestAClientThatGoesAwayIsNotAServerError(t *testing.T) {
	a := newApp(t, fullSecrets{})
	a.seedValid(t)
	a.session()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodPost, "/settings/mcp/start", strings.NewReader(""))
	req = req.WithContext(ctx)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", a.csrf)
	req.AddCookie(a.cookie)
	rec := httptest.NewRecorder()
	a.engine.ServeHTTP(rec, req)
	if rec.Code >= 500 {
		t.Errorf("a canceled request = %d, want no 5xx", rec.Code)
	}
}

// What the screen shows after each new action and in each new state never
// contains the stored key.
func TestProcessResponsesNeverContainTheKey(t *testing.T) {
	a := newApp(t, fullSecrets{})
	a.seedValid(t)
	a.session()
	key := a.loaded().APIKey
	if key == "" {
		t.Fatal("setup: no key")
	}
	var bodies []string
	add := func(rec *httptest.ResponseRecorder) {
		bodies = append(bodies, rec.Body.String(), rec.Header().Get("HX-Trigger"))
	}
	add(a.post("/settings/mcp/start", url.Values{}, true))
	add(a.post("/settings/mcp/start", url.Values{}, true)) // 409
	bodies = append(bodies, a.get("/settings/mcp-panel").Body.String())
	a.children[0].end(proc.Exit{Code: 9, Stderr: "panic: " + key}) // a crash whose output contains the key
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(a.get("/settings/mcp-panel").Body.String(), `data-state="error"`) {
		time.Sleep(time.Millisecond)
	}
	bodies = append(bodies, a.get("/settings/mcp-panel").Body.String(), a.get("/settings").Body.String())
	add(a.post("/settings/mcp/restart", url.Values{}, true))
	add(a.post("/settings/mcp/stop", url.Values{}, true))
	add(a.post("/settings/mcp/stop", url.Values{}, true)) // 409
	for i, b := range bodies {
		if strings.Contains(b, key) || strings.Contains(b, "panic") {
			t.Errorf("response %d contains the key or the process's output: %.300s", i, b)
		}
	}
}

// The new actions sit behind HostGuard like every other route: a DNS-rebinding
// page (another Host) cannot trigger them even with a valid cookie and token.
func TestProcessActionsAreRefusedForARebindingHost(t *testing.T) {
	a := newApp(t, fullSecrets{})
	a.seedValid(t)
	a.engine = router.New(router.WithSecretsStore(fullSecrets{}), router.WithMCPSettings(a.svc), router.WithAllowedHosts(middleware.LoopbackHosts()...))
	page := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/settings", nil)
	req.Host = "127.0.0.1:48080"
	a.engine.ServeHTTP(page, req)
	match := csrfMeta.FindStringSubmatch(page.Body.String())
	if page.Code != http.StatusOK || match == nil || len(page.Result().Cookies()) != 1 {
		t.Fatalf("setup: GET /settings = %d", page.Code)
	}
	for _, path := range processPaths {
		post := httptest.NewRequest(http.MethodPost, path, nil)
		post.Host = "evil.example:48080"
		post.Header.Set("Origin", "http://evil.example:48080")
		post.Header.Set(middleware.CSRFHeader, match[1])
		post.AddCookie(page.Result().Cookies()[0])
		rec := httptest.NewRecorder()
		a.engine.ServeHTTP(rec, post)
		if rec.Code != http.StatusForbidden {
			t.Errorf("POST %s with Host evil.example = %d, want 403", path, rec.Code)
		}
	}
	if len(a.children) != 0 {
		t.Fatal("a rebound request started a process")
	}
	// The same request on the real host does start it (the refusal above is
	// the guard's, not a broken setup).
	ok := httptest.NewRequest(http.MethodPost, "/settings/mcp/start", nil)
	ok.Host = "127.0.0.1:48080"
	ok.Header.Set(middleware.CSRFHeader, match[1])
	ok.AddCookie(page.Result().Cookies()[0])
	rec := httptest.NewRecorder()
	a.engine.ServeHTTP(rec, ok)
	if rec.Code != http.StatusOK || len(a.children) != 1 {
		t.Fatalf("POST on 127.0.0.1 = %d with %d processes", rec.Code, len(a.children))
	}
}
