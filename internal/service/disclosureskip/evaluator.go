package disclosureskip

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/safego"
)

const (
	// DefaultGiveUp is how long after a decision the Evaluator waits for a
	// fetch that covers it before it records the decision as unknown. It
	// exceeds newsfeed's longest backoff (30 minutes), so a feed that fails
	// and recovers within one incident still gets the decision covered.
	DefaultGiveUp = 60 * time.Minute

	// pendingLookback is how far back the Evaluator looks for decisions
	// without a record. Older ones are not recorded at all (the app was down
	// or the recorder not running): the MCP tool shows them as not recorded.
	pendingLookback = 6 * time.Hour
	// pendingBatch bounds the decisions one cycle evaluates.
	pendingBatch = 200
	// fetchRetention is how long the per-fetch rows are kept.
	fetchRetention = 7 * 24 * time.Hour
	pruneEvery     = time.Hour
)

// Evaluator stores one verdict per Jev entry-candidate decision, after the
// fact: it never sits in the decision path, it only reads jev_decisions.
type Evaluator struct {
	store  *Store
	cal    Calendar
	now    func() time.Time
	giveUp time.Duration

	lastPrune time.Time
}

// EvaluatorOption configures an Evaluator.
type EvaluatorOption func(*Evaluator)

// WithEvaluatorClock overrides time.Now (tests).
func WithEvaluatorClock(now func() time.Time) EvaluatorOption {
	return func(e *Evaluator) { e.now = now }
}

// WithGiveUp overrides DefaultGiveUp.
func WithGiveUp(d time.Duration) EvaluatorOption {
	return func(e *Evaluator) {
		if d > 0 {
			e.giveUp = d
		}
	}
}

// NewEvaluator returns an Evaluator over store and the TSE calendar cal.
func NewEvaluator(store *Store, cal Calendar, opts ...EvaluatorOption) *Evaluator {
	e := &Evaluator{store: store, cal: cal, now: time.Now, giveUp: DefaultGiveUp}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Cycle evaluates the decisions that have no record yet. A decision whose
// verdict would be unknown stays pending until the give-up time has passed
// (a later fetch may still cover it). A failure on one decision is logged and
// does not stop the others; only failing to list them is returned.
func (e *Evaluator) Cycle(ctx context.Context) error {
	now := e.now()
	decisions, err := e.store.Pending(ctx, now.Add(-pendingLookback), pendingBatch)
	if err != nil {
		return err
	}
	for _, d := range decisions {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := e.evaluate(ctx, d, now); err != nil {
			slog.Warn("disclosure skip: evaluating a decision failed", "jev_decision_id", d.ID, "symbol", d.Symbol, "error", err)
		}
	}
	e.prune(ctx, now)
	return nil
}

func (e *Evaluator) evaluate(ctx context.Context, d Decision, now time.Time) error {
	from := WindowStart(e.cal, d.At)
	// Fetches first, disclosures second: the recorder commits a fetch and its
	// items in one transaction, so a fetch seen here guarantees its items are
	// seen by the next read. The other order could read the disclosures just
	// before that commit and the fetch just after it, and call a covered window
	// "no skip" although the fetch listed a matching disclosure.
	fetches, err := e.store.Fetches(ctx, d.Symbol, d.At.Add(FetchLag), d.At.Add(e.giveUp))
	if err != nil {
		return err
	}
	disclosures, err := e.store.Disclosures(ctx, d.Symbol, from, d.At)
	if err != nil {
		return err
	}
	res := Evaluate(e.cal, d.Symbol, d.At, disclosures, fetches)
	if res.Verdict == VerdictUnknown && now.Sub(d.At) < e.giveUp {
		return nil
	}
	if err := e.store.SaveRecord(ctx, d, res, now); err != nil {
		return fmt.Errorf("save record: %w", err)
	}
	return nil
}

func (e *Evaluator) prune(ctx context.Context, now time.Time) {
	if now.Sub(e.lastPrune) < pruneEvery {
		return
	}
	e.lastPrune = now
	if _, err := e.store.PruneFetches(ctx, now.Add(-fetchRetention)); err != nil {
		slog.Warn("disclosure skip: pruning old fetches failed", "error", err)
	}
}

// Run calls Cycle every interval until ctx is done. A failed or panicking
// cycle is logged and the next one still runs.
func (e *Evaluator) Run(ctx context.Context, interval time.Duration) {
	wait := time.Duration(0)
	safego.Loop(ctx, "disclosure skip evaluator", func() time.Duration {
		d := wait
		wait = interval
		return d
	}, e.Cycle)
}
