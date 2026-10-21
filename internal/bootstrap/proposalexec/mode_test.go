package proposalexec_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/proposalexec"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/domain/orderproposal"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/service/risk"
)

// callCounter counts the calls that would decide, place, close or cancel
// something. Outside Paper mode every counter must stay 0 (issue #858).
type callCounter struct {
	mu                                                  sync.Mutex
	checkThenEnter, enter, closeAtMarket, cancelPending int
}

func (c *callCounter) total() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.checkThenEnter + c.enter + c.closeAtMarket + c.cancelPending
}

type countingRisk struct {
	proposalexec.Risk
	calls *callCounter
}

func (r countingRisk) CheckThenEnter(ctx context.Context, instrumentID int64, direction string, enter func(size risk.EntrySize) error) (bool, string, error) {
	r.calls.mu.Lock()
	r.calls.checkThenEnter++
	r.calls.mu.Unlock()
	return r.Risk.CheckThenEnter(ctx, instrumentID, direction, enter)
}

type countingExecution struct {
	proposalexec.Execution
	calls *callCounter
}

func (e countingExecution) Enter(ctx context.Context, req execution.EntryRequest) (execution.EntryResult, error) {
	e.calls.mu.Lock()
	e.calls.enter++
	e.calls.mu.Unlock()
	return e.Execution.Enter(ctx, req)
}

func (e countingExecution) CloseAtMarket(ctx context.Context, positionID int64, reason string, now time.Time) (domain.Position, error) {
	e.calls.mu.Lock()
	e.calls.closeAtMarket++
	e.calls.mu.Unlock()
	return e.Execution.CloseAtMarket(ctx, positionID, reason, now)
}

func (e countingExecution) CancelPendingEntry(ctx context.Context, orderID int64) error {
	e.calls.mu.Lock()
	e.calls.cancelPending++
	e.calls.mu.Unlock()
	return e.Execution.CancelPendingEntry(ctx, orderID)
}

// nonPaperFixture seeds every kind of work the processor has (a proposal to
// open, one to close an open position, one waiting for a session, one left in
// processing, one past its expiry, a submitted limit order with a cancellation
// request) and returns the fixture with its processor switched to mode and
// wrapped in the call counters.
func nonPaperFixture(t *testing.T, mode metalabelsettings.Mode) (*fixture, *callCounter) {
	t.Helper()
	f := newFixture(t)
	submitted := f.propose(spec{orderType: domain.OrderTypeLimit, limit: 2400, quantity: 100})
	f.cycle() // the Paper processor places the limit order: the proposal is submitted
	f.wantEnd(submitted, orderproposal.StatusSubmitted, "")
	f.requestCancel(submitted)

	f.propose(spec{quantity: 100})
	f.propose(spec{expires: at(9, 31, 0)}) // already expired
	waiting, processing := f.propose(spec{}), f.propose(spec{})
	for id, status := range map[int64]string{waiting: orderproposal.StatusWaitingSession, processing: orderproposal.StatusProcessing} {
		if _, err := f.db.Exec(`UPDATE order_proposals SET status = ? WHERE id = ?`, status, id); err != nil {
			t.Fatal(err)
		}
	}
	f.openPosition(100, 2500)
	f.propose(spec{action: orderproposal.ActionClose})

	calls := &callCounter{}
	f.proc.Mode = mode
	f.proc.Risk = countingRisk{Risk: f.proc.Risk, calls: calls}
	f.proc.Execution = countingExecution{Execution: f.proc.Execution, calls: calls}
	return f, calls
}

// snapshot is every table the processor writes, rendered row by row.
func (f *fixture) snapshot() string {
	f.t.Helper()
	var b strings.Builder
	for _, table := range []string{"order_proposals", "order_proposal_events", "trade_signals", "paper_orders", "positions"} {
		b.WriteString(table + "\n" + f.dump(`SELECT * FROM `+table+` ORDER BY id`))
	}
	return b.String()
}

const statusCounts = `SELECT status, count(*) AS n FROM order_proposals GROUP BY 1 ORDER BY 1`

// captureLog redirects the default logger for the test and returns what was
// written to it.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

var nonPaperModes = map[string]metalabelsettings.Mode{
	"live":  metalabelsettings.ModeLive,
	"empty": "",
}

// Cycle called directly outside Paper mode (live, or no mode at all) returns an
// error before it reads the database: nothing is written, and neither Risk nor
// Execution is called.
func TestProcessor_RefusesToRunOutsidePaperMode(t *testing.T) {
	for name, mode := range nonPaperModes {
		t.Run(name, func(t *testing.T) {
			f, calls := nonPaperFixture(t, mode)
			before, beforeStatus := f.snapshot(), f.dump(statusCounts)

			n, err := f.proc.Cycle(context.Background())
			if !errors.Is(err, proposalexec.ErrNotPaperMode) || n != 0 {
				t.Fatalf("Cycle = (%d, %v), want (0, ErrNotPaperMode)", n, err)
			}
			if _, err := f.proc.Recover(context.Background()); !errors.Is(err, proposalexec.ErrNotPaperMode) {
				t.Fatalf("Recover error = %v, want ErrNotPaperMode", err)
			}
			if after := f.snapshot(); after != before {
				t.Fatalf("rows changed outside Paper mode:\nbefore:\n%s\nafter:\n%s", before, after)
			}
			if got := calls.total(); got != 0 {
				t.Fatalf("Risk/Execution calls = %+v, want 0", calls)
			}
			t.Logf("mode=%q Cycle error: %v", mode, err)
			t.Logf("order_proposals by status, before:\n%safter:\n%s", beforeStatus, f.dump(statusCounts))
		})
	}
}

// Launch outside Paper mode logs one error line and starts neither Recover nor
// the Run goroutine: the WaitGroup stays empty, and the proposals (the one left
// in processing included) and every other table are untouched.
func TestLaunch_DoesNotStartOutsidePaperMode(t *testing.T) {
	for name, mode := range nonPaperModes {
		t.Run(name, func(t *testing.T) {
			f, calls := nonPaperFixture(t, mode)
			logs := captureLog(t)
			before, beforeStatus := f.snapshot(), f.dump(statusCounts)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var wg sync.WaitGroup
			f.proc.Launch(ctx, &wg)
			done := make(chan struct{})
			go func() { wg.Wait(); close(done) }()
			select {
			case <-done: // nothing was added to the group
			case <-time.After(5 * time.Second):
				t.Fatal("Launch started a goroutine outside Paper mode")
			}

			// Launch added nothing to the group, so no Run or Recover is running or can still write.
			if after := f.snapshot(); after != before {
				t.Fatalf("rows changed outside Paper mode:\nbefore:\n%s\nafter:\n%s", before, after)
			}
			if got := calls.total(); got != 0 {
				t.Fatalf("Risk/Execution calls = %+v, want 0", calls)
			}
			out := logs.String()
			if strings.Count(out, "proposalexec: refusing to run outside Paper mode") != 1 || !strings.Contains(out, "level=ERROR") ||
				!strings.Contains(out, `mode="`+string(mode)+`"`) && !strings.Contains(out, `mode=`+string(mode)) {
				t.Fatalf("log = %q, want one error line 'proposalexec: refusing to run outside Paper mode' with the mode", out)
			}
			if strings.Contains(out, "recovered order proposals") || strings.Contains(out, "recovery failed") {
				t.Fatalf("log = %q: Recover must not start", out)
			}
			t.Logf("log line: %s", strings.TrimSpace(out))
			t.Logf("order_proposals by status, before:\n%safter:\n%s", beforeStatus, f.dump(statusCounts))
		})
	}
}

// The same processor in Paper mode does work, so the tests above stop it by the
// mode and nothing else.
func TestProcessor_PaperModeStillProcesses(t *testing.T) {
	f, calls := nonPaperFixture(t, metalabelsettings.ModePaper)
	if n, err := f.proc.Cycle(context.Background()); err != nil || n == 0 {
		t.Fatalf("Cycle = (%d, %v), want work done", n, err)
	}
	if calls.total() == 0 {
		t.Fatal("Paper mode made no Risk/Execution call: the Live tests would pass vacuously")
	}
}
