package backtestrun_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The backtest record is never an input of trading (issue #1086): Policy, Risk,
// order placement, execution, the Paper guard, the Live path, the Jev prompts,
// RAG, the meta-model training and the self-improvement flow must not read it,
// and nothing that runs a research backtest may feed back into them. This walks
// the whole non-test source tree and fails when a file outside the few
// readers/writers below names the table, the repository, the lab or the job
// manager.
func TestBacktestRunsAreReachedOnlyByTheResearchTools(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	record := []string{
		"backtest_runs", "repository/backtestrun", "service/backtestlab", "paperresultsmcp/backtestjob",
	}
	readers := []string{
		"internal/repository/backtestrun/",                       // the record's own repository
		"internal/service/backtestlab/",                          // the replay of the parameter sets
		"internal/service/paperresultsmcp/backtestjob/",          // the job manager
		"internal/service/paperresultsmcp/mcpset/researchwrite/", // the narrow write path of the record
		"internal/service/paperresultsmcp/backtesttool/",         // backtest_start / backtest_get / backtest_list
		"internal/service/paperresultsmcp/backtestview/",         // their output
		"internal/service/paperresultsmcp/tooldef/",              // hands the job manager to the tools (Env)
		"internal/service/paperresultsmcp/server.go",             // builds the job manager from the options
		"internal/service/paperresultsmcp/tools.go",              // hands it to the tools (toolEnv)
		"cmd/paper-results-mcp/",                                 // wires the source of the replay
	}
	walk(t, root, func(rel string, src []byte) {
		for _, sym := range record {
			if !strings.Contains(string(src), sym) {
				continue
			}
			allowed := false
			for _, prefix := range readers {
				allowed = allowed || strings.HasPrefix(rel, prefix)
			}
			if !allowed {
				t.Errorf("%s references the backtest record (%q): only the research tools of the Paper results MCP may, never Policy, Risk, orders, the Paper guard, Live, the Jev prompts, RAG, training or self-improvement", rel, sym)
			}
		}
	})
}

// The lab's packages write one table only: the INSERT / UPDATE statements of
// the repository and the MCP's writer name backtest_runs and nothing else, and
// the lab and the job manager hold no SQL at all.
func TestBacktestPackagesWriteOnlyTheirOwnTable(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	for _, dir := range []string{
		"internal/repository/backtestrun", "internal/service/backtestlab", "internal/service/paperresultsmcp/backtestjob",
	} {
		walk(t, filepath.Join(root, dir), func(rel string, src []byte) {
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
					if !strings.HasPrefix(stmt, "backtest_runs") || !strings.Contains(rel, "repository/backtestrun") {
						t.Errorf("%s writes with %q outside the record's repository or to another table: %.60s", rel, verb, stmt)
					}
					rest = stmt
				}
			}
		})
	}
}

// Nothing in the record's code may name the settings table: a run reads stored
// market data and never changes a setting.
func TestBacktestPackagesNeverTouchRuntimeSettings(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	for _, dir := range []string{
		"internal/repository/backtestrun", "internal/service/backtestlab", "internal/service/paperresultsmcp/backtestjob",
	} {
		walk(t, filepath.Join(root, dir), func(rel string, src []byte) {
			if strings.Contains(string(src), "runtime_settings") {
				t.Errorf("%s names runtime_settings: a backtest never reads or writes a setting", rel)
			}
		})
	}
}

// walk calls fn with the root-relative slash path and the content of every
// non-test Go file under dir (or under root/internal and root/cmd when dir is root).
func walk(t *testing.T, dir string, fn func(rel string, src []byte)) {
	t.Helper()
	root := filepath.Join("..", "..", "..")
	dirs := []string{dir}
	if dir == root {
		dirs = []string{filepath.Join(root, "internal"), filepath.Join(root, "cmd")}
	}
	for _, d := range dirs {
		if _, err := os.Stat(d); err != nil {
			continue
		}
		err := filepath.WalkDir(d, func(path string, e fs.DirEntry, err error) error {
			if err != nil || e.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
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
			fn(filepath.ToSlash(rel), src)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
