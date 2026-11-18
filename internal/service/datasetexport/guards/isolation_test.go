package guards_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

var repoRoot = filepath.Join("..", "..", "..", "..")

// walkGo calls visit for every non-test .go file under dir (relative to the repository root).
func walkGo(t *testing.T, dir string, visit func(rel string, src []byte)) {
	t.Helper()
	err := filepath.WalkDir(filepath.Join(repoRoot, dir), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		rel, _ := filepath.Rel(repoRoot, path)
		visit(filepath.ToSlash(rel), src)
		return nil
	})
	if err != nil {
		t.Fatalf("%s is gone or moved: update this guard (%v)", dir, err)
	}
}

func importsOf(t *testing.T, src []byte) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "x.go", src, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, spec := range file.Imports {
		p, _ := strconv.Unquote(spec.Path.Value)
		out = append(out, p)
	}
	return out
}

// The dataset export is a research tool, never an input of trading: nothing that trades, sizes,
// risks, orders, labels, trains or prompts names it, and it does not import any of them.
func TestTradingCodeNeverReferencesTheDatasetExport(t *testing.T) {
	consumers := []string{
		"internal/service/policy", "internal/service/risk", "internal/service/execution", "internal/service/exitalign",
		"internal/service/metalabel", "internal/repository/metalabel", "internal/service/jev",
		"internal/service/rag", "internal/service/featureengine", "internal/service/calibration", "internal/repository/calibration",
		"internal/service/selfimprove", "internal/service/backtest", "internal/service/broker",
		"internal/repository/trading", "internal/repository/decisiontrade", "internal/repository/decisionsnapshot",
	}
	symbols := []string{"datasetexport", "exportdiag", "training_dataset_export"}
	for _, dir := range consumers {
		walkGo(t, dir, func(rel string, src []byte) {
			for _, sym := range symbols {
				if strings.Contains(string(src), sym) {
					t.Errorf("%s references the training dataset export (%q): its output must never reach trading, RAG, Calibration or training", rel, sym)
				}
			}
		})
	}
}

// Only the MCP tool's adapter imports the export, and the export imports nothing of the trading side.
func TestOnlyTheAdapterImportsTheExportAndItImportsLittle(t *testing.T) {
	const self = "github.com/ousiassllc/pitha-trador/internal/service/datasetexport"
	for _, dir := range []string{"internal", "cmd"} {
		walkGo(t, dir, func(rel string, src []byte) {
			if strings.HasPrefix(rel, "internal/service/datasetexport/") {
				return
			}
			if slices.Contains(importsOf(t, src), self) && !strings.HasPrefix(rel, "internal/service/paperresultsmcp/exportdiag/") {
				t.Errorf("%s imports the dataset export: only paperresultsmcp/exportdiag may", rel)
			}
		})
	}
	allowed := map[string]bool{
		"github.com/ousiassllc/pitha-trador/internal/config/researchjobsettings":    true, // the switch's key and validation
		"github.com/ousiassllc/pitha-trador/internal/domain":                        true, // the answer vocabulary
		"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil":            true, // the stored time layout
		"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query": true, // the read-only Q and RejectCode
	}
	walkGo(t, "internal/service/datasetexport", func(rel string, src []byte) {
		for _, imp := range importsOf(t, src) {
			if strings.HasPrefix(imp, "github.com/ousiassllc/") && !allowed[imp] {
				t.Errorf("%s imports %s: the export may import only %v", rel, imp, slices.Collect(func(yield func(string) bool) {
					for k := range allowed {
						if !yield(k) {
							return
						}
					}
				}))
			}
		}
	})
}

// The package writes no table (no statement but a read) and reads only the tables it documents.
func TestDatasetExportReadsOnlyItsTablesAndWritesNone(t *testing.T) {
	tables := []string{
		"jev_decisions", "market_snapshots", "instruments", "trade_signals", "calibration_outcomes", "research_horizon_labels",
		"counterfactual_labels", "jev_shadow_decisions", "jev_trial_decisions", "runtime_settings", "pragma_database_list",
	}
	from := regexp.MustCompile(`\b(?:FROM|JOIN) ([a-z_]+)`)
	for _, dir := range []string{"internal/service/datasetexport", "internal/service/paperresultsmcp/exportdiag"} {
		walkGo(t, dir, func(rel string, src []byte) {
			text := string(src)
			for _, verb := range []string{"INSERT ", "UPDATE ", "DELETE ", "REPLACE ", "DROP ", "ALTER ", "CREATE ", "PRAGMA ", "ATTACH ", "VACUUM"} {
				for _, quote := range []string{"`", "\""} {
					if strings.Contains(text, quote+verb) || strings.Contains(text, quote+"\n"+verb) {
						t.Errorf("%s issues a %s statement: the export only reads", rel, strings.TrimSpace(verb))
					}
				}
			}
			for _, m := range from.FindAllStringSubmatch(text, -1) {
				if !slices.Contains(tables, m[1]) {
					t.Errorf("%s reads %s, which is not one of the export's tables %v", rel, m[1], tables)
				}
			}
		})
	}
}

// Files are created only by store.go (one place to audit: folder 0700, files 0600, temp + rename, no overwrite).
func TestOnlyTheStoreWritesFiles(t *testing.T) {
	calls := regexp.MustCompile(`\bos\.(WriteFile|Create|OpenFile|Rename|MkdirAll|Mkdir|MkdirTemp|RemoveAll|Remove|Chmod|Symlink|Truncate)\(`)
	walkGo(t, "internal/service/datasetexport", func(rel string, src []byte) {
		if m := calls.FindString(string(src)); m != "" && !strings.HasSuffix(rel, "/store.go") {
			t.Errorf("%s calls %s: only store.go writes files", rel, m)
		}
	})
	walkGo(t, "internal/service/paperresultsmcp/exportdiag", func(rel string, src []byte) {
		if strings.Contains(string(src), "\"os\"") || strings.Contains(string(src), "path/filepath") {
			t.Errorf("%s touches the filesystem: the adapter must not (no path is ever its argument)", rel)
		}
	})
}
