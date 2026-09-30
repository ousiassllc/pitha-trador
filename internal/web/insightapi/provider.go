// Package insightapi serves the read-only JSON API routes that summarize
// Jev decisions, trade signals and realized performance (docs/api/
// endpoints.md §5 `/symbols/{symbol}/decisions`, `/signals`,
// `/signals/{symbol}`, `/performance`).
package insightapi

import (
	"context"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/insight"
)

// Provider is the data the routes need. internal/service/insight.Reader
// implements it. RecentDecisions and RecentSignals return an error
// wrapping execution.ErrInstrumentUnknown for an unregistered symbol.
type Provider interface {
	RecentDecisions(ctx context.Context, symbol string, limit int) ([]domain.JevDecision, error)
	ListSignals(ctx context.Context, limit int) ([]domain.TradeSignal, error)
	RecentSignals(ctx context.Context, symbol string, limit int) ([]domain.TradeSignal, error)
	Performance(ctx context.Context, now time.Time) (insight.Performance, error)
}

// StaticProvider is a fixed, empty Provider, used as
// internal/router.New()'s default until insight.Reader is wired in.
type StaticProvider struct{}

func (StaticProvider) RecentDecisions(context.Context, string, int) ([]domain.JevDecision, error) {
	return nil, nil
}

func (StaticProvider) ListSignals(context.Context, int) ([]domain.TradeSignal, error) {
	return nil, nil
}

func (StaticProvider) RecentSignals(context.Context, string, int) ([]domain.TradeSignal, error) {
	return nil, nil
}

func (StaticProvider) Performance(context.Context, time.Time) (insight.Performance, error) {
	return insight.Performance{}, nil
}
