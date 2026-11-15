package paperresultsmcp

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/disclosureskipdiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/entryqualitydiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/evaldiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/metalabeldiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/researchdiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/trialtool"
)

func init() {
	tools = append(tools, jevModelsTool, entryQualityWhatIfTool, disclosureSkipSummaryTool, trialCompareTool, trialCostTool, jevEvaluationTool, researchHorizonSummaryTool)
}

// Read-only tools: jev_models (#849, API §3.24), entry_quality_whatif (#911, §3.30), disclosure_skip_summary (#935, §3.31), jev_trial_compare / jev_trial_cost (#1060, §3.32), jev_evaluation (#1073, §3.33), research_horizon_summary (#1074, §3.34).
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

func queryTool[R any](name, desc string, props map[string]any, run func(context.Context, query.Q, query.Range, time.Time, R) (any, int, error)) tool { // the analysis is the package's own (trialtool, evaldiag, researchdiag)
	return diagTool(name, desc, props, func(ctx context.Context, e toolEnv, a R) (any, int, error) { return run(ctx, e.q, e.r, e.now, a) })
}

var (
	entryQualityWhatIfTool = diagTool("entry_quality_whatif", entryqualitydiag.Description, entryqualitydiag.SchemaProperties(), // the analysis is entryqualitydiag's
		func(ctx context.Context, e toolEnv, a entryqualitydiag.Request) (any, int, error) {
			return entryqualitydiag.Run(ctx, e.q, e.r, a, e.policy)
		})
	disclosureSkipSummaryTool  = queryTool("disclosure_skip_summary", disclosureskipdiag.Description, disclosureskipdiag.SchemaProperties(), disclosureskipdiag.Run)
	trialCompareTool           = queryTool("jev_trial_compare", trialtool.CompareDescription, trialtool.SchemaProperties(), trialtool.RunCompare)
	trialCostTool              = queryTool("jev_trial_cost", trialtool.CostDescription, trialtool.SchemaProperties(), trialtool.RunCost)
	jevEvaluationTool          = queryTool("jev_evaluation", evaldiag.Description, evaldiag.SchemaProperties(), evaldiag.Run)
	researchHorizonSummaryTool = queryTool("research_horizon_summary", researchdiag.Description, researchdiag.SchemaProperties(), researchdiag.Run)
)
