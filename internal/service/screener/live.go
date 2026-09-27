package screener

import (
	"context"
	"sync"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// LiveSource holds the most recent Run output, refreshed periodically by
// the composition root (internal/bootstrap, issue #45) from real Feature
// Engine output. It structurally implements
// internal/web/handler.CandidateSource's Candidates(ctx)
// ([]domain.Candidate, time.Time, error) method without importing that
// package (package doc.go's layer rule: service depends on domain/
// repository only), the same way internal/service/scheduler's own
// HeartbeatChecker/LogRotator interfaces mirror a sibling package's
// method set instead of importing it.
type LiveSource struct {
	mu    sync.RWMutex
	items []domain.Candidate
	asOf  time.Time
}

// NewLiveSource returns an empty LiveSource; Set populates it once the
// first refresh cycle completes.
func NewLiveSource() *LiveSource {
	return &LiveSource{}
}

// Candidates returns the candidates/timestamp from the most recent Set
// call, or (nil, zero time, nil) before the first refresh has run.
func (s *LiveSource) Candidates(context.Context) ([]domain.Candidate, time.Time, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.items, s.asOf, nil
}

// Set replaces the current candidate list/timestamp, for the composition
// root's periodic refresh cycle to call.
func (s *LiveSource) Set(items []domain.Candidate, asOf time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = items
	s.asOf = asOf
}
