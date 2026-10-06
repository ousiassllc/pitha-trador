package web_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	// `<th` followed by whitespace or `>` so `<thead>` is not matched.
	thOpenRe    = regexp.MustCompile(`<th[\s>][^>]*>?`)
	tableOpenRe = regexp.MustCompile(`<table[\s>][^>]*>`)
	tableBlock  = regexp.MustCompile(`(?s)<table[\s>].*?</table>`)
)

// Issue #544: a column header without scope leaves screen readers to guess
// the cell-to-header association, and a table without an accessible name
// (aria-label or <caption>) does not announce its purpose. Every templ
// under internal/web must give both, so a new table cannot silently regress.
func TestTemplTables_HaveScopedHeadersAndAccessibleName(t *testing.T) {
	root := filepath.Join(repoRoot(t), "internal", "web")
	tables := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".templ") {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		for _, th := range thOpenRe.FindAllString(string(src), -1) {
			if !strings.Contains(th, `scope="`) {
				t.Errorf("%s: <th> without scope attribute: %s", rel, th)
			}
		}
		for _, block := range tableBlock.FindAllString(string(src), -1) {
			tables++
			open := tableOpenRe.FindString(block)
			if !strings.Contains(open, `aria-label="`) && !strings.Contains(block, "<caption") {
				t.Errorf("%s: <table> without aria-label or <caption>: %s", rel, open)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	if tables == 0 {
		t.Fatal("no <table> found in internal/web templ files; the check is not running against anything")
	}
}
