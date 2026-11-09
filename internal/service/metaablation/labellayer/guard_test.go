package labellayer_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func sources(t *testing.T) map[string]*ast.File {
	t.Helper()
	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]*ast.File{}
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
	return out
}

// TestOnlyScoringAPIsOfTheModelAreUsed: the package reaches the metalabel
// package only through Model, Decide and their configuration, never through
// Train, calibration or the training package, so no learning function can run.
func TestOnlyScoringAPIsOfTheModelAreUsed(t *testing.T) {
	allowed := map[string]bool{
		"Model": true, "Decide": true, "DecideConfig": true, "DefaultKellyFraction": true, "Features": true,
		"ReasonNoModel": true, "ReasonModelUnusable": true, "ReasonInvalidConfig": true,
	}
	for name, f := range sources(t) {
		for _, imp := range f.Imports {
			p := imp.Path.Value
			if strings.Contains(p, "metalabel/training") || strings.Contains(p, "trainingflow") {
				t.Errorf("%s imports %s", name, p)
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == "metalabel" && !allowed[sel.Sel.Name] {
				t.Errorf("%s uses metalabel.%s, which is not a scoring API", name, sel.Sel.Name)
			}
			return true
		})
	}
}

// TestNoThresholdSearchAndNoAdoptionBar: no exported function that searches,
// tunes or optimises, and none of the adoption bar's names or numbers.
func TestNoThresholdSearchAndNoAdoptionBar(t *testing.T) {
	search := regexp.MustCompile(`(?i)optimi|tune|search|sweep|grid|calibrat|train|fit`)
	for name, f := range sources(t) {
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.FuncDecl:
				if x.Name.IsExported() && search.MatchString(x.Name.Name) {
					t.Errorf("%s: exported %s suggests a search/training path", name, x.Name.Name)
				}
			case *ast.Ident:
				for _, banned := range []string{"MinOOSDays", "MinOOSSamples", "DefaultAcceptanceConfig", "AssumedEdgeBps", "MinDetectableEdgeBps"} {
					if x.Name == banned {
						t.Errorf("%s uses %s", name, banned)
					}
				}
			case *ast.BasicLit:
				if x.Kind == token.INT && (x.Value == "370" || x.Value == "740") || x.Kind == token.FLOAT && x.Value == "11.0" {
					t.Errorf("%s: literal %s is an adoption-bar value", name, x.Value)
				}
			}
			return true
		})
	}
}
