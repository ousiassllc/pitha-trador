package researchlabel_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The research horizon labels are never an input of trading (issue #1074):
// Policy, Risk, order placement, execution, the Paper guard, the Live path, the
// Jev prompts, RAG, the meta-model training and the self-improvement flow must
// not read them. This walks the whole non-test source tree and fails when a
// file outside the few readers/writers below names the tables, the repository,
// the recorder or the MCP query symbols.
func TestResearchHorizonLabelsAreReadOnlyByTheirRecorderAndTheResearchTool(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	// Symbols of the record: its tables, its repository and recorder, its
	// settings-free MCP view. The package paths are checked as import paths.
	record := []string{
		"research_horizon_labels", "research_label_days", "research_label_day_stats",
		"repository/researchlabel", "service/researchlabel", "bootstrap/researchlabeljob", "paperresultsmcp/researchdiag",
		"domain/researchlabel",
	}
	readers := []string{
		"internal/repository/researchlabel/",                  // the record's own repository
		"internal/service/researchlabel/",                     // the recorder
		"internal/bootstrap/researchlabeljob/",                // wires the recorder into the maintenance task
		"internal/bootstrap/services_build.go",                // registers that task (one line)
		"internal/domain/researchlabel/",                      // the record's value types
		"internal/service/paperresultsmcp/researchdiag/",      // the read-only MCP query
		"internal/service/paperresultsmcp/jev_models_tool.go", // registers that tool (next to the other read-only diagnostic tools)
		"db/migrations/",                                      // the schema
	}

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
			rel = filepath.ToSlash(rel)
			src, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			for _, sym := range record {
				if !strings.Contains(string(src), sym) {
					continue
				}
				allowed := false
				for _, prefix := range readers {
					if strings.HasPrefix(rel, prefix) {
						allowed = true
					}
				}
				if !allowed {
					t.Errorf("%s references the research horizon labels (%q): only the recorder and the read-only research_horizon_summary tool may, never Policy, Risk, orders, the Paper guard, Live, the Jev prompts, RAG, training or self-improvement", rel, sym)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// The research tool reads the record and market_snapshots / jev_decisions only
// through SELECT, and nothing in the research packages writes any table but its
// own two (the recorder's INSERT / DELETE statements name only them).
func TestResearchPackagesWriteOnlyTheirOwnTables(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	own := []string{"research_horizon_labels", "research_label_days", "research_label_day_stats"}
	for _, dir := range []string{
		"internal/repository/researchlabel", "internal/service/researchlabel",
		"internal/bootstrap/researchlabeljob", "internal/service/paperresultsmcp/researchdiag",
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
						t.Errorf("%s writes with %q to a table that is not one of the research tables: %.60s", path, verb, stmt)
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

// services_build.go is the composition root, the file where wiring the research
// record into a trading component would most plausibly be added, so it is
// allow-listed above only for the maintenance task: every line that mentions
// research must be the researchlabeljob import or the one WithMaintenanceTask
// call, and nothing else (a repository or recorder passed to Policy, Risk or
// Live would name researchlabel directly).
func TestServicesBuildOnlyRegistersTheResearchMaintenanceTask(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "bootstrap", "services_build.go"))
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	for _, line := range strings.Split(string(src), "\n") {
		if !strings.Contains(strings.ToLower(line), "research") {
			continue
		}
		switch {
		case strings.TrimSpace(line) == `"github.com/ousiassllc/pitha-trador/internal/bootstrap/researchlabeljob"`:
		case strings.Contains(line, "scheduler.WithMaintenanceTask(researchlabeljob.New(state.DB, s.Settings).Task())"):
			calls++
		default:
			t.Errorf("services_build.go mentions research outside the maintenance task registration: %q", strings.TrimSpace(line))
		}
	}
	if calls != 1 {
		t.Errorf("researchlabeljob is registered %d times, want exactly 1", calls)
	}
}
