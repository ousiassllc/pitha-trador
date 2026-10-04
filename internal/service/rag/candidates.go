package rag

import (
	"context"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/execution/enrich"
)

// decisionCandidateFactor is how many times k nearest
// jev_decision_vectors candidates (labeled or not) decisionMatches
// fetches to backfill the slots outcome-labeled decisions leave open, so
// unlabeled trader decisions can still outrank scout ones.
const decisionCandidateFactor = 4

// labeledDecisionFilter restricts a jev_decision_vectors KNN search to
// decisions with at least one calibration_outcomes row. Scout decisions
// (indexed for every candidate every cycle) can never be labeled, so an
// unfiltered nearest-neighbour pool is routinely saturated by them.
const labeledDecisionFilter = `decision_id IN (SELECT jev_decision_id FROM calibration_outcomes)`

// decisionMatches returns the jev_decision_vectors hits Context ranks:
// first the k nearest outcome-labeled decisions (searched on their own,
// so they are guaranteed candidates however many unlabeled decisions
// are closer), then, only if those leave fewer than k, the
// k*decisionCandidateFactor nearest decisions of any kind (labeled
// ones already included are not repeated).
func (s *Service) decisionMatches(ctx context.Context, v Vector, k int) ([]match, error) {
	matches, err := s.search(ctx, "jev_decision_vectors", "decision_id", labeledDecisionFilter, v, k)
	if err != nil {
		return nil, fmt.Errorf("rag: search similar labeled decisions: %w", err)
	}
	if len(matches) >= k {
		return matches, nil
	}

	pool, err := s.search(ctx, "jev_decision_vectors", "decision_id", "", v, k*decisionCandidateFactor)
	if err != nil {
		return nil, fmt.Errorf("rag: search similar decisions: %w", err)
	}
	seen := make(map[int64]bool, len(matches))
	for _, m := range matches {
		seen[m.id] = true
	}
	for _, m := range pool {
		if !seen[m.id] {
			matches = append(matches, m)
		}
	}
	return matches, nil
}

// decisionCandidate is one jev_decision_vectors hit hydrated for
// ranking: the decision itself, its distance and its shortest-horizon
// calibration outcome (nil while unlabeled).
type decisionCandidate struct {
	decision domain.JevDecision
	distance float64
	outcome  *domain.CalibrationOutcome
}

// rank orders candidates per FR-RAG-2: decisions with a calibration
// outcome first, then trader decisions without one, then scout
// decisions (which can never be labeled and carry no direction). Ties
// keep the incoming (distance) order.
func (c decisionCandidate) rank() int {
	switch {
	case c.outcome != nil:
		return 0
	case c.decision.DecisionType == domain.JevDecisionTypeTrader:
		return 1
	default:
		return 2
	}
}

// hydrateDecisions loads the matched decisions in one batched query
// (skipping ones that no longer resolve), keeps matches' order, and
// joins their shortest-horizon calibration outcomes in a second query.
func (s *Service) hydrateDecisions(ctx context.Context, matches []match) ([]decisionCandidate, error) {
	ids := make([]int64, len(matches))
	for i, m := range matches {
		ids[i] = m.id
	}
	decisions, err := s.decisions.ListByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("rag: load similar decisions: %w", err)
	}
	byID := make(map[int64]domain.JevDecision, len(decisions))
	for _, d := range decisions {
		byID[d.ID] = d
	}

	candidates := make([]decisionCandidate, 0, len(matches))
	loadedIDs := make([]int64, 0, len(matches))
	for _, m := range matches {
		d, ok := byID[m.id]
		if !ok {
			continue
		}
		// Regime lives only in response_json (jev_decisions has no regime
		// column), so enrich.Decision restores it.
		candidates = append(candidates, decisionCandidate{decision: enrich.Decision(d), distance: m.distance})
		loadedIDs = append(loadedIDs, d.ID)
	}

	outcomes, err := s.calibration.ListByDecisionIDs(ctx, loadedIDs)
	if err != nil {
		return nil, fmt.Errorf("rag: join calibration outcomes: %w", err)
	}
	// outcomes is ordered by (decision, horizon asc): keep the first
	// (shortest-horizon) row per decision.
	shortest := make(map[int64]*domain.CalibrationOutcome, len(outcomes))
	for i := range outcomes {
		if _, ok := shortest[outcomes[i].JevDecisionID]; !ok {
			shortest[outcomes[i].JevDecisionID] = &outcomes[i]
		}
	}
	for i := range candidates {
		candidates[i].outcome = shortest[candidates[i].decision.ID]
	}
	return candidates, nil
}
