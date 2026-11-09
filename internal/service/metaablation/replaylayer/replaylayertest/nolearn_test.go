package replaylayertest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// The replay layer re-scores a stored model as it is: no training or
// calibration function is reachable from it. The package imports no training
// code and uses nothing of metalabel that fits, trains or calibrates.
func TestNoTrainingOrCalibrationIsReachable(t *testing.T) {
	files, err := filepath.Glob("../*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("glob: %v %v", files, err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if strings.Contains(path, "/training") || strings.Contains(path, "metalabel/store") || strings.Contains(path, "repository/metalabel") {
				t.Errorf("%s imports %s: the replay layer must not reach training or the model store", name, path)
			}
			if strings.HasPrefix(path, "database/sql") {
				t.Errorf("%s imports %s: the replay layer takes in-memory data only", name, path)
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "metalabel" {
				for _, bad := range []string{"Train", "Fit", "Calibrat", "Platt", "Isotonic", "Walk", "Sample"} {
					if strings.HasPrefix(sel.Sel.Name, bad) {
						t.Errorf("%s uses metalabel.%s: the replay layer never trains or calibrates", name, sel.Sel.Name)
					}
				}
			}
			return true
		})
	}
}
