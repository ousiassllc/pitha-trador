package researchbars_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The research daily-bar record is never an input of trading (issue #1082):
// Policy, Risk, order placement, execution, the Paper guard, the Live path, the
// Jev prompts, RAG, the Feature Engine, Scout and Trader must not read it. This
// walks the whole non-test source tree and fails when a file outside the few
// below names the tables, the repository, the collection or the MCP view.
func TestResearchDailyBarRecordIsReadOnlyByItsCollectionAndTheResearchTool(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	bars := []string{ // used by the daily-bar collection and its wiring alone
		"research_daily_bar_runs", "research_daily_bar_progress",
		"repository/researchbars", "paperresultsmcp/researchbarsdiag",
		"dailybars.NewResearch", "dailybars.ResearchConfig", "dailybars.Research ",
	}
	status := []string{"research_tachibana_status", "repository/researchstatus"} // shared with the 時価 recording (#1083)
	barsReaders := []string{
		"internal/repository/researchbars/",                      // the record's own repository
		"internal/bootstrap/dailybars/research.go",               // the collection
		"internal/bootstrap/researchwire/",                       // wires it (function-typed config, switch off by default)
		"internal/bootstrap/services.go",                         // holds it (one field)
		"internal/service/paperresultsmcp/researchbarsdiag/",     // the read-only MCP query
		"internal/service/paperresultsmcp/research_bars_tool.go", // registers that tool
		"db/migrations/", // the schema
	}
	statusReaders := append([]string{
		"internal/repository/researchstatus/",
		"internal/bootstrap/researchquotes/", "internal/bootstrap/researchquotejob/",
		"internal/service/paperresultsmcp/researchquotediag/", "internal/repository/researchquote/",
	}, barsReaders...)

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
			for syms, readers := range map[*[]string]*[]string{&bars: &barsReaders, &status: &statusReaders} {
				for _, sym := range *syms {
					if !strings.Contains(string(src), sym) {
						continue
					}
					allowed := false
					for _, prefix := range *readers {
						allowed = allowed || strings.HasPrefix(rel, prefix)
					}
					if !allowed {
						t.Errorf("%s references the research daily-bar record (%q): only the collection, its wiring and the read-only research_daily_bars_status tool may, never Policy, Risk, orders, the Paper guard, Live, the Jev prompts, RAG, the Feature Engine, Scout or Trader", rel, sym)
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// The collection writes daily_bars (the same store the nightly batch fills,
// through the dailybars.Bars interface) and its own tables, nothing else: the
// SQL statements of its repositories and of the MCP view name only them.
func TestResearchBarPackagesWriteOnlyTheirOwnTables(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	for dir, own := range map[string][]string{
		"internal/repository/researchbars":                  {"research_daily_bar_runs", "research_daily_bar_progress"},
		"internal/repository/researchstatus":                {"research_tachibana_status"},
		"internal/service/paperresultsmcp/researchbarsdiag": nil, // reads only
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
						t.Errorf("%s writes with %q to a table that is not one of its own: %.60s", path, verb, stmt)
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
