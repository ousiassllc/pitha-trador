package mcptests

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
)

// The docs spell out the tool count; it went stale once (issue #1070). Pin it.
func TestToolCountInDocsMatchesDispatchTable(t *testing.T) {
	n := []int{len(allTools), len(settingsTools), len(proposalTools), len(experimentTools), len(jevShadowTools)}
	total := len(paperresultsmcp.ToolNames())
	want := map[string]string{
		"architecture/paper-results-mcp-errors.md": fmt.Sprintf("§3 の %d 個（読み取り %d（期間指定 %d + `health_today`）、設定 %d、注文案 %d、実験 %d、Jev のプロンプト %d）", total, n[0], len(periodTools), n[1], n[2], n[3], n[4]),
		"components/paper-results-mcp.md":          fmt.Sprintf("（読み取り %d つ、設定 %d つ、注文案 %d つ、調整の実験 %d つ、Jev のプロンプト %d つ）", n[0], n[1], n[2], n[3], n[4]),
		"api/endpoints/paper-results-mcp.md":       fmt.Sprintf("読み取りの %d つ（", n[0]),
	}
	for file, s := range want {
		raw, err := os.ReadFile("../../../../docs/" + file)
		if err != nil || !strings.Contains(string(raw), s) || (strings.HasPrefix(file, "api") && !strings.Contains(string(raw), fmt.Sprintf("合計 %d。", total))) {
			t.Errorf("docs/%s must contain %q and match ToolNames() (%d tools): %v", file, s, total, err)
		}
	}
}
