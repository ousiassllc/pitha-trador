package paperresultsmcp

import (
	"context"
	"encoding/json"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset"
)

// Helpers of the tools that are still declared in this package. (The settings,
// order-proposal and experiment tools live in settingstool, proposaltool and
// experimenttool; see ext_tools.go.)

// paperCapital is the Paper capital for the return-rate calculations: the
// Settings value (risk.paper.initial_capital_jpy in runtime_settings, read
// through the read-only connection) when one is stored, otherwise the
// risk.yaml value the process started with.
func (e toolEnv) paperCapital(ctx context.Context) (float64, error) {
	return mcpset.PaperCapitalJPY(ctx, e.q, e.capitalJPY)
}

// noArgs decodes the (empty) arguments of a tool that takes none.
func noArgs(raw json.RawMessage) *toolError {
	var a struct{}
	return strictDecode(raw, &a)
}

// readTool is a read-only tool without arguments and without a period.
func readTool(name, description string, run runFunc) tool {
	return tool{name: name, description: description, schema: objectSchema(map[string]any{}), noPeriod: true,
		decode: func(raw json.RawMessage) (periodArgs, runFunc, *toolError) { return periodArgs{}, run, noArgs(raw) }}
}
