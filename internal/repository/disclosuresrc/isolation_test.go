package disclosuresrc_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The extra disclosure / news records are never an input of trading (issue
// #1096): the gates, Policy, Risk, order placement, execution, the Paper guard,
// Live, Jev, the RAG index, calibration, the meta-model training and the
// existing disclosure skip rules must not read them. This walks the whole
// non-test source tree and fails when a file outside the few below names the
// tables, the packages or the settings.
func TestDisclosureSourceRecordsAreReadOnlyByTheirRecorderAndTheStatusTool(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	record := []string{
		"research_edinet_documents", "research_edinet_fetch_days", "research_tachibana_news", "research_disclosure_source_status",
		"research_disclosure_source_events_v",
		"repository/disclosuresrc", "service/disclosuresrc", "domain/disclosuresrc", "bootstrap/disclosuresrcwire",
		"paperresultsmcp/disclosuresrcdiag", "config/disclosuresrcsettings",
	}
	// The recorders and the EDINET client, the value types, the settings, the wiring
	// of the EDINET task (one line of services_build.go), the read-only MCP query and
	// its registration, the Settings screen's validation and group, the schema.
	readers := []string{
		"internal/repository/disclosuresrc/",
		"internal/service/disclosuresrc/",
		"internal/domain/disclosuresrc/",
		"internal/config/disclosuresrcsettings/",
		"internal/bootstrap/disclosuresrcwire/",
		"internal/bootstrap/researchwire/",
		"internal/service/paperresultsmcp/disclosuresrcdiag/",
		"internal/service/paperresultsmcp/jev_models_tool.go",
		"internal/service/opsettings/",
		"internal/web/handler/settings/settings_ops_disclosuresrc.go",
		"db/migrations/",
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
					allowed = allowed || strings.HasPrefix(rel, prefix)
				}
				if !allowed {
					t.Errorf("%s references the disclosure source records (%q): only the recorders, the settings and the read-only disclosure_sources_status tool may", rel, sym)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

var writeStmt = regexp.MustCompile(`\b(?:INSERT(?: OR \w+)? INTO|UPDATE|DELETE FROM)\s+(\w+)`)

// The recorder's packages write only their own four tables: never
// disclosure_items, jev_decisions, jev_shadow_decisions, jev_trial_decisions,
// market_snapshots or daily_bars.
func TestDisclosureSourcePackagesWriteOnlyTheirOwnTables(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	own := map[string]bool{
		"research_edinet_documents": true, "research_edinet_fetch_days": true,
		"research_tachibana_news": true, "research_disclosure_source_status": true,
	}
	for _, dir := range []string{
		"internal/repository/disclosuresrc", "internal/service/disclosuresrc",
		"internal/bootstrap/disclosuresrcwire", "internal/service/paperresultsmcp/disclosuresrcdiag",
	} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			src, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			text := strings.ReplaceAll(string(src), "DO UPDATE SET", "DO SET")
			for _, m := range writeStmt.FindAllStringSubmatch(text, -1) {
				if !own[m[1]] {
					t.Errorf("%s writes with %q to %q, which is not one of the disclosure source tables", path, m[0], m[1])
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
