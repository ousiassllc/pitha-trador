package paperresultsmcp

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/backtesttool"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/jevshadowtool"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/tooldef"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/virtualpapertool"
)

// The tools declared in their own packages with tooldef join the table here:
// the one place that turns a tooldef.Def into a table entry.
func init() {
	for _, d := range slices.Concat(backtesttool.Defs(), jevshadowtool.Defs(), virtualpapertool.Defs()) {
		tools = append(tools, fromDef(d))
	}
}

// toolErrorOf is f as the root's error; nil stays nil.
func toolErrorOf(f *tooldef.Failure) *toolError {
	if f == nil {
		return nil
	}
	return &toolError{code: Code(f.Code), cause: f.Cause, detail: f.Detail}
}

func fromDef(d tooldef.Def) tool {
	return tool{
		name: d.Name, description: d.Description, schema: d.Schema, writes: d.Writes, noPeriod: d.NoPeriod,
		decode: func(raw json.RawMessage) (periodArgs, runFunc, *toolError) {
			p, run, f := d.Decode(raw)
			args := periodArgs{From: p.From, To: p.To}
			if f != nil {
				return args, nil, toolErrorOf(f)
			}
			return args, func(ctx context.Context, e toolEnv) (any, int, error) {
				data, n, err := run(ctx, tooldef.Env{
					Q: e.q, Range: e.r, Now: e.now, Client: e.client, Writer: e.writer, Backtests: e.backtests,
				})
				if f := (*tooldef.Failure)(nil); errors.As(err, &f) {
					return data, n, toolErrorOf(f)
				}
				return data, n, err
			}, nil
		},
	}
}
