// Package mcproute holds the router-level tests of the Settings screen's MCP
// section (issue #792): the card, the panel fragment, the save and key
// actions, the Setup Guard and CSRF behaviour, and that the generated API key
// never leaves the one response that reveals it. The pieces are covered in
// internal/service/paperresultsmcp/{settings,admin}.
package mcproute_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/admin"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/proc"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/settings"
	"github.com/ousiassllc/pitha-trador/internal/web/middleware"
)

const tailnetIP = "100.101.102.103"

var csrfMeta = regexp.MustCompile(`<meta name="csrf-token" content="([^"]+)"`)

// fullSecrets is a settings.SecretsStore holding every required secret, so the
// Setup Guard lets requests through; empty is the first-run state.
type fullSecrets struct{ empty bool }

func (f fullSecrets) Get(_ context.Context, key string) (string, bool, error) {
	return "x", !f.empty, nil
}
func (fullSecrets) Set(context.Context, string, string) error { return nil }
func (fullSecrets) Delete(context.Context, string) error      { return nil }

// app is the router under test plus the settings file behind it.
type app struct {
	t      *testing.T
	engine *gin.Engine
	svc    *admin.Service
	path   string
	dialOK bool
	dials  int
	// children are the processes the fake spawner started (process_test.go).
	children []*fakeChild
	cookie   *http.Cookie
	csrf     string
}

func newApp(t *testing.T, secrets fullSecrets) *app {
	t.Helper()
	gin.SetMode(gin.TestMode)
	a := &app{t: t, path: filepath.Join(t.TempDir(), settings.FileName)}
	svc := admin.New(admin.Options{
		Path:    a.path,
		ExePath: func() (string, error) { return filepath.Join(filepath.Dir(a.path), "paper-results-mcp"), nil },
		Dial: func(context.Context, string) error {
			a.dials++
			if a.dialOK {
				return nil
			}
			return errors.New("refused")
		},
		InterfaceAddrs: func() ([]net.Addr, error) {
			return []net.Addr{&net.IPNet{IP: net.ParseIP(tailnetIP), Mask: net.CIDRMask(32, 32)}}, nil
		},
		// No test starts a real process: the spawner records and fakes it.
		Proc: proc.Options{Spawn: a.spawn, ReadyWait: 20 * time.Millisecond, PollInterval: 2 * time.Millisecond},
	})
	t.Cleanup(svc.Shutdown)
	a.svc = svc
	a.engine = router.New(router.WithSecretsStore(secrets), router.WithMCPSettings(svc))
	return a
}

// session loads a page for the session cookie and CSRF token a browser holds.
func (a *app) session() {
	a.t.Helper()
	page := httptest.NewRecorder()
	a.engine.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/setup", nil))
	match := csrfMeta.FindStringSubmatch(page.Body.String())
	if len(page.Result().Cookies()) != 1 || match == nil {
		a.t.Fatalf("GET /setup gave cookies=%v csrf=%v", page.Result().Cookies(), match)
	}
	a.cookie, a.csrf = page.Result().Cookies()[0], match[1]
}

func (a *app) get(path string) *httptest.ResponseRecorder {
	a.t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if a.cookie != nil {
		req.AddCookie(a.cookie)
	}
	rec := httptest.NewRecorder()
	a.engine.ServeHTTP(rec, req)
	return rec
}

// post sends an HTMX form POST; withCSRF false omits the token header.
func (a *app) post(path string, form url.Values, withCSRF bool) *httptest.ResponseRecorder {
	a.t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	if withCSRF {
		req.Header.Set(middleware.CSRFHeader, a.csrf)
	}
	if a.cookie != nil {
		req.AddCookie(a.cookie)
	}
	rec := httptest.NewRecorder()
	a.engine.ServeHTTP(rec, req)
	return rec
}

func (a *app) save(listen string, enabled bool) *httptest.ResponseRecorder {
	form := url.Values{"listen": {listen}}
	if enabled {
		form.Set("enabled", "on")
	}
	return a.post("/settings/mcp", form, true)
}

func (a *app) loaded() settings.Settings {
	a.t.Helper()
	s, err := settings.Load(a.path)
	if err != nil {
		a.t.Fatalf("Load: %v", err)
	}
	return s
}
