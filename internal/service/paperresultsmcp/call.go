package paperresultsmcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"net"
	"net/http"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

type callParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// handleToolCall validates the call, takes a concurrency slot and runs the
// tool in one read-only transaction under the 3 s budget.
func (s *Server) handleToolCall(w http.ResponseWriter, r *http.Request, req rpcRequest) {
	start := s.now()
	toolName := ""
	var rng query.Range
	var rows int

	finish := func(te *toolError) {
		attrs := []any{"tool", toolName, "duration_ms", s.now().Sub(start).Milliseconds()}
		if !rng.From.IsZero() {
			attrs = append(attrs, "from", rng.From.In(query.JST).Format(time.DateOnly), "to", rng.To.In(query.JST).Format(time.DateOnly))
		}
		if te == nil {
			s.log.Info("paper-results-mcp: tool call", append(attrs, "code", "", "rows", rows)...)
			return
		}
		// Only the code is logged for a failure: the cause may quote SQL.
		s.log.Warn("paper-results-mcp: tool call failed", append(attrs, "code", string(te.code))...)
		writeFailure(w, req.ID, te)
	}

	var params callParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		finish(&toolError{code: CodeInvalidArgument, cause: err})
		return
	}
	t, ok := findTool(params.Name)
	if !ok {
		finish(fail(CodeInvalidArgument))
		return
	}
	toolName = t.name
	period, run, te := t.decode(params.Arguments)
	if te != nil {
		finish(te)
		return
	}
	if !t.noPeriod {
		if rng, te = period.resolve(s.now()); te != nil {
			finish(te)
			return
		}
	}

	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		finish(fail(CodeConcurrencyLimited))
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.opts.QueryTimeout)
	defer cancel()
	data, n, err := s.execute(ctx, run, rng, clientHost(r))
	if err != nil {
		finish(classify(ctx, err))
		return
	}
	rows = n
	finish(nil)

	body := map[string]any{"tool": t.name, "data": data}
	text, merr := json.Marshal(body)
	if merr != nil {
		writeError(w, req.ID, CodeInternal)
		return
	}
	writeResult(w, req.ID, map[string]any{
		"content":           []map[string]any{{"type": "text", "text": string(text)}},
		"structuredContent": body,
		"isError":           false,
	})
}

// execute runs one tool on the read-only database inside a single
// transaction, so all its queries see one snapshot. The transaction is only
// ever rolled back: nothing is written through it. The settings tools that
// write do so through env.writer, a separate narrow connection.
func (s *Server) execute(ctx context.Context, run runFunc, rng query.Range, client string) (any, int, error) {
	db, err := s.DB()
	if err != nil {
		return nil, 0, err
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = tx.Rollback() }()
	return run(ctx, toolEnv{
		q: tx, r: rng, capitalJPY: s.opts.CapitalJPY, now: s.now(),
		client: client, paperLimits: s.opts.PaperLimits, policy: s.opts.PolicyThresholds, writer: s.Writer,
	})
}

// clientHost is the caller's host for the audit log: the IP of the TCP peer,
// never a header value (headers are caller-controlled).
func clientHost(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
