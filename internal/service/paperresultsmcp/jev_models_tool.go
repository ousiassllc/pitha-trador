package paperresultsmcp

import (
	"context"
	"encoding/json"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/entryqualitydiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/metalabeldiag"
)

// jev_models (issue #849; docs/api/endpoints/paper-results-mcp.md §3.24): the
// Jev model names the decisions were answered with, against the model the LLM
// cutoff was confirmed for. It reads through the read-only connection and
// writes nothing.

func init() {
	tools = append(tools, jevModelsTool, entryQualityWhatIfTool)
}

var jevModelsTool = tool{
	name: "jev_models",
	description: "Jev が実際に応答しているモデル名: 期間内の jev_decisions.model_id ごとの件数と最初・最後に出た時刻（JST）、JST 日別の件数、" +
		"metalabel.llm_cutoff_model と直近の model_id が完全一致するか（match / mismatch / unset。再学習の cutoff 判定と同じ比較）。secrets は返さない（読み取り専用）",
	schema: periodSchema(nil),
	decode: func(raw json.RawMessage) (periodArgs, runFunc, *toolError) {
		var a periodArgs
		if err := strictDecode(raw, &a); err != nil {
			return a, nil, err
		}
		return a, func(ctx context.Context, e toolEnv) (any, int, error) {
			d, err := metalabeldiag.JevModelsUsage(ctx, e.q, e.r)
			return d, len(d.Models), err
		}, nil
	},
}

// entry_quality_whatif (issue #911, API spec §3.30): what the entry guards would
// do under another min_entry_quality; read only, the analysis is entryqualitydiag's.
var entryQualityWhatIfTool = tool{
	name: "entry_quality_whatif", description: entryqualitydiag.Description, schema: periodSchema(entryqualitydiag.SchemaProperties()),
	decode: func(raw json.RawMessage) (periodArgs, runFunc, *toolError) {
		var a entryqualitydiag.Request
		err := strictDecode(raw, &a)
		return periodArgs{From: a.From, To: a.To}, func(ctx context.Context, e toolEnv) (any, int, error) {
			return entryqualitydiag.Run(ctx, e.q, e.r, a, e.policy)
		}, err
	},
}
