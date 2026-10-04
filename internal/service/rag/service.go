package rag

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/service/execution/enrich"
)

// DefaultK is FR-RAG-2's initial top-k similarity search size.
const DefaultK = 5

// Service builds, persists and searches the standardized feature
// embeddings backing Jev RAG (functional.md §4.13). It is the single
// entry point internal/service/jev and internal/service/featureengine
// depend on; vec0 tables are not modeled through internal/repository
// because each one is a plain (rowid, embedding) pair, not a domain
// entity with its own CRUD surface.
type Service struct {
	db          *sql.DB
	decisions   *judgement.DecisionRepository
	calibration *judgement.CalibrationRepository
	snapshots   *market.SnapshotRepository
}

// NewService returns a Service backed by db (which must have
// modernc.org/sqlite/vec registered - internal/repository/sqlitedb/db.go does
// this via a blank import before Open applies db/migrations' vec0
// CREATE VIRTUAL TABLE statements) and used to hydrate similarity-search
// hits into SimilarCase summaries via decisions/snapshots, joined with
// db's calibration_outcomes (FR-RAG-2/3).
func NewService(db *sql.DB, decisions *judgement.DecisionRepository, snapshots *market.SnapshotRepository) *Service {
	return &Service{db: db, decisions: decisions, calibration: judgement.NewCalibrationRepository(db), snapshots: snapshots}
}

// IndexSnapshot standardizes in and writes it to market_snapshot_vectors
// for snapshotID (FR-RAG-1).
func (s *Service) IndexSnapshot(ctx context.Context, snapshotID int64, in FeatureInput) error {
	return s.insertVector(ctx, "market_snapshot_vectors", "snapshot_id", snapshotID, Build(in))
}

// IndexDecision standardizes in and writes it to jev_decision_vectors for
// decisionID (FR-RAG-1).
func (s *Service) IndexDecision(ctx context.Context, decisionID int64, in FeatureInput) error {
	return s.insertVector(ctx, "jev_decision_vectors", "decision_id", decisionID, Build(in))
}

func (s *Service) insertVector(ctx context.Context, table, idColumn string, id int64, v Vector) error {
	embedding, err := v.json()
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO `+table+` (`+idColumn+`, embedding) VALUES (?, ?)`, id, embedding); err != nil { //nolint:gosec // G202: table/idColumn are package-internal constants (never user input); values are bound parameters
		return fmt.Errorf("rag: insert %s row %d: %w", table, id, err)
	}
	return nil
}

// match is one sqlite-vec similarity search hit: the matched row's id
// (snapshot_id or decision_id) and its L2 distance from the query vector
// (smaller is more similar).
type match struct {
	id       int64
	distance float64
}

func (s *Service) search(ctx context.Context, table, idColumn string, v Vector, k int) ([]match, error) {
	if k <= 0 {
		return nil, nil
	}
	embedding, err := v.json()
	if err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT `+idColumn+`, distance FROM `+table+` WHERE embedding MATCH ? ORDER BY distance LIMIT ?`, //nolint:gosec // G202: table/idColumn are package-internal constants (never user input); values are bound parameters
		embedding, k)
	if err != nil {
		return nil, fmt.Errorf("rag: search %s: %w", table, err)
	}
	defer func() { _ = rows.Close() }()

	var out []match
	for rows.Next() {
		var m match
		if err := rows.Scan(&m.id, &m.distance); err != nil {
			return nil, fmt.Errorf("rag: scan %s match: %w", table, err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rag: iterate %s matches: %w", table, err)
	}
	return out, nil
}

// SimilarCase is one past market state RAG found similar to the state
// Jev is currently evaluating, summarized for Jev's few-shot prompt
// (FR-RAG-3). Source records which vec0 table matched: "jev_decision" or
// "market_snapshot".
type SimilarCase struct {
	Source    string    `json:"source"`
	Symbol    string    `json:"symbol"`
	Timestamp time.Time `json:"timestamp"`
	Distance  float64   `json:"distance"`

	// Direction/Confidence/Regime are only ever set when Source is
	// "jev_decision" and the matched decision recorded a Jev Trader
	// judgement (domain.JevDecision.Direction/Confidence/Regime are nil
	// for decision_type=scout, functional.md §4.5).
	Direction  *string  `json:"direction,omitempty"`
	Confidence *float64 `json:"confidence,omitempty"`
	Regime     *string  `json:"regime,omitempty"`

	// HorizonMinutes/FutureReturn/WasDirectionCorrect are the matched
	// decision's realized calibration_outcomes row (FR-RAG-3), set only
	// when Outcome Labeling has labeled it. When several horizons are
	// labeled, the shortest one is reported (the Trader question asks
	// for the next few minutes). FutureReturn is the raw percentage
	// price return as stored (1.0 == +1%, not a fraction);
	// WasDirectionCorrect stays nil for a NONE decision (er.md:
	// direction=NONEの場合NULL).
	HorizonMinutes      *int     `json:"horizon_minutes,omitempty"`
	FutureReturn        *float64 `json:"future_return,omitempty"`
	WasDirectionCorrect *bool    `json:"was_direction_correct,omitempty"`
}

// Context is the few-shot context the RAG Context Builder injects into a
// Jev Scout/Trader request (FR-RAG-3, architecture/overview.md §7). An
// empty Context (nil/zero-length Cases) is the expected cold-start value
// (FR-RAG-4): callers still send it to Jev, which falls back to its
// normal judgement.
type Context struct {
	Cases []SimilarCase `json:"cases,omitempty"`
}

// decisionCandidateFactor is how many times k jev_decision_vectors
// candidates Context fetches before re-ranking: an outcome-labeled
// decision is only preferred if it is among the candidates, so the pool
// must be wider than the k that survive.
const decisionCandidateFactor = 4

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

// Context returns the FR-RAG-2 top-k (DefaultK if k <= 0) similar past
// states for in. jev_decisions matches come first, ranked outcome-labeled
// (calibration_outcomes joined) before unlabeled trader decisions before
// scout decisions, each group by ascending distance; market_snapshots
// matches backfill up to k total.
//
// A search or hydration error is only ever returned for a true
// technical failure (e.g. a closed/broken database connection); a
// cold-start empty result is not an error (FR-RAG-4).
func (s *Service) Context(ctx context.Context, in FeatureInput, k int) (Context, error) {
	if k <= 0 {
		k = DefaultK
	}
	v := Build(in)

	decisionMatches, err := s.search(ctx, "jev_decision_vectors", "decision_id", v, k*decisionCandidateFactor)
	if err != nil {
		return Context{}, fmt.Errorf("rag: search similar decisions: %w", err)
	}

	candidates, err := s.hydrateDecisions(ctx, decisionMatches)
	if err != nil {
		return Context{}, err
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].rank() < candidates[j].rank() })
	if len(candidates) > k {
		candidates = candidates[:k]
	}

	cases := make([]SimilarCase, 0, k)
	for _, c := range candidates {
		sc := SimilarCase{
			Source:     "jev_decision",
			Symbol:     c.decision.Symbol,
			Timestamp:  c.decision.Timestamp,
			Distance:   c.distance,
			Direction:  c.decision.Direction,
			Confidence: c.decision.Confidence,
			Regime:     c.decision.Regime,
		}
		if c.outcome != nil {
			sc.HorizonMinutes = &c.outcome.HorizonMinutes
			sc.FutureReturn = &c.outcome.FutureReturn
			sc.WasDirectionCorrect = c.outcome.WasDirectionCorrect
		}
		cases = append(cases, sc)
	}

	if len(cases) < k {
		snapshotMatches, err := s.search(ctx, "market_snapshot_vectors", "snapshot_id", v, k-len(cases))
		if err != nil {
			return Context{}, fmt.Errorf("rag: search similar snapshots: %w", err)
		}
		for _, m := range snapshotMatches {
			snap, err := s.snapshots.Get(ctx, m.id)
			if err != nil {
				continue
			}
			cases = append(cases, SimilarCase{
				Source:    "market_snapshot",
				Symbol:    snap.Symbol,
				Timestamp: snap.Timestamp,
				Distance:  m.distance,
			})
		}
	}

	if len(cases) == 0 {
		return Context{}, nil
	}
	return Context{Cases: cases}, nil
}

// hydrateDecisions loads each matched decision (skipping ones that no
// longer resolve) and joins its shortest-horizon calibration outcome.
func (s *Service) hydrateDecisions(ctx context.Context, matches []match) ([]decisionCandidate, error) {
	candidates := make([]decisionCandidate, 0, len(matches))
	ids := make([]int64, 0, len(matches))
	for _, m := range matches {
		d, err := s.decisions.Get(ctx, m.id)
		if err != nil {
			continue
		}
		// Regime lives only in response_json (jev_decisions has no regime
		// column), so enrich.Decision restores it.
		candidates = append(candidates, decisionCandidate{decision: enrich.Decision(d), distance: m.distance})
		ids = append(ids, d.ID)
	}

	outcomes, err := s.calibration.ListByDecisionIDs(ctx, ids)
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
