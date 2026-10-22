package proposalexec_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/proposalexec"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/domain/orderproposal"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/service/risk"
)

// calls counts the calls that decide, place, close or cancel; outside Paper
// mode it must stay 0 (issue #858).
type calls struct{ checkThenEnter, enter, closeAtMarket, cancelPending atomic.Int32 }

func (c *calls) total() int32 {
	return c.checkThenEnter.Load() + c.enter.Load() + c.closeAtMarket.Load() + c.cancelPending.Load()
}

type countingRisk struct {
	proposalexec.Risk
	c *calls
}

func (r countingRisk) CheckThenEnter(ctx context.Context, id int64, dir string, enter func(risk.EntrySize) error) (bool, string, error) {
	r.c.checkThenEnter.Add(1)
	return r.Risk.CheckThenEnter(ctx, id, dir, enter)
}

type countingExecution struct {
	proposalexec.Execution
	c *calls
}

func (e countingExecution) Enter(ctx context.Context, req execution.EntryRequest) (execution.EntryResult, error) {
	e.c.enter.Add(1)
	return e.Execution.Enter(ctx, req)
}

func (e countingExecution) CloseAtMarket(ctx context.Context, id int64, reason string, now time.Time) (domain.Position, error) {
	e.c.closeAtMarket.Add(1)
	return e.Execution.CloseAtMarket(ctx, id, reason, now)
}

func (e countingExecution) CancelPendingEntry(ctx context.Context, id int64) error {
	e.c.cancelPending.Add(1)
	return e.Execution.CancelPendingEntry(ctx, id)
}

// nonPaperFixture seeds every kind of work (entry, close, waiting, processing,
// submitted with a cancellation request) and switches the processor to mode.
func nonPaperFixture(t *testing.T, mode metalabelsettings.Mode) (*fixture, *calls) {
	t.Helper()
	f := newFixture(t)
	submitted := f.propose(spec{orderType: domain.OrderTypeLimit, limit: 2400, quantity: 100})
	f.cycle() // the Paper processor places the limit order
	f.wantEnd(submitted, orderproposal.StatusSubmitted, "")
	f.requestCancel(submitted)
	f.propose(spec{quantity: 100})
	f.mustExec(`UPDATE order_proposals SET status = 'waiting_session' WHERE id = ?`, f.propose(spec{}))
	f.mustExec(`UPDATE order_proposals SET status = 'processing' WHERE id = ?`, f.propose(spec{}))
	f.openPosition(100, 2500)
	f.propose(spec{action: orderproposal.ActionClose})

	c := &calls{}
	f.proc.Mode = mode
	f.proc.Risk = countingRisk{Risk: f.proc.Risk, c: c}
	f.proc.Execution = countingExecution{Execution: f.proc.Execution, c: c}
	return f, c
}

func (f *fixture) snapshot() string {
	f.t.Helper()
	var b strings.Builder
	for _, table := range []string{"order_proposals", "order_proposal_events", "trade_signals", "paper_orders", "positions"} {
		b.WriteString(table + "\n" + f.dump(`SELECT * FROM `+table+` ORDER BY id`))
	}
	return b.String()
}

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

var nonPaperModes = map[string]metalabelsettings.Mode{"live": metalabelsettings.ModeLive, "empty": ""}

// Cycle and Recover called directly outside Paper mode error before reading the database.
func TestProcessor_RefusesToRunOutsidePaperMode(t *testing.T) {
	for name, mode := range nonPaperModes {
		t.Run(name, func(t *testing.T) {
			f, c := nonPaperFixture(t, mode)
			before := f.snapshot()
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
			if got := c.total(); got != 0 {
				t.Fatalf("Risk/Execution calls = %d, want 0", got)
			}
		})
	}
}

// Launch outside Paper mode logs one error and starts no Recover and no Run goroutine.
func TestLaunch_DoesNotStartOutsidePaperMode(t *testing.T) {
	for name, mode := range nonPaperModes {
		t.Run(name, func(t *testing.T) {
			f, c := nonPaperFixture(t, mode)
			logs := captureLog(t)
			before := f.snapshot()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var wg sync.WaitGroup
			f.proc.Launch(ctx, &wg)
			done := make(chan struct{})
			go func() { wg.Wait(); close(done) }()
			select {
			case <-done: // nothing was added to the group: no Run, no Recover
			case <-time.After(5 * time.Second):
				t.Fatal("Launch started a goroutine outside Paper mode")
			}
			if after := f.snapshot(); after != before {
				t.Fatalf("rows changed outside Paper mode:\nbefore:\n%s\nafter:\n%s", before, after)
			}
			if got := c.total(); got != 0 {
				t.Fatalf("Risk/Execution calls = %d, want 0", got)
			}
			out := logs.String()
			if strings.Count(out, "proposalexec: refusing to run outside Paper mode") != 1 || !strings.Contains(out, "level=ERROR") ||
				strings.Contains(out, "recovered order proposals") {
				t.Fatalf("log = %q, want exactly the refusal error", out)
			}
		})
	}
}
