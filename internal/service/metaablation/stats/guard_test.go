package stats_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoAdoptionBarInTheVerdictCode keeps the model adoption bar (issue #862:
// 370 takes, 11 bps, MinOOSDays) out of the verdict code: no import of the
// training package, none of its names and none of those numbers as literals.
func TestNoAdoptionBarInTheVerdictCode(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		checked++
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			if strings.Contains(imp.Path.Value, "metalabel/training") {
				t.Errorf("%s imports %s", name, imp.Path.Value)
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.Ident:
				for _, banned := range []string{"MinOOSDays", "MinOOSSamples", "DefaultAcceptanceConfig", "AssumedEdgeBps", "MinDetectableEdgeBps", "DefaultMinOOSDays"} {
					if x.Name == banned {
						t.Errorf("%s uses %s", name, banned)
					}
				}
			case *ast.BasicLit:
				if x.Kind == token.INT || x.Kind == token.FLOAT {
					if x.Value == "370" || x.Value == "740" || x.Value == "11" || x.Value == "11.0" {
						t.Errorf("%s: literal %s is an adoption-bar value", name, x.Value)
					}
				}
			}
			return true
		})
	}
	if checked == 0 {
		t.Fatal("no source files were scanned: the guard is vacuous")
	}
}
