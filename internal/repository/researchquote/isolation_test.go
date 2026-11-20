package researchquote_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The research 時価 samples are never an input of trading (issue #1083): the
// Feature Engine, Scout, Trader, Paper, Policy, Risk, order placement, execution,
// the Live path, the Jev prompts and RAG must not read them. This walks the whole
// non-test source tree and fails when a file outside the few readers/writers below
// names the table, the repository, the recorder, the retention task or the MCP
// query.
func TestResearchQuoteSamplesAreReadOnlyByTheirRecorderRetentionAndTheResearchTool(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	record := []string{
		"research_quote_samples", "repository/researchquote", "bootstrap/researchquotes", "bootstrap/researchquotejob",
		"paperresultsmcp/researchquotediag",
	}
	allowed := []string{
		"internal/repository/researchquote/",                       // the record's own repository
		"internal/bootstrap/researchquotes/",                       // the recorder
		"internal/bootstrap/researchquotejob/",                     // the retention task
		"internal/service/snapshotarchive/",                        // archives the table before the task deletes it
		"internal/service/paperresultsmcp/researchquotediag/",      // the read-only MCP query
		"internal/service/paperresultsmcp/research_quotes_tool.go", // registers that tool
		"internal/repository/researchindex/",                       // the index record copies the index codes' rows (SELECT only; its isolation test pins that it writes only its own tables)
		"internal/service/paperresultsmcp/indexdiag/",              // the read-only MCP query counts those rows (SELECT only)
		"internal/bootstrap/researchwire/",                         // wires the recorder (research-only, with the daily bars)
		"internal/bootstrap/services.go",                           // holds it
		"internal/bootstrap/services_build.go",                     // registers the retention task (one line)
		"db/migrations/",                                           // the schema
	}
	walkSources(t, root, func(rel, text string) {
		for _, sym := range record {
			if !strings.Contains(text, sym) {
				continue
			}
			ok := false
			for _, prefix := range allowed {
				ok = ok || strings.HasPrefix(rel, prefix)
			}
			if !ok {
				t.Errorf("%s references the research 時価 record (%q): only the recorder, the retention task and the read-only research_quote_source_compare tool may, never the Feature Engine, Scout, Trader, Paper, Policy, Risk, orders, Live or the Jev prompts", rel, sym)
			}
		}
	})
}

// The recorder writes through the research repository only: it holds no
// market_snapshots writer (kabu's data path is not its), reads no kabu data,
// and names no order, risk or policy package.
func TestRecorderHasNoPathToMarketSnapshotsOrTrading(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	forbidden := []string{
		"INTO market_snapshots", "UPDATE market_snapshots", "FROM market_snapshots", "repository/market\"", "internal/service/risk", "internal/service/policy", "internal/service/paper",
		"internal/service/jev", "internal/service/execution", "internal/service/order", "internal/service/featureengine",
	}
	for _, dir := range []string{"internal/bootstrap/researchquotes", "internal/bootstrap/researchquotejob", "internal/repository/researchquote"} {
		walkSources(t, filepath.Join(root, dir), func(rel, text string) {
			for _, f := range forbidden {
				if strings.Contains(text, f) {
					t.Errorf("%s/%s mentions %q: the research 時価 recording must not touch it", dir, rel, f)
				}
			}
		})
	}
}

// The research packages write no table but their own (INSERT / DELETE name only
// research_quote_samples), and the MCP tool only selects.
func TestResearchQuotePackagesWriteOnlyTheirOwnTable(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	for _, dir := range []string{
		"internal/repository/researchquote", "internal/bootstrap/researchquotes",
		"internal/bootstrap/researchquotejob", "internal/service/paperresultsmcp/researchquotediag",
	} {
		walkSources(t, filepath.Join(root, dir), func(rel, text string) {
			for _, verb := range []string{"INSERT OR IGNORE INTO ", "INSERT OR REPLACE INTO ", "INSERT INTO ", "UPDATE ", "DELETE FROM ", "DROP ", "ALTER TABLE ", "CREATE "} {
				for rest := text; ; {
					i, j := strings.Index(rest, "`"+verb), strings.Index(rest, "\""+verb)
					if i < 0 || (j >= 0 && j < i) {
						i = j
					}
					if i < 0 {
						break
					}
					stmt := rest[i+1+len(verb):]
					if !strings.HasPrefix(stmt, "research_quote_samples") || strings.Contains(dir, "researchquotediag") {
						t.Errorf("%s/%s writes with %q outside research_quote_samples: %.60s", dir, rel, verb, stmt)
					}
					rest = stmt
				}
			}
		})
	}
}

// services_build.go is the composition root, where wiring the record into a
// trading component would most plausibly be added: every line there that
// mentions the 時価 record must be the researchquotejob import or the one task
// registration.
func TestServicesBuildOnlyRegistersTheQuoteRetentionTask(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "bootstrap", "services_build.go"))
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	for _, line := range strings.Split(string(src), "\n") {
		if !strings.Contains(line, "researchquote") {
			continue
		}
		switch {
		case strings.TrimSpace(line) == `"github.com/ousiassllc/pitha-trador/internal/bootstrap/researchquotejob"`:
		case strings.Contains(line, "scheduler.WithMaintenanceTask(researchquotejob.New(state.DB, s.Settings, state.Paths.DBPath).Task())"):
			calls++
		default:
			t.Errorf("services_build.go mentions the research 時価 record outside the task registration: %q", strings.TrimSpace(line))
		}
	}
	if calls != 1 {
		t.Errorf("researchquotejob is registered %d times, want exactly 1", calls)
	}
}

// walkSources calls fn with the slash path relative to dir's walk root (or to root
// for the whole tree) and the text of every non-test Go file below.
func walkSources(t *testing.T, root string, fn func(rel, text string)) {
	t.Helper()
	for _, dir := range sourceRoots(root) {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
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
			fn(filepath.ToSlash(rel), string(src))
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// sourceRoots are the directories walked below root: internal and cmd when root
// is the repository root, root itself otherwise.
func sourceRoots(root string) []string {
	var out []string
	for _, d := range []string{"internal", "cmd"} {
		if _, err := os.Stat(filepath.Join(root, d)); err == nil {
			out = append(out, filepath.Join(root, d))
		}
	}
	if len(out) == 0 {
		return []string{root}
	}
	return out
}
