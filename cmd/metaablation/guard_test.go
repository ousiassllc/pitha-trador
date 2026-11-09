package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// cliDirs are the CLI and the two packages that touch the database and the
// stored model (non-test sources).
var cliDirs = []string{".", "../../internal/service/metaablation/runner", "../../internal/service/metaablation/loader"}

func parseAll(t *testing.T) map[string]*ast.File {
	t.Helper()
	out := map[string]*ast.File{}
	for _, d := range cliDirs {
		names, err := filepath.Glob(filepath.Join(d, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range names {
			if strings.HasSuffix(n, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(token.NewFileSet(), n, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			out[n] = f
		}
	}
	return out
}

// TestNoTrainingOrCalibrationIsReachable: the CLI's package graph holds no
// training package, and the sources use only the scoring side of the metalabel
// package (a model parsed from its stored parameters, Decide, the features).
func TestNoTrainingOrCalibrationIsReachable(t *testing.T) {
	// The go command is a fixed binary; the argument is a constant package path.
	b, err := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}}", "github.com/ousiassllc/pitha-trador/cmd/metaablation").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	for _, p := range strings.Fields(string(b)) {
		if strings.Contains(p, "metalabel/training") || strings.Contains(p, "trainingflow") || strings.Contains(p, "selfimprove") || strings.HasSuffix(p, "/internal/service/calibration") {
			t.Errorf("the CLI's package graph contains %s", p)
		}
	}
	allowed := map[string]bool{
		"Model": true, "Decide": true, "DecideConfig": true, "DefaultKellyFraction": true, "Features": true,
		"NewFeatures": true, "ParseModel": true,
	}
	for name, f := range parseAll(t) {
		ast.Inspect(f, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == "metalabel" && !allowed[sel.Sel.Name] {
					t.Errorf("%s uses metalabel.%s, which is not a scoring API", name, sel.Sel.Name)
				}
			}
			return true
		})
	}
}

// TestDatabaseIsOnlyReachedThroughTheReadOnlyOpener: no writable open, no
// migration and no statement that writes appears in the CLI sources.
func TestDatabaseIsOnlyReachedThroughTheReadOnlyOpener(t *testing.T) {
	write := regexp.MustCompile(`(?i)^(Exec|ExecContext|Begin|BeginTx|Prepare|PrepareContext|Migrate|Insert\w*|Update\w*|Delete\w*|Save\w*)$`)
	for name, f := range parseAll(t) {
		for _, imp := range f.Imports {
			if p := imp.Path.Value; strings.Contains(p, "migrat") || strings.Contains(p, "sidecar") {
				t.Errorf("%s imports %s", name, p)
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); ok {
				switch {
				case id.Name == "sqlitedb" && sel.Sel.Name != "OpenReadOnly":
					t.Errorf("%s uses sqlitedb.%s: only OpenReadOnly may open the database", name, sel.Sel.Name)
				case id.Name == "sql" && sel.Sel.Name == "Open":
					t.Errorf("%s opens a database with sql.Open", name)
				}
			}
			if write.MatchString(sel.Sel.Name) {
				t.Errorf("%s calls %s, which writes", name, sel.Sel.Name)
			}
			return true
		})
	}
}
