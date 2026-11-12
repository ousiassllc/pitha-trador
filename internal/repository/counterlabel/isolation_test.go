package counterlabel_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The counterfactual labels of skipped candidates are never an input of
// trading (issue #1052): Policy, Risk, order placement, execution, the Paper
// guard, the Live path, RAG, the feature engine, the Jev prompts and the
// self-improvement flow must not read them. This walks the whole non-test
// source tree and fails when a file outside the few readers/writers below names
// the table, its repository, its origin view or the training switch.
func TestCounterfactualLabelsAreReadOnlyByTrainingAndTheirRecorder(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	// Symbols of the record itself.
	record := []string{"counterfactual_labels", "counterlabel", "calibration_outcome_origins", "ListTrainingRowsWithSkipped"}
	recordReaders := []string{
		"internal/repository/counterlabel/",                  // the record's own repository
		"internal/repository/metalabel/training_",            // the nightly retraining's rows
		"internal/service/calibration/counterfactual",        // the recorder
		"internal/service/metalabel/training/",               // the nightly retraining
		"internal/bootstrap/metalabeljob/",                   // wires the recorder into the nightly job
		"internal/service/paperresultsmcp/query/trialquery/", // jev_trial_compare (#1060): read-only MCP view that shows the labels as a proxy, never an input of anything
		"internal/repository/metalabel/training_repo.go",     // ListTrainingRowsWithSkipped
		"db/migrations/", // the schema
	}
	// The training switch.
	switchSymbols := []string{"IncludeSkippedLabels", "include_skipped_labels", "KeyIncludeSkippedLabels"}
	switchReaders := append([]string{
		"internal/config/metalabelsettings/", // the setting
		"internal/web/handler/settings/",     // the Settings screen row
		"internal/repository/metalabel/",     // the training scope
	}, recordReaders...)

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
			check := func(symbols, allowed []string, what string) {
				for _, sym := range symbols {
					if !strings.Contains(string(src), sym) {
						continue
					}
					ok := false
					for _, prefix := range allowed {
						if strings.HasPrefix(rel, prefix) {
							ok = true
						}
					}
					if !ok {
						t.Errorf("%s references %s (%q): only training and the recorder may read it, never Policy, Risk, orders, the Paper guard, Live or the Jev prompts", rel, what, sym)
					}
				}
			}
			check(record, recordReaders, "the counterfactual label record")
			check(switchSymbols, switchReaders, "the skipped-label training switch")
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
