package mcptests

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
)

// The docs spell out the tool counts; they went stale before (issues #1070, #1226). Pin them, writingTools included.
func TestToolCountInDocsMatchesDispatchTable(t *testing.T) {
	n := []int{len(allTools), len(settingsTools), len(proposalTools), len(experimentTools), len(jevShadowTools), len(researchStatusTools), len(replayTools), len(exportTools), len(researchTools), len(researchNoteTools), len(updateTools)}
	total, w := len(paperresultsmcp.ToolNames()), len(writingTools)
	want := map[string][]string{
		"architecture/paper-results-mcp-errors.md":       {fmt.Sprintf("§3 の %d 個（読み取り %d（期間指定 %d + `health_today`・`ai_roles` + `promotion_checklist`）、設定 %d、注文案 %d、実験 %d、Jev のプロンプト %d、研究用の状況 %d、リプレイ %d、書き出し %d、バックテストと仮想 Paper %d、研究ノートと FX・指数の系列 %d、自動更新 %d）", total, n[0], len(periodTools), n[1], n[2], n[3], n[4], n[5], n[6], n[7], n[8], n[9], n[10])},
		"components/paper-results-mcp.md":                {fmt.Sprintf("（読み取り %d つ、設定 %d つ、注文案 %d つ、調整の実験 %d つ、Jev のプロンプト %d つ、研究用の状況 %d つ、リプレイ %d つ、データセットの書き出し %d つ、バックテストと仮想 Paper %d つ、研究ノートと FX・指数の系列 %d つ、自動更新 %d つ）", n[0], n[1], n[2], n[3], n[4], n[5], n[6], n[7], n[8], n[9], n[10]), fmt.Sprintf("書き込みツール（%d 本。", w)},
		"api/endpoints/paper-results-mcp.md":             {fmt.Sprintf("読み取りの %d つ（", n[0]), fmt.Sprintf("合計 %d。", total)},
		"architecture/paper-results-mcp.md":              {fmt.Sprintf("書き込み %d ツール。§7.3", w), fmt.Sprintf("`writingTools` の %d ツール。§7.3", w), fmt.Sprintf("書き込みを行うツールは %d 本（", w)},
		"requirements/functional/components-platform.md": {fmt.Sprintf("`writingTools` の %d ツールと一致", w)},
		"architecture/er.md":                             {fmt.Sprintf("書き込みツール %d 本の一覧", w)},
		"architecture/overview/packages.md":              {fmt.Sprintf("書き込みツール %d 本の一覧", w)},
		"overview.md":                                    {fmt.Sprintf("書き込みツールは計 %d 本", w)},
	}
	for file, subs := range want {
		raw, err := os.ReadFile("../../../../docs/" + file)
		for _, s := range subs {
			if err != nil || !strings.Contains(string(raw), s) {
				t.Errorf("docs/%s must contain %q and match ToolNames() (%d tools) and writingTools (%d): %v", file, s, total, w, err)
			}
		}
	}
}
