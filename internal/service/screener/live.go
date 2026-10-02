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

	// scan is the latest cycle's per-symbol results (issue #303), nil
	// before the first cycle; scout accumulates Jev Scout outcomes for
	// that cycle's candidates as the jev-scout jobs complete.
	scan  *domain.ScanCycle
	scout map[string]domain.ScoutOutcome
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

// SetScan replaces the retained latest-cycle scan result (issue #303) and
// clears the Scout outcomes of the previous cycle. cycle.Symbols is
// retained as-is and must not be modified afterwards. Only the latest
// cycle is kept, so the retention cost is one slice of ~4,000 small
// structs, replaced every cycle.
func (s *LiveSource) SetScan(cycle domain.ScanCycle) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scan = &cycle
	s.scout = make(map[string]domain.ScoutOutcome)
}

// RecordScout records the Jev Scout outcome for symbol in the current
// cycle (internal/service/jev.ScoutRecorder). A symbol that is not one of
// the current cycle's candidates - e.g. a job enqueued by the previous
// cycle that finished after this one was published - is ignored.
func (s *LiveSource) RecordScout(symbol string, outcome domain.ScoutOutcome) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.scan == nil {
		return
	}
	for _, c := range s.items {
		if c.Symbol == symbol {
			s.scout[symbol] = outcome
			return
		}
	}
}

// Scan returns the latest cycle with its Scout outcomes and the Scout
// stage counts of its funnel filled in, or ok=false before the first
// cycle has run. The returned Symbols slice is shared and read-only; the
// Scout map is a copy.
func (s *LiveSource) Scan(context.Context) (domain.ScanCycle, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.scan == nil {
		return domain.ScanCycle{}, false, nil
	}
	cycle := *s.scan
	cycle.Scout = make(map[string]domain.ScoutOutcome, len(s.scout))
	for symbol, outcome := range s.scout {
		cycle.Scout[symbol] = outcome
		switch outcome {
		case domain.ScoutPassed:
			cycle.Funnel.ScoutEvaluated++
			cycle.Funnel.ScoutPassed++
		case domain.ScoutFailed:
			cycle.Funnel.ScoutEvaluated++
		}
	}
	return cycle, true, nil
}
