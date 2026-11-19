package evalrecord_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The daily evaluation record is never an input of trading (issue #1089): Policy, Risk, order
// placement, execution, the Paper guard, the Live path, the Jev prompts, RAG, the meta-model
// training and the self-improvement flow must not read it. This walks the whole non-test source
// tree and fails when a file outside the few writers/readers below names the table, the
// repository, the recorder or the MCP tool's package.
func TestEvaluationRecordIsReadOnlyByItsRecorderAndTheTrendTool(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	record := []string{
		"jev_evaluation_daily",
		"repository/evalrecord", "service/evalrecord", "paperresultsmcp/trenddiag", "bootstrap/nightlyjobs",
	}
	readers := []string{
		"internal/repository/evalrecord/",                    // the record's own repository
		"internal/service/evalrecord/",                       // the recorder (and its test fixture)
		"internal/bootstrap/nightlyjobs/",                    // wires the recorder into the maintenance task
		"internal/bootstrap/services_build.go",               // registers that task (one line)
		"internal/service/researchjobstatus/",                // the Settings card's read-only status: the task's name only, never the table
		"internal/service/paperresultsmcp/trenddiag/",        // the read-only MCP query
		"internal/service/paperresultsmcp/research_tools.go", // registers that tool next to the other research tools
		"db/migrations/",                                     // the schema
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
			code := withoutComments(string(src))
			for _, sym := range record {
				if !strings.Contains(code, sym) {
					continue
				}
				allowed := false
				for _, prefix := range readers {
					allowed = allowed || strings.HasPrefix(rel, prefix)
				}
				if !allowed {
					t.Errorf("%s references the daily evaluation record (%q): only the recorder, its task and the read-only jev_evaluation_trend tool may, never Policy, Risk, orders, the Paper guard, Live, the Jev prompts, RAG, training or self-improvement", rel, sym)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// Everything that writes in the record's packages writes jev_evaluation_daily and nothing
// else: the repository's, the recorder's, the task's and the MCP tool's INSERT / UPDATE / DELETE
// statements name only that table.
func TestEvaluationRecordPackagesWriteOnlyTheirOwnTable(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	const own = "jev_evaluation_daily"
	wrote := 0
	for _, dir := range []string{
		"internal/repository/evalrecord", "internal/service/evalrecord",
		"internal/bootstrap/nightlyjobs", "internal/service/paperresultsmcp/trenddiag",
	} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			// The nightly task of the research model (model.go, issue #1092) is not the record's.
			if filepath.Base(path) == "model.go" || filepath.Base(path) == "doc.go" || strings.Contains(path, "evalfixture") {
				return nil
			}
			src, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			text := string(src)
			for _, verb := range []string{"INSERT OR IGNORE INTO ", "INSERT OR REPLACE INTO ", "INSERT INTO ", "REPLACE INTO ", "UPDATE ", "DELETE FROM ", "DROP ", "ALTER TABLE ", "CREATE ", "PRAGMA ", "VACUUM"} {
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
					if !strings.HasPrefix(stmt, own) {
						t.Errorf("%s writes with %q to something that is not %s: %.60s", path, verb, own, stmt)
					}
					wrote++
					rest = stmt
				}
			}
			// Only the repository executes statements: a write path elsewhere in these packages would escape the scan above.
			if !strings.Contains(filepath.ToSlash(path), "internal/repository/evalrecord/") {
				for _, call := range []string{".Exec(", ".ExecContext(", ".BeginTx(", ".PrepareContext("} {
					if strings.Contains(text, call) {
						t.Errorf("%s calls %s: only repository/evalrecord may execute SQL", path, call)
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if wrote != 2 { // the repository's INSERT OR IGNORE and its DELETE FROM (purge)
		t.Errorf("found %d write statements, want 2 (insert, purge): the scan is vacuous or a statement was added", wrote)
	}
}

// withoutComments drops the lines that are only a // comment: documentation may name the
// table, only code is a use of it.
func withoutComments(src string) string {
	var b strings.Builder
	for _, line := range strings.Split(src, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "//") {
			b.WriteString(line + "\n")
		}
	}
	return b.String()
}

// services_build.go is the composition root, the file where wiring the record into a trading
// component would most plausibly be added, so it is allow-listed above only for the maintenance
// tasks: once the import and the scheduler.WithMaintenanceTask(nightlyjobs.X(state.DB, s.Settings))
// registrations are removed, nothing in it may name nightlyjobs, the record's repository or the
// recorder any more.
func TestServicesBuildOnlyRegistersTheNightlyMaintenanceTasks(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "bootstrap", "services_build.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(src), `"github.com/ousiassllc/pitha-trador/internal/bootstrap/nightlyjobs"`, "")
	text = regexp.MustCompile(`scheduler\.WithMaintenanceTask\(nightlyjobs\.\w+\(state\.DB, s\.Settings\)\)`).ReplaceAllString(text, "")
	for _, line := range strings.Split(text, "\n") {
		for _, sym := range []string{"nightlyjobs", "evalrecord", "jev_evaluation_daily"} {
			if strings.Contains(line, sym) {
				t.Errorf("services_build.go mentions %q outside the maintenance task registration: %q", sym, strings.TrimSpace(line))
			}
		}
	}
}
