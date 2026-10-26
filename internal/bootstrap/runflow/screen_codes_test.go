package runflow_test

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap"
	"github.com/ousiassllc/pitha-trador/internal/bootstrap/mcpwire"
	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/web/labels"
	"github.com/ousiassllc/pitha-trador/internal/web/settingspages"
)

var (
	scriptStyle = regexp.MustCompile(`(?is)<(script|style)\b.*?</(script|style)>`)
	anyTag      = regexp.MustCompile(`(?s)<[^>]*>`)
	// readAttr picks the attributes whose value an operator reads or hears. The
	// markup-side values (data-*, href, id, name, value, class, hx-*) are what
	// the code keeps for the browser, the API and the tests, and are not
	// screen text: only the text nodes and these attributes are checked.
	readAttr = regexp.MustCompile(`\b(?:title|aria-label|placeholder|hx-confirm|data-confirm)="([^"]*)"`)
)

// screenText is what an operator can read or hear on a rendered page: the
// text nodes (help text included, it is in the DOM) and the reading attributes.
func screenText(body string) string {
	var attrs []string
	for _, m := range readAttr.FindAllStringSubmatch(body, -1) {
		attrs = append(attrs, m[1])
	}
	text := anyTag.ReplaceAllString(scriptStyle.ReplaceAllString(body, " "), " ")
	return html.UnescapeString(text + "\n" + strings.Join(attrs, "\n"))
}

// forbiddenNames is every internal name a screen must not print: the codes the
// label tables name (those with an underscore or a hyphen are never an
// ordinary word), every setting key, every MCP tool name and the config files.
func forbiddenNames() map[string]string {
	out := map[string]string{}
	add := func(kind string, names ...string) {
		for _, n := range names {
			if n != "" {
				out[n] = kind
			}
		}
	}
	for kind, table := range map[string]map[string]string{
		"exit reason": labels.ExitReasons, "Kill Switch reason": labels.KillReasons, "reject reason": labels.RejectReasons,
		"proposal reason": labels.ProposalReasons, "retraining skip": labels.RetrainSkips, "activity type": labels.ActivityTypes,
		"job queue": labels.JobQueues, "job status": labels.JobStatuses,
	} {
		for code := range table {
			if strings.ContainsAny(code, "_-") {
				add(kind, code)
			}
		}
	}
	add("setting key", opsettings.Keys()...)
	add("setting key", config.AllowedSecretKeys()...)
	// keys that are not in the registries above: the yaml fields the help used to name
	add("setting key", "initial_capital", "max_daily_loss_pct", "scan.full_scan_enabled", "scan.full_scan_max_snapshot_age_seconds")
	add("MCP tool", paperresultsmcp.ToolNames()...)
	add("config file", "strategy.yaml", "risk.yaml")
	add("database name", "model_id", "request_cost", "policy_version", "exit_reason", "reject_reason", "metalabel_models")
	return out
}

// screenTargets is every screen and fragment the app serves to an operator.
func screenTargets() []string {
	targets := []string{
		"/", "/home/trade-status", "/home/pnl", "/home/positions", "/home/attention", "/home/details", "/home/attention-badge",
		"/scanner", "/scanner/scan", "/watchlist", "/symbols/7203",
		"/order-proposals", "/order-proposals?status=rejected", "/performance", "/performance?tab=backtest", "/calibration", "/activity",
		"/setup", settingspages.BasePath,
		"/settings/mcp-panel", "/settings/mcp/experiments", "/settings/jev-prompt-panel", "/system/update-panel", "/system/status",
	}
	for i := 1; i <= 24; i++ { // the seeded order proposals' detail pages
		targets = append(targets, "/order-proposals/"+itoa(i))
	}
	for _, page := range settingspages.Pages() {
		targets = append(targets, page.Path())
	}
	return targets
}

func itoa(i int) string { return string(rune('0'+i/10)) + string(rune('0'+i%10)) }

// newSeededEngine builds the production router (bootstrap.RouterOptions, as
// both entrypoints do) over a database holding one row for every code.
func newSeededEngine(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // the MCP card's settings file
	t.Setenv("APPDATA", t.TempDir())
	state, err := bootstrap.Run(bootstrap.Config{DBPath: filepath.Join(t.TempDir(), "pitha.db")})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	t.Cleanup(func() { _ = state.Close() })
	seedSecrets(t, state)
	seedEveryCode(t, state.DB)

	secretsRepo, secrets, err := bootstrap.LoadSecrets(context.Background(), state)
	if err != nil {
		t.Fatalf("LoadSecrets: %v", err)
	}
	services := bootstrap.BuildServices(state, secrets)
	return router.New(append(bootstrap.RouterOptions(services, state, secretsRepo), mcpwire.RouterOptions(mcpwire.New())...)...)
}

// No server-rendered screen prints an internal code, a setting key, an MCP
// tool name or a config file name (issue #904, as #902's TestScreens_
// ShowNoIssuePRorFRNumbers does for development notes). The database holds one
// row for every code the label tables name and some they do not, so a code a
// screen forgot to name shows here instead of in front of the operator. The
// Lit islands (activity feed, calibration, scanner table) render in the
// browser; their bun tests check the same.
func TestScreens_ShowNoInternalCodes(t *testing.T) {
	engine := newSeededEngine(t)
	forbidden := forbiddenNames()
	names := make([]string, 0, len(forbidden))
	for n := range forbidden {
		names = append(names, n)
	}
	slices.Sort(names)

	for _, target := range screenTargets() {
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		if rec.Code == http.StatusNotFound && strings.HasPrefix(target, "/order-proposals/") {
			continue // more detail pages were asked for than rows were seeded
		}
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", target, rec.Code)
			continue
		}
		text := screenText(rec.Body.String())
		for _, name := range names {
			if i := strings.Index(text, name); i >= 0 {
				t.Errorf("GET %s prints the %s %q: …%s…", target, forbidden[name], name, snippet(text, i, len(name)))
			}
		}
	}
}

func snippet(text string, at, n int) string {
	from, to := max(at-30, 0), min(at+n+30, len(text))
	return strings.ReplaceAll(text[from:to], "\n", " ")
}

// The screens that name a code keep its value in the markup (data-* attributes)
// and the API: the label is a display-only layer (issue #904).
func TestScreens_KeepCodeValuesInTheMarkup(t *testing.T) {
	engine := newSeededEngine(t)
	body := func(target string) string {
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		return rec.Body.String()
	}
	for target, wants := range map[string][]string{
		"/home/details":    {`data-code="jev_none"`, `data-code="entry_quality_below_threshold"`},
		"/order-proposals": {`data-status="waiting_session"`},
		"/activity":        {`data-event-type="job"`},
	} {
		got := body(target)
		for _, want := range wants {
			if !strings.Contains(got, want) {
				t.Errorf("GET %s lost %s", target, want)
			}
		}
	}
	// the API and the MCP keep returning the raw codes
	for target, want := range map[string]string{
		"/api/v1/positions?status=closed": `"exit_reason":"take_profit"`,
	} {
		if got := body(target); !strings.Contains(got, want) {
			t.Errorf("GET %s lost %s: %.300s", target, want, got)
		}
	}
}
