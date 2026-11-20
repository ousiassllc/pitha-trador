// Package backtestjob runs the research backtests the Paper results MCP starts
// (issue #1086; docs/architecture/paper-results-mcp-backtest.md) in the
// background of the MCP process: at most one at a time, each under a
// wall-clock cap, each recorded in backtest_runs from the queued row to its
// final status.
//
// It reads stored market data through an injected backtestlab.ConfigSource
// (never opened here: the service must not import internal/bootstrap) and
// writes only its own run rows through an injected Store. A run changes no
// setting and nothing it produces is an input of trading.
package backtestjob

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/backtestrun"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/backtestlab"
	"github.com/ousiassllc/pitha-trador/internal/version"
)

// Limits of the job manager.
const (
	// MaxRunning is how many backtests may run at once. A second start is
	// refused (ErrBusy) and writes nothing.
	MaxRunning = 1
	// DefaultRunTimeout is the wall-clock cap of one run, from the moment it
	// starts replaying. A run that is over it is stopped (its context is
	// cancelled) and recorded as failed with error_code timed_out.
	DefaultRunTimeout = 10 * time.Minute
	// finalWriteTimeout bounds each write of a run's final status, which must
	// land even after the run's context ended.
	finalWriteTimeout = 5 * time.Second
	// finalWriteAttempts is how many times the final status is written.
	finalWriteAttempts = 3
)

var (
	// ErrBusy is returned by Start while another backtest is running.
	ErrBusy = errors.New("backtestjob: a backtest is already running")
	// ErrUnavailable is returned by Start when no source of the replay input is
	// configured, or the manager is closed.
	ErrUnavailable = errors.New("backtestjob: backtests are not available")
)

// SourceFactory builds the replay input for a replay mode ("paper" or "live").
// It runs inside the job, so it may open a database lazily; ctx is the run's.
type SourceFactory func(ctx context.Context, replayMode string) (backtestlab.ConfigSource, error)

// Store is the write side of the run rows. mcpset.Writer satisfies it.
type Store interface {
	CreateBacktestRun(ctx context.Context, client string, req backtestrun.NewRun) (backtestrun.Run, error)
	StartBacktestRun(ctx context.Context, id int64) error
	FinishBacktestRun(ctx context.Context, id int64, out backtestrun.Outcome) error
	FailOrphanBacktestRuns(ctx context.Context, owner string) (int64, error)
}

// Config configures a Manager.
type Config struct {
	// Source builds the replay input. Nil: Start returns ErrUnavailable.
	Source SourceFactory
	// Store opens the write side on first use (a failed open is retried).
	Store func() (Store, error)
	// Owner stamps the rows this process creates; "" is a fresh random token.
	Owner string
	// RunTimeout is the wall-clock cap of a run; zero is DefaultRunTimeout.
	RunTimeout time.Duration
	// CodeVersion is recorded with every run; "" is version.Version.
	CodeVersion string
	// Logger receives the operational log; nil is slog.Default.
	Logger *slog.Logger
	// OnFinish, when set, is called after a run's final status was written (a
	// test seam: it lets a test wait for the end without sleeping).
	OnFinish func(runID int64)
}

// Job is one accepted request.
type Job struct {
	// Client is the caller's host (audit).
	Client string
	// FromDate / ToDate are the JST dates of the period, [from, to).
	FromDate, ToDate string
	Period           backtest.Period
	// ReplayMode is "paper" or "live".
	ReplayMode string
	Request    backtestlab.Request
}

// Manager starts and tracks the backtests of one process.
type Manager struct {
	cfg  Config
	log  *slog.Logger
	root context.Context
	stop context.CancelFunc
	wg   sync.WaitGroup

	mu        sync.Mutex
	running   int
	closed    bool
	recovered bool
}

// New returns a Manager. It performs no I/O.
func New(cfg Config) *Manager {
	if cfg.Owner == "" {
		cfg.Owner = newOwner()
	}
	if cfg.RunTimeout <= 0 {
		cfg.RunTimeout = DefaultRunTimeout
	}
	if cfg.CodeVersion == "" {
		cfg.CodeVersion = version.Version
	}
	m := &Manager{cfg: cfg, log: cfg.Logger}
	if m.log == nil {
		m.log = slog.Default()
	}
	m.root, m.stop = context.WithCancel(context.Background())
	return m
}

// newOwner is a random token that tells this process's rows from an earlier one's.
func newOwner() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil { // crypto/rand does not fail on supported platforms
		return "owner-unknown"
	}
	return hex.EncodeToString(b)
}

// Owner is the token this process stamps on its rows.
func (m *Manager) Owner() string { return m.cfg.Owner }

// Start accepts job: it takes the single run slot (ErrBusy when taken, nothing
// written), records the run as queued (mcpset.ErrWriteDisabled while the write
// switch is off, nothing written) and replays it in the background. It
// returns the queued row at once. Before the first run it marks the runs an
// earlier process left unfinished as failed (interrupted).
func (m *Manager) Start(ctx context.Context, job Job) (backtestrun.Run, error) {
	if m.cfg.Source == nil {
		return backtestrun.Run{}, ErrUnavailable
	}
	store, err := m.cfg.Store()
	if err != nil {
		return backtestrun.Run{}, err
	}
	if err := m.recoverOrphans(ctx, store); err != nil {
		return backtestrun.Run{}, err
	}
	if err := m.acquire(); err != nil {
		return backtestrun.Run{}, err
	}
	request, err := json.Marshal(job.Request)
	if err != nil {
		m.release()
		return backtestrun.Run{}, fmt.Errorf("backtestjob: encode request: %w", err)
	}
	run, err := store.CreateBacktestRun(ctx, job.Client, backtestrun.NewRun{
		Owner: m.cfg.Owner, FromDate: job.FromDate, ToDate: job.ToDate, ReplayMode: job.ReplayMode,
		CodeVersion: m.cfg.CodeVersion, ExploredCount: len(job.Request.Variants), RequestJSON: string(request),
	})
	if err != nil {
		m.release()
		return backtestrun.Run{}, err
	}
	go m.execute(store, run, job)
	return run, nil
}

// acquire takes the run slot.
func (m *Manager) acquire() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch {
	case m.closed:
		return ErrUnavailable
	case m.running >= MaxRunning:
		return ErrBusy
	}
	m.running++
	m.wg.Add(1) // under the lock: Close sets closed under it before it waits
	return nil
}

func (m *Manager) release() {
	m.mu.Lock()
	m.running--
	m.mu.Unlock()
	m.wg.Done()
}

// recoverOrphans runs once per process, before the first run: rows an earlier
// process left queued or running (it was stopped mid-run) become failed with
// error_code interrupted. A failure is retried by the next start.
func (m *Manager) recoverOrphans(ctx context.Context, store Store) error {
	m.mu.Lock()
	done := m.recovered
	m.mu.Unlock()
	if done {
		return nil
	}
	n, err := store.FailOrphanBacktestRuns(ctx, m.cfg.Owner)
	if err != nil {
		return err
	}
	if n > 0 {
		m.log.Warn("paper-results-mcp: backtest runs left unfinished by an earlier process were marked interrupted", "runs", n)
	}
	m.mu.Lock()
	m.recovered = true
	m.mu.Unlock()
	return nil
}

// Close stops the run in flight (it is recorded as interrupted), waits for it
// and refuses further starts.
func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()
	m.stop()
	m.wg.Wait()
}

// execute replays one run and records how it ended. It holds the run slot
// until the final status is written (or given up on).
func (m *Manager) execute(store Store, run backtestrun.Run, job Job) {
	ctx, cancel := context.WithTimeout(m.root, m.cfg.RunTimeout)
	defer cancel()

	m.write(func(ctx context.Context) error { return store.StartBacktestRun(ctx, run.ID) })
	out := m.replay(ctx, job)
	m.write(func(ctx context.Context) error { return store.FinishBacktestRun(ctx, run.ID, out) })
	m.log.Info("paper-results-mcp: backtest run ended", "run_id", run.ID, "error_code", out.ErrorCode)
	m.release()
	if m.cfg.OnFinish != nil {
		m.cfg.OnFinish(run.ID)
	}
}

// write runs one write of the run's own row. It does not depend on the run's
// context (a timed-out run must still be recorded) and is tried a few times:
// the application may hold the database's write lock for a moment.
func (m *Manager) write(fn func(ctx context.Context) error) {
	var err error
	for range finalWriteAttempts {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(m.root), finalWriteTimeout)
		err = fn(ctx)
		cancel()
		if err == nil {
			return
		}
	}
	// Only the fact is logged: the cause may quote SQL or a path. The row stays
	// queued/running until the next process start marks it interrupted.
	m.log.Error("paper-results-mcp: could not record a backtest run's status")
}

// replay runs job and returns its outcome. A panic of the engine is a failed
// run (internal), not a crashed MCP.
func (m *Manager) replay(ctx context.Context, job Job) (out backtestrun.Outcome) {
	defer func() {
		if recover() != nil {
			out = backtestrun.Outcome{ErrorCode: backtestrun.ErrorInternal}
		}
	}()
	src, err := m.cfg.Source(ctx, job.ReplayMode)
	if err != nil {
		return failure(ctx, err, backtestrun.ErrorUnavailable)
	}
	result, coverage, err := backtestlab.Run(ctx, src, job.Period, job.Request)
	if err != nil {
		return failure(ctx, err, backtestrun.ErrorInternal)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return backtestrun.Outcome{ErrorCode: backtestrun.ErrorInternal}
	}
	return backtestrun.Outcome{ResultJSON: string(encoded), FirstSnapshotAt: coverage.FirstSnapshotAt, LastSnapshotAt: coverage.LastSnapshotAt}
}

// failure maps an error of a run to its fixed error code: the cap's deadline
// is timed_out, a stop of the MCP is interrupted, anything else is other.
func failure(ctx context.Context, err error, other string) backtestrun.Outcome {
	switch {
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded):
		return backtestrun.Outcome{ErrorCode: backtestrun.ErrorTimedOut}
	case errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled):
		return backtestrun.Outcome{ErrorCode: backtestrun.ErrorInterrupted}
	}
	return backtestrun.Outcome{ErrorCode: other}
}
