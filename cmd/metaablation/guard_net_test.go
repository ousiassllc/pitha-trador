package main

import (
	"go/ast"
	"strings"
	"testing"
)

// TestNoNetworkUse pins that the CLI makes no network or client call: the jev and
// newsfeed packages are reached only through the pure functions and constants the
// feature rebuild needs (the same ones training uses), never a client or a
// constructor, and no network package is imported by the CLI's own sources.
func TestNoNetworkUse(t *testing.T) {
	allowed := map[string]map[string]bool{
		"jev":      {"TraderResponse": true, "TraderCohort": true},
		"newsfeed": {"TDnetScoreNeutral": true, "ScoreNewsContext": true, "DefaultTTL": true},
	}
	for name, f := range parseAll(t) {
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if p == "net" || strings.HasPrefix(p, "net/") || strings.Contains(p, "systemone") || strings.Contains(p, "kabu") || strings.Contains(p, "broker") || strings.Contains(p, "/slack") {
				t.Errorf("%s imports %s", name, p)
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); ok {
				if set, watched := allowed[id.Name]; watched && !set[sel.Sel.Name] {
					t.Errorf("%s uses %s.%s, which is not on the allow-list of pure helpers", name, id.Name, sel.Sel.Name)
				}
			}
			return true
		})
	}
}
