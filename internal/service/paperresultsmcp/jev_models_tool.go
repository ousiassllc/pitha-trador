package paperresultsmcp

import (
	"context"
	"encoding/json"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/disclosureskipdiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/entryqualitydiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/metalabeldiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/trialtool"
)

func init() {
	tools = append(tools, jevModelsTool, entryQualityWhatIfTool, disclosureSkipSummaryTool, trialCompareTool, trialCostTool)
}

// Read-only tools: jev_models (#849, API §3.24), entry_quality_whatif (#911, §3.30), disclosure_skip_summary (#935, §3.31), jev_trial_compare / jev_trial_cost (#1060, §3.32).
var jevModelsTool = diagTool("jev_models", "Jev が実際に応答しているモデル名: 期間内の jev_decisions.model_id ごとの件数と最初・最後に出た時刻（JST）、JST 日別の件数、"+
	"metalabel.llm_cutoff_model と直近の model_id が完全一致するか（match / mismatch / unset。再学習の cutoff 判定と同じ比較）。secrets は返さない（読み取り専用）",
	nil, func(ctx context.Context, e toolEnv, _ periodArgs) (any, int, error) {
		d, err := metalabeldiag.JevModelsUsage(ctx, e.q, e.r)
		return d, len(d.Models), err
	})

func diagTool[R any](name, desc string, props map[string]any, run func(context.Context, toolEnv, R) (any, int, error)) tool { // R: strictly decoded arguments, a struct with from/to
	return tool{name: name, description: desc, schema: periodSchema(props), decode: func(raw json.RawMessage) (periodArgs, runFunc, *toolError) {
		a, p := *new(R), periodArgs{}
		err := strictDecode(raw, &a)
		_ = json.Unmarshal(raw, &p) // from/to; R has the same member names, so it decoded if strictDecode passed
		return p, func(ctx context.Context, e toolEnv) (any, int, error) { return run(ctx, e, a) }, err
	}}
}

func trialTool(name, desc string, run trialtool.RunFunc) tool { // the analysis is trialtool's
	return diagTool(name, desc, trialtool.SchemaProperties(),
		func(ctx context.Context, e toolEnv, a trialtool.Request) (any, int, error) {
			return run(ctx, e.q, e.r, e.now, a)
		})
}

var (
	entryQualityWhatIfTool = diagTool("entry_quality_whatif", entryqualitydiag.Description, entryqualitydiag.SchemaProperties(), // the analysis is entryqualitydiag's
		func(ctx context.Context, e toolEnv, a entryqualitydiag.Request) (any, int, error) {
			return entryqualitydiag.Run(ctx, e.q, e.r, a, e.policy)
		})
	disclosureSkipSummaryTool = diagTool("disclosure_skip_summary", disclosureskipdiag.Description, disclosureskipdiag.SchemaProperties(), // the analysis is disclosureskipdiag's
		func(ctx context.Context, e toolEnv, a disclosureskipdiag.Request) (any, int, error) {
			return disclosureskipdiag.Run(ctx, e.q, e.r, a)
		})
	trialCompareTool = trialTool("jev_trial_compare", trialtool.CompareDescription, trialtool.RunCompare)
	trialCostTool    = trialTool("jev_trial_cost", trialtool.CostDescription, trialtool.RunCost)
)
