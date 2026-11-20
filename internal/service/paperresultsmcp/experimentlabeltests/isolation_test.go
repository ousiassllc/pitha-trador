package experimentlabeltests

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The label-based comparison and the unrecorded-change notice are research
// output (issue #1088): they are never an input of trading. Policy, Risk, order
// placement, execution, the Paper guard, the Live path, Jev, the meta-model
// training and the self-improvement flow must not reference them. This walks
// the whole non-test source tree and fails when a file outside their readers
// names the packages or SetWithExperiment (the one write: settings_set with a
// hypothesis, behind the write switch and the whitelist).
func TestLabelComparisonIsReachedOnlyByTheExperimentTools(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..")
	symbols := []string{"experiment/labelcompare", "experiment/unrecorded", "SetWithExperiment"}
	readers := []string{
		"internal/service/paperresultsmcp/experiment/labelcompare/",
		"internal/service/paperresultsmcp/experiment/unrecorded/",
		"internal/service/paperresultsmcp/labeltool/",              // wires them into the experiment tools
		"internal/service/paperresultsmcp/experimenttool/",         // the experiment tools' descriptions and run functions
		"internal/service/paperresultsmcp/mcpset/setexperiment.go", // the one write path
	}
	for _, dir := range []string{"internal", "cmd"} {
		walkSources(t, filepath.Join(root, dir), func(path string, src string) {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				t.Fatal(err)
			}
			rel = filepath.ToSlash(rel)
			for _, sym := range symbols {
				if strings.Contains(src, sym) && !hasPrefixIn(rel, readers) {
					t.Errorf("%s references %q: only the experiment tools may, never Policy, Risk, orders, the Paper guard, Live, Jev, training or self-improvement", rel, sym)
				}
			}
		})
	}
}

// The two research packages read through the MCP's read-only connection and
// write nothing, and import nothing of the trading side: of the application
// they may use the domain types, the day-cluster statistics, the MCP's own
// query / experiment packages and the whitelist of what the MCP may write.
func TestLabelComparisonPackagesAreReadOnlyAndTradingFree(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..")
	allowedImports := []string{
		"internal/domain", "internal/config/mcpwritesettings", "internal/repository/sqlutil",
		"internal/service/actuals", "internal/service/paperresultsmcp/experiment", "internal/service/paperresultsmcp/query",
	}
	write := regexp.MustCompile("(?i)[\"`]\\s*(INSERT|UPDATE|DELETE|DROP|ALTER|CREATE|REPLACE)\\b")
	imp := regexp.MustCompile(`"github.com/ousiassllc/pitha-trador/([^"]+)"`)
	for _, dir := range []string{
		"internal/service/paperresultsmcp/experiment/labelcompare", "internal/service/paperresultsmcp/experiment/unrecorded",
	} {
		walkSources(t, filepath.Join(root, dir), func(path, src string) {
			if m := write.FindString(src); m != "" {
				t.Errorf("%s contains a write statement (%q)", path, m)
			}
			for _, m := range imp.FindAllStringSubmatch(src, -1) {
				if !hasPrefixIn(m[1], allowedImports) {
					t.Errorf("%s imports %s, which is not on the allow-list of the research packages", path, m[1])
				}
			}
		})
	}
}

func walkSources(t *testing.T, dir string, visit func(path, src string)) {
	t.Helper()
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("source tree %s: %v", dir, err)
	}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		visit(path, string(b))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func hasPrefixIn(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}
