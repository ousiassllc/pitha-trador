package researchindex_test

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const modulePath = "github.com/ousiassllc/pitha-trador/"

// The research index record is never an input of trading (issue #1084): Feature
// Engine (and its market context), Policy, Risk, order placement, execution, the
// Paper guard, the Live path, the Jev prompts, RAG, the meta-model training and
// the self-improvement flow must not read it. This walks the whole non-test source tree
// and fails when a file outside the few readers/writers below names the tables,
// the repository, the recorder, the settings or the MCP query symbols.
func TestResearchIndexRecordIsReadOnlyByItsRecorderAndTheResearchTools(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	record := []string{
		"research_index_minutes", "research_index_days",
		"repository/researchindex", "service/researchindex", "bootstrap/researchindexjob", "domain/researchindex",
		"config/researchindexsettings", "paperresultsmcp/indexdiag", "research.index.",
	}
	readers := []string{
		"internal/repository/researchindex/",                       // the record's own repository
		"internal/service/researchindex/",                          // the recorder
		"internal/bootstrap/researchindexjob/",                     // wires the recorder into the maintenance task
		"internal/bootstrap/researchextrawire/",                    // registers that task (one line)
		"internal/domain/researchindex/",                           // the record's value types
		"internal/config/researchindexsettings/",                   // the switch and the codes
		"internal/config/researchrecordsettings/",                  // the one view of the FX and index settings that opsettings reaches them through
		"internal/service/paperresultsmcp/indexdiag/",              // the read-only MCP queries and the join
		"internal/service/paperresultsmcp/jev_models_tool.go",      // registers index_series_summary (next to the other read-only research tools)
		"internal/service/paperresultsmcp/evaldiag/",               // jev_evaluation's optional index_symbol context (read-only)
		"internal/service/opsettings/",                             // the Settings screen's storage of the two keys
		"internal/config/mcpwritesettings/mcpwritesettings.go",     // the switch on the MCP settings whitelist
		"internal/web/handler/settings/settings_ops_researchfx.go", // the Settings group (shared with the FX record)
		"db/migrations/", // the schema
	}
	walkSources(t, root, func(rel, text string) {
		for _, sym := range record {
			if !strings.Contains(text, sym) {
				continue
			}
			allowed := false
			for _, prefix := range readers {
				allowed = allowed || strings.HasPrefix(rel, prefix)
			}
			if !allowed {
				t.Errorf("%s references the research index record (%q): only the recorder and the read-only research tools may, never Feature Engine, Policy, Risk, orders, the Paper guard, Live, the Jev prompts, RAG, training or self-improvement", rel, sym)
			}
		}
	})
}

// The trading-side packages are named one by one so that a new file there that
// reads the record is reported with its package, and an emptied or renamed
// package cannot make the check vacuous.
func TestTradingSidePackagesDoNotMentionTheIndexRecord(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	dirs := []string{
		"internal/service/featureengine", "internal/service/featureengine/marketcontext", "internal/service/policy", "internal/service/risk",
		"internal/service/execution", "internal/service/broker", "internal/service/marketdata", "internal/service/jev", "internal/service/jevprompt",
		"internal/service/rag", "internal/service/orderflow", "internal/service/metalabel", "internal/service/selfimprove", "internal/service/calibration",
		"internal/service/screener", "internal/bootstrap/marketdatajob", "internal/bootstrap/rankingwatch", "internal/bootstrap/paperexec", "internal/bootstrap/paperrisk",
	}
	for _, dir := range dirs {
		files := 0
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			files++
			src, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			for _, sym := range []string{"researchindex", "research_index", "research.index"} {
				if strings.Contains(string(src), sym) {
					t.Errorf("%s mentions %q", path, sym)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if files == 0 {
			t.Errorf("%s has no source files: the check is vacuous", dir)
		}
	}
}

// The record makes no kabu registration request: the packages that fetch and
// store it (the recorder, its repository, its job) depend on no broker, kabu or
// tachibana client, no Feature Engine / Policy / Risk / order / Jev code, and so
// cannot consume one of the kabu registration slots or reach a trading path.
func TestRecordingPackagesDependOnNoBrokerOrTradingCode(t *testing.T) {
	forbidden := []string{
		"internal/service/broker", "internal/service/marketdata", "internal/service/featureengine", "internal/service/policy", "internal/service/risk",
		"internal/service/execution", "internal/service/jev", "internal/service/jevprompt", "internal/service/rag", "internal/service/orderflow",
		"internal/service/screener", "internal/service/symbolcache", "internal/service/metalabel", "internal/service/selfimprove", "internal/service/calibration",
		"internal/bootstrap/marketdatajob", "internal/bootstrap/rankingwatch", "internal/bootstrap/heldposition", "internal/bootstrap/paperexec", "internal/bootstrap/paperrisk",
	}
	for _, pkg := range []string{
		"internal/domain/researchindex", "internal/repository/researchindex", "internal/service/researchindex", "internal/bootstrap/researchindexjob",
		"internal/config/researchindexsettings",
	} {
		// The go command is a fixed binary; the argument is a constant package path.
		out, err := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}}", modulePath+pkg).Output()
		if err != nil {
			t.Fatalf("go list %s: %v", pkg, err)
		}
		for _, dep := range strings.Fields(string(out)) {
			for _, f := range forbidden {
				if dep == modulePath+f || strings.HasPrefix(dep, modulePath+f+"/") {
					t.Errorf("%s depends on %s: the index record must not reach a broker or a trading path", pkg, dep)
				}
			}
		}
	}
}

// The research index packages write nothing but their own two tables.
func TestResearchIndexPackagesWriteOnlyTheirOwnTables(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	own := []string{"research_index_minutes", "research_index_days"}
	for _, dir := range []string{
		"internal/repository/researchindex", "internal/service/researchindex", "internal/bootstrap/researchindexjob", "internal/service/paperresultsmcp/indexdiag",
	} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			src, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			text := string(src)
			for _, verb := range []string{"INSERT OR IGNORE INTO ", "INSERT OR REPLACE INTO ", "INSERT INTO ", "UPDATE ", "DELETE FROM ", "DROP ", "ALTER TABLE ", "CREATE "} {
				for rest := text; ; {
					i := strings.Index(rest, "`"+verb)
					j := strings.Index(rest, "\""+verb)
					if i < 0 || (j >= 0 && j < i) {
						i = j
					}
					if i < 0 {
						break
					}
					stmt := rest[i+1+len(verb):]
					ok := false
					for _, table := range own {
						ok = ok || strings.HasPrefix(stmt, table)
					}
					if !ok {
						t.Errorf("%s writes with %q to a table that is not one of the research index tables: %.60s", path, verb, stmt)
					}
					rest = stmt
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// The read-only tool package never writes at all: no statement but SELECT.
func TestIndexDiagIssuesOnlySelects(t *testing.T) {
	dir := filepath.Join("..", "..", "service", "paperresultsmcp", "indexdiag")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, verb := range []string{"INSERT", "UPDATE ", "DELETE", "DROP ", "ALTER ", "CREATE ", "ExecContext", ".Exec(", "BeginTx"} {
			if strings.Contains(string(src), verb) {
				t.Errorf("indexdiag/%s contains %q: the tool package must only read", e.Name(), verb)
			}
		}
	}
}

// services_build.go is the composition root, where wiring the record into a
// trading component would most plausibly be added, so it must not mention the
// index record at all; the one place that does is researchextrawire, where every
// line that mentions it must be the researchindexjob import or the one
// WithMaintenanceTask call.
func TestOnlyTheResearchWireRegistersTheIndexMaintenanceTask(t *testing.T) {
	build, err := os.ReadFile(filepath.Join("..", "..", "bootstrap", "services_build.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(build), "\n") {
		if strings.Contains(strings.ToLower(line), "researchindex") {
			t.Errorf("services_build.go mentions the index record: %q", strings.TrimSpace(line))
		}
	}
	src, err := os.ReadFile(filepath.Join("..", "..", "bootstrap", "researchextrawire", "researchextrawire.go"))
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	for _, line := range strings.Split(string(src), "\n") {
		if !strings.Contains(strings.ToLower(line), "researchindex") {
			continue
		}
		switch {
		case strings.TrimSpace(line) == `"github.com/ousiassllc/pitha-trador/internal/bootstrap/researchindexjob"`:
		case strings.Contains(line, "scheduler.WithMaintenanceTask(researchindexjob.New(w.db, w.settings).Task())"):
			calls++
		default:
			t.Errorf("researchextrawire.go mentions the index record outside the maintenance task registration: %q", strings.TrimSpace(line))
		}
	}
	if calls != 1 {
		t.Errorf("researchindexjob is registered %d times, want exactly 1", calls)
	}
}

func walkSources(t *testing.T, root string, visit func(rel, text string)) {
	t.Helper()
	for _, dir := range []string{"internal", "cmd"} {
		abs := filepath.Join(root, dir)
		if _, err := os.Stat(abs); err != nil {
			continue
		}
		err := filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				return rerr
			}
			src, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			visit(filepath.ToSlash(rel), string(src))
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
