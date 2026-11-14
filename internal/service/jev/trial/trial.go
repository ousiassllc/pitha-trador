// Package trial runs the model-comparison trial (issue #1059): for every live
// Paper trader decision it asks each configured arm (another decision model,
// stage 1: OpenRouter's Clef, Clef-flash, Gemini Decision 1) the same question
// group about the same state and RAG context, in parallel, and stores each
// arm's answer, or why there is none, in jev_trial_decisions.
//
// It follows the Jev shadow (internal/service/jev/shadow) but is a separate
// track: its own table, runner, settings (jevtrialsettings, `jev.trial.*`) and
// per-arm API keys and HTTP clients. An arm's answer is never used: Trial has
// no path to Policy, the meta-model, Risk, order placement, the RAG index
// (jev_decision_vectors), Calibration or the meta-model training, and an arm
// never sees another arm's answer. It writes jev_trial_decisions, jev_trial_skips
// (which live decision an arm was not asked about, and why: no answer, status or
// cost) and a few runtime_settings rows (the failed-call counters and the stop
// records).
//
// Everything is off until a person turns the trial on on the Settings screen:
// with the switch off (the default) or no arms, Submit makes no call and
// writes nothing. It runs in Paper only (metalabelsettings.RuntimeMode).
package trial

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/domain/jevtrial"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trialwire"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
)

const (
	// maxInFlight bounds the arm calls running at once, over all arms. A full
	// queue drops that arm's comparison (logged) instead of waiting: the live
	// decision must never be slowed, and a backlog would pile up calls beyond
	// the daily caps. The calls in flight are held against the caps by
	// reservations (reserve.go). Same bound as the shadow's.
	maxInFlight = 4
	// armCallTimeout bounds one arm call. A trial call is a single attempt
	// (trialwire never retries).
	armCallTimeout = 45 * time.Second
	// settingsTimeout bounds the runtime_settings reads and writes of the
	// trial that must not hang the live path (Submit) or a shutdown.
	settingsTimeout = 5 * time.Second
)

// Results is the jev_trial_decisions access Trial needs;
// *judgement.TrialRepository implements it.
type Results interface {
	Insert(ctx context.Context, d jevtrial.Decision) (jevtrial.Decision, error)
	SpentBetween(ctx context.Context, from, to time.Time) (judgement.TrialSpend, error)
	SpentByArmBetween(ctx context.Context, arm string, from, to time.Time) (judgement.TrialSpend, error)
	// InsertSkip records that an arm was not asked about a live decision
	// (jev_trial_skips). It is a count of decisions the comparison did not cover,
	// never a result row.
	InsertSkip(ctx context.Context, s jevtrial.Skip) error
}

// Settings is the runtime_settings access Trial needs: it reads the switch,
// the arms, the caps and the stop records, and writes the stop records and the
// failed-call counters. *system.RuntimeSettingsRepository implements it.
type Settings interface {
	jevtrialsettings.Store
	Set(ctx context.Context, key, value string, updatedAt time.Time) error
}

// Caller is one arm's model client; *trialwire.Client implements it. An arm
// whose Caller is nil or not Configured (no API key) is never called.
type Caller interface {
	Configured() bool
	Trader(ctx context.Context, req jev.TraderRequest) (trialwire.Response, time.Duration, int, error)
}

// Trial is the model-comparison runner. It implements jev.ShadowSubmitter.
type Trial struct {
	results  Results
	settings Settings
	callers  func(jevtrialsettings.Arm) Caller
	mode     metalabelsettings.Mode
	now      func() time.Time
	slots    chan struct{}
	wg       sync.WaitGroup
	// base is the parent of every call's context; Close cancels it.
	base   context.Context
	cancel context.CancelFunc
	// recMu serializes the read-modify-write of the stop records and the
	// failed-call counters.
	recMu sync.Mutex
	// paceMu guards the per-arm pacing and in-memory cooldown.
	paceMu      sync.Mutex
	minInterval time.Duration
	pace        func() time.Time
	lastCall    map[string]time.Time
	// throttledUntil is the end of an arm's cooldown after a 429/529, kept in
	// memory so it holds even when the throttle record cannot be written.
	throttledUntil map[string]time.Time
	// halted holds the JST day of a 402/403 stop per key (KeyHalt for the whole
	// trial, HaltKey(arm) for an arm), in memory, so such a stop holds even when
	// its record cannot be written (the cost caps are re-derived from stored
	// rows, these two are not).
	halted map[string]string
	// resMu guards the reservations of the calls that are allowed but whose rows
	// are not stored yet (reserve.go): over all arms and per arm.
	resMu         sync.Mutex
	reservedTotal float64
	reservedArm   map[string]float64
	// secrets are redacted from stored error excerpts (WithSecrets).
	secrets []string
	// closeMu orders Submit's wg.Add against Close's wg.Wait.
	closeMu sync.Mutex
	closed  bool
}

var _ jev.ShadowSubmitter = (*Trial)(nil)

// New returns a Trial that stores rows via results, reads/records its settings
// via settings and asks each arm through callers(arm). mode is the operating
// mode of this build (metalabelsettings.RuntimeMode): anything but
// metalabelsettings.ModePaper makes Submit do nothing. now is the clock
// (time.Now in production).
func New(results Results, settings Settings, callers func(jevtrialsettings.Arm) Caller, mode metalabelsettings.Mode, now func() time.Time, opts ...Option) *Trial {
	base, cancel := context.WithCancel(context.Background())
	t := &Trial{results: results, settings: settings, callers: callers, mode: mode, now: now,
		slots: make(chan struct{}, maxInFlight), base: base, cancel: cancel,
		minInterval: jevtrialsettings.MinCallInterval, pace: time.Now,
		reservedArm: map[string]float64{}, lastCall: map[string]time.Time{}, throttledUntil: map[string]time.Time{}, halted: map[string]string{}}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// Submit hands live (a persisted trader decision) and the exact state and RAG
// context it was decided with to the trial, and returns at once: even the
// settings are read in the background, so the live decision never waits for the
// database on the trial's account. It does nothing outside Paper, for a nil
// Trial and after Close. While the trial is off or has no arms (the default) it
// reads the settings and stops: no call, no write. Otherwise every arm is asked
// in its own goroutine, in parallel; each gets the same inputs and none sees
// another's answer.
func (t *Trial) Submit(live domain.JevDecision, state jev.ScoutState, ragContext rag.Context) {
	if t == nil || t.mode != metalabelsettings.ModePaper {
		return
	}
	t.closeMu.Lock()
	if t.closed {
		t.closeMu.Unlock()
		return
	}
	t.wg.Add(1)
	t.closeMu.Unlock()
	go func() {
		defer t.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				slog.Error("jev trial: dispatch panicked; the live decision is unaffected", "decision_id", live.ID, "panic", r)
			}
		}()
		ctx, cancel := context.WithTimeout(t.base, settingsTimeout)
		cfg := jevtrialsettings.Load(ctx, t.settings)
		cancel()
		if !cfg.Enabled || len(cfg.Arms) == 0 {
			return
		}
		if cfg.ReadErr != nil {
			slog.Warn("jev trial: the settings could not be read in full; not calling any arm", "decision_id", live.ID, "error", cfg.ReadErr)
			for _, arm := range cfg.Arms {
				t.recordSkip(live.ID, arm.ID, jevtrial.SkipCheckFailed)
			}
			return
		}
		for _, arm := range cfg.Arms {
			select {
			case t.slots <- struct{}{}:
			default:
				slog.Warn("jev trial: queue full, skipping the comparison", "decision_id", live.ID, "arm", arm.ID)
				t.recordSkip(live.ID, arm.ID, jevtrial.SkipConcurrencyFull)
				continue
			}
			t.wg.Add(1)
			go t.runArm(cfg, arm, live, state, ragContext)
		}
	}()
}

// runArm is one arm's goroutine: panics are recovered (the live decision is
// unaffected), the slot is released and a failure is a log line.
func (t *Trial) runArm(cfg jevtrialsettings.Settings, arm jevtrialsettings.Arm, live domain.JevDecision, state jev.ScoutState, ragContext rag.Context) {
	defer t.wg.Done()
	defer func() { <-t.slots }()
	defer func() {
		if r := recover(); r != nil {
			slog.Error("jev trial: arm panicked; the live decision is unaffected", "decision_id", live.ID, "arm", arm.ID, "panic", r)
		}
	}()
	ctx, cancel := context.WithTimeout(t.base, armCallTimeout)
	defer cancel()
	if err := t.run(ctx, cfg, arm, live, state, ragContext); err != nil {
		slog.Warn("jev trial: comparison failed; the live decision is unaffected", "decision_id", live.ID, "symbol", live.Symbol, "arm", arm.ID, "error", err)
	}
}

// Wait blocks until every submitted comparison has finished (tests).
func (t *Trial) Wait() { t.wg.Wait() }

// Close cancels the calls in flight and waits for them to return (the
// application's stop calls it through jev.Trader.Close). Comparisons submitted
// afterwards are dropped. A nil Trial is a no-op.
func (t *Trial) Close() {
	if t == nil {
		return
	}
	t.closeMu.Lock()
	t.closed = true
	t.closeMu.Unlock()
	t.cancel()
	t.wg.Wait()
}
