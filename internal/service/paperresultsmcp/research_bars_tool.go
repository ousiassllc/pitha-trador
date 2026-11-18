package paperresultsmcp

import (
	"context"
	"encoding/json"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/researchbarsdiag"
)

func init() {
	tools = append(tools, statusTool("research_daily_bars_status", researchbarsdiag.Description, researchbarsdiag.Read))
}

// statusTool is a read-only tool with neither a period nor arguments (research_daily_bars_status, issue #1082, API §3.36).
func statusTool(name, desc string, run func(context.Context, query.Q) (any, int, error)) tool {
	return tool{name: name, description: desc, schema: objectSchema(map[string]any{}), noPeriod: true, decode: func(raw json.RawMessage) (periodArgs, runFunc, *toolError) {
		return periodArgs{}, func(ctx context.Context, e toolEnv) (any, int, error) { return run(ctx, e.q) }, noArgs(raw)
	}}
}
