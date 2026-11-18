package jquants

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

// LedgerStore is the persistence CapLedger needs (repository/jquants).
type LedgerStore interface {
	ReserveRequest(ctx context.Context, day, dataset string, limit int, at time.Time) (bool, error)
	RecordFailure(ctx context.Context, day, dataset, code string) error
}

// CapSource returns the daily request cap (jquantssettings.Load), read at every
// request so a change on the Settings screen applies to the next one.
type CapSource func(ctx context.Context) (int, error)

// CapLedger is the Ledger over the database: it counts a request in
// research_jquants_fetch_log before it is sent, and refuses (ErrCapReached) once
// the JST day's count has reached the cap. The count is kept in the database, so
// it survives a restart and is shared by every task of the process.
type CapLedger struct {
	store LedgerStore
	limit CapSource
	now   func() time.Time
}

// NewCapLedger returns a CapLedger.
func NewCapLedger(store LedgerStore, limit CapSource) *CapLedger {
	return &CapLedger{store: store, limit: limit, now: time.Now}
}

// WithClock overrides time.Now (tests).
func (l *CapLedger) WithClock(now func() time.Time) *CapLedger {
	l.now = now
	return l
}

// Day is the JST day (YYYY-MM-DD) of t, the day the cap counts in.
func Day(t time.Time) string { return t.In(marketcalendar.JST).Format(time.DateOnly) }

// Reserve implements Ledger. A cap that cannot be read refuses the request: the
// adapter must not guess its way past a limit.
func (l *CapLedger) Reserve(ctx context.Context, dataset string) error {
	limit, err := l.limit(ctx)
	if err != nil {
		return fmt.Errorf("jquants: read the request cap: %w", err)
	}
	now := l.now()
	ok, err := l.store.ReserveRequest(ctx, Day(now), dataset, limit, now)
	if err != nil {
		return err
	}
	if !ok {
		return ErrCapReached
	}
	return nil
}

// Failed implements Ledger. The code is one of the fixed Code* values.
func (l *CapLedger) Failed(ctx context.Context, dataset, code string) {
	if err := l.store.RecordFailure(ctx, Day(l.now()), dataset, code); err != nil {
		slog.WarnContext(ctx, "jquants: could not record a failed request", "dataset", dataset, "code", code, "error", err)
	}
}
