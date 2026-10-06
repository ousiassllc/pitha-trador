package rag

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	calrepo "github.com/ousiassllc/pitha-trador/internal/repository/calibration"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
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
	calibration *calrepo.CalibrationRepository
	snapshots   *market.SnapshotRepository
}

// NewService returns a Service backed by db (which must have
// modernc.org/sqlite/vec registered - internal/repository/sqlitedb/db.go does
// this via a blank import before Open applies db/migrations' vec0
// CREATE VIRTUAL TABLE statements) and used to hydrate similarity-search
// hits into SimilarCase summaries via decisions/snapshots, joined with
// db's calibration_outcomes (FR-RAG-2/3).
func NewService(db *sql.DB, decisions *judgement.DecisionRepository, snapshots *market.SnapshotRepository) *Service {
	return &Service{db: db, decisions: decisions, calibration: calrepo.NewCalibrationRepository(db), snapshots: snapshots}
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

// search runs a sqlite-vec KNN query on table for the k nearest rows to v.
// filter is an optional extra vec0 constraint on the id column (e.g.
// "decision_id IN (...)") with its bound args, applied during the KNN scan
// so a restricted subset still yields up to k hits; "" searches every row.
func (s *Service) search(ctx context.Context, table, idColumn, filter string, filterArgs []any, v Vector, k int) ([]match, error) {
	if k <= 0 {
		return nil, nil
	}
	embedding, err := v.json()
	if err != nil {
		return nil, err
	}
	if filter != "" {
		filter = " AND " + filter
	}

	args := append([]any{embedding}, filterArgs...)
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+idColumn+`, distance FROM `+table+` WHERE embedding MATCH ?`+filter+` ORDER BY distance LIMIT ?`, //nolint:gosec // G202: table/idColumn/filter are package-internal constants (never user input); values are bound parameters
		append(args, k)...)
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

// Context returns the FR-RAG-2 top-k (DefaultK if k <= 0) similar past
// states for in. jev_decisions matches come first, ranked outcome-labeled
// (calibration_outcomes joined) before unlabeled trader decisions before
// scout decisions, each group by ascending distance; market_snapshots
// matches backfill up to k total. See decisionMatches for how the
// candidate pool keeps labeled decisions from being crowded out.
//
// sub identifies the queried state: it is never returned as its own
// "similar past case" (FR-RAG-4). Its same-symbol decisions at/after its
// timestamp (e.g. the Scout decision a Trader call follows) and its
// same-symbol snapshots within SnapshotRecencyGuard are excluded, so a
// cold start - nothing but the current state indexed - yields an empty
// Context.
//
// A search or hydration error is only ever returned for a true
// technical failure (e.g. a closed/broken database connection); a
// cold-start empty result is not an error (FR-RAG-4).
func (s *Service) Context(ctx context.Context, in FeatureInput, sub Subject, k int) (Context, error) {
	if k <= 0 {
		k = DefaultK
	}
	v := Build(in)

	decisionMatches, err := s.decisionMatches(ctx, v, k, sub)
	if err != nil {
		return Context{}, err
	}

	candidates, err := s.hydrateDecisions(ctx, decisionMatches)
	if err != nil {
		return Context{}, err
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if ri, rj := candidates[i].rank(), candidates[j].rank(); ri != rj {
			return ri < rj
		}
		return candidates[i].distance < candidates[j].distance
	})
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
		snapshotMatches, err := s.searchExcluding(ctx, "market_snapshot_vectors", "snapshot_id", "", nil, v, k-len(cases), sub.snapshotSelf())
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
