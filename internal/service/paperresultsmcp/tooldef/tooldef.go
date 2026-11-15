// Package tooldef is the contract between the Paper results MCP's dispatch
// table (package paperresultsmcp) and the tool packages that live beside it
// (backtesttool, virtualpapertool, jevshadowtool): a tool is declared here as a
// Def, built on the exported twins of the root's private argument, schema and
// failure helpers, and the root adapts each Def into its own table entry
// (ext_tools.go). A tool package can therefore not reach anything but what Env
// hands it; the table stays the only way for a request to reach SQL.
package tooldef

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/backtestjob"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// DefaultListLimit is the default of a listing tool's limit argument (API spec §2).
const DefaultListLimit = 50

// PeriodArgs are the from/to arguments of a period tool. Embed it in the
// tool's argument struct.
type PeriodArgs struct {
	From *string `json:"from"`
	To   *string `json:"to"`
}

// Env is what a tool needs besides its arguments.
type Env struct {
	Q      query.Q     // the read-only transaction of the call
	Range  query.Range // the resolved period (zero for a NoPeriod tool)
	Now    time.Time
	Client string // the caller's host, for the audit log
	// Writer is the narrow write connection, opened on first use.
	Writer func() (*mcpset.Writer, error)
	// Backtests is backtest_start's job manager.
	Backtests *backtestjob.Manager
}

// Run executes the tool and returns its data and the row count to log. An
// error that is (or wraps) a *Failure becomes that spec error; any other is
// classified by the root (busy, timeout, unavailable, internal).
type Run func(ctx context.Context, e Env) (data any, count int, err error)

// Def is one tool of the dispatch table.
type Def struct {
	Name        string
	Description string
	Schema      map[string]any
	// Writes marks a tool that can change state (annotations.readOnlyHint false).
	Writes bool
	// NoPeriod marks a tool without from/to arguments.
	NoPeriod bool
	// Decode parses the arguments (rejecting unknown ones) and returns the
	// period arguments plus a Run over the resolved period.
	Decode func(raw json.RawMessage) (PeriodArgs, Run, *Failure)
}

// StrictDecode unmarshals raw into dst, rejecting unknown fields, wrong types and trailing data; empty raw is {}.
func StrictDecode(raw json.RawMessage, dst any) *Failure {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		raw = json.RawMessage("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return &Failure{Code: CodeInvalidArgument, Cause: err}
	}
	if _, err := dec.Token(); err != io.EOF {
		return Fail(CodeInvalidArgument)
	}
	return nil
}

// ResolveLimit applies the default and the tool maximum. A limit above the
// maximum is limit_exceeded; below 1 is an invalid argument.
func ResolveLimit(requested *int, maxLimit int) (int, *Failure) {
	if requested == nil {
		return min(DefaultListLimit, maxLimit), nil
	}
	switch {
	case *requested < 1:
		return 0, Fail(CodeInvalidArgument)
	case *requested > maxLimit:
		return 0, Fail(CodeLimitExceeded)
	}
	return *requested, nil
}

// ObjectSchema is a closed object schema with the given properties.
func ObjectSchema(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

// PeriodSchema is ObjectSchema of the from/to arguments plus extra properties.
func PeriodSchema(extra map[string]any) map[string]any {
	props := map[string]any{
		"from": map[string]any{"type": "string", "description": "JST の開始日 YYYY-MM-DD（含む）。to と両方指定するか、両方省略（直近 30 日）"},
		"to":   map[string]any{"type": "string", "description": "JST の終了日 YYYY-MM-DD（含まない）。期間は最大 366 日"},
	}
	for k, v := range extra {
		props[k] = v
	}
	return map[string]any{"type": "object", "properties": props, "additionalProperties": false}
}

// LimitProp is the schema of a listing tool's limit argument.
func LimitProp(maxLimit int) map[string]any {
	return map[string]any{"type": "integer", "minimum": 1, "maximum": maxLimit, "default": min(DefaultListLimit, maxLimit)}
}

// NoArgs decodes the (empty) arguments of a tool that takes none.
func NoArgs(raw json.RawMessage) *Failure {
	return StrictDecode(raw, &struct{}{})
}
