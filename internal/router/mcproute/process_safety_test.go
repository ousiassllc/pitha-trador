package mcproute_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/proc"
	"github.com/ousiassllc/pitha-trador/internal/web/middleware"
)

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

// The one-time key display must survive: a panel that reveals the key never
// polls, even while the process is still starting.
func TestRevealedKeyPanelNeverPolls(t *testing.T) {
	a := newApp(t, fullSecrets{})
	a.seedValid(t)
	a.session()
	_ = a.post("/settings/mcp/start", url.Values{}, true)
	a.dialOK = false // starting
	body := a.post("/settings/mcp/key", url.Values{}, true).Body.String()
	if !strings.Contains(body, "mcp-key-reveal") || !strings.Contains(body, `data-state="starting"`) {
		t.Fatalf("setup: want the reveal block on a starting panel; body %s", body)
	}
	if strings.Contains(body, `hx-trigger="every`) {
		t.Error("the panel that shows the key polls: the swap would erase the only display of the key")
	}
}
