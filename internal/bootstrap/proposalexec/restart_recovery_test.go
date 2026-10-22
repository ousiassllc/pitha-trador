package proposalexec_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain/orderproposal"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// Past expires_at without an order: expired right away; within time: received;
// an order went out: its outcome. Afterwards nothing is in processing.
func TestRestart_ExpiredProcessingProposalBecomesExpired(t *testing.T) {
	f := newFixture(t)
	expired := f.propose(spec{expires: at(10, 0, 10)}) // past at 10:00:30
	ordered := f.propose(spec{quantity: 100})
	f.cycle() // ordered fills (an order and a position); expired expires
	f.stopMidFlight(ordered)
	f.mustExec(`UPDATE order_proposals SET expires_at = ? WHERE id = ?`, sqlutil.FormatTime(at(10, 0, 20)), ordered)
	fresh := f.propose(spec{symbol: "6758", quantity: 100})
	f.mustExec(`UPDATE order_proposals SET status = 'processing' WHERE id IN (?, ?)`, expired, fresh)

	logs := captureLog(t)
	if n := f.recover(); n != 3 {
		t.Fatalf("Recover = %d, want 3", n)
	}
	f.wantEnd(expired, orderproposal.StatusExpired, "expired")
	f.wantEnd(fresh, orderproposal.StatusReceived, "recovered_after_restart")
	f.wantEnd(ordered, orderproposal.StatusFilled, "")
	if n := f.count("order_proposals", "WHERE status = 'processing'"); n != 0 {
		t.Fatalf("%d proposals left in processing", n)
	}
	if !strings.Contains(logs.String(), "count=3 expired=1 received=1 failed=0 still_processing=0") {
		t.Fatalf("recovery log = %s", logs.String())
	}
}

// An unrecoverable proposal (filled order, position gone) is logged and failed; the others are recovered.
func TestRestart_RecoveryFailureDoesNotLeaveProcessing(t *testing.T) {
	f := newFixture(t)
	broken := f.propose(spec{quantity: 100})
	f.cycle()
	f.stopMidFlight(broken)
	f.mustExec(`DELETE FROM positions`)
	other := f.propose(spec{symbol: "6758", quantity: 100})
	f.mustExec(`UPDATE order_proposals SET status = 'processing' WHERE id = ?`, other)

	logs := captureLog(t)
	n, err := f.proc.Recover(context.Background())
	if err == nil || n != 2 {
		t.Fatalf("Recover = (%d, %v), want 2 and the one failure", n, err)
	}
	f.wantEnd(broken, orderproposal.StatusFailed, "processing_error")
	f.wantEnd(other, orderproposal.StatusReceived, "recovered_after_restart")
	if !strings.Contains(logs.String(), "level=ERROR") || !strings.Contains(logs.String(), "proposal_id=1 ") {
		t.Fatalf("the failure was not logged with the proposal: %s", logs.String())
	}
}

func TestRestart_LaunchLeavesNothingInProcessing(t *testing.T) {
	f := newFixture(t)
	f.propose(spec{expires: at(10, 0, 10)})
	f.propose(spec{quantity: 100})
	f.mustExec(`UPDATE order_proposals SET status = 'processing'`)
	logs := captureLog(t)
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	f.proc.Launch(ctx, &wg)
	for deadline := time.Now().Add(10 * time.Second); f.count("order_proposals", "WHERE status = 'processing'") != 0 && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	wg.Wait()
	if n := f.count("order_proposals", "WHERE status = 'processing'"); n != 0 {
		t.Fatalf("%d proposals in processing after startup", n)
	}
	if !strings.Contains(logs.String(), "bootstrap: recovered order proposals left in processing\" count=2") {
		t.Fatalf("startup log lacks the recovery line: %s", logs.String())
	}
}

// A panic while recovering one proposal fails that one; the others are recovered.
func TestRestart_RecoveryPanicDoesNotLeaveProcessing(t *testing.T) {
	f := newFixture(t)
	broken := f.propose(spec{quantity: 100})
	f.cycle()
	f.stopMidFlight(broken) // has a signal: recovery looks its order up
	other := f.propose(spec{symbol: "6758", quantity: 100})
	f.mustExec(`UPDATE order_proposals SET status = 'processing' WHERE id = ?`, other)
	f.proc.Orders = nil // the order lookup now panics

	n, err := f.proc.Recover(context.Background())
	if err == nil || n != 2 || !strings.Contains(err.Error(), "panic while recovering proposal") {
		t.Fatalf("Recover = (%d, %v), want 2 and the panic as an error", n, err)
	}
	f.wantEnd(broken, orderproposal.StatusFailed, "processing_error")
	f.wantEnd(other, orderproposal.StatusReceived, "recovered_after_restart")
}
