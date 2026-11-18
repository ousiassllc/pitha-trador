package featuremodel_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// root is the repository root.
var root = filepath.Join("..", "..", "..")

// goSources walks the non-test Go sources of the directories (relative to the root)
// and calls fn with the slash-separated relative path and the text.
func goSources(t *testing.T, dirs []string, fn func(rel, text string)) {
	t.Helper()
	for _, dir := range dirs {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			fn(filepath.ToSlash(rel), string(src))
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// The research model (issue #1092) is never an input of trading: Policy, Risk, order
// placement, execution, the Paper guard, the Live path, the Jev prompts, RAG, the
// meta-model training and adoption and the self-improvement flow must not read its
// tables or call its packages. This walks the whole non-test source tree and fails when
// a file outside the few below names them.
func TestResearchModelIsOnlyUsedByItsOwnPackagesTheJobAndTheLedger(t *testing.T) {
	record := []string{
		"research_model_runs", "research_model_predictions",
		"repository/featuremodel", "service/featuremodel", "featuremodel.NewRunner", "featuremodel.NewRepository",
	}
	allowed := []string{
		"internal/repository/featuremodel/",            // the persistence
		"internal/service/featuremodel/",               // training and prediction (and its test fixture)
		"internal/bootstrap/nightlyjobs/model.go",      // wires them into the maintenance task
		"internal/service/paperresultsmcp/ledgerdiag/", // the read-only candidate ledger that compares the predictions with Jev
		"internal/config/researchjobsettings/",         // the on/off switch and retention (names the tables in comments only)
		"db/migrations/",                               // the schema
	}
	goSources(t, []string{"internal", "cmd"}, func(rel, text string) {
		for _, sym := range record {
			if !strings.Contains(text, sym) {
				continue
			}
			ok := false
			for _, prefix := range allowed {
				ok = ok || strings.HasPrefix(rel, prefix)
			}
			if !ok {
				t.Errorf("%s references the research model (%q): only its own packages, the nightly job and the read-only candidate ledger may, never Policy, Risk, orders, execution, the Paper guard, Live, the Jev prompts, RAG, the meta-model or self-improvement", rel, sym)
			}
		}
	})
}

// The test fixture package writes label tables, so only test files may import it.
func TestFixturePackageIsImportedOnlyByTests(t *testing.T) {
	goSources(t, []string{"internal", "cmd"}, func(rel, text string) {
		if strings.Contains(text, "featuremodel/modeltest") && !strings.HasPrefix(rel, "internal/service/featuremodel/modeltest/") {
			t.Errorf("%s imports the test fixture package featuremodel/modeltest", rel)
		}
	})
}

var (
	writeVerbs = []string{"INSERT OR IGNORE INTO ", "INSERT OR REPLACE INTO ", "INSERT INTO ", "UPDATE ", "DELETE FROM ", "DROP ", "ALTER TABLE ", "CREATE ", "REPLACE INTO "}
	readTable  = regexp.MustCompile(`\b(?:FROM|JOIN)\s+([a-z_][a-z0-9_]*)`)
)

// The model's own packages write only research_model_runs and research_model_predictions
// (every INSERT / UPDATE / DELETE statement names one of them) and read only the label
// record, the snapshots' features and the Jev decisions' ids, instruments and times
// (every FROM / JOIN names one of the tables below): never trade_signals, orders, the
// calibration outcomes, the meta-model tables or runtime_settings (which can hold secret
// paths).
func TestModelPackagesWriteOnlyTheirTablesAndReadOnlyTheirInputs(t *testing.T) {
	own := []string{"research_model_runs", "research_model_predictions"}
	readable := append([]string{"research_horizon_labels", "research_label_days", "market_snapshots", "jev_decisions"}, own...)
	dirs := []string{"internal/repository/featuremodel", "internal/service/featuremodel", "internal/bootstrap/nightlyjobs"}
	goSources(t, dirs, func(rel, text string) {
		if strings.HasPrefix(rel, "internal/service/featuremodel/modeltest/") || (strings.HasPrefix(rel, "internal/bootstrap/nightlyjobs/") && rel != "internal/bootstrap/nightlyjobs/model.go") {
			return // the fixture seeds synthetic rows (tests only); the other nightly task is not this model's
		}
		for _, verb := range writeVerbs {
			for rest := text; ; {
				i := strings.Index(rest, "`"+verb)
				if j := strings.Index(rest, "\""+verb); i < 0 || (j >= 0 && j < i) {
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
					t.Errorf("%s writes with %q to a table that is not one of the model's two: %.60s", rel, verb, stmt)
				}
				rest = stmt
			}
		}
		for _, m := range readTable.FindAllStringSubmatch(text, -1) {
			ok := false
			for _, table := range readable {
				ok = ok || m[1] == table
			}
			if !ok {
				t.Errorf("%s reads %q: the model reads only %v", rel, m[1], readable)
			}
		}
	})
}

// services_build.go is the composition root, where wiring the model into a trading
// component would most plausibly be added: the only line that may mention the model
// is the one registering the nightly task, and nothing names its repository or
// service.
func TestServicesBuildOnlyRegistersTheModelMaintenanceTask(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(root, "internal", "bootstrap", "services_build.go"))
	if err != nil {
		t.Fatal(err)
	}
	registrations := 0
	for _, line := range strings.Split(string(src), "\n") {
		if strings.Contains(line, "featuremodel") || strings.Contains(line, "research_model") {
			t.Errorf("services_build.go names the model's packages or tables: %q", strings.TrimSpace(line))
		}
		registrations += strings.Count(line, "scheduler.WithMaintenanceTask(nightlyjobs.ModelTask(state.DB, s.Settings))")
		if strings.Contains(line, "ModelTask") && !strings.Contains(line, "scheduler.WithMaintenanceTask(nightlyjobs.ModelTask(state.DB, s.Settings))") {
			t.Errorf("services_build.go uses ModelTask outside the maintenance task registration: %q", strings.TrimSpace(line))
		}
	}
	if registrations != 1 {
		t.Errorf("ModelTask is registered %d times, want exactly 1", registrations)
	}
}
