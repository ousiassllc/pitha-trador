// Package adminroutes holds the router-level tests of the admin hub (issue
// #793): every admin screen stays reachable at its current URL, the hub
// lists every Settings dialog and main-nav page, and every hub-listed page
// links back to `/admin`.
package adminroutes_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/admin"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/settings"
)

// secrets is a settings.SecretsStore. full=true holds every key (Setup Guard
// satisfied); empty is the first-run state.
type secrets struct{ full bool }

func (s secrets) Get(context.Context, string) (string, bool, error) { return "x", s.full, nil }
func (secrets) Set(context.Context, string, string) error           { return nil }
func (secrets) Delete(context.Context, string) error                { return nil }

// opsStore is an in-memory opsettings.Store.
type opsStore map[string]string

func (s opsStore) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := s[key]
	return v, ok, nil
}
func (s opsStore) Set(_ context.Context, key, value string, _ time.Time) error {
	s[key] = value
	return nil
}
func (s opsStore) Delete(_ context.Context, key string) error { delete(s, key); return nil }

// tachibanaSession makes the running adapter 立花, so the 立花 card shows its
// session state (the full Settings page).
type tachibanaSession struct{}

func (tachibanaSession) Capabilities() broker.Capabilities {
	return broker.Capabilities{Name: config.BrokerTachibana}
}
func (tachibanaSession) Status() broker.SessionStatus { return broker.SessionStatus{} }

// newEngine returns the router wired like production for the Settings screen:
// operational settings, a broker session and the MCP settings, so /settings
// renders every card and dialog.
func newEngine(t *testing.T, s secrets) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	direction := config.PolicyDirectionThresholds{MinProbability: 0.7, MinEntryQuality: "good"}
	strategy := config.StrategyConfig{Policy: config.PolicyConfig{Long: direction, Short: direction}}
	mcp := admin.New(admin.Options{
		Path:           filepath.Join(t.TempDir(), settings.FileName),
		ExePath:        func() (string, error) { return "paper-results-mcp", nil },
		Dial:           func(context.Context, string) error { return nil },
		InterfaceAddrs: func() ([]net.Addr, error) { return nil, nil },
	})
	return router.New(
		router.WithSecretsStore(s),
		router.WithOperationalSettings(opsettings.New(opsStore{}, strategy, t.TempDir())),
		router.WithBrokerSession(tachibanaSession{}),
		router.WithMCPSettings(mcp),
	)
}

// get performs GET target (no session needed for reads).
func get(engine *gin.Engine, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

var (
	dialogIDPattern = regexp.MustCompile(`<dialog[^>]*\bid="([^"]+)"`)
	navPattern      = regexp.MustCompile(`(?s)<nav aria-label="メインナビゲーション".*?</nav>`)
	navLinkPattern  = regexp.MustCompile(`<a\b([^>]*)>([^<]*)</a>`)
	hrefPattern     = regexp.MustCompile(`\bhref="([^"]+)"`)
)

// navLink is one main-navigation link as rendered.
type navLink struct {
	Href    string
	Label   string
	Current bool // carries aria-current="page"
}

// mainNav returns the links of the rendered main navigation.
func mainNav(t *testing.T, body string) []navLink {
	t.Helper()
	nav := navPattern.FindString(body)
	if nav == "" {
		t.Fatalf("no main navigation in body: %.300s", body)
	}
	var links []navLink
	for _, m := range navLinkPattern.FindAllStringSubmatch(nav, -1) {
		href := hrefPattern.FindStringSubmatch(m[1])
		if href == nil {
			t.Fatalf("nav link without href: %s", m[0])
		}
		links = append(links, navLink{Href: href[1], Label: m[2], Current: regexp.MustCompile(`aria-current="page"`).MatchString(m[1])})
	}
	return links
}
