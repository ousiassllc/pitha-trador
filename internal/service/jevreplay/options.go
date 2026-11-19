package jevreplay

import (
	"context"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
)

// Option adjusts a Runner.
type Option func(*Runner)

// WithMinInterval replaces the least time between two calls of one arm
// (jevtrialsettings.MinCallInterval); 0 turns pacing off. Tests use it.
func WithMinInterval(d time.Duration) Option { return func(r *Runner) { r.minInterval = d } }

// WithPollInterval replaces DefaultPollInterval.
func WithPollInterval(d time.Duration) Option { return func(r *Runner) { r.poll = d } }

// WithSecrets sets the configured credential strings (the OpenRouter key) that
// are redacted, as exact substrings, from every stored error excerpt. They are
// held in memory only and never logged.
func WithSecrets(secrets ...string) Option {
	return func(r *Runner) { r.secrets = append([]string(nil), secrets...) }
}

// New returns a Runner over store, sharing the daily caps with the trial's rows
// (trialSpend), reading its settings from settings and calling each arm through
// callers. mode is the operating mode of this build (metalabelsettings.RuntimeMode):
// anything but metalabelsettings.ModePaper makes the Runner do nothing.
func New(store Store, trialSpend TrialSpend, settings jevtrialsettings.Store, callers func(jevtrialsettings.Arm) Caller, mode metalabelsettings.Mode, now func() time.Time, opts ...Option) *Runner {
	base, cancel := context.WithCancel(context.Background())
	r := &Runner{store: store, trialSpend: trialSpend, settings: settings, callers: callers, mode: mode, now: now,
		poll: DefaultPollInterval, slots: make(chan struct{}, maxInFlight), base: base, cancel: cancel,
		minInterval: jevtrialsettings.MinCallInterval, pace: time.Now, sleep: sleepCtx,
		lastCall: map[string]time.Time{}, halted: map[string]string{}, throttledUntil: map[string]time.Time{}, reservedArm: map[string]float64{}}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
