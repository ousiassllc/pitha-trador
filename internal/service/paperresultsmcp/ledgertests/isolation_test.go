package ledgertests

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The candidate ledger and the promotion checklist are research outputs (issues #1090, #1091): nothing
// that trades may read them, and they may not write or reach trading code. This walks the non-test source
// tree and pins (1) who references the two packages, (2) what the two packages import, and (3) the tables
// the two packages' SQL reads (SELECT only; no write verb at all).
const root = "../../../.."

var ownPackages = []string{"internal/service/paperresultsmcp/ledgerdiag", "internal/service/paperresultsmcp/promotiondiag"}

func walkGo(t *testing.T, dir string, f func(rel string, src []byte)) {
	t.Helper()
	err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		rel, _ := filepath.Rel(root, path)
		f(filepath.ToSlash(rel), src)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Only the MCP's research tool registration (and the two packages themselves) may name the packages: not
// Policy, Risk, orders, execution, the Paper guard, Live, Jev, RAG, training or self-improvement.
func TestOnlyTheResearchToolRegistrationReferencesTheLedgerAndChecklist(t *testing.T) {
	t.Parallel()
	allowed := append([]string{"internal/service/paperresultsmcp/research_tools.go"}, ownPackages...)
	for _, dir := range []string{"internal", "cmd"} {
		walkGo(t, dir, func(rel string, src []byte) {
			if !strings.Contains(string(src), "paperresultsmcp/ledgerdiag") && !strings.Contains(string(src), "paperresultsmcp/promotiondiag") {
				return
			}
			for _, prefix := range allowed {
				if strings.HasPrefix(rel, prefix) {
					return
				}
			}
			t.Errorf("%s references the candidate ledger / promotion checklist: only the MCP's research tool registration may", rel)
		})
	}
}

// The two packages import only what a read-only analysis needs: settings loaders, the domain constants,
// the sqlutil time codec, the live-version resolver, the statistics and the MCP's own read-only helpers.
func TestPackagesImportOnlyReadOnlyHelpers(t *testing.T) {
	t.Parallel()
	const module = "github.com/ousiassllc/pitha-trador/"
	allowed := map[string]bool{
		"internal/config/jevshadowsettings": true, "internal/config/metalabelsettings": true, "internal/config/promotionsettings": true,
		"internal/domain": true, "internal/repository/sqlutil": true, "internal/service/jev": true,
		"internal/service/metaablation/stats":                true,
		"internal/service/paperresultsmcp/evaldiag":          true,
		"internal/service/paperresultsmcp/ledgerdiag":        true,
		"internal/service/paperresultsmcp/query":             true,
		"internal/service/paperresultsmcp/query/shadowquery": true,
	}
	for _, pkg := range ownPackages {
		walkGo(t, pkg, func(rel string, src []byte) {
			f, err := parser.ParseFile(token.NewFileSet(), rel, src, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, imp := range f.Imports {
				path, _ := strconv.Unquote(imp.Path.Value)
				if rest, ok := strings.CutPrefix(path, module); ok && !allowed[rest] {
					t.Errorf("%s imports %s: not on the read-only allow-list", rel, rest)
				}
			}
		})
	}
}

var (
	tableRef  = regexp.MustCompile(`\b(?:FROM|JOIN)\s+([a-z_]+)`)
	writeVerb = regexp.MustCompile("[`\"](?:INSERT|UPDATE|DELETE|REPLACE|CREATE|DROP|ALTER|ATTACH|VACUUM|PRAGMA)\\b")
)

// The SQL of the two packages is SELECTs over a fixed list of tables (never the raw-body columns of any).
func TestSQLIsReadOnlyAndOverTheDocumentedTables(t *testing.T) {
	t.Parallel()
	tables := map[string]bool{
		"jev_decisions": true, "market_snapshots": true, "research_horizon_labels": true, "jev_shadow_decisions": true,
		"jev_trial_decisions": true, "research_model_predictions": true, "research_model_runs": true, "runtime_settings": true,
	}
	seen := map[string]bool{}
	for _, pkg := range ownPackages {
		walkGo(t, pkg, func(rel string, src []byte) {
			text := string(src)
			if writeVerb.MatchString(text) {
				t.Errorf("%s contains a write statement", rel)
			}
			for _, m := range tableRef.FindAllStringSubmatch(text, -1) {
				seen[m[1]] = true
				if !tables[m[1]] {
					t.Errorf("%s reads table %s, which is not documented for the ledger / checklist", rel, m[1])
				}
			}
			for _, body := range []string{"response_json", "state_json", "raw_data_json", "error_excerpt"} {
				if strings.Contains(text, body) {
					t.Errorf("%s names the raw body column %s", rel, body)
				}
			}
		})
	}
	for _, table := range []string{"research_model_predictions", "jev_trial_decisions", "jev_shadow_decisions", "research_horizon_labels"} {
		if !seen[table] {
			t.Errorf("the table scan never saw %s: the check is vacuous", table)
		}
	}
}
