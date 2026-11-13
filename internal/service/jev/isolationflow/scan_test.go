package isolationflow_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// repoRoot is the repository root relative to this package.
var repoRoot = filepath.Join("..", "..", "..", "..")

// walkGoSources calls visit for every non-test .go file under dir (relative to
// the repository root). A missing dir fails the test unless optional.
func walkGoSources(t *testing.T, dir string, optional bool, visit func(path string, src []byte)) {
	t.Helper()
	abs := filepath.Join(repoRoot, dir)
	if _, err := os.Stat(abs); err != nil {
		if optional && os.IsNotExist(err) {
			return
		}
		t.Fatalf("%s is gone or moved: update this guard (%v)", dir, err)
	}
	err := filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		visit(path, src)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// importsOf returns the import paths of the Go source file path.
func importsOf(t *testing.T, path string) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var out []string
	for _, spec := range file.Imports {
		p, uerr := strconv.Unquote(spec.Path.Value)
		if uerr != nil {
			t.Fatalf("%s: import %s: %v", path, spec.Path.Value, uerr)
		}
		out = append(out, p)
	}
	return out
}
