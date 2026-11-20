// Package notetool defines the four research-note MCP tools (issue #1095;
// docs/api/endpoints/research-tools/paper-results-mcp-research-notes.md): research_note_create
// and research_note_update append one note version each through mcpset.Writer
// (behind the write switch); research_notes_list and research_note_get only
// read. A note is a record: none of them changes a setting or trading state,
// and key-shaped strings in the text are masked before anything is stored.
//
// The package does not import the MCP server package: the server turns each
// Spec into a tool of its dispatch table and maps an *Error to its spec error
// code (ErrorCode / ErrorDetail).
package notetool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/researchnote"
)

// The spec error codes this package returns (the server's Code values).
const (
	codeInvalidArgument = "invalid_argument"
	codeInvalidPeriod   = "invalid_period"
	codeLimitExceeded   = "limit_exceeded"
	codeNotFound        = "research_note_not_found"
	codeConflict        = "research_note_version_conflict"
	codeWriteDisabled   = "write_disabled"
)

// Env is what a tool run gets from the server: the read-only handle, the
// clock, the caller's host and the narrow write connection (opened on first use).
type Env struct {
	Q      query.Q
	Now    time.Time
	Client string
	Writer func() (*mcpset.Writer, error)
}

// Run executes a decoded call and returns the data, the row count to log and
// an error (an *Error for the tool's own failures; anything else is left to
// the server to classify: busy, timeout, unavailable, internal).
type Run func(ctx context.Context, env Env) (data any, count int, err error)

// Spec is one tool. Decode parses the arguments strictly (unknown fields,
// wrong types and trailing data are refused) and returns the run function.
type Spec struct {
	Name        string
	Description string
	Schema      map[string]any
	Writes      bool
	Decode      func(raw json.RawMessage) (Run, error)
}

// Specs returns the four research-note tools.
func Specs() []Spec {
	return []Spec{createSpec(), updateSpec(), listSpec(), getSpec()}
}

// Error is a tool failure with a spec error code. Detail is shown to the
// caller (it names the argument and the rule, never a value).
type Error struct {
	Code   string
	Detail string
	Err    error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return e.Code + ": " + e.Err.Error()
	}
	return e.Code
}

// Unwrap returns the cause, if any.
func (e *Error) Unwrap() error { return e.Err }

// ErrorCode is the spec error code.
func (e *Error) ErrorCode() string { return e.Code }

// ErrorDetail is the client-visible reason ("" for none).
func (e *Error) ErrorDetail() string { return e.Detail }

func fail(code string) *Error { return &Error{Code: code} }

func failDetail(code, detail string) *Error { return &Error{Code: code, Detail: detail} }

// strictDecode unmarshals raw into dst, rejecting unknown fields, wrong types
// and trailing data; empty raw is {}.
func strictDecode(raw json.RawMessage, dst any) error {
	if t := bytes.TrimSpace(raw); len(t) == 0 || bytes.Equal(t, []byte("null")) {
		raw = json.RawMessage("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return &Error{Code: codeInvalidArgument, Err: err}
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return fail(codeInvalidArgument)
	}
	return nil
}

func objectSchema(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

// failureOf maps a research-note error to the tool's spec error; anything
// else is returned as it is.
func failureOf(err error) error {
	if reason, ok := researchnote.IsInvalid(err); ok {
		return failDetail(codeInvalidArgument, reason)
	}
	var conflict *researchnote.ConflictError
	switch {
	case errors.As(err, &conflict):
		return failDetail(codeConflict, "current_version は "+strconv.Itoa(conflict.Current)+" です")
	case errors.Is(err, researchnote.ErrNotFound):
		return fail(codeNotFound)
	case errors.Is(err, mcpset.ErrWriteDisabled):
		return fail(codeWriteDisabled)
	}
	return err
}
