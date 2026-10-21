package proposaltests

import (
	"database/sql"
	"net/http"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

type cancelRequested struct {
	ProposalID        int64  `json:"proposal_id"`
	Status            string `json:"status"`
	CancelRequestedAt string `json:"cancel_requested_at"`
	Replayed          bool   `json:"replayed"`
}

func requestCancel(t *testing.T, srv *testServer, id int64) cancelRequested {
	t.Helper()
	var out cancelRequested
	srv.call(t, "order_proposal_cancel", map[string]any{"proposal_id": id}).data(t, "order_proposal_cancel", &out)
	return out
}

// setStatus puts a stored proposal into a state the application would have
// moved it to (the MCP cannot).
func setStatus(t *testing.T, w *sql.DB, id int64, status string) {
	t.Helper()
	testkit.MustExec(t, w, `UPDATE order_proposals SET status = ? WHERE id = ?`, status, id)
}

// order_proposal_cancel only records the request: the status stays, one event
// is appended, and nothing of the application's (orders, positions, signals) is
// touched. The application does the cancelling.
func TestCancelOnlyRecordsTheRequest(t *testing.T) {
	for _, status := range []string{"received", "waiting_session", "submitted"} {
		t.Run(status, func(t *testing.T) {
			srv, w := proposalServer(t)
			switchOn(t, w)
			id := submit(t, srv, validProposal("proposal-0001")).ProposalID
			setStatus(t, w, id, status)
			before := rowCounts(t, w)

			got := requestCancel(t, srv, id)
			if got.ProposalID != id || got.Status != status || got.Replayed || got.CancelRequestedAt != "2026-10-18T03:00:00Z" {
				t.Fatalf("cancel = %+v", got)
			}
			after := rowCounts(t, w)
			if after["order_proposal_events"] != before["order_proposal_events"]+1 {
				t.Fatalf("events %d -> %d, want one more", before["order_proposal_events"], after["order_proposal_events"])
			}
			for _, table := range []string{"order_proposals", "paper_orders", "positions", "trade_signals", "kill_switch_events", "runtime_settings"} {
				if after[table] != before[table] {
					t.Errorf("%s changed: %d -> %d", table, before[table], after[table])
				}
			}
			var from, to, reason, actor string
			if err := w.QueryRowContext(t.Context(), `SELECT from_status, to_status, reason_code, actor FROM order_proposal_events ORDER BY id DESC LIMIT 1`).
				Scan(&from, &to, &reason, &actor); err != nil {
				t.Fatal(err)
			}
			if from != status || to != status || reason != "cancel_requested" || actor != "mcp" {
				t.Errorf("event = %s>%s %q by %s, want %s>%s cancel_requested by mcp", from, to, reason, actor, status, status)
			}
			var st string
			if err := w.QueryRowContext(t.Context(), `SELECT status FROM order_proposals WHERE id = ?`, id).Scan(&st); err != nil || st != status {
				t.Fatalf("status = %q (%v), want it untouched: %s", st, err, status)
			}
		})
	}
}

func TestCancelRequestedTwiceReturnsTheFirstOne(t *testing.T) {
	srv, w := proposalServer(t)
	switchOn(t, w)
	id := submit(t, srv, validProposal("proposal-0001")).ProposalID
	first := requestCancel(t, srv, id)
	before := rowCounts(t, w)
	second := requestCancel(t, srv, id)
	if !second.Replayed || second.CancelRequestedAt != first.CancelRequestedAt {
		t.Fatalf("second = %+v, first = %+v", second, first)
	}
	wantSameCounts(t, w, before)
}

// A proposal the application already finished (or is working on) cannot be cancelled.
func TestCancelOfAFinishedOrProcessingProposalIsNotCancellable(t *testing.T) {
	for _, status := range []string{"processing", "filled", "closed", "rejected", "expired", "cancelled", "failed"} {
		t.Run(status, func(t *testing.T) {
			srv, w := proposalServer(t)
			switchOn(t, w)
			id := submit(t, srv, validProposal("proposal-0001")).ProposalID
			setStatus(t, w, id, status)
			before := rowCounts(t, w)
			srv.call(t, "order_proposal_cancel", map[string]any{"proposal_id": id}).wantError(t, http.StatusConflict, paperresultsmcp.CodeNotCancellable)
			wantSameCounts(t, w, before)
			if n := count(t, w, `SELECT COUNT(*) FROM order_proposals WHERE cancel_requested_at IS NOT NULL`); n != 0 {
				t.Fatalf("%d proposals carry a cancel request, want 0", n)
			}
		})
	}
}

func TestCancelOfUnknownProposalAndBadArguments(t *testing.T) {
	srv, w := proposalServer(t)
	switchOn(t, w)
	srv.call(t, "order_proposal_cancel", map[string]any{"proposal_id": 42}).wantError(t, http.StatusNotFound, paperresultsmcp.CodeProposalNotFound)
	for name, args := range map[string]map[string]any{
		"missing id": {}, "zero": {"proposal_id": 0}, "negative": {"proposal_id": -1}, "unknown field": {"proposal_id": 1, "force": true},
	} {
		t.Run(name, func(t *testing.T) {
			srv.call(t, "order_proposal_cancel", args).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
		})
	}
}

// Cancelling reduces risk, so it works while the intake switch is off.
func TestCancelWorksWhileTheIntakeSwitchIsOff(t *testing.T) {
	srv, w := proposalServer(t)
	switchOn(t, w)
	id := submit(t, srv, validProposal("proposal-0001")).ProposalID
	srv.call(t, "order_proposals_disable", map[string]any{}).data(t, "order_proposals_disable", &struct{}{})
	if got := requestCancel(t, srv, id); got.Replayed || got.Status != "received" {
		t.Fatalf("cancel = %+v", got)
	}
}
