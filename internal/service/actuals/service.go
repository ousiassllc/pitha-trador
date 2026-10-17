// Package actuals evaluates the realized (Paper) results of closed positions
// for the question "is it winning?" (issue #777): PnL, win rate, EV, Profit
// Factor and the JPY / capital-ratio max drawdown by period, direction, exit
// reason, meta-labeling model version and A/B arm, with sample sizes and
// confidence intervals, the gross/net cost breakdown, Jev's hit rate and
// Brier score, the meta model's OOS metrics beside the same trades' realized
// EV, and the 合格ライン verdict.
//
// It is read-only and computed on read from existing tables: nothing is
// persisted and no market data (board/quote values) is read or returned.
package actuals

import (
	"context"
	"fmt"
	"slices"
	"time"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/actuals"
)

// Source supplies the evaluation inputs; *internal/repository/actuals.Reader
// implements it.
type Source interface {
	ClosedTrades(ctx context.Context) ([]repo.Trade, error)
	JevSamples(ctx context.Context) ([]repo.JevSample, error)
	MetaModels(ctx context.Context) ([]repo.MetaModel, error)
}

// Range restricts the evaluation to closed positions with From <= closed_at <
// To (and Jev decisions timestamped in it). A zero bound is unbounded.
type Range struct {
	From, To time.Time
}

func (r Range) contains(t time.Time) bool {
	return (r.From.IsZero() || !t.Before(r.From)) && (r.To.IsZero() || t.Before(r.To))
}

// Service evaluates the closed Paper trades.
type Service struct {
	source   Source
	capital  float64
	criteria Criteria
	now      func() time.Time
}

// New returns a Service reading from source. initialCapital is the risk
// config's initial_capital (<= 0: not set); criteria the 合格ライン.
func New(source Source, initialCapital float64, criteria Criteria) *Service {
	return &Service{source: source, capital: initialCapital, criteria: criteria, now: time.Now}
}

// Evaluator evaluates the closed Paper trades over a range; *Service
// implements it.
type Evaluator interface {
	Evaluate(ctx context.Context, r Range) (Evaluation, error)
}

// EvaluatorOf returns v when it is an Evaluator, otherwise an Evaluator over
// no data with the default criteria (the evaluation of a database without
// trades): the providers wired into the router implement Evaluator only when
// the real database is behind them.
func EvaluatorOf(v any) Evaluator {
	if e, ok := v.(Evaluator); ok {
		return e
	}
	return New(noData{}, 0, DefaultCriteria())
}

type noData struct{}

func (noData) ClosedTrades(context.Context) ([]repo.Trade, error)   { return nil, nil }
func (noData) JevSamples(context.Context) ([]repo.JevSample, error) { return nil, nil }
func (noData) MetaModels(context.Context) ([]repo.MetaModel, error) { return nil, nil }

// Evaluate computes the Evaluation over r.
func (s *Service) Evaluate(ctx context.Context, r Range) (Evaluation, error) {
	all, err := s.source.ClosedTrades(ctx)
	if err != nil {
		return Evaluation{}, fmt.Errorf("actuals: %w", err)
	}
	samples, err := s.source.JevSamples(ctx)
	if err != nil {
		return Evaluation{}, fmt.Errorf("actuals: %w", err)
	}
	models, err := s.source.MetaModels(ctx)
	if err != nil {
		return Evaluation{}, fmt.Errorf("actuals: %w", err)
	}

	trades := slices.DeleteFunc(slices.Clone(all), func(t repo.Trade) bool { return !r.contains(t.ClosedAt) })
	slices.SortStableFunc(trades, func(a, b repo.Trade) int {
		if c := a.ClosedAt.Compare(b.ClosedAt); c != 0 {
			return c
		}
		return int(a.PositionID - b.PositionID)
	})
	samples = slices.DeleteFunc(slices.Clone(samples), func(j repo.JevSample) bool { return !r.contains(j.DecidedAt) })

	e := Evaluation{
		GeneratedAt:    s.now().UTC(),
		InitialCapital: s.capital,
		Criteria:       s.criteria,
		Overall:        summarize(trades, s.capital, s.criteria),
		Jev:            jevGroups(samples, s.criteria),
		Meta:           metaComparisons(models, trades, s.capital, s.criteria),
	}
	if !r.From.IsZero() {
		e.From = &r.From
	}
	if !r.To.IsZero() {
		e.To = &r.To
	}
	groupings(&e, trades, s.capital, s.criteria)
	e.Verdict = judge(e.Overall, s.capital, s.criteria)
	return e, nil
}
