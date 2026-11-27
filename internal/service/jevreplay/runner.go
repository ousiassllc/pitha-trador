package jevreplay

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevreplaysettings"
	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain/jevreplay"
	repo "github.com/ousiassllc/pitha-trador/internal/repository/jevreplay"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/safego"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trialwire"
)

const (
	// maxInFlight bounds the arm calls running at once, over all arms (the
	// trial's bound). The calls in flight are held against the caps by
	// reservations (gate.go).
	maxInFlight = 4
	// armCallTimeout bounds one arm call. A replay call is a single attempt
	// (trialwire never retries).
	armCallTimeout = 45 * time.Second
	// settingsTimeout bounds the runtime_settings reads and the run bookkeeping
	// writes that must not hang a tick or a shutdown.
	settingsTimeout = 5 * time.Second
	// DefaultPollInterval is how often Run looks for work.
	DefaultPollInterval = 30 * time.Second
)

// Store is the replay tables' access the Runner needs; *repo.Repository
// implements it (it is also the read handle BuildPlan takes).
type Store interface {
	repo.Querier
	NextActive(ctx context.Context) (run jevreplay.Run, ok bool, err error)
	StopRequested(ctx context.Context, id int64) (bool, error)
	MarkRunning(ctx context.Context, id int64, at time.Time) error
	SetBlocked(ctx context.Context, id int64, reason string, at time.Time) error
	Finish(ctx context.Context, id int64, status, stopReason string, at time.Time) error
	InsertResult(ctx context.Context, res jevreplay.Result) error
	DoneKeys(ctx context.Context, runID int64) (map[repo.Key]struct{}, error)
	SpentBetween(ctx context.Context, from, to time.Time) (repo.Spend, error)
	SpentByArmBetween(ctx context.Context, arm string, from, to time.Time) (repo.Spend, error)
	FailedByArmBetween(ctx context.Context, arm string, from, to time.Time) (int, error)
}

// TrialSpend is the live trial's cost the daily caps are shared with
// (jev_trial_decisions); *judgement.TrialRepository implements it. Replay only
// reads it.
type TrialSpend interface {
	SpentBetween(ctx context.Context, from, to time.Time) (judgement.TrialSpend, error)
	SpentByArmBetween(ctx context.Context, arm string, from, to time.Time) (judgement.TrialSpend, error)
}

// Caller is one arm's model client for a saved input; *trialwire.Client
// implements it. An arm whose Caller is nil or not Configured (no API key) is
// never called, and a run that cannot call every arm ends without a request.
type Caller interface {
	Configured() bool
	TraderRaw(ctx context.Context, questionVersion string, stateJSON json.RawMessage) (trialwire.Response, time.Duration, int, error)
}

var _ Caller = (*trialwire.Client)(nil)

// Runner executes replay runs. It reads the switch and the trial's arms, caps
// and stop records from settings (it writes none of them), asks each arm
// through callers(arm) and records results in the replay tables. It runs in
// Paper only. With the switch off it does nothing.
type Runner struct {
	store      Store
	trialSpend TrialSpend
	settings   jevtrialsettings.Store
	callers    func(jevtrialsettings.Arm) Caller
	mode       metalabelsettings.Mode
	now        func() time.Time
	poll       time.Duration
	slots      chan struct{}
	// base is the parent of every call's context; Close cancels it.
	base   context.Context
	cancel context.CancelFunc
	// wg counts the loop and the ticks in progress; closeMu orders its Add
	// against Close's Wait; tickMu allows one tick at a time.
	wg      sync.WaitGroup
	closeMu sync.Mutex
	closed  bool
	tickMu  sync.Mutex
	// paceMu guards the per-arm pacing and the in-memory stops that hold across
	// ticks: a 403 for the JST day, a 429/529 until a time.
	paceMu         sync.Mutex
	minInterval    time.Duration
	pace           func() time.Time
	sleep          func(ctx context.Context, d time.Duration) error
	lastCall       map[string]time.Time
	halted         map[string]string
	throttledUntil map[string]time.Time
	// resMu guards the reservations of the calls that are allowed but whose rows
	// are not stored yet.
	resMu         sync.Mutex
	reservedTotal float64
	reservedArm   map[string]float64
	// secrets are redacted from stored error excerpts.
	secrets []string
}

// Start launches the loop that ticks every poll interval until Close. It does
// nothing for a nil Runner or after Close.
func (r *Runner) Start() {
	if r == nil || !r.enter() {
		return
	}
	go func() {
		defer r.wg.Done()
		t := time.NewTicker(r.poll)
		defer t.Stop()
		for {
			if err := safego.Try("jev replay tick", func() error { return r.tick(r.base) }); err != nil {
				slog.Warn("jev replay: tick failed; the live decisions are unaffected", "error", err)
			}
			select {
			case <-r.base.Done():
				return
			case <-t.C:
			}
		}
	}()
}

// enter registers one unit of work (a loop or a tick) unless Close was called.
func (r *Runner) enter() bool {
	r.closeMu.Lock()
	defer r.closeMu.Unlock()
	if r.closed {
		return false
	}
	r.wg.Add(1)
	return true
}

// Close cancels the calls in flight and waits for the loop and the tick in
// progress to return (the application's stop calls it). A run that was cut off
// stays running and continues, without repeating a recorded call, when the
// application starts again. A nil Runner is a no-op.
func (r *Runner) Close() {
	if r == nil {
		return
	}
	r.closeMu.Lock()
	r.closed = true
	r.closeMu.Unlock()
	r.cancel()
	r.wg.Wait()
}

// Tick runs one pass: it picks the oldest pending or running run and executes it
// until it is completed, stopped, blocked by a cap or a stop, or cut off by ctx.
// Tests call it directly; Start calls it on a timer.
func (r *Runner) Tick(ctx context.Context) error {
	if r == nil || !r.enter() {
		return nil
	}
	defer r.wg.Done()
	return r.tick(ctx)
}

func (r *Runner) tick(parent context.Context) error {
	if r.mode != metalabelsettings.ModePaper {
		return nil
	}
	r.tickMu.Lock()
	defer r.tickMu.Unlock()
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	stop := context.AfterFunc(r.base, cancel)
	defer stop()

	sctx, scancel := context.WithTimeout(ctx, settingsTimeout)
	on, err := jevreplaysettings.Enabled(sctx, r.settings)
	scancel()
	if err != nil || !on {
		return err // off (or unknown): nothing is read, nothing is sent
	}
	run, ok, err := r.store.NextActive(ctx)
	if err != nil || !ok {
		return err
	}
	return r.process(ctx, run)
}

// process executes run (see Tick).
func (r *Runner) process(ctx context.Context, run jevreplay.Run) error {
	sctx, scancel := context.WithTimeout(ctx, settingsTimeout)
	cfg := jevtrialsettings.Load(sctx, r.settings)
	scancel()
	if cfg.ReadErr != nil {
		return fmt.Errorf("jev replay: the trial settings could not be read in full; not calling any arm: %w", cfg.ReadErr)
	}
	if run.MaxDecisionID == 0 {
		// An estimate without decisions pins nothing; never read "no limit" as all decisions.
		return r.finish(ctx, run.ID, jevreplay.StatusCompleted, "")
	}
	stopped, err := r.store.StopRequested(ctx, run.ID)
	if err != nil {
		return err
	}
	if stopped {
		return r.finish(ctx, run.ID, jevreplay.StatusStopped, jevreplay.StopRequested)
	}
	arms, err := jevtrialsettings.ParseArms(run.ArmsJSON)
	if err != nil || len(arms) == 0 {
		return r.finish(ctx, run.ID, jevreplay.StatusFailed, jevreplay.StopNoArms)
	}
	callers := map[string]Caller{}
	for _, a := range arms {
		if !jev.IsTraderVersion(a.PromptVersion) {
			return r.finish(ctx, run.ID, jevreplay.StatusFailed, jevreplay.StopNoArms)
		}
		if c := r.callers(a); c != nil && c.Configured() {
			callers[a.ID] = c
		}
	}
	if len(callers) != len(arms) {
		// Without a key (it is one key for every arm) nothing is sent.
		return r.finish(ctx, run.ID, jevreplay.StatusFailed, jevreplay.StopKeyNotSet)
	}
	if err := r.store.MarkRunning(ctx, run.ID, r.now()); err != nil {
		return err
	}
	plan, err := BuildPlan(ctx, r.store, PlanInput{Spec: run.Spec, From: run.PeriodFrom, To: run.PeriodTo, Arms: arms, MaxID: run.MaxDecisionID})
	if err != nil {
		return err
	}
	done, err := r.store.DoneKeys(ctx, run.ID)
	if err != nil {
		return err
	}
	var work []Call
	for _, c := range plan.Calls {
		key := repo.Key{DecisionID: plan.Decisions[c.Decision].ID, ArmID: plan.Arms[c.Arm].Arm.ID}
		if _, ok := done[key]; !ok {
			work = append(work, c)
		}
	}
	e := &execution{r: r, run: run, cfg: cfg, plan: plan, callers: callers, spent: run.CountedCostUSD, armBlock: map[string]string{}}
	return e.dispatch(ctx, work)
}

// finish ends run with status and reason. It does not run after a shutdown
// began: the run then stays as it is and continues at the next start.
func (r *Runner) finish(ctx context.Context, id int64, status, reason string) error {
	if ctx.Err() != nil {
		return nil
	}
	fctx, cancel := r.recordCtx()
	defer cancel()
	if err := r.store.Finish(fctx, id, status, reason, r.now()); err != nil {
		return err
	}
	slog.Info("jev replay: run ended", "run_id", id, "status", status, "reason", reason)
	return nil
}

// recordCtx is a short context for a write that must land even when the call
// that caused it was cancelled.
func (r *Runner) recordCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(r.base), settingsTimeout)
}
