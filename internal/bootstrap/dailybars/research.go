package dailybars

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/tachibanasource"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/researchstatus"
	"github.com/ousiassllc/pitha-trador/internal/safego"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/market"
)

// The two reasons ResearchConfig.Acquire gives for not handing out a source.
// The wiring maps the research hub's errors onto them; both mean "no request,
// no login" and end the cycle with a plain status.
var (
	// ErrNotConfigured: the 立花 認証ID or 秘密鍵 path is unset.
	ErrNotConfigured = errors.New("dailybars: 立花の認証情報が未設定です")
	// ErrBrokerIsTachibana: 立花 is the selected broker; its own nightly batch runs.
	ErrBrokerIsTachibana = errors.New("dailybars: ブローカーが立花です")
)

// ResearchStatus is where Research reports its state (researchstatus.Repository).
type ResearchStatus interface {
	Set(ctx context.Context, component, state, detail string, now time.Time) error
}

// ResearchStore is the research daily-bar store (researchbars.Repository): the
// per-night run rows and the per-symbol progress a resumed night reads.
type ResearchStore interface {
	Runs
	// LastNights is the night each ever-attempted symbol was last attempted.
	LastNights(ctx context.Context) (map[string]string, error)
	// RecordAttempt notes that symbol was attempted on night (ok: answered, saved: bars written).
	RecordAttempt(ctx context.Context, symbol, night string, ok bool, saved int, now time.Time) error
}

// ResearchConfig configures Research.
type ResearchConfig struct {
	// Settings is read every cycle: the switch (Research.BarsEnabled) and the
	// nightly batch's RunTime, rate, 市場区分 and exclusions it reuses.
	Settings func(ctx context.Context) (tachibanasource.TachibanaSourceSettings, error)
	// Acquire returns the 立花 session once the night needs it (creating it,
	// and logging in, on the first call) or ErrNotConfigured / ErrBrokerIsTachibana.
	// It is never called while the switch is off or before the night is due.
	Acquire func(ctx context.Context) (Source, error)
	// Bars is the daily_bars store the history lands in.
	Bars   Bars
	Store  ResearchStore
	Status ResearchStatus
	// Clock defaults to the wall clock (tests inject a fake).
	Clock tachibana.Clock
}

// Research records 立花 daily bars next to the selected broker (issue #1082,
// research only): the same nightly walk as Runner, but behind its own switch
// (off by default), on the shared research session, and resuming per symbol
// (a symbol attempted on a night is not requested again that night, so a night
// cut short, by a restart or the 8:00 window, continues where it stopped; the
// first successful request of a symbol is its whole history, the backfill).
// Nothing it writes feeds the Feature Engine, Scout, Trader, Paper or Policy.
type Research struct {
	cfg        ResearchConfig
	clock      tachibana.Clock
	retryAfter time.Time // set after an aborted night
}

// NewResearch returns a Research; nothing runs until Run (or RunDue) is called.
func NewResearch(cfg ResearchConfig) *Research {
	return &Research{cfg: cfg, clock: tachibana.OrReal(cfg.Clock)}
}

// Run checks every minute until ctx is done and runs the due night.
func (r *Research) Run(ctx context.Context) {
	safego.Loop(ctx, "tachibana research daily bars", func() time.Duration { return pollInterval }, r.RunDue)
}

// RunDue is one cycle. Switch off: status "disabled" and nothing else. Not
// due (before the run time, in 8:00〜15:30 or in the closed 3:30〜5:30): status
// "waiting". Due: the night is walked (see Research) unless it has succeeded
// already or the last abort is less than retryBackoff ago.
func (r *Research) RunDue(ctx context.Context) error {
	s, err := r.cfg.Settings(ctx)
	if err != nil {
		return err
	}
	now := r.clock.Now()
	switch {
	case !s.Research.BarsEnabled:
		r.report(ctx, researchstatus.StateDisabled, "")
		return nil
	case !dueAt(now, s.Nightly.RunTime) || tachibana.InClosedWindow(now):
		r.report(ctx, researchstatus.StateWaiting, "夜間（"+s.Nightly.RunTime+" 以降、日中 8:00〜15:30 と閉局 3:30〜5:30 を除く）の実行を待っています")
		return nil
	case now.Before(r.retryAfter):
		return nil
	}
	night := NightOf(now)
	prev, found, err := r.cfg.Store.Get(ctx, night)
	if err != nil {
		return err
	}
	if found && prev.Status == domain.DailyBarRunSucceeded {
		return nil
	}
	if !found {
		prev = domain.DailyBarRun{RunDate: night}
	}

	source, err := r.cfg.Acquire(ctx)
	switch {
	case errors.Is(err, ErrNotConfigured):
		r.report(ctx, researchstatus.StateNotConfigured, "立花の認証情報（認証ID・秘密鍵のパス）が未設定です")
		return nil
	case errors.Is(err, ErrBrokerIsTachibana):
		r.report(ctx, researchstatus.StateBrokerIsTachibana, "ブローカーが立花のため、立花の夜間バッチが日足を記録します")
		return nil
	case err != nil:
		return err
	}

	if _, err := source.DailyBarTargets(ctx); errors.Is(err, market.ErrMasterNotLoaded) {
		// The login just happened: the morning master loads in the background.
		// Wait for the next cycle instead of failing the night.
		r.report(ctx, researchstatus.StateWaiting, "立花の銘柄マスタの取得を待っています")
		return nil
	}
	r.report(ctx, researchstatus.StateRunning, night+" の夜の日足を取得しています")
	run := r.batch(source, s.Nightly).night(ctx, prev)
	if run.Status == domain.DailyBarRunSucceeded {
		r.report(ctx, researchstatus.StateOK, night+" の夜の日足を記録しました")
		return nil
	}
	if ctx.Err() == nil {
		r.retryAfter = r.clock.Now().Add(retryBackoff)
	}
	r.report(context.WithoutCancel(ctx), researchstatus.StateError, night+" の夜は途中で止まりました（再開します）／"+run.Error)
	slog.Warn("tachibana: research daily bars did not finish; they resume with the symbols not yet attempted",
		"run_date", run.RunDate, "symbols", run.Symbols, "requests", run.Requests, "saved_bars", run.SavedBars,
		"failed", run.Failed, "duration_ms", run.DurationMS, "reason", run.Error)
	return nil
}

// batch is the night's walk over source: it resumes with the symbols the
// progress table has not seen on the night and records each attempt there.
func (r *Research) batch(source Source, nightly tachibanasource.TachibanaNightlySettings) *batch {
	return &batch{
		name: "research", source: source, bars: r.cfg.Bars, runs: r.cfg.Store, settings: nightly, clock: r.clock,
		stopInClosedWindow: true,
		remaining: func(ctx context.Context, run domain.DailyBarRun, symbols []string) ([]string, error) {
			last, err := r.cfg.Store.LastNights(ctx)
			if err != nil {
				return nil, err
			}
			return resumeOrder(symbols, last, run.RunDate), nil
		},
		attempted: func(ctx context.Context, run domain.DailyBarRun, symbol string, saved int, err error) error {
			return r.cfg.Store.RecordAttempt(ctx, symbol, run.RunDate, err == nil, saved, r.clock.Now())
		},
	}
}

// resumeOrder is the symbols to request on night, in the order to do it: those
// attempted on night already are left out (never twice on a night), the rest go
// by the night they were last attempted, never-attempted first, so a night cut
// short is continued by the next one where it stopped; ties keep symbol order.
func resumeOrder(symbols []string, last map[string]string, night string) []string {
	todo := make([]string, 0, len(symbols))
	for _, s := range symbols {
		if last[s] != night {
			todo = append(todo, s)
		}
	}
	slices.SortStableFunc(todo, func(a, b string) int { return strings.Compare(last[a], last[b]) })
	return todo
}

// report stores the state; a failing status write is logged, never fatal.
func (r *Research) report(ctx context.Context, state, detail string) {
	if err := r.cfg.Status.Set(ctx, researchstatus.ComponentBars, state, detail, r.clock.Now()); err != nil {
		slog.Error("tachibana: set the research daily bars status", "state", state, "error", err)
	}
}
