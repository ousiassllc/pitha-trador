// Package newstargets decides which symbols News Ingest polls: the current
// Fast Screener candidates and the symbols of open positions. Those are the
// only symbols whose news is ever read (Jev Scout/Trader news_context for a
// candidate or a held position, and the candidate-only event-driven news
// flag), so polling the whole ~4,000-symbol universe would only burn the
// external feed's rate limit and the Luna budget (issue #531).
package newstargets

import (
	"context"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// CandidateSource returns the latest Fast Screener candidates
// (screener.LiveSource).
type CandidateSource interface {
	Candidates(ctx context.Context) ([]domain.Candidate, time.Time, error)
}

// PositionSource lists the currently open positions
// (trading.PositionRepository).
type PositionSource interface {
	ListOpen(ctx context.Context) ([]domain.Position, error)
}

// Source implements newsfeed.SymbolSource.
type Source struct {
	candidates CandidateSource
	positions  PositionSource
}

// New returns a Source over candidates and positions.
func New(candidates CandidateSource, positions PositionSource) *Source {
	return &Source{candidates: candidates, positions: positions}
}

// NewsSymbols returns the held symbols followed by the candidate symbols
// not already held, each symbol once.
func (s *Source) NewsSymbols(ctx context.Context) ([]string, error) {
	open, err := s.positions.ListOpen(ctx)
	if err != nil {
		return nil, fmt.Errorf("newstargets: list open positions: %w", err)
	}
	candidates, _, err := s.candidates.Candidates(ctx)
	if err != nil {
		return nil, fmt.Errorf("newstargets: list candidates: %w", err)
	}
	seen := make(map[string]struct{}, len(open)+len(candidates))
	symbols := make([]string, 0, len(open)+len(candidates))
	add := func(symbol string) {
		if _, ok := seen[symbol]; ok {
			return
		}
		seen[symbol] = struct{}{}
		symbols = append(symbols, symbol)
	}
	for _, p := range open {
		add(p.Symbol)
	}
	for _, c := range candidates {
		add(c.Symbol)
	}
	return symbols, nil
}
