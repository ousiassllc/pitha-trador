package jquants_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The J-Quants data is research-only and private-use (issue #1093): nothing that
// decides, places or sizes a trade, and nothing that feeds the models, may read it.
// The test walks the source of those packages and fails when one names the tables,
// the repository, the adapter, the wiring or the settings of the adapter.
func TestTradingAndModelPackagesNeverReferenceJQuants(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	symbols := []string{"research_jquants_", "repository/jquants", "marketdata/jquants", "bootstrap/jquantswire", "domain/jquants", "jquantssettings", "jquantsdiag", "JQuantsAPIKey", "JQUANTS_API_KEY", "jquantsrelabel"}
	guarded := []string{
		"internal/service/policy", "internal/service/risk", "internal/service/execution", "internal/service/broker", "internal/service/orderflow",
		"internal/service/jev", "internal/service/jevprompt", "internal/service/jevtrial", "internal/service/calibration", "internal/service/rag",
		"internal/service/metalabel", "internal/service/selfimprove", "internal/service/backtest", "internal/service/featureengine", "internal/service/screener",
		"internal/service/regime", "internal/service/fillmodel", "internal/service/exitalign", "internal/service/actuals", "internal/service/disclosureskip",
		"internal/service/newsfeed", "internal/service/marketdata/kabu", "internal/repository/trading", "internal/repository/judgement", "internal/repository/calibration",
		"internal/repository/metalabel", "internal/repository/counterlabel", "internal/repository/market", "internal/repository/decisionsnapshot", "internal/repository/actuals",
	}
	for _, dir := range guarded {
		abs := filepath.Join(root, dir)
		if _, err := os.Stat(abs); err != nil {
			continue
		}
		err := filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			src, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			for _, sym := range symbols {
				if strings.Contains(string(src), sym) {
					t.Errorf("%s references the J-Quants adapter (%q): its data is research-only and never a trading or model input", path, sym)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// The composition root names the adapter only in researchwire, where it registers
// the maintenance task and holds the key: services*.go do not mention J-Quants, and
// every line of researchwire.go that does is the package comment, the import, the key
// field or the one task registration.
func TestCompositionRootOnlyRegistersTheMaintenanceTaskAndHoldsTheKey(t *testing.T) {
	bootstrap := filepath.Join("..", "..", "bootstrap")
	for _, name := range []string{"services_build.go", "services.go"} {
		src, err := os.ReadFile(filepath.Join(bootstrap, name))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.ToLower(string(src)), "jquants") {
			t.Errorf("%s mentions J-Quants: only researchwire wires it", name)
		}
	}
	allowed := []string{
		`"github.com/ousiassllc/pitha-trador/internal/bootstrap/jquantswire"`,
		"jquants  string", "scheduler.WithMaintenanceTask(jquantswire.New(w.db, w.settings, w.jquants).Task())(s)",
		"jquants: secrets.JQuantsAPIKey",
	}
	src, err := os.ReadFile(filepath.Join(bootstrap, "researchwire", "researchwire.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(src), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "//") || !strings.Contains(strings.ToLower(line), "jquants") {
			continue // comments name the adapter freely; code lines are checked
		}
		ok := false
		for _, a := range allowed {
			ok = ok || strings.Contains(line, a)
		}
		if !ok {
			t.Errorf("researchwire.go mentions J-Quants outside the task registration and the key: %q", strings.TrimSpace(line))
		}
	}
}

// The repository writes only the research_jquants_* tables and touches no table of
// another source: every statement names one of them, and market_snapshots,
// daily_bars, disclosure_items and the jev_* tables do not appear in its code.
func TestRepositoryTouchesOnlyItsOwnTables(t *testing.T) {
	forbidden := regexp.MustCompile(`\b(market_snapshots|daily_bars|disclosure_items|jev_[a-z]+)\b`)
	verb := regexp.MustCompile("(?i)(INSERT INTO|INSERT OR IGNORE INTO|INSERT OR REPLACE INTO|DELETE FROM|UPDATE) +([a-z_]+)")
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		src, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatal(err)
		}
		// Strip comments: they may name the tables the package keeps its hands off.
		code := regexp.MustCompile(`(?m)^\s*//.*$`).ReplaceAllString(string(src), "")
		if m := forbidden.FindString(code); m != "" {
			t.Errorf("%s names %q", e.Name(), m)
		}
		for _, m := range verb.FindAllStringSubmatch(code, -1) {
			if strings.EqualFold(m[2], "set") { // ON CONFLICT ... DO UPDATE SET
				continue
			}
			if !strings.HasPrefix(m[2], "research_jquants_") {
				t.Errorf("%s writes to %q, not a research_jquants_ table", e.Name(), m[2])
			}
		}
	}
}
