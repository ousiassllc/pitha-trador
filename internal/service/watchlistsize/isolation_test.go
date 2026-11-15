package watchlistsize_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The runtime watch list size is for people reading the MCP: no trading, risk,
// policy, order, Paper/Live or Jev code may read it or import this package
// (issue #920: the record must not change any trade). Only the writer (this
// package and its repository), the MCP's read model and the composition root's
// wiring may name the table or the package.
func TestOnlyDiagnosticsCodeTouchesTheRuntimeWatchListSize(t *testing.T) {
	allowed := []string{
		filepath.FromSlash("internal/service/watchlistsize/"),
		filepath.FromSlash("internal/repository/watchlistruntime/"),
		filepath.FromSlash("internal/service/paperresultsmcp/"),
		filepath.FromSlash("internal/bootstrap/lifecycle.go"),
		filepath.FromSlash("db/"),
	}
	root := filepath.Join("..", "..", "..")
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		for _, a := range allowed {
			if strings.HasPrefix(rel, a) {
				return nil
			}
		}
		body, err := os.ReadFile(path) //nolint:gosec // a repository source file
		if err != nil {
			return err
		}
		for _, needle := range []string{"watch_list_runtime_days", "internal/service/watchlistsize"} {
			if strings.Contains(string(body), needle) {
				t.Errorf("%s mentions %q: the runtime watch list size must stay out of the trading, risk and Jev paths", rel, needle)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
