package mcproute_test

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/proc"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/settings"
)

// fakeChild is a process that runs until it ends (Terminate, or end for a
// crash). While it lives the harness's address answers, like a real MCP.
type fakeChild struct {
	a    *app
	done chan struct{}
	once sync.Once
	exit proc.Exit
}

func (c *fakeChild) Done() <-chan struct{} { return c.done }
func (c *fakeChild) Exit() proc.Exit       { return c.exit }
func (c *fakeChild) Terminate(time.Duration) {
	c.end(proc.Exit{Code: -1})
}

func (c *fakeChild) end(e proc.Exit) {
	c.once.Do(func() {
		c.exit = e
		c.a.dialOK = false
		close(c.done)
	})
}

// spawn is the harness's Proc.Spawn.
func (a *app) spawn(proc.Spec) (proc.Child, error) {
	k := &fakeChild{a: a, done: make(chan struct{})}
	a.children = append(a.children, k)
	a.dialOK = true
	return k, nil
}

func (a *app) seedValid(t *testing.T) {
	t.Helper()
	if err := settings.Save(a.path, settings.Settings{Enabled: true, Listen: settings.DefaultListen, APIKey: "pitha-mcp-k-0123456789"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(strings.TrimSuffix(a.path, settings.FileName)+"paper-results-mcp", []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
}

var processPaths = []string{"/settings/mcp/start", "/settings/mcp/stop", "/settings/mcp/restart"}

// Safety condition 5: starting, stopping and restarting are POSTs behind the
// session cookie and CSRF token, and closed until setup is complete.
func TestProcessActionsNeedSessionCSRFAndSetupCompletion(t *testing.T) {
	a := newApp(t, fullSecrets{})
	a.seedValid(t)
	a.session()
	good := a.cookie

	for _, path := range processPaths {
		a.cookie = good
		if rec := a.post(path, url.Values{}, false); rec.Code != http.StatusForbidden {
			t.Errorf("POST %s without the CSRF token = %d, want 403", path, rec.Code)
		}
		a.cookie = nil
		if rec := a.post(path, url.Values{}, true); rec.Code != http.StatusForbidden {
			t.Errorf("POST %s without the session cookie = %d, want 403", path, rec.Code)
		}
		a.cookie = &http.Cookie{Name: good.Name, Value: "forged"}
		if rec := a.post(path, url.Values{}, true); rec.Code != http.StatusForbidden {
			t.Errorf("POST %s with a forged cookie = %d, want 403", path, rec.Code)
		}
	}
	a.cookie = good
	// A cross-site request cannot carry the token it cannot read.
	if rec := a.post("/settings/mcp/start", url.Values{"_csrf": {"guess"}}, false); rec.Code != http.StatusForbidden {
		t.Errorf("a wrong CSRF form field = %d, want 403", rec.Code)
	}
	if len(a.children) != 0 {
		t.Fatalf("%d process(es) started by rejected requests", len(a.children))
	}

	// GET never starts anything: the routes are POST only.
	for _, path := range processPaths {
		if rec := a.get(path); rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("GET %s = %d, want 404/405", path, rec.Code)
		}
	}
	if len(a.children) != 0 {
		t.Fatal("a GET started a process")
	}

	fresh := newApp(t, fullSecrets{empty: true})
	fresh.seedValid(t)
	fresh.session()
	for _, path := range processPaths {
		rec := fresh.post(path, url.Values{}, true)
		if rec.Code != http.StatusNoContent || rec.Header().Get("HX-Redirect") != "/setup" {
			t.Errorf("POST %s before setup = %d / %q, want 204 + HX-Redirect /setup", path, rec.Code, rec.Header().Get("HX-Redirect"))
		}
	}
	if len(fresh.children) != 0 {
		t.Fatal("a process started before setup was complete")
	}
}

func TestProcessRoutesAreAbsentWithoutWithMCPSettings(t *testing.T) {
	a := newAppWithout(t)
	a.session()
	for _, path := range processPaths {
		if rec := a.post(path, url.Values{}, true); rec.Code != http.StatusNotFound {
			t.Errorf("POST %s without the MCP service = %d, want 404", path, rec.Code)
		}
	}
}

func TestStartStopRestart_ThroughTheScreen(t *testing.T) {
	a := newApp(t, fullSecrets{})
	a.seedValid(t)
	a.session()

	body := a.get("/settings/mcp-panel").Body.String()
	for _, want := range []string{`data-state="stopped"`, "停止中", `data-testid="mcp-start"`, `data-testid="mcp-stop"`, `data-testid="mcp-restart"`, "次回起動時も自動で起動します", `data-autostart="true"`} {
		if !strings.Contains(body, want) {
			t.Errorf("idle panel lacks %q", want)
		}
	}

	rec := a.post("/settings/mcp/start", url.Values{}, true)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `data-state="listening"`) || !strings.Contains(rec.Body.String(), "待受中") {
		t.Fatalf("start = %d; body %s", rec.Code, rec.Body)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("the action response is cacheable")
	}
	if len(a.children) != 1 {
		t.Fatalf("%d processes started", len(a.children))
	}

	// 二重起動しない
	if dup := a.post("/settings/mcp/start", url.Values{}, true); dup.Code != http.StatusConflict || !strings.Contains(dup.Body.String(), "二重には起動しません") {
		t.Fatalf("second start = %d; body %s", dup.Code, dup.Body)
	}
	if len(a.children) != 1 {
		t.Fatal("a second process was started")
	}

	rec = a.post("/settings/mcp/restart", url.Values{}, true)
	if rec.Code != http.StatusOK || len(a.children) != 2 || !strings.Contains(rec.Body.String(), `data-state="listening"`) {
		t.Fatalf("restart = %d with %d processes; body %s", rec.Code, len(a.children), rec.Body)
	}
	select {
	case <-a.children[0].done:
	default:
		t.Fatal("restart left the old process running")
	}

	rec = a.post("/settings/mcp/stop", url.Values{}, true)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `data-state="stopped"`) {
		t.Fatalf("stop = %d; body %s", rec.Code, rec.Body)
	}
	if rec := a.post("/settings/mcp/stop", url.Values{}, true); rec.Code != http.StatusConflict {
		t.Errorf("stop with nothing running = %d, want 409", rec.Code)
	}
}

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
	time.Sleep(20 * time.Millisecond) // a later modification time
	if rec := a.save("127.0.0.1:48111", true); rec.Code != http.StatusOK {
		t.Fatalf("save = %d", rec.Code)
	}
	if body := a.get("/settings/mcp-panel").Body.String(); !strings.Contains(body, "mcp-config-changed") {
		t.Errorf("a saved change must ask for a restart; body %s", body)
	}
}
