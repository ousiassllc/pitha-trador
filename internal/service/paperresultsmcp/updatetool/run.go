package updatetool

import (
	"context"

	"github.com/ousiassllc/pitha-trador/internal/repository/updatecontrol"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/tooldef"
)

// Reasons of this package, besides updatecontrol.ReasonNotReady and the SafeGate kinds.
const (
	reasonUnavailable = "unavailable" // no updater, or the application is not running
	reasonTimeout     = "timeout"     // the application took no request in time; withdrawn
)

// Results of update_apply.
const (
	resultAccepted = "accepted"
	resultDeferred = "deferred"
	resultPending  = "pending"
)

// checkData is update_check_now's data: the status after the check, plus how the check ended.
type checkData struct {
	view
	// Check is completed / failed (error_kind says why) / running (not finished within the call) / expired.
	Check string `json:"check,omitempty"`
}

func runCheckNow(ctx context.Context, e tooldef.Env) (any, int, error) {
	v, err := readView(ctx, e.Q, e.Now)
	if err != nil || v.Unavailable {
		return checkData{view: v, Check: ""}, 1, err
	}
	id, err := enqueue(ctx, e, updatecontrol.KindCheck, false)
	if err != nil {
		return nil, 0, err
	}
	q, closeFn, err := freshRead(e)
	if err != nil {
		return nil, 0, err
	}
	defer closeFn()
	req, err := wait(ctx, q, id, waitBudget)
	if err != nil {
		return nil, 0, err
	}
	if v, err = readView(ctx, q, e.Now); err != nil {
		return nil, 0, err
	}
	return checkData{view: v, Check: checkState(req)}, 1, nil
}

func checkState(req updatecontrol.Request) string {
	switch {
	case !req.Done():
		return "running"
	case req.Outcome == updatecontrol.OutcomeChecked:
		return "completed"
	case req.Outcome == updatecontrol.OutcomeCheckFailed:
		return "failed"
	}
	return "expired"
}

// applyData is update_apply's data.
type applyData struct {
	Result string `json:"result"`
	// Reason is set for a deferral: a SafeGate kind, not_ready, unavailable or timeout.
	Reason     string `json:"reason,omitempty"`
	ReasonText string `json:"reason_text,omitempty"`
	RequestID  int64  `json:"request_id,omitempty"`
}

func deferred(reason string, id int64) applyData {
	return applyData{Result: resultDeferred, Reason: reason, ReasonText: reasonText(reason), RequestID: id}
}

func runApply(ctx context.Context, e tooldef.Env) (any, int, error) {
	// The write switch first, before anything is read about the updater or recorded.
	on, err := mcpset.WriteEnabled(ctx, mcpset.ReadStore{Q: e.Q})
	if err != nil {
		return nil, 0, err
	}
	if !on {
		return nil, 0, tooldef.Fail(tooldef.CodeWriteDisabled)
	}
	v, err := readView(ctx, e.Q, e.Now)
	if err != nil {
		return nil, 0, err
	}
	if v.Unavailable {
		return deferred(reasonUnavailable, 0), 1, nil
	}
	id, err := enqueue(ctx, e, updatecontrol.KindApply, true) // the switch is checked again in the transaction that records the request
	if err != nil {
		return nil, 0, err
	}
	q, closeFn, err := freshRead(e)
	if err != nil {
		return nil, 0, err
	}
	defer closeFn()
	req, err := wait(ctx, q, id, waitBudget)
	if err != nil {
		return nil, 0, err
	}
	if !req.Done() {
		// Never let the request run later, after this call has answered "not done".
		withdrawn, err := withdraw(ctx, e, id)
		if err != nil {
			return nil, 0, err
		}
		if withdrawn {
			return deferred(reasonTimeout, id), 1, nil
		}
		if req, err = wait(ctx, q, id, settleBudget); err != nil { // the application took it in the meantime
			return nil, 0, err
		}
	}
	return applyResult(req), 1, nil
}

func applyResult(req updatecontrol.Request) applyData {
	switch {
	case !req.Done():
		return applyData{Result: resultPending, RequestID: req.ID}
	case req.Outcome == updatecontrol.OutcomeAccepted:
		return applyData{Result: resultAccepted, RequestID: req.ID}
	case req.Outcome == updatecontrol.OutcomeDeferred:
		return deferred(req.Reason, req.ID)
	}
	return deferred(reasonTimeout, req.ID) // expired: dropped unexecuted
}
