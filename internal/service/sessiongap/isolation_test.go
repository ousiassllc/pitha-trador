package sessiongap_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The daily session record is for people reading the MCP and Home: no trading,
// risk, policy, order, Paper/Live or Jev code may read it or import this
// package (issue #1054: a gap day must not change any trade). Only the writer
// (this package), the MCP's read model (paperresultsmcp) and the composition root's wiring may name the table or the package.
func TestOnlyDiagnosticsCodeTouchesTheDailyRecord(t *testing.T) {
	allowed := []string{
		filepath.FromSlash("internal/service/sessiongap/"),
		filepath.FromSlash("internal/service/paperresultsmcp/"),
		filepath.FromSlash("internal/bootstrap/healthalertwire/"),
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
		for _, needle := range []string{"session_diagnostics_daily", "internal/service/sessiongap"} {
			if strings.Contains(string(body), needle) {
				t.Errorf("%s mentions %q: the daily session record must stay out of the trading, risk and Jev paths", rel, needle)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
