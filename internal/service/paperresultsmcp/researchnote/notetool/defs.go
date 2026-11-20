package notetool

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/tooldef"
)

// Defs are the four tools as dispatch-table definitions (the root joins them in
// ext_tools.go). None takes a period.
func Defs() []tooldef.Def {
	specs := Specs()
	defs := make([]tooldef.Def, 0, len(specs))
	for _, s := range specs {
		defs = append(defs, tooldef.Def{
			Name: s.Name, Description: s.Description, Schema: s.Schema, Writes: s.Writes, NoPeriod: true,
			Decode: func(raw json.RawMessage) (tooldef.PeriodArgs, tooldef.Run, *tooldef.Failure) {
				run, err := s.Decode(raw)
				if err != nil {
					return tooldef.PeriodArgs{}, nil, failureFrom(err)
				}
				return tooldef.PeriodArgs{}, func(ctx context.Context, e tooldef.Env) (any, int, error) {
					return run(ctx, Env{Q: e.Q, Now: e.Now, Client: e.Client, Writer: e.Writer})
				}, nil
			},
		})
	}
	return defs
}

// failureFrom is a decode error as the spec failure: a coded error keeps its
// code and detail; anything else is an invalid argument.
func failureFrom(err error) *tooldef.Failure {
	var ce query.CodedError
	if errors.As(err, &ce) {
		return &tooldef.Failure{Code: ce.ErrorCode(), Detail: ce.ErrorDetail(), Cause: err}
	}
	return &tooldef.Failure{Code: tooldef.CodeInvalidArgument, Cause: err}
}
