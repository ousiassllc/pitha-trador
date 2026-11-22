package middleware_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/web/middleware"
)

// Heartbeat keeps no path list: every timer-driven `hx-trigger="... every Ns"`
// element must send BackgroundHeader via hx-headers or an idle tab keeps the
// dead-man's switch alive (FR-RISK-6, issue #1210). The router tests only see
// the polls a page happens to render; this scans the template sources, so a
// poll that renders only in some state (like the MCP panel's "starting") is
// covered too. A poll counts as marked when an hx-headers attribute naming the
// header sits within a few lines of its hx-trigger.
func TestEveryNsPollsInTemplateSourcesSendBackgroundHeader(t *testing.T) {
	every := regexp.MustCompile(`every \d+(ms|s)\b`)
	const window = 8
	polls := 0
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !strings.HasSuffix(path, ".templ") && !strings.HasSuffix(path, ".go") ||
			strings.HasSuffix(path, "_templ.go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		lines := strings.Split(string(src), "\n")
		for i, line := range lines {
			if !strings.Contains(line, "hx-trigger") || !every.MatchString(line) {
				continue
			}
			polls++
			marked := false
			for _, near := range lines[max(0, i-window):min(len(lines), i+window+1)] {
				if strings.Contains(near, "hx-headers") &&
					(strings.Contains(near, middleware.BackgroundHeader) || strings.Contains(near, "refreshHeaders")) {
					marked = true
				}
			}
			if !marked {
				t.Errorf("%s:%d: `every Ns` poll does not send %s via hx-headers: %s", path, i+1, middleware.BackgroundHeader, strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if polls < 7 {
		t.Fatalf("found %d `every Ns` polls under internal/web, want at least 7: the scan is not seeing the sources", polls)
	}
}
