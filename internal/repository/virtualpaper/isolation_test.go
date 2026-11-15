package virtualpaper_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The virtual Paper record is never an input of trading (issue #1087): Policy,
// Risk, order placement, execution, the Paper guard, the Live path, Jev,
// RAG, the meta-model training and the self-improvement flow must not read it,
// and the virtual Paper code must not reach their write paths. This walks the
// whole non-test source tree and fails when a file outside the few readers /
// writers below names the tables, the repository, the service, the job or the
// MCP tool symbols.
func TestVirtualPaperIsReadOnlyByItsRecorderAndTheResearchTools(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	record := []string{
		"virtual_paper_profiles", "virtual_paper_trades", "virtual_paper_days",
		"repository/virtualpaper", "service/virtualpaper", "bootstrap/virtualpaperjob",
	}
	readers := []string{
		"internal/repository/virtualpaper/",                      // the record's own repository
		"internal/service/virtualpaper/",                         // profiles, the recorder and the comparison
		"internal/bootstrap/virtualpaperjob/",                    // wires the recorder into the maintenance task
		"internal/bootstrap/services_build.go",                   // registers that task (one line)
		"internal/service/paperresultsmcp/virtualpapertool/",     // the three MCP tools
		"internal/service/paperresultsmcp/mcpset/researchwrite/", // their two narrow writes
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
				t.Errorf("%s references the virtual Paper record (%q): only the recorder, the job and the MCP tools may, never Policy, Risk, orders, the Paper guard, Live, Jev, RAG, training or self-improvement", rel, sym)
			}
		}
	})
}

// The virtual Paper packages reach no trading component: they import none of
// the write paths of orders, positions, capital, risk, policy, execution, Jev or
// the self-improvement flow, and none of the settings stores, so they cannot
// write what they never name.
func TestVirtualPaperPackagesImportNoTradingComponent(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	forbidden := []string{
		"internal/repository/trading", "internal/repository/system", "internal/repository/judgement",
		"internal/service/execution", "internal/service/policy", "internal/service/risk", "internal/service/jev",
		"internal/service/selfimprove", "internal/service/paper", "internal/service/broker", "internal/service/scheduler/market",
	}
	for _, dir := range []string{"internal/repository/virtualpaper", "internal/service/virtualpaper"} {
		walkDir(t, filepath.Join(root, dir), func(path, text string) {
			for _, line := range strings.Split(text, "\n") {
				if !strings.HasPrefix(strings.TrimSpace(line), "\"github.com/ousiassllc/pitha-trador/") && !strings.Contains(line, " \"github.com/ousiassllc/pitha-trador/") {
					continue
				}
				for _, f := range forbidden {
					if strings.Contains(line, "pitha-trador/"+f) {
						t.Errorf("%s imports %s: the virtual Paper record must not depend on a trading component", path, f)
					}
				}
			}
		})
	}
}

// The virtual Paper code writes with INSERT / UPDATE / DELETE only into its own
// three tables (none of paper_orders, positions, capital, signals or settings).
func TestVirtualPaperPackagesWriteOnlyTheirOwnTables(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	own := []string{"virtual_paper_profiles", "virtual_paper_trades", "virtual_paper_days"}
	for _, dir := range []string{"internal/repository/virtualpaper", "internal/service/virtualpaper", "internal/bootstrap/virtualpaperjob"} {
		walkDir(t, filepath.Join(root, dir), func(path, text string) {
			for _, verb := range []string{"INSERT OR IGNORE INTO ", "INSERT OR REPLACE INTO ", "INSERT INTO ", "UPDATE ", "DELETE FROM ", "DROP ", "ALTER TABLE ", "CREATE ", "REPLACE INTO "} {
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
						t.Errorf("%s writes with %q to a table that is not a virtual_paper_ table: %.60s", path, verb, stmt)
					}
					rest = stmt
				}
			}
		})
	}
}

// services_build.go is the composition root, the file where wiring the record into
// a trading component would most plausibly be added, so every line that mentions
// virtualpaper must be the import or the one WithMaintenanceTask call.
func TestServicesBuildOnlyRegistersTheVirtualPaperMaintenanceTask(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "bootstrap", "services_build.go"))
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	for _, line := range strings.Split(string(src), "\n") {
		if !strings.Contains(strings.ToLower(line), "virtualpaper") {
			continue
		}
		switch {
		case strings.TrimSpace(line) == `"github.com/ousiassllc/pitha-trador/internal/bootstrap/virtualpaperjob"`:
		case strings.Contains(line, "scheduler.WithMaintenanceTask(virtualpaperjob.New(state.DB, state.Strategy, state.Risk).Task())"):
			calls++
		default:
			t.Errorf("services_build.go mentions virtualpaper outside the maintenance task registration: %q", strings.TrimSpace(line))
		}
	}
	if calls != 1 {
		t.Errorf("virtualpaperjob is registered %d times, want exactly 1", calls)
	}
}

func walkSources(t *testing.T, root string, fn func(rel, text string)) {
	t.Helper()
	for _, dir := range []string{"internal", "cmd"} {
		abs := filepath.Join(root, dir)
		if _, err := os.Stat(abs); err != nil {
			continue
		}
		walkDir(t, abs, func(path, text string) {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				t.Fatal(err)
			}
			fn(filepath.ToSlash(rel), text)
		})
	}
}

// walkDir calls fn with every non-test .go file under dir.
func walkDir(t *testing.T, dir string, fn func(path, text string)) {
	t.Helper()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		fn(path, string(src))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
