package paperresultsmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/tooldef"
)

// Every code a tool package can raise is a code of the errors document: tooldef's
// string equals the root constant and the root's table has it.
func TestToolDefCodesAreTheRootsCodes(t *testing.T) {
	for def, root := range map[string]Code{
		tooldef.CodeInvalidArgument: CodeInvalidArgument, tooldef.CodeLimitExceeded: CodeLimitExceeded,
		tooldef.CodeConcurrencyLimited: CodeConcurrencyLimited, tooldef.CodeWriteDisabled: CodeWriteDisabled,
		tooldef.CodeInvalidValue: CodeInvalidValue, tooldef.CodeBacktestNotFound: CodeBacktestNotFound,
		tooldef.CodeBacktestUnavailable: CodeBacktestUnavailable, tooldef.CodeKeyNotAllowed: CodeKeyNotAllowed,
		tooldef.CodeProposalsDisabled: CodeProposalsDisabled, tooldef.CodeIdempotencyConflict: CodeIdempotencyConflict,
		tooldef.CodeProposalNotFound: CodeProposalNotFound, tooldef.CodeNotCancellable: CodeNotCancellable,
		tooldef.CodeMultipleVariables: CodeMultipleVariables, tooldef.CodeExperimentNotFound: CodeExperimentNotFound,
		tooldef.CodeExperimentNotRunning: CodeExperimentNotRunning,
	} {
		if _, ok := codeTable[Code(def)]; !ok || Code(def) != root {
			t.Errorf("tooldef code %q is not the root's code %q (or not in codeTable)", def, root)
		}
	}
}

// The adapter keeps the Failure's code and detail from Decode and from Run (also
// wrapped), and leaves any other error to classify.
func TestFromDefMapsFailures(t *testing.T) {
	runErr := error(nil)
	d := tooldef.Def{Name: "x", Decode: func(raw json.RawMessage) (tooldef.PeriodArgs, tooldef.Run, *tooldef.Failure) {
		if string(raw) == "bad" {
			return tooldef.PeriodArgs{}, nil, tooldef.FailDetail(tooldef.CodeInvalidValue, "why")
		}
		return tooldef.PeriodArgs{}, func(context.Context, tooldef.Env) (any, int, error) { return nil, 0, runErr }, nil
	}}
	_, _, te := fromDef(d).decode(json.RawMessage("bad"))
	if te == nil || te.code != CodeInvalidValue || te.detail != "why" {
		t.Fatalf("decode failure = %+v", te)
	}
	_, run, _ := fromDef(d).decode(json.RawMessage("{}"))
	for _, c := range []struct {
		err  error
		code Code
		det  string
	}{
		{tooldef.FailDetail(tooldef.CodeLimitExceeded, "d"), CodeLimitExceeded, "d"},
		{fmt.Errorf("wrapped: %w", tooldef.Fail(tooldef.CodeWriteDisabled)), CodeWriteDisabled, ""},
		{errors.New("boom"), CodeInternal, ""},
	} {
		runErr = c.err
		_, _, err := run(context.Background(), toolEnv{})
		if got := classify(context.Background(), err); got.code != c.code || got.detail != c.det {
			t.Errorf("run error %v -> %+v, want %s %q", c.err, got, c.code, c.det)
		}
	}
}
